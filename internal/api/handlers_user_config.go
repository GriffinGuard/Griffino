// Copyright 2025 GriffinGuard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// GET /api/v1/plugins/{id}/user-config
// Returns the config.user.json schema, accessible to all logged-in users / 返回 config.user.json schema，所有已登录用户可访问
//
//	@Summary	Get plugin user-config schema
//	@Tags		Plugins
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Plugin ID"
//	@Success	200	{object}	map[string]interface{}
//	@Failure	404	{object}	api.AppError
//	@Failure	500	{object}	api.AppError
//	@Router		/plugins/{id}/user-config [get]
func (s *Server) handleGetUserConfigSchema(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	instance, err := s.st.GetPlugin(pluginID)
	if err != nil || instance == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
			map[string]interface{}{"id": pluginID})
		return
	}
	// Re-load the plugin package to get UserConfig / 直接重新 Load 插件包拿 UserConfig
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest")
		return
	}
	if pkg.UserConfig == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configs": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, pkg.UserConfig)
}

// GET /api/v1/plugins/{id}/user-config/values
// Returns the current user's saved config values for the plugin / 返回当前用户对该插件的已保存配置值
//
//	@Summary	Get plugin user-config values
//	@Tags		Plugins
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Plugin ID"
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/plugins/{id}/user-config/values [get]
func (s *Server) handleGetUserConfigValues(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")

	values, err := s.userConfigStore.Get(r.Context(), session.UserID, pluginID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserConfigFetchFailed, "Failed to get user config")
		return
	}

	// Mask password fields (best-effort: skip masking if schema cannot be loaded) / 脱敏密码字段（尽力而为：schema 加载失败时跳过脱敏）
	if instance, err := s.st.GetPlugin(pluginID); err == nil && instance != nil {
		if pkg, err := manifest.Load(instance.PluginDir); err == nil && pkg.UserConfig != nil {
			values = maskUserConfigValues(values, pkg.UserConfig.Configs)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"values": values})
}

// POST /api/v1/plugins/{id}/user-config/values
// Saves the current user's config values for the plugin; only keys declared in the schema are allowed / 保存当前用户对该插件的配置值，只允许保存 schema 中声明的 key
//
//	@Summary	Set plugin user-config values
//	@Tags		Plugins
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string				true	"Plugin ID"
//	@Param		body	body		object	true	"config values keyed by schema key"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	404		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/plugins/{id}/user-config/values [post]
func (s *Server) handleSetUserConfigValues(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")

	instance, err := s.st.GetPlugin(pluginID)
	if err != nil || instance == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
			map[string]interface{}{"id": pluginID})
		return
	}

	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest")
		return
	}

	var incoming map[string]any
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrUserConfigInvalidRequest, "Invalid request format")
		return
	}

	// Only keep keys declared in the schema, filter out invalid fields / 只保留 schema 中声明的 key，过滤非法字段
	allowed := map[string]manifest.ConfigParam{}
	if pkg.UserConfig != nil {
		for _, param := range pkg.UserConfig.Configs {
			allowed[param.Key] = param
		}
	}
	// Load existing values so placeholder password submissions can be preserved / 预加载旧值，使密码占位符提交时能还原为已存储的真实密码
	existing, _ := s.userConfigStore.Get(r.Context(), session.UserID, pluginID)
	if existing == nil {
		existing = map[string]any{}
	}

	filtered := map[string]any{}
	for k, v := range incoming {
		param, ok := allowed[k]
		if !ok {
			continue
		}
		clean, err := normalizeUserConfigValue(param, v)
		if err != nil {
			writeAppError(w, http.StatusBadRequest, ErrUserConfigInvalidRequest, err.Error(),
				map[string]interface{}{"key": k})
			return
		}
		filtered[k] = clean
	}

	// Replace placeholder values for password fields with the previously stored secret / 将密码字段中的占位符替换为已存储的真实密码
	if pkg.UserConfig != nil {
		preserveExistingPasswords(filtered, existing, pkg.UserConfig.Configs)
	}

	if err := s.userConfigStore.Set(r.Context(), session.UserID, pluginID, filtered); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserConfigSaveFailed, "Failed to save user config")
		return
	}
	// Notify plugin that user config has been updated (fire-and-forget; failure does not block response) / 通知插件：用户配置已更新（fire-and-forget，失败不阻断响应）
	s.notifyUserConfigUpdated(instance, session.UserID, s.displayNameForSession(session))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// passwordPlaceholder is the sentinel string returned for password fields on GET /values.
