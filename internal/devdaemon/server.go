package devdaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
	"log/slog"
	
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/util"
	"github.com/GriffinGuard/Griffino/internal/service"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// Server 是运行在 daemon 进程内的 Unix Socket 服务器。
// 它持有 daemon 已经初始化好的 store 和 pluginSvc，
// 因此所有操作都在 daemon 进程内完成，不会产生 DB 锁冲突。
type Server struct {
	socketPath string
	store      *store.Store
	pluginSvc  *service.PluginService
	listener   net.Listener
}

// NewServer 创建 Server 实例。socketPath 通常来自 config.SocketPath()。
func NewServer(socketPath string, st *store.Store, svc *service.PluginService) *Server {
	return &Server{
		socketPath: socketPath,
		store:      st,
		pluginSvc:  svc,
	}
}

// Start 开始监听 socket，阻塞直到 ctx 取消。
// 应在独立 goroutine 中调用。
func (s *Server) Start(ctx context.Context) error {
	// 清理上次可能残留的 socket 文件
	_ = os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create dev socket: %w", err)
	}
	s.listener = ln

	slog.Info("devdaemon listening", "socket", s.socketPath)
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgDevDaemonListening,
		map[string]interface{}{"Path": s.socketPath}))

	// ctx 取消时关闭 listener，使 Accept 返回错误退出循环
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
		slog.Info("devdaemon socket closed")
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// ctx 取消导致的关闭，正常退出
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

// handleConn 处理单个连接：读取一个请求，执行操作，写回响应，关闭连接。
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
		s.writeError(conn, fmt.Sprintf("plugin already installed: %s (status: %s)", pkg.Manifest.ID, existing.Status))
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
		ID:      pkg.Manifest.ID,
		Name:    pkg.Manifest.Name.Default,
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

	// 插件正在运行时的处理
	if instance.Status == store.StatusRunning {
		if !p.Force {
			s.writeError(conn, fmt.Sprintf("plugin %s is running, stop it first or use -r", p.PluginID))
			return
		}
		// -r：先执行完整 stop 流程
		if err := s.pluginSvc.StopDevPlugin(ctx, p.PluginID); err != nil {
			s.writeError(conn, fmt.Sprintf("failed to stop plugin: %s", err.Error()))
			return
		}
	}

	// 删除 DB 记录
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