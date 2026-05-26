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

	"github.com/GriffinGuard/Griffino/internal/system"
)

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

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "running",
		"system": map[string]any{
			"rabbitmq": map[string]any{
				"container": mqContainer,
				"port":      sysState.RabbitMQPort,
				"mgmtPort":  sysState.RabbitMQManagementPort,
			},
			"redis": map[string]any{
				"container": rdContainer,
				"port":      sysState.RedisPort,
			},
		},
	})
}