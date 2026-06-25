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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// handleTriggerAction POST /api/v1/plugins/{id}/actions/{actionId}
// After rate-limit check, publishes the action message to griffino.actions exchange / 限流校验通过后向 actions exchange 发布动作消息.
//
//	@Summary	Trigger a plugin action
//	@Tags		Plugins
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id			path		string	true	"Plugin ID"
//	@Param		actionId	path		string	true	"Action ID"
//	@Param		payload		body		object	false	"Action payload (passed through to the plugin)"
//	@Success	202			{object}	map[string]interface{}
//	@Failure	404			{object}	api.AppError
//	@Failure	409			{object}	api.AppError
//	@Failure	429			{object}	api.AppError
//	@Failure	500			{object}	api.AppError
//	@Router		/plugins/{id}/actions/{actionId} [post]
func (s *Server) handleTriggerAction(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")
	actionID := r.PathValue("actionId")

	// 1. Verify plugin exists and is running / 校验插件存在且运行中
	instance, err := s.st.GetPlugin(pluginID)
	if err != nil || instance == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
			map[string]interface{}{"id": pluginID})
		return
	}
	if instance.Status != store.StatusRunning {
		writeAppError(w, http.StatusConflict, ErrPluginNotRunning, "Plugin is not running")
		return
	}

	// 2. Validate actionId: actions are declared inside component node trees,
	//    not at the top level; collect all declared action IDs across components / 校验 actionId，动作声明在 component 节点树内
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest")
		return
	}
	if !manifest.HasAction(pkg.Manifest.Components, actionID) {
		writeAppError(w, http.StatusNotFound, ErrActionNotFound, "Action not found",
			map[string]interface{}{"actionId": actionID})
		return
	}

	// 3. Server-side rate limiting: at most 1 trigger per user per action within a 5s window / 5 秒窗口内同一用户对同一动作最多触发 1 次
	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrActionSendFailed, "Failed to get system state")
		return
	}
	redisAddr := fmt.Sprintf("localhost:%d", sysState.RedisPort)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: sysState.RedisPassword,
	})
	defer rdb.Close()

	rateLimitKey := fmt.Sprintf("ratelimit:action:%s:%s:%s", pluginID, session.UserID, actionID)
	count, err := rdb.Incr(r.Context(), rateLimitKey).Result()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrActionSendFailed, "Rate limit check failed")
		return
	}
	if count == 1 {
		// Set TTL on first write / 首次写入时设置 TTL
		rdb.Expire(r.Context(), rateLimitKey, 5*time.Second)
	}
	if count > 1 {
		writeAppError(w, http.StatusTooManyRequests, ErrActionRateLimited, "Action triggered too frequently, please wait",
			map[string]interface{}{"retryAfterSeconds": 5})
		return
	}

	// 4. Build and publish the message to griffino.actions exchange;
	//    Web-UI sends <Action> payload as the entire body (no wrapping), passed through as-is / 构造消息发布到 actions exchange，payload 原样透传
	payload, err := readActionPayload(r)
	if err != nil {
		writeAppError(w, http.StatusBadRequest, ErrActionInvalidRequest, "Invalid action payload")
		return
	}
	routingKey := fmt.Sprintf("action.%s.%s.%s.v1", pluginID, session.UserID, actionID)
	body, _ := json.Marshal(map[string]any{
		"msgId":       uuid.New().String(),
		"userId":      session.UserID,
		"displayName": s.displayNameForSession(session),
		"actionId":    actionID,
		"payload":     payload,
	})

	if err := s.router.PublishAction(routingKey, body); err != nil {
		slog.Error("handleTriggerAction: failed to publish action",
			"pluginId", pluginID, "actionId", actionID, "error", err)
		writeAppError(w, http.StatusInternalServerError, ErrActionSendFailed, "Failed to send action to plugin")
		return
	}

	slog.Info("action triggered",
		"pluginId", pluginID, "actionId", actionID, "userId", session.UserID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"actionId": actionID,
	})
}

// readActionPayload parses the entire request body as the action payload.
// Web-UI sends the payload object directly (not {payload:...} wrapped).
// Returns an empty map on nil/empty body / 解析请求体为 payload，空 body 返回空对象.
func readActionPayload(r *http.Request) (map[string]any, error) {
	payload := map[string]any{}
	if r.Body == nil {
		return payload, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		if errors.Is(err, io.EOF) {
			return payload, nil
		}
		return nil, err
	}
	return payload, nil
}
