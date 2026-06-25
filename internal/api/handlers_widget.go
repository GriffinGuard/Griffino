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
	"log/slog"
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// widgetDefinition is a single record returned by the widget list API, aligned with
// Web-UI's WidgetDefinition: { id, name, refreshMs?, root }. root is the normalized node tree / widget 列表接口返回的单条记录，与 Web-UI 的 WidgetDefinition 对齐，root 是已规整的节点树.
type widgetDefinition struct {
	ID        string              `json:"id"`
	Name      manifest.I18nString `json:"name"`
	RefreshMs int                 `json:"refreshMs,omitempty"`
	Root      manifest.WidgetNode `json:"root"`
}

// handleListWidgets GET /api/v1/plugins/{id}/widgets
// Returns the node tree of components (i.e. widgets under the unified concept) declared in the plugin manifest, without live data / 返回插件 manifest 中声明的 components（即 widgets）的节点树，不含实时数据.
//
//	@Summary	List plugin widgets
//	@Tags		Widgets
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Plugin ID"
//	@Success	200	{object}	map[string]interface{}
//	@Failure	404	{object}	api.AppError
//	@Failure	500	{object}	api.AppError
//	@Router		/plugins/{id}/widgets [get]
func (s *Server) handleListWidgets(w http.ResponseWriter, r *http.Request) {
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

	widgets := make([]widgetDefinition, 0, len(pkg.Manifest.Components))
	for _, c := range pkg.Manifest.Components {
		widgets = append(widgets, widgetDefinition{
			ID:        c.ID,
			Name:      c.Name,
			RefreshMs: c.RefreshMs,
			Root:      c.Root.Node,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"widgets": widgets})
}

// handleGetWidgetData GET /api/v1/plugins/{id}/widgets/{widgetId}/data
// Reads the Redis state for each bind in the component tree, user-isolated, merges and returns / 读取组件树里每个 bind 对应的、当前用户隔离的 Redis 状态并合并返回.
//
//	@Summary	Get widget data
//	@Tags		Widgets
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id			path		string	true	"Plugin ID"
//	@Param		widgetId	path		string	true	"Widget ID"
//	@Success	200			{object}	map[string]interface{}
//	@Failure	404			{object}	api.AppError
//	@Failure	500			{object}	api.AppError
//	@Router		/plugins/{id}/widgets/{widgetId}/data [get]
func (s *Server) handleGetWidgetData(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	pluginID := r.PathValue("id")
	widgetID := r.PathValue("widgetId")

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

	var target *manifest.Component
	for i := range pkg.Manifest.Components {
		if pkg.Manifest.Components[i].ID == widgetID {
			target = &pkg.Manifest.Components[i]
			break
		}
	}
	if target == nil {
		writeAppError(w, http.StatusNotFound, ErrWidgetNotFound, "Widget not found",
			map[string]interface{}{"widgetId": widgetID})
		return
	}

	data := map[string]any{}

	// Return empty data when plugin is not running (no error) / 插件未运行时返回空数据，不报错
	if instance.Status == store.StatusRunning {
		for _, bind := range manifest.CollectBinds(target.Root.Node) {
			values, err := s.statusViewReader.readKV(r.Context(), pluginID, session.UserID, bind)
			if err != nil {
				writeAppError(w, http.StatusInternalServerError, ErrWidgetDataFailed, "Failed to fetch widget data",
					map[string]interface{}{"detail": err.Error()})
				return
			}
			for k, v := range values {
				data[k] = v
			}
		}

		if s.componentData != nil {
			componentData, err := s.componentData.RenderComponentData(r.Context(), pluginID, widgetID, session.UserID)
			if err != nil {
				if !errors.Is(err, errComponentDataNotSupported) {
					slog.Debug("handleGetWidgetData: component data RPC failed; falling back to Redis state",
						"pluginId", pluginID, "widgetId", widgetID, "error", err)
				}
			} else {
				for k, v := range componentData {
					data[k] = v
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"widgetId": widgetID,
		"data":     data,
	})
}

// handleGetDashboard GET /api/v1/me/dashboard
// Returns the current user's saved dashboard layout (opaque JSON); dashboard is null if never saved / 返回当前用户保存的仪表盘布局（不透明 JSON），从未保存过时 dashboard 为 null.
//
//	@Summary	Get dashboard layout
//	@Tags		Dashboard
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/me/dashboard [get]
func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	raw, err := s.dashboardStore.Get(r.Context(), session.UserID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrDashboardSaveFailed, "Failed to load dashboard",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	var dashboard any
	if raw != nil {
		dashboard = json.RawMessage(raw)
	}
	writeJSON(w, http.StatusOK, map[string]any{"dashboard": dashboard})
}

// handlePutDashboard PUT /api/v1/me/dashboard
// Stores the dashboard value from { dashboard: <DashboardState> } as-is (no schema validation) / 原样存储请求体中 dashboard 值，不校验结构.
//
//	@Summary	Save dashboard layout
//	@Tags		Dashboard
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		object	true	"dashboard layout payload"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/me/dashboard [put]
func (s *Server) handlePutDashboard(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var body struct {
		Dashboard json.RawMessage `json:"dashboard"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Dashboard) == 0 {
		writeAppError(w, http.StatusBadRequest, ErrDashboardInvalidRequest, "Invalid dashboard payload")
		return
	}

	if err := s.dashboardStore.Set(r.Context(), session.UserID, body.Dashboard); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrDashboardSaveFailed, "Failed to save dashboard",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
