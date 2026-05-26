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
	amqp "github.com/rabbitmq/amqp091-go"
)

// GET /api/v1/plugins/{id}/user-config
// 返回 config.user.json schema，所有已登录用户可访问
func (s *Server) handleGetUserConfigSchema(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	instance, err := s.st.GetPlugin(pluginID)
	if err != nil || instance == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
			map[string]interface{}{"id": pluginID})
		return
	}
	// 直接重新 Load 插件包拿 UserConfig
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
// 返回当前用户对该插件的已保存配置值
func (s *Server) handleGetUserConfigValues(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")

	values, err := s.userConfigStore.Get(r.Context(), session.UserID, pluginID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserConfigFetchFailed, "Failed to get user config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": values})
}

// POST /api/v1/plugins/{id}/user-config/values
// 保存当前用户对该插件的配置值，只允许保存 schema 中声明的 key
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

	var incoming map[string]string
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrUserConfigInvalidRequest, "Invalid request format")
		return
	}

	// 只保留 schema 中声明的 key，过滤非法字段
	allowed := map[string]bool{}
	if pkg.UserConfig != nil {
		for _, param := range pkg.UserConfig.Configs {
			allowed[param.Key] = true
		}
	}
	filtered := map[string]string{}
	for k, v := range incoming {
		if allowed[k] {
			filtered[k] = v
		}
	}

	if err := s.userConfigStore.Set(r.Context(), session.UserID, pluginID, filtered); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserConfigSaveFailed, "Failed to save user config")
		return
	}
	// 通知插件：用户配置已更新（fire-and-forget，失败不阻断响应）
	s.notifyUserConfigUpdated(instance, session.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// notifyUserConfigUpdated 向插件的 consumer 队列发送用户配置更新通知
// 丢弃策略：队列不存在或发送失败时只记日志，不影响 API 响应
func (s *Server) notifyUserConfigUpdated(instance *store.PluginInstance, userID string) {
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
	body, _ := json.Marshal(map[string]any{
		"event":    "user.config_updated",
		"userId":   userID,
		"pluginId": instance.ID,
	})

	err = ch.PublishWithContext(context.Background(),
		"", // 默认 exchange，直接发到队列名
		queueName,
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		// 队列不存在时 RabbitMQ 会返回错误，这是预期行为（插件没有监听则丢弃）
		slog.Debug("notifyUserConfigUpdated: publish skipped or failed",
			"pluginId", instance.ID, "queue", queueName, "error", err)
	}
}