// When the same value is submitted on POST /values, the existing stored secret is preserved.
const passwordPlaceholder = "**masked**"

func normalizeUserConfigValue(param manifest.ConfigParam, value any) (any, error) {
	if param.Type != manifest.ConfigTypeGroupArray {
		return value, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("config %q must be an array", param.Key)
	}
	if param.MinItems != nil && len(items) < *param.MinItems {
		return nil, fmt.Errorf("config %q must contain at least %d items", param.Key, *param.MinItems)
	}
	if param.MaxItems != nil && len(items) > *param.MaxItems {
		return nil, fmt.Errorf("config %q must contain at most %d items", param.Key, *param.MaxItems)
	}

	fieldByKey := make(map[string]manifest.ConfigParam, len(param.Fields))
	for _, field := range param.Fields {
		fieldByKey[field.Key] = field
	}
	normalized := make([]any, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config %q item %d must be an object", param.Key, i)
		}
		// Check required sub-fields / 检查必填子字段
		for _, field := range param.Fields {
			if !field.Optional {
				if _, present := obj[field.Key]; !present {
					return nil, fmt.Errorf("config %q item %d: required field %q is missing", param.Key, i, field.Key)
				}
			}
		}
		// Filter unknown keys and validate each field value / 过滤未知 key 并校验各字段值
		clean := make(map[string]any, len(obj))
		for k, v := range obj {
			field, ok := fieldByKey[k]
			if !ok {
				continue // unknown field: drop
			}
			if err := validateGroupArrayFieldValue(field, v, param.Key, i); err != nil {
				return nil, err
			}
			clean[k] = v
		}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}

// validateGroupArrayFieldValue checks that val conforms to the type and validation
// constraints declared in field. It is called for each known sub-field of a group_array item.
func validateGroupArrayFieldValue(field manifest.ConfigParam, val any, parentKey string, itemIdx int) error {
	if val == nil {
		return nil
	}
	path := fmt.Sprintf("config %q item %d field %q", parentKey, itemIdx, field.Key)
	switch field.Type {
	case manifest.ConfigTypeBoolean:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, val)
		}
	case manifest.ConfigTypeString, manifest.ConfigTypePassword,
		manifest.ConfigTypeMultilineString, manifest.ConfigTypeOptions:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("%s: expected string, got %T", path, val)
		}
		if v := field.Validation; v != nil {
			if v.MinLength != nil && len(s) < *v.MinLength {
				return fmt.Errorf("%s: length %d is less than minLength %d", path, len(s), *v.MinLength)
			}
			if v.MaxLength != nil && len(s) > *v.MaxLength {
				return fmt.Errorf("%s: length %d exceeds maxLength %d", path, len(s), *v.MaxLength)
			}
		}
	case manifest.ConfigTypeInt, manifest.ConfigTypeFloat:
		n, ok := val.(float64)
		if !ok {
			return fmt.Errorf("%s: expected number, got %T", path, val)
		}
		if field.Type == manifest.ConfigTypeInt && n != float64(int64(n)) {
			return fmt.Errorf("%s: expected integer, got %v", path, n)
		}
		if v := field.Validation; v != nil {
			if v.Minimum != nil && n < *v.Minimum {
				return fmt.Errorf("%s: value %v is less than minimum %v", path, n, *v.Minimum)
			}
			if v.Maximum != nil && n > *v.Maximum {
				return fmt.Errorf("%s: value %v exceeds maximum %v", path, n, *v.Maximum)
			}
		}
	}
	return nil
}

// maskUserConfigValues returns a copy of values with password fields replaced by
// passwordPlaceholder, including password sub-fields inside group_array items.
func maskUserConfigValues(values map[string]any, schema []manifest.ConfigParam) map[string]any {
	schemaByKey := make(map[string]manifest.ConfigParam, len(schema))
	for _, p := range schema {
		schemaByKey[p.Key] = p
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		param, ok := schemaByKey[k]
		if !ok {
			out[k] = v
			continue
		}
		out[k] = maskConfigValue(param, v)
	}
	return out
}

