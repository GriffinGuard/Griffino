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
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/google/uuid"
)

// resolveNodeSchemas resolves port schemas for blueprint nodes, returning nodeID -> *CachedSchema.
// Built-in nodes, capabilities without interfaceRef, and uncached schemas are omitted —
// their edges are allowed through during validation / 解析蓝图各节点端口 schema，内置/未声明 interfaceRef/未缓存的节点不出现，它们的边校验时放行.
func (s *Server) resolveNodeSchemas(nodes []taskscheduler.Node) map[string]*taskscheduler.CachedSchema {
	out := make(map[string]*taskscheduler.CachedSchema, len(nodes))
	// Cache manifests per plugin within a single request to avoid repeated loading / 同一插件的 manifest 请求内缓存，避免重复加载.
	manifestCache := make(map[string]*manifest.PluginManifest)

	for _, node := range nodes {
		if node.PluginID == "" || node.PluginID == taskscheduler.BuiltinPluginID {
			continue
		}

		m, ok := manifestCache[node.PluginID]
		if !ok {
			plugin, err := s.st.GetPlugin(node.PluginID)
			if err != nil {
				manifestCache[node.PluginID] = nil
				continue
			}
			pkg, err := manifest.Load(plugin.PluginDir)
			if err != nil {
				manifestCache[node.PluginID] = nil
				continue
			}
			m = pkg.Manifest
			manifestCache[node.PluginID] = m
		}
		if m == nil {
			continue
		}

		var capability *manifest.Capability
		for i := range m.Capabilities {
			if m.Capabilities[i].ID == node.CapabilityID {
				capability = &m.Capabilities[i]
				break
			}
		}
		if capability == nil {
			continue
		}

		// Prefer a registered standard interface; fall back to an inline custom
		// interface declared directly in the manifest so custom capabilities still
		// participate in port validation / 优先标准接口，否则回退内联自定义接口.
		if capability.StandardInterfaceRef != "" {
			schema, err := s.schemaStore.Get(capability.StandardInterfaceRef)
			if err != nil {
				continue
			}
			out[node.ID] = schema
			continue
		}
		if capability.InterfaceSpec != nil {
			out[node.ID] = cachedSchemaFromInline(node.CapabilityID, capability.InterfaceSpec)
		}
	}
	return out
}

// cachedSchemaFromInline converts a manifest inline interface spec into a
// taskscheduler CachedSchema for blueprint port validation. The InterfaceRef is a
// synthetic identifier (inline custom interfaces are not in the schema registry) /
// 将内联接口规格转为用于端口校验的 CachedSchema.
func cachedSchemaFromInline(capabilityID string, spec *manifest.InlineInterfaceSpec) *taskscheduler.CachedSchema {
	conv := func(ports []manifest.InterfacePort) []taskscheduler.PortSpec {
		out := make([]taskscheduler.PortSpec, 0, len(ports))
		for _, p := range ports {
			out = append(out, taskscheduler.PortSpec{
				ID:          p.ID,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
			})
		}
		return out
	}
	return &taskscheduler.CachedSchema{
		InterfaceRef: "inline:" + capabilityID,
		InputPorts:   conv(spec.InputPorts),
		OutputPorts:  conv(spec.OutputPorts),
	}
}

// GET /api/v1/blueprints — returns all blueprints for the current user / 返回当前用户的所有蓝图
//
//	@Summary	List blueprints
//	@Tags		Blueprints
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string][]taskscheduler.Blueprint
//	@Failure	500	{object}	api.AppError
//	@Router		/blueprints [get]
func (s *Server) handleListBlueprints(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	blueprints, err := s.bpStore.ListByUser(session.UserID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintFetchFailed, "Failed to list blueprints")
		return
	}
	if blueprints == nil {
		blueprints = []*taskscheduler.Blueprint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"blueprints": blueprints})
}

