package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// handleListPlugins GET /api/v1/plugins
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	plugins, err := s.st.ListPlugins()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginListFailed, "Failed to list plugins")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": toPluginDTOs(plugins)})
}

// handleGetPlugin GET /api/v1/plugins/{id}
func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plugin, err := s.st.GetPlugin(id)
	if err != nil || plugin == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
    		map[string]interface{}{"id": id})
		return
	}
	writeJSON(w, http.StatusOK, toPluginDTO(plugin))
}

// handleGetPluginConfig GET /api/v1/plugins/{id}/config
func (s *Server) handleGetPluginConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plugin, err := s.st.GetPlugin(id)
	if err != nil || plugin == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
    		map[string]interface{}{"id": id})
		return
	}

	pkg, err := manifest.Load(plugin.PluginDir)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to load plugin manifest",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	// 构建回显值：非 password 字段返回当前值，password 字段返回占位符
	const redacted = "__REDACTED__"
	currentValues := make(map[string]map[string]string)
	for _, svc := range pkg.BootConfig.Services {
		svcValues := make(map[string]string)
		for _, param := range svc.Configs {
			if savedVal, ok := plugin.AdminConfig[svc.ID][param.Key]; ok && savedVal != "" {
				if param.Type == manifest.ConfigTypePassword {
					svcValues[param.Key] = redacted
				} else {
					svcValues[param.Key] = savedVal
				}
			}
		}
		if len(svcValues) > 0 {
			currentValues[svc.ID] = svcValues
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pluginId":      id,
		"name":          pkg.BootConfig.Name,
		"services":      pkg.BootConfig.Services,
		"currentValues": currentValues,
	})
}

// handleConfigPlugin POST /api/v1/plugins/{id}/config
// 支持 action 字段：
//   "save"           仅保存，不启动
//   "save_and_start" 保存并启动（适用于 pending_setup/ready/stopped/failed）
//   "save_and_restart" 保存并重启（适用于 running）
func (s *Server) handleConfigPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	plugin, err := s.st.GetPlugin(id)
	if err != nil || plugin == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
    		map[string]interface{}{"id": id})
		return
	}

	var body struct {
		Config map[string]map[string]string `json:"config"`
		Action string                       `json:"action"` // save | save_and_start | save_and_restart
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrAuthInvalidRequest, "Invalid request format")
		return
	}
	if body.Action == "" {
		body.Action = "save"
	}

	// 合并配置：password 字段值为 __REDACTED__ 时沿用旧值
	const redacted = "__REDACTED__"
	if plugin.AdminConfig == nil {
		plugin.AdminConfig = make(map[string]map[string]string)
	}
	for svcID, svcValues := range body.Config {
		if plugin.AdminConfig[svcID] == nil {
			plugin.AdminConfig[svcID] = make(map[string]string)
		}
		for key, val := range svcValues {
			if val == redacted {
				continue // 沿用旧值
			}
			plugin.AdminConfig[svcID][key] = val
		}
	}

	// 根据当前状态更新 status 和 configDirty
	switch plugin.Status {
	case store.StatusPendingSetup:
		plugin.Status = store.StatusReady
		plugin.ConfigDirty = false
	case store.StatusRunning:
		plugin.ConfigDirty = true
	default:
		plugin.ConfigDirty = false
	}

	if err := s.st.SavePlugin(plugin); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginSaveFailed, "Failed to save plugin config")
		return
	}

	// 根据当前状态更新 status 和 configDirty
	switch body.Action {
	case "save_and_start":
		if plugin.Status == store.StatusRunning {
			writeAppError(w, http.StatusConflict, ErrPluginAlreadyRunning, "Plugin is already running, use save_and_restart instead")
			return
		}
		if err := s.pluginSvc.StartPluginFromInstance(plugin); err != nil {
			writeAppError(w, http.StatusBadRequest, ErrPluginStartFailed, "Failed to schedule plugin start",
				map[string]interface{}{"detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": store.StatusPulling,
			"action": "save_and_start",
		})
	case "save_and_restart":
		if plugin.Status != store.StatusRunning {
			writeAppError(w, http.StatusConflict, ErrPluginNotRunning, "Plugin is not running, use save_and_start instead")
			return
		}
		if err := s.pluginSvc.StopThenStartAsync(id); err != nil {
			writeAppError(w, http.StatusBadRequest, ErrPluginStartFailed, "Failed to schedule plugin restart",
				map[string]interface{}{"detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": store.StatusPulling,
			"action": "save_and_restart",
		})
	default: // "save"
		writeJSON(w, http.StatusOK, map[string]any{
			"status": plugin.Status,
			"action": "save",
		})
	}
}

// handleStartPlugin POST /api/v1/plugins/{id}/start
// Dev 插件会被 StartPlugin 内部拒绝，返回明确错误。
func (s *Server) handleStartPlugin(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := s.pluginSvc.StartPluginAsync(id); err != nil {
        writeAppError(w, http.StatusBadRequest, ErrPluginStartFailed, "Failed to schedule plugin start",
            map[string]interface{}{"detail": err.Error()})
        return
    }
    writeJSON(w, http.StatusAccepted, map[string]any{
        "status": store.StatusPulling,
    })
}

// handleStopPlugin POST /api/v1/plugins/{id}/stop
// Dev 插件会被 StopPlugin 内部拒绝，返回明确错误。
func (s *Server) handleStopPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.pluginSvc.StopPlugin(r.Context(), id); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginStopFailed, "Failed to stop plugin",
    		map[string]interface{}{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleUninstallPlugin DELETE /api/v1/plugins/{id}
func (s *Server) handleUninstallPlugin(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    plugin, err := s.st.GetPlugin(id)
    if err != nil || plugin == nil {
        writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found",
            map[string]interface{}{"id": id})
        return
    }

    if err := s.pluginSvc.UninstallPlugin(r.Context(), id); err != nil {
        writeAppError(w, http.StatusInternalServerError, ErrPluginUninstallFailed, "Failed to uninstall plugin",
            map[string]interface{}{"detail": err.Error()})
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

// installFromRegistry 供 handlers_registry.go 在完成下载和白名单检查后调用。
// 不对外暴露 API 路由。
func (s *Server) installFromRegistry(w http.ResponseWriter, pluginDir string) {
	pkg, err := manifest.Load(pluginDir)
	if err != nil {
		writeAppError(w, http.StatusBadRequest, ErrPluginLoadFailed, "Failed to load plugin manifest",
    		map[string]interface{}{"detail": err.Error()})
		return
	}
	if err := manifest.Validate(pkg); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrPluginLoadFailed, "Plugin manifest validation failed",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	existing, _ := s.st.GetPlugin(pkg.Manifest.ID)
	if existing != nil {
		writeAppError(w, http.StatusConflict, ErrPluginAlreadyInstalled, "Plugin already installed",
    		map[string]interface{}{"id": pkg.Manifest.ID})
		return
	}

	instance := &store.PluginInstance{
		ID:          pkg.Manifest.ID,
		PluginDir:   pluginDir,
		Status:      store.StatusPendingSetup,
		InstalledAt: time.Now(),
		IsDevPlugin: false, // Registry 安装的插件永远不是 Dev 插件
		AdminConfig: make(map[string]map[string]string),
	}
	if err := s.st.SavePlugin(instance); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrPluginSaveFailed, "Failed to save plugin")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      pkg.Manifest.ID,
		"name":    pkg.Manifest.Name,
		"version": pkg.Manifest.PluginVersion,
		"status":  store.StatusPendingSetup,
	})
}