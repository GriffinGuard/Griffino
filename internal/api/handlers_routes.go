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
// Returns capabilities of all running plugins, used by the wiring UI / 返回所有运行中插件的 capabilities，接线界面用
//
//	@Summary	List plugin capabilities
//	@Tags		Capabilities
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/plugins/capabilities [get]
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

// GET /api/v1/plugins/triggers
// Returns the emitted-event declarations of all running plugins, used by the
// blueprint editor to discover available triggers / 返回运行中插件可发出的事件，蓝图编辑器发现触发器用
//
//	@Summary	List plugin triggers
//	@Tags		Capabilities
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/plugins/triggers [get]
func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	plugins, err := s.st.ListPlugins()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrRouteFetchFailed, "Failed to list plugins")
		return
	}

	type TriggerItem struct {
		PluginID    string `json:"pluginId"`
		PluginName  string `json:"pluginName"`
		EventType   string `json:"eventType"`
		SchemaRef   string `json:"schemaRef,omitempty"`
		Name        string `json:"name,omitempty"`
		Description string `json:"description,omitempty"`
	}

	var items []TriggerItem
	for _, p := range plugins {
		if p.Status != store.StatusRunning {
			continue
		}
		pkg, err := manifest.Load(p.PluginDir)
		if err != nil {
			continue
		}
		for _, e := range pkg.Manifest.Emits {
			items = append(items, TriggerItem{
				PluginID:    p.ID,
				PluginName:  pkg.Manifest.Name.Default,
				EventType:   e.EventType,
				SchemaRef:   e.SchemaRef,
				Name:        e.Name.Default,
				Description: e.Description.Default,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": items})
}

// GET /api/v1/users/routes
// Returns the current user's route table / 返回当前用户的路由表
//
//	@Summary	Get user routes
//	@Tags		Routes
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string][]router.Route
//	@Failure	500	{object}	api.AppError
//	@Router		/users/routes [get]
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
// Saves the current user's route table / 保存当前用户的路由表
//
//	@Summary	Set user routes
//	@Tags		Routes
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		object	true	"routes array"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/users/routes [post]
func (s *Server) handleSetRoutes(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var req struct {
		Routes []router.Route `json:"routes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrRouteInvalidRequest, "Invalid request format")
		return
	}

	// Authoritatively fill each provider's interfaceRef from its manifest so the
	// router can reject cross-major substitution; never trust the client for this /
	// 服务端按 manifest 补全 provider 的 interfaceRef，供 router 主版本兼容校验.
	s.enrichRouteInterfaceRefs(req.Routes)

	if err := s.routeStore.SetRoutes(r.Context(), session.UserID, req.Routes); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrRouteSaveFailed, "Failed to save routes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// enrichRouteInterfaceRefs fills ProviderEntry.InterfaceRef for every route from the
// provider plugin's manifest (the provider capability whose type matches the route's
// capability type). Manifests are cached per call / 按 manifest 补全各 provider 的 interfaceRef.
func (s *Server) enrichRouteInterfaceRefs(routes []router.Route) {
	manifestCache := make(map[string]*manifest.PluginManifest)
	loadManifest := func(pluginID string) *manifest.PluginManifest {
		if m, ok := manifestCache[pluginID]; ok {
			return m
		}
		var m *manifest.PluginManifest
		if p, err := s.st.GetPlugin(pluginID); err == nil {
			if pkg, err := manifest.Load(p.PluginDir); err == nil {
				m = pkg.Manifest
			}
		}
		manifestCache[pluginID] = m
		return m
	}

	for i := range routes {
		for j := range routes[i].Providers {
			pe := &routes[i].Providers[j]
			m := loadManifest(pe.ProviderID)
			if m == nil {
				continue
			}
			for _, cap := range m.Capabilities {
				if cap.Role == "provider" && cap.Type == routes[i].CapabilityType {
					pe.InterfaceRef = cap.StandardInterfaceRef
					break
				}
			}
		}
	}
}
