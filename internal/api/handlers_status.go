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
	"fmt"
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/redis/go-redis/v9"
)

// statusViewRedis 在 Server 初始化时创建，供状态视图 handler 直接读 Redis
// 使用 admin 账号（系统 Redis），不受插件 ACL 限制
type statusViewReader struct {
	rdb *redis.Client
}

func newStatusViewReader(addr, password string) *statusViewReader {
	return &statusViewReader{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

// readKV 读取 plugin:{pluginId}:state:{userId}:{key} 的值
// RedisKeyPattern 为单个 key（不含通配符）时使用
func (r *statusViewReader) readKV(ctx context.Context, pluginID, userID, keyPattern string) (map[string]string, error) {
	prefix := fmt.Sprintf("plugin:%s:state:%s:", pluginID, userID)
	fullKey := prefix + keyPattern

	// 如果 pattern 中含 * 则用 KEYS 扫描（仅用于 kv 类型的多 key 场景）
	if containsWildcard(keyPattern) {
		keys, err := r.rdb.Keys(ctx, fullKey).Result()
		if err != nil {
			return nil, err
		}
		result := make(map[string]string, len(keys))
		for _, k := range keys {
			val, err := r.rdb.Get(ctx, k).Result()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				return nil, err
			}
			// 去掉 prefix，只保留相对 key
			result[k[len(prefix):]] = val
		}
		return result, nil
	}

	// 单 key
	val, err := r.rdb.Get(ctx, fullKey).Result()
	if err == redis.Nil {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{keyPattern: val}, nil
}

func containsWildcard(s string) bool {
	for _, c := range s {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// handleListStatusViews GET /api/v1/plugins/{id}/status-views
// 返回插件 manifest 中声明的 statusViews 列表（不含实际数据）
func (s *Server) handleListStatusViews(w http.ResponseWriter, r *http.Request) {
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

	views := pkg.Manifest.StatusViews
	if views == nil {
		views = []manifest.StatusView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"statusViews": views})
}

// handleGetStatusView GET /api/v1/plugins/{id}/status/{viewId}
// 读取当前用户在该视图对应的 Redis 数据并返回
func (s *Server) handleGetStatusView(w http.ResponseWriter, r *http.Request) {
	session  := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")
	viewID   := r.PathValue("viewId")

	instance, err := s.st.GetPlugin(pluginID)
	if err != nil || instance == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
			map[string]interface{}{"id": pluginID})
		return
	}

	// 只有运行中的插件才有状态数据，但不阻断请求，返回空数据即可
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest")
		return
	}

	// 找到对应的 view 声明
	var targetView *manifest.StatusView
	for i := range pkg.Manifest.StatusViews {
		if pkg.Manifest.StatusViews[i].ID == viewID {
			targetView = &pkg.Manifest.StatusViews[i]
			break
		}
	}
	if targetView == nil {
		writeAppError(w, http.StatusNotFound, ErrStatusViewNotFound, "Status view not found",
			map[string]interface{}{"viewId": viewID})
		return
	}

	// 插件未运行时返回空数据，不报错
	if instance.Status != store.StatusRunning {
		writeJSON(w, http.StatusOK, map[string]any{
			"viewId": viewID,
			"type":   targetView.Type,
			"data":   map[string]string{},
		})
		return
	}

	data, err := s.statusViewReader.readKV(r.Context(), pluginID, session.UserID, targetView.RedisKeyPattern)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrStatusViewFetchFailed, "Failed to fetch status view data",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"viewId": viewID,
		"type":   targetView.Type,
		"data":   data,
	})
}