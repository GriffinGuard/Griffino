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

package devdaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/service"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/util"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// Server is a Unix Socket server running inside the daemon process.
// It holds already-initialized store and pluginSvc from the daemon,
// so all operations run in-process without DB lock conflicts / 运行在 daemon 进程内的 Unix Socket 服务器，持有已初始化的 store 和 pluginSvc，不会产生 DB 锁冲突.
type Server struct {
	socketPath string
	store      *store.Store
	pluginSvc  *service.PluginService
	listener   net.Listener
}

// NewServer creates a Server instance. socketPath typically comes from config.SocketPath() / 创建 Server 实例，socketPath 通常来自 config.SocketPath().
func NewServer(socketPath string, st *store.Store, svc *service.PluginService) *Server {
	return &Server{
		socketPath: socketPath,
		store:      st,
		pluginSvc:  svc,
	}
}

// Start begins listening on the socket, blocking until ctx is cancelled. Call it in a
// dedicated goroutine / 开始监听 socket，阻塞直到 ctx 取消；应在独立 goroutine 中调用.
func (s *Server) Start(ctx context.Context) error {
	// clean up any socket file left over from last time / 清理上次残留的 socket 文件
	_ = os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create dev socket: %w", err)
	}
	s.listener = ln

	slog.Info("devdaemon listening", "socket", s.socketPath)
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgDevDaemonListening,
		map[string]interface{}{"Path": s.socketPath}))

	// close the listener when ctx is cancelled, so Accept returns an error and the loop exits
	// ctx 取消时关闭 listener，使 Accept 报错退出循环
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
		slog.Info("devdaemon socket closed")
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// closed due to ctx cancellation: normal exit / ctx 取消导致的关闭，正常退出
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("devdaemon accept error: %w", err)
			}
		}
		go s.handleConn(ctx, conn)
	}
}

// handleConn handles a single connection: read one request, perform the action, write the
// response, close the connection / 处理单个连接：读请求、执行、写回响应、关闭.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.writeError(conn, "invalid request format: "+err.Error())
		return
	}

	switch req.Op {
	case OpDevInstall:
		s.handleInstall(ctx, conn, req.Payload)
	case OpDevStart:
		s.handleStart(ctx, conn, req.Payload)
	case OpDevStop:
		s.handleStop(ctx, conn, req.Payload)
	case OpDevUninstall:
		s.handleUninstall(ctx, conn, req.Payload)
	default:
		s.writeError(conn, fmt.Sprintf("unknown operation: %s", req.Op))
	}
}

func (s *Server) handleInstall(ctx context.Context, conn net.Conn, raw json.RawMessage) {
	var p InstallPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.Path == "" {
		s.writeError(conn, "dev_install: missing path")
		return
	}

	// path-traversal check (CLI already made it absolute; this is a second line of defense)
	// 路径穿越校验（CLI 侧已转绝对路径，这里做第二道防御）
	if err := util.ValidatePluginDir(p.Path); err != nil {
		s.writeError(conn, "path validation failed: "+err.Error())
		return
	}

	pkg, err := manifest.Load(p.Path)
	if err != nil {
		s.writeError(conn, "failed to load manifest: "+err.Error())
		return
	}
	if err := manifest.Validate(pkg); err != nil {
		s.writeError(conn, "manifest validation failed: "+err.Error())
		return
	}

	existing, _ := s.store.GetPlugin(pkg.Manifest.ID)
	if existing != nil {
		if !p.Force {
			s.writeError(conn, fmt.Sprintf("plugin already installed: %s (status: %s); use --force to overwrite", pkg.Manifest.ID, existing.Status))
			return
		}
		// Overwrite in place: stop if running, preserve AdminConfig, re-point at the new dir / 就地覆盖：运行中先停、保留 AdminConfig、切到新目录
		if _, err := s.pluginSvc.ReinstallDevPlugin(ctx, pkg, p.Path, existing); err != nil {
			s.writeError(conn, err.Error())
			return
		}
		data, _ := json.Marshal(InstallData{
			ID:            pkg.Manifest.ID,
			Name:          pkg.Manifest.Name.Default,
			PluginVersion: pkg.Manifest.PluginVersion,
			Overwritten:   true,
		})
		s.writeOK(conn, data)
		return
	}

	instance := &store.PluginInstance{
		ID:          pkg.Manifest.ID,
		PluginDir:   p.Path,
		Status:      store.StatusPendingSetup,
		InstalledAt: time.Now(),
		IsDevPlugin: true,
		AdminConfig: make(map[string]map[string]string),
	}
	if err := s.store.SavePlugin(instance); err != nil {
		s.writeError(conn, "failed to save plugin: "+err.Error())
		return
	}

	data, _ := json.Marshal(InstallData{
		ID:            pkg.Manifest.ID,
		Name:          pkg.Manifest.Name.Default,
		PluginVersion: pkg.Manifest.PluginVersion,
	})
	s.writeOK(conn, data)
}

