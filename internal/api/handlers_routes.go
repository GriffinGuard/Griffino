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
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/router"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// GET /api/v1/plugins/capabilities
// 返回所有运行中插件的 capabilities，接线界面用
func (s *Server) handleListCapabilities(w http.ResponseWriter, r *http.Request) {
	plugins, err := s.st.ListPlugins()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrRouteFetchFailed, "Failed to list plugins")
		return
	}

	type SlotItem struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	type CapabilityItem struct {
		PluginID    string     `json:"pluginId"`
		PluginName  string     `json:"pluginName"`
		ID          string     `json:"id"`
		Role        string     `json:"role"`
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Type        string     `json:"type"`
		Slots       []SlotItem `json:"slots,omitempty"`
	}

	var items []CapabilityItem
	for _, p := range plugins {
		if p.Status != store.StatusRunning {
			continue
		}
		pkg, err := manifest.Load(p.PluginDir)
		if err != nil {
			continue
		}
		for _, cap := range pkg.Manifest.Capabilities {
			item := CapabilityItem{
				PluginID:    p.ID,
				PluginName:  pkg.Manifest.Name.Default,
				ID:          cap.ID,
				Role:        cap.Role,
				Name:        cap.Name.Default,
				Description: cap.Description.Default,
				Type:        cap.Type,
			}
			for _, slot := range cap.Slots {
				item.Slots = append(item.Slots, SlotItem{
					ID:          slot.ID,
					Name:        slot.Name.Default,
					Description: slot.Description.Default,
				})
			}
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": items})
}

// GET /api/v1/users/routes
// 返回当前用户的路由表
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	routes, err := s.routeStore.GetRoutes(r.Context(), session.UserID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrRouteFetchFailed, "Failed to get routes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
}

// POST /api/v1/users/routes
// 保存当前用户的路由表
func (s *Server) handleSetRoutes(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var req struct {
		Routes []router.Route `json:"routes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrRouteInvalidRequest, "Invalid request format")
		return
	}

	if err := s.routeStore.SetRoutes(r.Context(), session.UserID, req.Routes); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrRouteSaveFailed, "Failed to save routes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}