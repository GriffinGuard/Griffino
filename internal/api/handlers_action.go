package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// handleListActions GET /api/v1/plugins/{id}/actions
// 返回插件 manifest 中声明的 actions 列表（不执行任何操作）
func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
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

	actions := pkg.Manifest.Actions
	if actions == nil {
		actions = []manifest.Action{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

// handleTriggerAction POST /api/v1/plugins/{id}/actions/{actionId}
// 限流校验通过后，向 griffino.actions exchange 发布动作消息
func (s *Server) handleTriggerAction(w http.ResponseWriter, r *http.Request) {
	session  := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")
	actionID := r.PathValue("actionId")

	// 1. 校验插件存在且运行中
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

	// 2. 校验 actionId 在 manifest 中声明
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest")
		return
	}
	found := false
	for _, a := range pkg.Manifest.Actions {
		if a.ID == actionID {
			found = true
			break
		}
	}
	if !found {
		writeAppError(w, http.StatusNotFound, ErrActionNotFound, "Action not found",
			map[string]interface{}{"actionId": actionID})
		return
	}

	// 3. 服务端限流：5 秒窗口内同一用户对同一动作最多触发 1 次
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
		// 首次写入时设置 TTL
		rdb.Expire(r.Context(), rateLimitKey, 5*time.Second)
	}
	if count > 1 {
		writeAppError(w, http.StatusTooManyRequests, ErrActionRateLimited, "Action triggered too frequently, please wait",
			map[string]interface{}{"retryAfterSeconds": 5})
		return
	}

	// 4. 构造消息并发布到 griffino.actions exchange
	routingKey := fmt.Sprintf("action.%s.%s.%s.v1", pluginID, session.UserID, actionID)
	body, _ := json.Marshal(map[string]any{
		"msgId":    uuid.New().String(),
		"userId":   session.UserID,
		"actionId": actionID,
		"payload":  map[string]any{},
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

// actionRateLimiter 封装限流逻辑，复用 Redis 连接（可选优化，避免每次请求新建连接）
// 当前实现为简单版本：每次请求新建连接。生产优化可将 rdb 提升为 Server 字段。
func actionRateLimitKey(pluginID, userID, actionID string) string {
	return fmt.Sprintf("ratelimit:action:%s:%s:%s", pluginID, userID, actionID)
}

// ensureActionExchange 供测试或 daemon 启动时调用，确保 exchange 存在
// 正常路径下由 router.Start() 统一声明，此函数仅作备用
func ensureActionExchange(ctx context.Context, amqpURL string) error {
	// 实际声明由 router.Start() 负责，此处留空占位
	_ = ctx
	_ = amqpURL
	return nil
}