func (s *Server) handleStart(ctx context.Context, conn net.Conn, raw json.RawMessage) {
	var p PluginIDPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.PluginID == "" {
		s.writeError(conn, "dev_start: missing pluginId")
		return
	}

	instance, err := s.pluginSvc.StartDevPlugin(ctx, p.PluginID)
	if err != nil {
		s.writeError(conn, err.Error())
		return
	}

	data, _ := json.Marshal(StartData{
		Network:      instance.RuntimeInfo.Network,
		RabbitMQUser: instance.RuntimeInfo.RabbitMQUser,
		Containers:   instance.RuntimeInfo.Containers,
	})
	s.writeOK(conn, data)
}

func (s *Server) handleStop(ctx context.Context, conn net.Conn, raw json.RawMessage) {
	var p PluginIDPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.PluginID == "" {
		s.writeError(conn, "dev_stop: missing pluginId")
		return
	}

	if err := s.pluginSvc.StopDevPlugin(ctx, p.PluginID); err != nil {
		s.writeError(conn, err.Error())
		return
	}

	s.writeOK(conn, nil)
}

func (s *Server) handleUninstall(ctx context.Context, conn net.Conn, raw json.RawMessage) {
	var p UninstallPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.PluginID == "" {
		s.writeError(conn, "dev_uninstall: missing pluginId")
		return
	}

	instance, err := s.store.GetPlugin(p.PluginID)
	if err != nil || instance == nil {
		s.writeError(conn, fmt.Sprintf("plugin not found: %s", p.PluginID))
		return
	}
	if !instance.IsDevPlugin {
		s.writeError(conn, fmt.Sprintf("plugin %s is not a dev plugin, uninstall via Web-UI", p.PluginID))
		return
	}

	// handle the case where the plugin is running / 插件正在运行时的处理
	if instance.Status == store.StatusRunning {
		if !p.Force {
			s.writeError(conn, fmt.Sprintf("plugin %s is running, stop it first or use -r", p.PluginID))
			return
		}
		// -r: run the full stop flow first / -r：先执行完整 stop 流程
		if err := s.pluginSvc.StopDevPlugin(ctx, p.PluginID); err != nil {
			s.writeError(conn, fmt.Sprintf("failed to stop plugin: %s", err.Error()))
			return
		}
	}

	// delete the DB record / 删除 DB 记录
	if err := s.store.DeletePlugin(p.PluginID); err != nil {
		s.writeError(conn, fmt.Sprintf("failed to delete plugin record: %s", err.Error()))
		return
	}

	s.writeOK(conn, nil)
}

func (s *Server) writeOK(conn net.Conn, data json.RawMessage) {
	resp := Response{OK: true, Data: data}
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) writeError(conn net.Conn, msg string) {
	resp := Response{OK: false, Error: msg}
	_ = json.NewEncoder(conn).Encode(resp)
}
