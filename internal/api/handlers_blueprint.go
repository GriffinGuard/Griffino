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
	"github.com/google/uuid"
)

// GET /api/v1/blueprints
// 返回当前用户的所有蓝图
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

// POST /api/v1/blueprints
// 创建新蓝图
func (s *Server) handleCreateBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var req struct {
		Name     string                    `json:"name"`
		Trigger  taskscheduler.Trigger     `json:"trigger"`
		Nodes    []taskscheduler.Node      `json:"nodes"`
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

	// 为每个没有 ID 的节点生成 ID
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

	if err := s.bpStore.Save(bp); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintSaveFailed, "Failed to save blueprint")
		return
	}

	writeJSON(w, http.StatusCreated, bp)
}

// GET /api/v1/blueprints/{id}
// 获取单个蓝图
func (s *Server) handleGetBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	bp, err := s.bpStore.Get(id)
	if err != nil {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	// 确保只能访问自己的蓝图
	if bp.UserID != session.UserID {
		writeAppError(w, http.StatusNotFound, ErrBlueprintNotFound, "Blueprint not found",
			map[string]any{"id": id})
		return
	}

	writeJSON(w, http.StatusOK, bp)
}

// PUT /api/v1/blueprints/{id}
// 更新蓝图（整体替换）
func (s *Server) handleUpdateBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	// 先确认存在且属于当前用户
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

	// 为没有 ID 的节点生成 ID
	for i := range req.Nodes {
		if req.Nodes[i].ID == "" {
			req.Nodes[i].ID = uuid.New().String()
		}
	}

	// 保留原始 CreatedAt，更新其余字段
	existing.Name = req.Name
	existing.Trigger = req.Trigger
	existing.Nodes = req.Nodes
	existing.Metadata = req.Metadata
	existing.UpdatedAt = time.Now()

	if err := s.bpStore.Save(existing); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrBlueprintSaveFailed, "Failed to save blueprint")
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

// DELETE /api/v1/blueprints/{id}
// 删除蓝图
func (s *Server) handleDeleteBlueprint(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	// 先确认存在且属于当前用户
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