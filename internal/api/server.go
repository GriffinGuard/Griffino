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
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/GriffinGuard/Griffino/internal/api/web"
	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/metrics"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/router"
	"github.com/GriffinGuard/Griffino/internal/service"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	_ "github.com/GriffinGuard/Griffino/docs/api" // generated OpenAPI spec (swag init)
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// PluginService abstracts the plugin service methods HTTP handlers depend on, for easy fake substitution in tests / 抽象出 HTTP handlers 实际依赖的插件服务方法，便于在测试中以假实现替换.
// *service.PluginService implicitly satisfies this interface (see NewServer assignment) / 具体的 *service.PluginService 隐式满足该接口（见 NewServer 的赋值）.
type PluginService interface {
	StartPluginAsync(pluginID string) error
	StartPluginFromInstance(instance *store.PluginInstance) error
	StopPlugin(ctx context.Context, pluginID string) error
	StopThenStartAsync(pluginID string) error
	UninstallPlugin(ctx context.Context, pluginID string) error
	UpgradePlugin(ctx context.Context, pluginID, newDir string, newPkg *manifest.PluginPackage) error
}

type Server struct {
	cfg              *config.Config
	st               *store.Store
	sysMgr           *system.Manager
	dockerCli        *client.Client
	pluginSvc        PluginService
	sessionMgr       *auth.SessionManager
	httpServer       *http.Server
	userConfigStore  *store.UserConfigStore
	routeStore       *router.RouteStore
	bpStore          *taskscheduler.BlueprintStore
	taskStore        *taskscheduler.TaskStore
	schemaStore      *taskscheduler.SchemaStore
	statusViewReader *statusViewReader
	dashboardStore   *store.DashboardStore
	router           *router.Router
	componentData    componentDataRenderer
	logDir           string
	version          string
	startedAt        time.Time
}

