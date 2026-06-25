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
	"net/http"
	"time"

	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
)

// handleAdminMetrics returns aggregated counts for plugins, tasks, users and system health.
//
//	@Summary	Get admin dashboard metrics
//	@Tags		Admin
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/admin/metrics [get]
func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Plugins / 插件统计
	plugins, err := s.st.ListPlugins()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to list plugins")
		return
	}
	pluginTotal, pluginRunning, pluginStopped, pluginFailed := 0, 0, 0, 0
	for _, p := range plugins {
		pluginTotal++
		switch p.Status {
		case store.StatusRunning:
			pluginRunning++
		case store.StatusStopped:
			pluginStopped++
		case store.StatusFailed:
			pluginFailed++
		}
	}

	// Tasks / 任务统计
	tasks, err := s.taskStore.ListAll(ctx)
	if err != nil {
		tasks = []*taskscheduler.Task{} // non-fatal: Redis may be warming up / 非致命，Redis 预热中
	}
	taskTotal, taskRunning, taskCompleted, taskFailed := 0, 0, 0, 0
	for _, t := range tasks {
		taskTotal++
		switch t.Status {
		case taskscheduler.TaskStatusRunning:
			taskRunning++
		case taskscheduler.TaskStatusCompleted:
			taskCompleted++
		case taskscheduler.TaskStatusFailed:
			taskFailed++
		}
	}

	// Users / 用户统计
	users, err := s.st.ListUsers()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to list users")
		return
	}
	userTotal, userActive, userDisabled := 0, 0, 0
	for _, u := range users {
		userTotal++
		if u.Disabled {
			userDisabled++
		} else {
			userActive++
		}
	}

	// System health / 系统健康检查
	mqContainer := system.RabbitMQContainerName
	rdContainer := system.RedisContainerName
	if sysState, err := s.sysMgr.GetSystemState(); err == nil {
		if sysState.RabbitMQContainerName != "" {
			mqContainer = sysState.RabbitMQContainerName
		}
		if sysState.RedisContainerName != "" {
			rdContainer = sysState.RedisContainerName
		}
	}
	dockerAvailable := s.dockerAvailable(ctx)
	mqHealthy := dockerAvailable && s.containerRunning(ctx, mqContainer)
	rdHealthy := dockerAvailable && s.containerRunning(ctx, rdContainer)

	writeJSON(w, http.StatusOK, map[string]any{
		"plugins": map[string]any{
			"total":   pluginTotal,
			"running": pluginRunning,
			"stopped": pluginStopped,
			"failed":  pluginFailed,
		},
		"tasks": map[string]any{
			"total":     taskTotal,
			"running":   taskRunning,
			"completed": taskCompleted,
			"failed":    taskFailed,
		},
		"users": map[string]any{
			"total":    userTotal,
			"active":   userActive,
			"disabled": userDisabled,
		},
		"system": map[string]any{
			"rabbitmqHealthy": mqHealthy,
			"redisHealthy":    rdHealthy,
			"dockerAvailable": dockerAvailable,
		},
	})
}
