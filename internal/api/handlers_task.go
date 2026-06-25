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
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
)

// GET /api/v1/tasks
// Returns all Workflow run instances (Tasks) for the current user, ordered by creation time descending / 返回当前用户的所有 Workflow 运行实例（Task），按创建时间降序
//
//	@Summary	List tasks
//	@Tags		Tasks
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string][]taskscheduler.Task
//	@Failure	500	{object}	api.AppError
//	@Router		/tasks [get]
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	tasks, err := s.taskStore.ListByUser(r.Context(), session.UserID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrTaskFetchFailed, "Failed to list tasks")
		return
	}
	if tasks == nil {
		tasks = []*taskscheduler.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// GET /api/v1/tasks/{id}
// Get a single Task's details including accumulated context, status, and failure reason / 获取单个 Task 详情（含累积 context、状态、失败原因）
//
//	@Summary	Get a task
//	@Tags		Tasks
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Task ID"
//	@Success	200	{object}	taskscheduler.Task
//	@Failure	404	{object}	api.AppError
//	@Router		/tasks/{id} [get]
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	id := r.PathValue("id")

	task, err := s.taskStore.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, http.StatusNotFound, ErrTaskNotFound, "Task not found",
			map[string]any{"id": id})
		return
	}

	// Ensure user can only access their own Tasks / 确保只能访问自己的 Task
	if task.UserID != session.UserID {
		writeAppError(w, http.StatusNotFound, ErrTaskNotFound, "Task not found",
			map[string]any{"id": id})
		return
	}

	writeJSON(w, http.StatusOK, task)
}
