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
	"time"

	"github.com/GriffinGuard/Griffino/internal/system"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// handleSystemStatus reports daemon, Docker, and infrastructure health.
//
//	@Summary	Get system status
//	@Tags		System
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	503	{object}	api.AppError
//	@Router		/system/status [get]
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		writeAppError(w, http.StatusServiceUnavailable, ErrSystemNotInitialized, "System is not initialized")
		return
	}

	mqContainer := sysState.RabbitMQContainerName
	if mqContainer == "" {
		mqContainer = system.RabbitMQContainerName
	}
	rdContainer := sysState.RedisContainerName
	if rdContainer == "" {
		rdContainer = system.RedisContainerName
	}

	// Health signals for the setup wizard / dashboard. Bounded so a wedged
	// Docker socket can't hang the request.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	dockerAvailable := s.dockerAvailable(ctx)
	mqHealthy := dockerAvailable && s.containerRunning(ctx, mqContainer)
	rdHealthy := dockerAvailable && s.containerRunning(ctx, rdContainer)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "running",
		"docker": map[string]any{
			"available": dockerAvailable,
		},
		"system": map[string]any{
			"rabbitmq": map[string]any{
				"container": mqContainer,
				"port":      sysState.RabbitMQPort,
				"mgmtPort":  sysState.RabbitMQManagementPort,
				"healthy":   mqHealthy,
				"image":     system.RabbitMQImage,
			},
			"redis": map[string]any{
				"container":   rdContainer,
				"port":        sysState.RedisPort,
				"healthy":     rdHealthy,
				"image":       system.RedisImage,
				"persistence": "AOF",
			},
		},
		"daemon": map[string]any{
			"version":       s.version,
			"startedAt":     s.startedAt.UTC().Format(time.RFC3339),
			"uptime":        formatUptime(time.Since(s.startedAt)),
			"systemNetwork": system.SystemNetwork,
			"databasePath":  s.cfg.DatabasePath,
			"webUiStatus":   "unknown",
		},
	})
}

// formatUptime converts a duration into a human-readable string like "2d 4h 15m".
func formatUptime(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// dockerAvailable reports whether the Docker daemon is reachable.
func (s *Server) dockerAvailable(ctx context.Context) bool {
	if s.dockerCli == nil {
		return false
	}
	_, err := s.dockerCli.Ping(ctx)
	return err == nil
}

// containerRunning reports whether a container with the given name is currently
// running (ContainerList without All returns only running containers).
func (s *Server) containerRunning(ctx context.Context, name string) bool {
	if s.dockerCli == nil {
		return false
	}
	containers, err := s.dockerCli.ContainerList(ctx, dockercontainer.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "/"+name)),
	})
	if err != nil {
		return false
	}
	for _, ct := range containers {
		for _, n := range ct.Names {
			if n == "/"+name {
				return true
			}
		}
	}
	return false
}
