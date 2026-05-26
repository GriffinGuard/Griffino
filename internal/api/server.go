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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/router"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
	"github.com/GriffinGuard/Griffino/internal/service"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/docker/docker/client"
)


type Server struct {
	cfg        *config.Config
	st      *store.Store
	sysMgr     *system.Manager
	dockerCli  *client.Client
	pluginSvc  *service.PluginService
	sessionMgr *auth.SessionManager
	httpServer *http.Server
	userConfigStore *store.UserConfigStore
	routeStore *router.RouteStore
	bpStore 	*taskscheduler.BlueprintStore
	statusViewReader *statusViewReader
	router *router.Router
	logDir string
}

func NewServer(
	cfg *config.Config,
	st *store.Store,
	sysMgr *system.Manager,
	dockerCli *client.Client,
	pluginSvc *service.PluginService,
	r *router.Router, 
) *Server {
	sysState, _ := sysMgr.GetSystemState()
	redisAddr := fmt.Sprintf("localhost:%d", sysState.RedisPort)
	sessionMgr := auth.NewSessionManager("localhost", sysState.RedisPort, sysState.RedisPassword)

	s := &Server{
		cfg:        cfg,
		st:         st,
		sysMgr:     sysMgr,
		dockerCli:  dockerCli,
		sessionMgr: sessionMgr,
	}

	s.pluginSvc = service.NewPluginService(cfg, st, sysMgr, dockerCli)

	s.routeStore = router.NewRouteStore(redisAddr, sysState.RedisPassword)

	s.bpStore = taskscheduler.NewBlueprintStore(st.DB())

	s.statusViewReader = newStatusViewReader(redisAddr, sysState.RedisPassword)

	s.router = r

	s.userConfigStore = store.NewUserConfigStore(redisAddr, sysState.RedisPassword)
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         ":7070",
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
    slog.Info("API server listening", "addr", s.httpServer.Addr)
    addr := strings.TrimPrefix(s.httpServer.Addr, ":")
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgAPIServerListening,
		map[string]interface{}{"Addr": "localhost:" + addr}))
    return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// registerRoutes 注册所有路由
func (s *Server) registerRoutes(mux *http.ServeMux) {
    // 公开
    mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

    // 需要登录
    protected := http.NewServeMux()
    protected.HandleFunc("POST /api/v1/auth/logout",   s.handleLogout)
    protected.HandleFunc("GET /api/v1/auth/me",         s.handleMe)
    protected.HandleFunc("POST /api/v1/auth/password",  s.handleChangePassword)
    protected.HandleFunc("GET /api/v1/system/status",   s.handleSystemStatus)
    protected.HandleFunc("GET /api/v1/plugins",         s.handleListPlugins)
    protected.HandleFunc("GET /api/v1/plugins/{id}",    s.handleGetPlugin)
    protected.HandleFunc("GET /api/v1/plugins/{id}/config", s.handleGetPluginConfig)
	protected.HandleFunc("GET /api/v1/plugins/{id}/user-config",	s.handleGetUserConfigSchema)
	protected.HandleFunc("GET /api/v1/plugins/{id}/user-config/values",	s.handleGetUserConfigValues)
	protected.HandleFunc("POST /api/v1/plugins/{id}/user-config/values",	s.handleSetUserConfigValues)
	protected.HandleFunc("GET /api/v1/blueprints",          s.handleListBlueprints)
	protected.HandleFunc("POST /api/v1/blueprints",         s.handleCreateBlueprint)
	protected.HandleFunc("GET /api/v1/blueprints/{id}",     s.handleGetBlueprint)
	protected.HandleFunc("PUT /api/v1/blueprints/{id}",     s.handleUpdateBlueprint)
	protected.HandleFunc("DELETE /api/v1/blueprints/{id}",  s.handleDeleteBlueprint)
	protected.HandleFunc("GET /api/v1/plugins/{id}/status-views",        s.handleListStatusViews)
	protected.HandleFunc("GET /api/v1/plugins/{id}/status/{viewId}",     s.handleGetStatusView)
	protected.HandleFunc("GET  /api/v1/plugins/{id}/actions",             s.handleListActions)
	protected.HandleFunc("POST /api/v1/plugins/{id}/actions/{actionId}",  s.handleTriggerAction)

    // 仅 admin
    adminMux := http.NewServeMux()
    adminMux.HandleFunc("POST /api/v1/plugins/{id}/config",            s.handleConfigPlugin)
    adminMux.HandleFunc("POST /api/v1/plugins/{id}/start",             s.handleStartPlugin)
    adminMux.HandleFunc("POST /api/v1/plugins/{id}/stop",              s.handleStopPlugin)
    adminMux.HandleFunc("DELETE /api/v1/plugins/{id}",                 s.handleUninstallPlugin)
    adminMux.HandleFunc("GET /api/v1/users",                           s.handleListUsers)
    adminMux.HandleFunc("POST /api/v1/users",                          s.handleCreateUser)
    adminMux.HandleFunc("PATCH /api/v1/users/{username}",              s.handleUpdateUser)
    adminMux.HandleFunc("DELETE /api/v1/users/{username}",             s.handleDeleteUser)
    adminMux.HandleFunc("GET /api/v1/registry/plugins",                s.handleListRegistryPlugins)
    adminMux.HandleFunc("POST /api/v1/registry/plugins/{id}/install",  s.handleInstallRegistryPlugin)
	protected.HandleFunc("GET /api/v1/plugins/capabilities",  s.handleListCapabilities)
	protected.HandleFunc("GET /api/v1/users/routes",          s.handleGetRoutes)
	protected.HandleFunc("POST /api/v1/users/routes",         s.handleSetRoutes)

    protected.Handle("/api/v1/", s.adminOnly(adminMux))
    mux.Handle("/api/v1/", s.corsMiddleware(s.authMiddleware(protected)))

	// 健康检查端点（供前端探活）
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
})

}

// --- 通用响应辅助函数 ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}