func NewServer(
	cfg *config.Config,
	st *store.Store,
	sysMgr *system.Manager,
	dockerCli *client.Client,
	pluginSvc *service.PluginService,
	r *router.Router,
	version string,
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
		version:    version,
		startedAt:  time.Now(),
	}

	s.pluginSvc = service.NewPluginService(cfg, st, sysMgr, dockerCli)

	s.routeStore = router.NewRouteStore(redisAddr, sysState.RedisPassword)

	s.bpStore = taskscheduler.NewBlueprintStore(st.DB())

	s.taskStore = taskscheduler.NewTaskStore(redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: sysState.RedisPassword,
	}))

	s.schemaStore = taskscheduler.NewSchemaStore(st.DB())
	// Seed the embedded v1 standard interface port specs so StandardInterfaceRef
	// resolves during blueprint port validation. Non-fatal: on failure, validation
	// falls back to bypassing unresolved nodes / 灌入标准接口端口规格，失败不致命，退回放行.
	if err := taskscheduler.SeedStandardSchemas(s.schemaStore); err != nil {
		slog.Warn("failed to seed standard interface schemas", "err", err)
	}

	s.statusViewReader = newStatusViewReader(redisAddr, sysState.RedisPassword)

	s.dashboardStore = store.NewDashboardStore(redisAddr, sysState.RedisPassword)

	s.router = r

	s.userConfigStore = store.NewUserConfigStore(redisAddr, sysState.RedisPassword)
	s.componentData = newRabbitComponentDataRenderer(sysMgr)
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Defensive fallback: empty host would bind to all interfaces (:port), violating localhost-only; port defaults to 7070 / 防御性兜底，空 host 绑到所有网卡违背本机优先，端口缺省回落 7070.
	listenHost := cfg.Server.ListenHost
	if listenHost == "" {
		listenHost = "127.0.0.1"
	}
	listenPort := cfg.Server.ListenPort
	if listenPort == 0 {
		listenPort = 7070
	}
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(listenPort))
	s.httpServer = &http.Server{
		Addr:         listenAddr,
		Handler:      otelhttp.NewHandler(s.corsMiddleware(s.metricsMiddleware(mux)), "griffino-api"),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	slog.Info("API server listening", "addr", s.httpServer.Addr)
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgAPIServerListening,
		map[string]interface{}{"Addr": s.httpServer.Addr}))
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// registerRoutes registers all routes / 注册所有路由
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Public / 公开
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	// First-boot probe and initial admin creation (no login required; creation only allowed when no users exist yet) / 首启探测与首个管理员创建（无需登录，创建仅在尚无任何用户时允许）
	mux.HandleFunc("GET /api/v1/setup/state", s.handleSetupState)
	mux.HandleFunc("POST /api/v1/setup/admin", s.handleSetupCreateAdmin)

	// Prometheus scrape endpoint: no auth needed — API is localhost-only (127.0.0.1) / Prometheus 抓取端点无需鉴权，API 仅绑定本机.
	mux.Handle("GET /metrics", metrics.Handler())

	// Login required / 需要登录
	protected := http.NewServeMux()
	protected.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	protected.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	protected.HandleFunc("POST /api/v1/auth/password", s.handleChangePassword)
	protected.HandleFunc("GET /api/v1/system/status", s.handleSystemStatus)
	protected.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	protected.HandleFunc("GET /api/v1/plugins", s.handleListPlugins)
	protected.HandleFunc("GET /api/v1/plugins/{id}", s.handleGetPlugin)
	protected.HandleFunc("GET /api/v1/plugins/{id}/config", s.handleGetPluginConfig)
	protected.HandleFunc("GET /api/v1/plugins/{id}/user-config", s.handleGetUserConfigSchema)
	protected.HandleFunc("GET /api/v1/plugins/{id}/user-config/values", s.handleGetUserConfigValues)
	protected.HandleFunc("POST /api/v1/plugins/{id}/user-config/values", s.handleSetUserConfigValues)
	protected.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	protected.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	protected.HandleFunc("GET /api/v1/blueprints", s.handleListBlueprints)
	protected.HandleFunc("POST /api/v1/blueprints", s.handleCreateBlueprint)
	protected.HandleFunc("GET /api/v1/blueprints/{id}", s.handleGetBlueprint)
	protected.HandleFunc("PUT /api/v1/blueprints/{id}", s.handleUpdateBlueprint)
	protected.HandleFunc("DELETE /api/v1/blueprints/{id}", s.handleDeleteBlueprint)
	protected.HandleFunc("GET /api/v1/plugins/{id}/widgets", s.handleListWidgets)
	protected.HandleFunc("GET /api/v1/plugins/{id}/widgets/{widgetId}/data", s.handleGetWidgetData)
	protected.HandleFunc("POST /api/v1/plugins/{id}/actions/{actionId}", s.handleTriggerAction)
	protected.HandleFunc("GET /api/v1/me/dashboard", s.handleGetDashboard)
	protected.HandleFunc("PUT /api/v1/me/dashboard", s.handlePutDashboard)
	protected.HandleFunc("GET /api/v1/me/sessions", s.handleListMySessions)
	protected.HandleFunc("DELETE /api/v1/me/sessions/{sessionID}", s.handleRevokeMySession)

	// Admin only / 仅 admin
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("POST /api/v1/setup/complete", s.handleSetupComplete)
	adminMux.HandleFunc("POST /api/v1/plugins/{id}/config", s.handleConfigPlugin)
	adminMux.HandleFunc("POST /api/v1/plugins/{id}/start", s.handleStartPlugin)
	adminMux.HandleFunc("POST /api/v1/plugins/{id}/stop", s.handleStopPlugin)
	adminMux.HandleFunc("DELETE /api/v1/plugins/{id}", s.handleUninstallPlugin)
	adminMux.HandleFunc("GET /api/v1/users", s.handleListUsers)
	adminMux.HandleFunc("POST /api/v1/users", s.handleCreateUser)
	adminMux.HandleFunc("PATCH /api/v1/users/{username}", s.handleUpdateUser)
	adminMux.HandleFunc("PATCH /api/v1/users/{username}/profile", s.handleUpdateUserProfile)
	adminMux.HandleFunc("DELETE /api/v1/users/{username}", s.handleDeleteUser)
	adminMux.HandleFunc("GET /api/v1/registry/plugins", s.handleListRegistryPlugins)
	adminMux.HandleFunc("GET /api/v1/registry/plugins/{id}", s.handleGetRegistryPlugin)
	adminMux.HandleFunc("POST /api/v1/registry/plugins/{id}/install", s.handleInstallRegistryPlugin)
	adminMux.HandleFunc("POST /api/v1/registry/plugins/{id}/upgrade", s.handleUpgradeRegistryPlugin)
	adminMux.HandleFunc("GET /api/v1/admin/settings/security", s.handleGetSecuritySettings)
	adminMux.HandleFunc("PUT /api/v1/admin/settings/security", s.handlePutSecuritySettings)
	adminMux.HandleFunc("GET /api/v1/admin/metrics", s.handleAdminMetrics)
	adminMux.HandleFunc("GET /api/v1/audit-logs", s.handleListAuditLogs)
	adminMux.HandleFunc("POST /api/v1/setup/reset", s.handleSetupReset)
	adminMux.HandleFunc("GET /api/v1/system/smtp", s.handleGetSMTP)
	adminMux.HandleFunc("PUT /api/v1/system/smtp", s.handlePutSMTP)
	adminMux.HandleFunc("POST /api/v1/system/smtp/test", s.handleTestSMTP)
	protected.HandleFunc("GET /api/v1/plugins/capabilities", s.handleListCapabilities)
	protected.HandleFunc("GET /api/v1/plugins/triggers", s.handleListTriggers)
	protected.HandleFunc("GET /api/v1/users/routes", s.handleGetRoutes)
	protected.HandleFunc("POST /api/v1/users/routes", s.handleSetRoutes)

	protected.Handle("/api/v1/", s.adminOnly(adminMux))
	mux.Handle("/api/v1/", s.corsMiddleware(s.authMiddleware(protected)))

	// Health check endpoint (for frontend liveness probes) / 健康检查端点（供前端探活）
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Swagger UI + raw spec. No auth needed — API is localhost-only (127.0.0.1) / Swagger UI 和原始 spec 无需鉴权，API 仅绑定本机.
	// UI at /swagger/index.html, raw spec at /swagger/doc.json / UI 在 /swagger/index.html，原始 spec 在 /swagger/doc.json.
	mux.Handle("GET /swagger/", httpSwagger.WrapHandler)

	// Embedded web console: non-/api, /health paths go to SPA (with client-side routing fallback) / 内嵌的 Web 控制台，非 /api、/health 的路径交给 SPA 含客户端路由回退.
	// More specific /api/v1/ and /health patterns match first, so "/" here catches the rest / 更具体的 /api/v1/ 和 /health 模式优先匹配，"只兜其余路径.
	if webHandler, err := web.Handler(); err != nil {
		slog.Error("failed to initialize embedded web UI", "err", err)
	} else {
		mux.Handle("/", webHandler)
	}
}

// --- Common response helpers --- / 通用响应辅助函数

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
