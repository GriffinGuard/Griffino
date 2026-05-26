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