func maskConfigValue(param manifest.ConfigParam, val any) any {
	switch param.Type {
	case manifest.ConfigTypePassword:
		return passwordPlaceholder
	case manifest.ConfigTypeGroupArray:
		items, ok := val.([]any)
		if !ok {
			return val
		}
		masked := make([]any, len(items))
		for i, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				masked[i] = item
				continue
			}
			maskedObj := make(map[string]any, len(obj))
			for k, v := range obj {
				maskedObj[k] = v
			}
			for _, field := range param.Fields {
				if field.Type == manifest.ConfigTypePassword {
					if _, exists := maskedObj[field.Key]; exists {
						maskedObj[field.Key] = passwordPlaceholder
					}
				}
			}
			masked[i] = maskedObj
		}
		return masked
	default:
		return val
	}
}

// preserveExistingPasswords replaces passwordPlaceholder submissions for password
// fields with the previously stored value, preventing the UI round-trip from
// overwriting a real secret with the placeholder sentinel.
func preserveExistingPasswords(filtered, existing map[string]any, schema []manifest.ConfigParam) {
	for _, param := range schema {
		switch param.Type {
		case manifest.ConfigTypePassword:
			if v, ok := filtered[param.Key]; ok && v == passwordPlaceholder {
				if old, ok := existing[param.Key]; ok {
					filtered[param.Key] = old
				} else {
					delete(filtered, param.Key)
				}
			}
		case manifest.ConfigTypeGroupArray:
			items, ok := filtered[param.Key].([]any)
			if !ok {
				continue
			}
			oldItems, _ := existing[param.Key].([]any)
			for i, item := range items {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				for _, field := range param.Fields {
					if field.Type != manifest.ConfigTypePassword {
						continue
					}
					v, ok := obj[field.Key]
					if !ok || v != passwordPlaceholder {
						continue
					}
					if i < len(oldItems) {
						if oldObj, ok := oldItems[i].(map[string]any); ok {
							if oldVal, ok := oldObj[field.Key]; ok {
								obj[field.Key] = oldVal
								continue
							}
						}
					}
					delete(obj, field.Key)
				}
			}
		}
	}
}

// notifyUserConfigUpdated sends a user-config update notification to the plugin's consumer queue.
// Discard policy: if the queue doesn't exist or sending fails, only log; don't affect the API response / 向插件的 consumer 队列发送用户配置更新通知，队列不存在或发送失败时只记日志，不影响 API 响应.
func (s *Server) notifyUserConfigUpdated(instance *store.PluginInstance, userID, displayName string) {
	if instance.Status != store.StatusRunning {
		return
	}
	if instance.RuntimeInfo == nil {
		return
	}

	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		return
	}

	amqpURL := fmt.Sprintf("amqp://%s:%s@localhost:%d/",
		sysState.RabbitMQAdminUser,
		sysState.RabbitMQAdminPassword,
		sysState.RabbitMQPort,
	)
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		slog.Warn("notifyUserConfigUpdated: failed to connect to RabbitMQ", "pluginId", instance.ID, "error", err)
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		slog.Warn("notifyUserConfigUpdated: failed to open channel", "pluginId", instance.ID, "error", err)
		return
	}
	defer ch.Close()

	queueName := fmt.Sprintf("plugin.%s.consumer.user_config_updated", instance.ID)
	payload := map[string]any{
		"pluginId": instance.ID,
	}
	body, _ := json.Marshal(map[string]any{
		"msgId":       uuid.New().String(),
		"event":       "user.config_updated",
		"userId":      userID,
		"displayName": displayName,
		"pluginId":    instance.ID,
		"payload":     payload,
	})

	err = ch.PublishWithContext(context.Background(),
		"", // Default exchange, send directly to queue name / 默认 exchange，直接发到队列名
		queueName,
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		// If the queue doesn't exist, RabbitMQ will return an error; this is expected (discard if plugin isn't listening) / 队列不存在时 RabbitMQ 返回错误，是预期行为（插件没有监听则丢弃）
		slog.Debug("notifyUserConfigUpdated: publish skipped or failed",
			"pluginId", instance.ID, "queue", queueName, "error", err)
	}
}