// POST /api/v1/blueprints — create a new blueprint / 创建新蓝图
//
//	@Summary	Create a blueprint
//	@Tags		Blueprints
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		object	true	"name, trigger, nodes, metadata"
//	@Success	201		{object}	taskscheduler.Blueprint
//	@Failure	400		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/blueprints [post]
func (s *Server) handleCreateBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var req struct {
		Name     string                      `json:"name"`
		Trigger  taskscheduler.Trigger       `json:"trigger"`
		Nodes    []taskscheduler.Node        `json:"nodes"`
		Metadata taskscheduler.BlueprintMeta `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Invalid request format")
		return
	}
	if req.Name == "" {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Blueprint name is required")
		return
	}
	if req.Trigger.EventType == "" {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Trigger eventType is required")
		return
	}

	// Generate IDs for nodes that don't have one yet / 为没有 ID 的节点生成 ID
	for i := range req.Nodes {
		if req.Nodes[i].ID == "" {
			req.Nodes[i].ID = uuid.New().String()
		}
	}

	now := time.Now()
	bp := &taskscheduler.Blueprint{
		ID:        uuid.New().String(),
		UserID:    session.UserID,
		Name:      req.Name,
		Trigger:   req.Trigger,
		Nodes:     req.Nodes,
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if mismatches := taskscheduler.ValidateBlueprintPorts(bp, s.resolveNodeSchemas(bp.Nodes)); len(mismatches) > 0 {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintPortMismatch,
			"Blueprint has incompatible port connections", map[string]any{"mismatches": mismatches})
		return
	}

	if err := s.bpStore.Save(bp); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintSaveFailed, "Failed to save blueprint")
		return
	}

	writeJSON(w, http.StatusCreated, bp)
}

// GET /api/v1/blueprints/{id} — get a single blueprint / 获取单个蓝图
//
//	@Summary	Get a blueprint
//	@Tags		Blueprints
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Blueprint ID"
//	@Success	200	{object}	taskscheduler.Blueprint
//	@Failure	404	{object}	api.AppError
//	@Router		/blueprints/{id} [get]
func (s *Server) handleGetBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	bp, err := s.bpStore.Get(id)
	if err != nil {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	// Ensure user can only access their own blueprints / 确保只能访问自己的蓝图
	if bp.UserID != session.UserID {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	writeJSON(w, http.StatusOK, bp)
}

// PUT /api/v1/blueprints/{id} — update a blueprint (full replacement) / 更新蓝图（整体替换）
//
//	@Summary	Update a blueprint
//	@Tags		Blueprints
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string	true	"Blueprint ID"
//	@Param		body	body		object	true	"name, trigger, nodes, metadata"
//	@Success	200		{object}	taskscheduler.Blueprint
//	@Failure	400		{object}	api.AppError
//	@Failure	404		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/blueprints/{id} [put]
func (s *Server) handleUpdateBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	// Verify the blueprint exists and belongs to the current user / 先确认存在且属于当前用户
	existing, err := s.bpStore.Get(id)
	if err != nil || existing.UserID != session.UserID {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	var req struct {
		Name     string                      `json:"name"`
		Trigger  taskscheduler.Trigger       `json:"trigger"`
		Nodes    []taskscheduler.Node        `json:"nodes"`
		Metadata taskscheduler.BlueprintMeta `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Invalid request format")
		return
	}
	if req.Name == "" {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Blueprint name is required")
		return
	}
	if req.Trigger.EventType == "" {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintInvalidRequest, "Trigger eventType is required")
		return
	}

	// Generate IDs for nodes that don't have one / 为没有 ID 的节点生成 ID
	for i := range req.Nodes {
		if req.Nodes[i].ID == "" {
			req.Nodes[i].ID = uuid.New().String()
		}
	}

	// Preserve original CreatedAt, update the rest / 保留原始 CreatedAt，更新其余字段
	existing.Name = req.Name
	existing.Trigger = req.Trigger
	existing.Nodes = req.Nodes
	existing.Metadata = req.Metadata
	existing.UpdatedAt = time.Now()

	if mismatches := taskscheduler.ValidateBlueprintPorts(existing, s.resolveNodeSchemas(existing.Nodes)); len(mismatches) > 0 {
		writeAppError(w, http.StatusBadRequest, ErrBlueprintPortMismatch,
			"Blueprint has incompatible port connections", map[string]any{"mismatches": mismatches})
		return
	}

	if err := s.bpStore.Save(existing); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintSaveFailed, "Failed to save blueprint")
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

// DELETE /api/v1/blueprints/{id} — delete a blueprint / 删除蓝图
//
//	@Summary	Delete a blueprint
//	@Tags		Blueprints
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	string	true	"Blueprint ID"
//	@Success	204	"No Content"
//	@Failure	404	{object}	api.AppError
//	@Failure	500	{object}	api.AppError
//	@Router		/blueprints/{id} [delete]
func (s *Server) handleDeleteBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	// Verify the blueprint exists and belongs to the current user / 先确认存在且属于当前用户
	existing, err := s.bpStore.Get(id)
	if err != nil || existing.UserID != session.UserID {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	if err := s.bpStore.Delete(id); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintDeleteFailed, "Failed to delete blueprint")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
