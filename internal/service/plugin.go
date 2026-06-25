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

package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/imagecheck"
	"github.com/GriffinGuard/Griffino/internal/logger"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/pkg/broker"
	"github.com/GriffinGuard/Griffino/pkg/container"
	"github.com/GriffinGuard/Griffino/pkg/env"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/GriffinGuard/Griffino/pkg/redisacl"
	tmpl "github.com/GriffinGuard/Griffino/pkg/template"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type PluginService struct {
	cfg           *config.Config
	store         *store.Store
	sysMgr        *system.Manager
	dockerCli     *client.Client
	streamCancels map[string]context.CancelFunc
	streamMu      sync.Mutex
	streamCtx     context.Context
	streamCancel  context.CancelFunc
}

func NewPluginService(
	cfg *config.Config,
	store *store.Store,
	sysMgr *system.Manager,
	dockerCli *client.Client,
) *PluginService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PluginService{
		cfg:           cfg,
		store:         store,
		sysMgr:        sysMgr,
		dockerCli:     dockerCli,
		streamCancels: make(map[string]context.CancelFunc),
		streamCtx:     ctx,
		streamCancel:  cancel,
	}
}

// StartPlugin starts a plugin via API/Web-UI.
// Dev plugins are rejected — they must be started via griffino dev start / 通过 API/Web-UI 启动插件，Dev 插件被拒绝，必须通过 griffino dev start.
func (s *PluginService) StartPlugin(ctx context.Context, pluginID string) (*store.PluginInstance, error) {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if instance.IsDevPlugin {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status == store.StatusRunning {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginAlreadyRunning, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status != store.StatusReady && instance.Status != store.StatusStopped && instance.Status != store.StatusFailed {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStart, map[string]interface{}{
			"ID":     pluginID,
			"Status": instance.Status,
		}))
	}

	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginLoadManifest) + ": " + err.Error())
	}
	slog.Info("checking image whitelist", "plugin", pluginID)
	if err := imagecheck.CheckBootSpec(pkg.BootSpec); err != nil {
		return nil, err
	}

	return s.startCore(ctx, instance, pkg)
}

// StartPluginAsync starts a plugin asynchronously, returning immediately while startCore runs in the background.
// Pre-checks are done synchronously; if they fail, the error is returned synchronously / 异步启动插件，立即返回，后台执行 startCore，前置校验同步完成.
func (s *PluginService) StartPluginAsync(pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status == store.StatusRunning {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginAlreadyRunning, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status == store.StatusPulling || instance.Status == store.StatusStarting {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStart, map[string]interface{}{
			"ID": pluginID, "Status": instance.Status,
		}))
	}
	if instance.Status != store.StatusReady && instance.Status != store.StatusStopped && instance.Status != store.StatusFailed {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStart, map[string]interface{}{
			"ID": pluginID, "Status": instance.Status,
		}))
	}
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginLoadManifest) + ": " + err.Error())
	}
	slog.Info("checking image whitelist", "plugin", pluginID)
	if err := imagecheck.CheckBootSpec(pkg.BootSpec); err != nil {
		return err
	}
	go func() {
		ctx := context.Background()
		if _, err := s.startCore(ctx, instance, pkg); err != nil {
			slog.Error("async plugin start failed", "plugin", pluginID, "error", err)
		}
	}()
	return nil
}

// StopThenStartAsync stops then restarts asynchronously; used for save_and_restart / 异步停止后重启，用于 save_and_restart 场景.
func (s *PluginService) StopThenStartAsync(pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status != store.StatusRunning {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotRunning, map[string]interface{}{
			"ID": pluginID, "Status": instance.Status,
		}))
	}
	go func() {
		ctx := context.Background()
		if err := s.stopCore(ctx, instance); err != nil {
			slog.Error("async stop failed before restart", "plugin", pluginID, "error", err)
			return
		}
		updated, err := s.store.GetPlugin(pluginID)
		if err != nil || updated == nil {
			slog.Error("async restart: failed to reload plugin after stop", "plugin", pluginID)
			return
		}
		pkg, err := manifest.Load(updated.PluginDir)
		if err != nil {
			s.setFailed(pluginID, "restart", err.Error())
			return
		}
		if _, err := s.startCore(ctx, updated, pkg); err != nil {
			slog.Error("async restart failed", "plugin", pluginID, "error", err)
		}
	}()
	return nil
}

// StartDevPlugin starts a Dev plugin via griffino dev start.
// Skips IsDevPlugin checks and image whitelist validation / 通过 griffino dev start 启动 Dev 插件，跳过 IsDevPlugin 拦截和镜像白名单检查.
func (s *PluginService) StartDevPlugin(ctx context.Context, pluginID string) (*store.PluginInstance, error) {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if !instance.IsDevPlugin {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status == store.StatusRunning {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginAlreadyRunning, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status != store.StatusReady && instance.Status != store.StatusStopped && instance.Status != store.StatusFailed {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStart, map[string]interface{}{
			"ID":     pluginID,
			"Status": instance.Status,
		}))
	}

	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginLoadManifest) + ": " + err.Error())
	}
	return s.startCore(ctx, instance, pkg)
}

// StopPlugin stops a plugin via API/Web-UI.
// Dev plugins are rejected — they must be stopped via griffino dev stop / 通过 API/Web-UI 停止插件，Dev 插件被拒绝，必须通过 griffino dev stop.
func (s *PluginService) StopPlugin(ctx context.Context, pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status != store.StatusRunning {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStop, map[string]interface{}{
			"ID":     pluginID,
			"Status": instance.Status,
		}))
	}
	return s.stopCore(ctx, instance)
}

// StopDevPlugin stops a Dev plugin via griffino dev stop / 通过 griffino dev stop 停止 Dev 插件.
func (s *PluginService) StopDevPlugin(ctx context.Context, pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if !instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotDevPlugin, map[string]interface{}{"ID": pluginID}))
	}
	if instance.Status != store.StatusRunning {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginInvalidStateStop, map[string]interface{}{
			"ID":     pluginID,
			"Status": instance.Status,
		}))
	}
	return s.stopCore(ctx, instance)
}

// ReinstallDevPlugin overwrites an already-installed Dev plugin in place (griffino dev install --force).
// If the plugin is running it is stopped first, then re-pointed at newDir while preserving AdminConfig.
// If the new manifest introduces a required field with no default that the existing AdminConfig lacks,
// the plugin drops back to pending_setup so the user re-runs config; otherwise it becomes ready /
// 就地覆盖已安装的 Dev 插件：运行中先停，切到 newDir 并保留 AdminConfig；
// 若新 manifest 引入了缺失的必填项则回到 pending_setup 要求重配，否则置为 ready.
func (s *PluginService) ReinstallDevPlugin(ctx context.Context, pkg *manifest.PluginPackage, newDir string, existing *store.PluginInstance) (*store.PluginInstance, error) {
	if existing == nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": ""}))
	}
	if !existing.IsDevPlugin {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotDevPlugin, map[string]interface{}{"ID": existing.ID}))
	}

	// Stop containers first if running, so the overwrite starts from a clean runtime / 运行中先停掉容器，从干净的运行时状态覆盖
	if existing.Status == store.StatusRunning {
		if err := s.StopDevPlugin(ctx, existing.ID); err != nil {
			return nil, err
		}
		reloaded, err := s.store.GetPlugin(existing.ID)
		if err != nil || reloaded == nil {
			return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginReadStatus))
		}
		existing = reloaded
	}

	// Preserve AdminConfig: a re-install never destructively clears the user's config / 保留 AdminConfig：覆盖安装不破坏性清空用户配置
	adminConfig := existing.AdminConfig
	if adminConfig == nil {
		adminConfig = make(map[string]map[string]string)
	}

	instance := &store.PluginInstance{
		ID:          existing.ID,
		PluginDir:   newDir,
		InstalledAt: existing.InstalledAt,
		IsDevPlugin: true,
		AdminConfig: adminConfig,
		// RuntimeInfo intentionally left nil: stale after stop/overwrite / RuntimeInfo 置空：停止/覆盖后已失效
	}
	// Config compatibility: if the new manifest needs a required field the saved config lacks, force re-config / 配置兼容性：新 manifest 若需要已保存配置缺失的必填项，则强制重新配置
	if missingRequiredConfig(adminConfig, pkg.BootConfig) {
		instance.Status = store.StatusPendingSetup
	} else {
		instance.Status = store.StatusReady
	}

	if err := s.store.SavePlugin(instance); err != nil {
		return nil, err
	}
	return instance, nil
}

// startCore is the core startup flow, shared by Dev and non-Dev plugins / 核心启动流程，Dev/非Dev 共用.
func (s *PluginService) startCore(ctx context.Context, instance *store.PluginInstance, pkg *manifest.PluginPackage) (*store.PluginInstance, error) {
	pluginID := instance.ID

	// startCore starts / startCore 开始
	slog.Info("starting plugin", "plugin", pluginID)
	progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgPluginStarting, map[string]interface{}{"ID": pluginID}))

	// Get system state / 获取系统状态
	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		return nil, err
	}

	// Create RabbitMQ resources / 创建 RabbitMQ 资源
	slog.Info("provisioning RabbitMQ resources", "plugin", pluginID)
	rabbitClient := broker.NewClient(
		s.cfg.RabbitMQ.Host,
		sysState.RabbitMQManagementPort,
		sysState.RabbitMQAdminUser,
		sysState.RabbitMQAdminPassword,
	)
	if err := rabbitClient.HealthCheck(); err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginRabbitMQUnreachable) + ": " + err.Error())
	}

	provisioner := broker.NewProvisioner(rabbitClient, "/")
	provisioner.SetLogger(broker.ProvisionLoggerFunc(func(text string) {
		slog.Info(text, "plugin", pluginID)
		progress.Log(pluginID, text)
	}))

	var resources *broker.PluginResources
	if instance.RuntimeInfo != nil && instance.RuntimeInfo.RabbitMQPassword != "" {
		if err := provisioner.SyncCredentials(
			instance.RuntimeInfo.RabbitMQUser,
			instance.RuntimeInfo.RabbitMQPassword,
			pkg.Manifest,
		); err != nil {
			return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginRabbitMQSync) + ": " + err.Error())
		}
		resources = &broker.PluginResources{
			Username: instance.RuntimeInfo.RabbitMQUser,
			Password: instance.RuntimeInfo.RabbitMQPassword,
			Vhost:    "/",
		}
	} else {
		resources, err = provisioner.Provision(pkg.Manifest)
		if err != nil {
			return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginRabbitMQProvision) + ": " + err.Error())
		}
	}

	// Create Redis ACL user / 创建 Redis ACL 用户
	slog.Info("provisioning Redis ACL user", "plugin", pluginID)
	redisACL := redisacl.NewClient("localhost", sysState.RedisPort, sysState.RedisPassword)
	var redisUser, redisPass string
	if instance.RuntimeInfo != nil && instance.RuntimeInfo.RedisPassword != "" {
		redisUser = instance.RuntimeInfo.RedisUser
		redisPass = instance.RuntimeInfo.RedisPassword
		if err := redisACL.SyncPlugin(ctx, pluginID, redisPass); err != nil {
			slog.Warn("failed to sync Redis ACL", "plugin", pluginID, "error", err)
		}
	} else {
		redisUser, redisPass, err = redisACL.ProvisionPlugin(ctx, pluginID)
		if err != nil {
			return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginRedisProvision) + ": " + err.Error())
		}
	}

	// Create Docker network / 创建 Docker 网络
	slog.Info("creating Docker network", "plugin", pluginID)
	networkName := container.NetworkName(pluginID)
	if _, err := container.CreateNetwork(ctx, s.dockerCli, pluginID); err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNetworkCreate) + ": " + err.Error())
	}

	// Build template resolution context / 构建模板解析上下文
	resolveCtx := &tmpl.ResolveContext{
		System: tmpl.SystemContext{
			RabbitMQ: tmpl.RabbitMQContext{
				Host:     system.RabbitMQContainerName,
				Port:     5672,
				User:     resources.Username,
				Password: resources.Password,
				Vhost:    "/",
			},
			Redis: tmpl.RedisContext{
				Host:     system.RedisContainerName,
				Port:     6379,
				User:     redisUser,
				Password: redisPass,
			},
		},
		Services: make(map[string]tmpl.ServiceContext),
	}
	for serviceID, svcSpec := range pkg.BootSpec.Services {
		portMap := make(map[string]int)
		for _, p := range svcSpec.Ports {
			portMap[p.Name] = p.Internal
		}
		resolveCtx.Services[serviceID] = tmpl.ServiceContext{
			Name:  container.ContainerName(pluginID, serviceID),
			Ports: portMap,
		}
	}

	// Assemble environment variables / 组装环境变量
	slog.Info("building environment variables", "plugin", pluginID)
	envMap, err := env.Build(pkg.BootSpec, instance.AdminConfig, resolveCtx)
	if err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginEnvBuild) + ": " + err.Error())
	}

	// Pull images / 拉取镜像
	slog.Info("pulling images", "plugin", pluginID)
	_ = s.store.UpdateStatus(pluginID, store.StatusPulling)
	if err := container.PullImages(ctx, s.dockerCli, pkg, pluginID, instance.IsDevPlugin); err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		s.setFailed(pluginID, "pull", err.Error())
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginContainerStart) + ": " + err.Error())
	}

	// Start containers / 启动容器
	slog.Info("starting containers", "plugin", pluginID)
	_ = s.store.UpdateStatus(pluginID, store.StatusStarting)
	containers, err := container.StartContainers(ctx, s.dockerCli, pkg, envMap, networkName, pluginID)
	if err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.StopPlugin(ctx, s.dockerCli, pluginID)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		s.setFailed(pluginID, "start", err.Error())
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginContainerStart) + ": " + err.Error())
	}

	// Join containers to the system network / 将容器加入系统网络
	for _, containerName := range containers {
		if err := s.sysMgr.ConnectPluginToSystemNetwork(ctx, containerName); err != nil {
			if !strings.Contains(err.Error(), "already exists in network") {
				slog.Warn("failed to connect container to system network", "container", containerName, "error", err)
			}
		}
	}

	// Save runtime info / 保存运行时信息
	runtimeInfo := &store.RuntimeInfo{
		Containers:       containers,
		Network:          networkName,
		RabbitMQUser:     resources.Username,
		RabbitMQPassword: resources.Password,
		RedisUser:        redisUser,
		RedisPassword:    redisPass,
	}
	if err := s.store.UpdateRuntimeInfo(pluginID, runtimeInfo); err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginRuntimeSave) + ": " + err.Error())
	}
	if err := s.store.UpdateStatus(pluginID, store.StatusRunning); err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginStatusUpdate) + ": " + err.Error())
	}

	instance.Status = store.StatusRunning
	instance.RuntimeInfo = runtimeInfo

	// Start a log stream goroutine for each container / 为每个容器启动日志 stream goroutine
	pluginCtx, pluginCancel := context.WithCancel(s.streamCtx)
	s.streamMu.Lock()
	s.streamCancels[pluginID] = pluginCancel
	s.streamMu.Unlock()
	for serviceID, containerName := range containers {
		go streamPluginLogs(pluginCtx, s.dockerCli, pluginID, serviceID, containerName)
	}

	// startCore done (after containers are started) / startCore 结束（containers 启动完成后）
	slog.Info("plugin started", "plugin", pluginID)
	progress.Success(pluginID, griffinoi18n.T(griffinoi18n.MsgPluginStarted, map[string]interface{}{"ID": pluginID}))

	return instance, nil
}

// setFailed marks the plugin as failed, recording the failure stage and reason / 将插件标记为失败状态，记录失败阶段和原因.
func (s *PluginService) setFailed(pluginID, stage, reason string) {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil || instance == nil {
		return
	}
	instance.Status = store.StatusFailed
	instance.FailStage = stage
	instance.FailReason = reason
	_ = s.store.SavePlugin(instance)
}

// cleanupProvisionedResources rolls back created RabbitMQ and Redis ACL resources.
// Only called on mid-startup failure; normal stop goes through stopCore / 回滚已创建的 RabbitMQ 和 Redis ACL 资源，仅启动中途失败时调用.
func (s *PluginService) cleanupProvisionedResources(
	ctx context.Context,
	pluginID string,
	redisACL *redisacl.Client,
	provisioner *broker.Provisioner,
	resources *broker.PluginResources,
) {
	if err := redisACL.DeletePlugin(ctx, pluginID); err != nil {
		slog.Warn("failed to rollback Redis ACL", "plugin", pluginID, "error", err)
	}
	if resources != nil {
		if err := provisioner.Teardown(resources); err != nil {
			slog.Warn("failed to rollback RabbitMQ resources", "plugin", pluginID, "error", err)
		}
	}
}

// stopCore is the core stop flow, shared by Dev and non-Dev plugins / 核心停止流程，Dev/非Dev 共用.
func (s *PluginService) stopCore(ctx context.Context, instance *store.PluginInstance) error {
	pluginID := instance.ID

	slog.Info("stopping containers", "plugin", pluginID)

	// Stop log collection / 停止日志采集
	s.streamMu.Lock()
	if cancel, ok := s.streamCancels[pluginID]; ok {
		cancel()
		delete(s.streamCancels, pluginID)
	}
	s.streamMu.Unlock()
	logger.ClosePluginLogger(pluginID)

	if err := container.StopPlugin(ctx, s.dockerCli, pluginID); err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginContainerStop) + ": " + err.Error())
	}

	slog.Info("removing Docker network", "plugin", pluginID)
	if err := container.RemoveNetwork(ctx, s.dockerCli, pluginID); err != nil {
		slog.Warn("failed to remove Docker network", "plugin", pluginID, "error", err)
	}

	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		slog.Warn("failed to get system state, skipping resource cleanup", "plugin", pluginID, "error", err)
	} else {
		if instance.RuntimeInfo != nil && instance.RuntimeInfo.RedisUser != "" {
			redisACL := redisacl.NewClient("localhost", sysState.RedisPort, sysState.RedisPassword)
			if err := redisACL.DeletePlugin(ctx, pluginID); err != nil {
				slog.Warn("failed to cleanup Redis ACL user", "plugin", pluginID, "error", err)
			}
		}
		if instance.RuntimeInfo != nil && instance.RuntimeInfo.RabbitMQUser != "" {
			slog.Info("cleaning up RabbitMQ resources", "plugin", pluginID)
			rabbitClient := broker.NewClient(
				s.cfg.RabbitMQ.Host,
				sysState.RabbitMQManagementPort,
				sysState.RabbitMQAdminUser,
				sysState.RabbitMQAdminPassword,
			)
			provisioner := broker.NewProvisioner(rabbitClient, "/")
			if err := provisioner.Teardown(&broker.PluginResources{
				Username: instance.RuntimeInfo.RabbitMQUser,
			}); err != nil {
				slog.Warn("failed to cleanup RabbitMQ resources", "plugin", pluginID, "error", err)
			}
		}
	}

	instance.RuntimeInfo = nil
	instance.Status = store.StatusStopped
	return s.store.SavePlugin(instance)
}

// StopAll is called when the daemon exits.
// Stops containers only; does not clean ACL, does not change status — resumes on next start (except Dev plugins) / daemon 退出时调用，只停容器不清理 ACL 不改状态，下次启动时自动恢复（Dev 插件除外）.
func (s *PluginService) StopAll(ctx context.Context) {
	plugins, err := s.store.ListPlugins()
	if err != nil {
		slog.Error("StopAll: failed to list plugins", "error", err)
		return
	}
	for _, p := range plugins {
		if p.Status != store.StatusRunning {
			continue
		}
		slog.Info("stopping plugin containers", "plugin", p.ID)
		if err := container.StopPluginContainers(ctx, s.dockerCli, p.ID); err != nil {
			slog.Warn("failed to stop plugin containers", "plugin", p.ID, "error", err)
		} else {
			slog.Info("plugin containers stopped", "plugin", p.ID)
		}
		if p.IsDevPlugin {
			_ = s.store.UpdateStatus(p.ID, store.StatusStopped)
			slog.Info("dev plugin marked as stopped, will not auto-recover", "plugin", p.ID)
		}
	}
	s.streamCancel()
}

// UninstallPlugin performs a full uninstall: clean up resources → delete DB record → remove local files and images.
// Dev plugins don't delete local code directories or images (the developer manages them) / 完整卸载流程：清理资源 → 删除 DB → 删除本地文件和镜像，Dev 插件不删代码和镜像.
func (s *PluginService) UninstallPlugin(ctx context.Context, pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}

	// 1. If running, do a full stop first (clean up containers, RabbitMQ, Redis ACL) / 1. 如果正在运行，先走完整 stop 流程
	if instance.Status == store.StatusRunning {
		if err := s.stopCore(ctx, instance); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginStopBeforeUninstall) + ": " + err.Error())
		}
		instance, err = s.store.GetPlugin(pluginID)
		if err != nil || instance == nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginReadStatus) + ": " + err.Error())
		}
	}

	// 2. Delete DB record / 2. 删除 DB 记录
	if err := s.store.DeletePlugin(pluginID); err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginDeleteRecord) + ": " + err.Error())
	}

	// 3. Dev plugins stop here — don't delete code directory or images / 3. Dev 插件到此结束，不删代码目录和镜像
	if instance.IsDevPlugin {
		return nil
	}

	// 4. Read manifest to get image list (before directory deletion) / 4. 先读 manifest 获取镜像列表（目录删除前）
	var imagesToRemove []string
	if instance.PluginDir != "" {
		if pkg, err := manifest.Load(instance.PluginDir); err == nil {
			for _, svcSpec := range pkg.BootSpec.Services {
				if svcSpec.Image != "" {
					imagesToRemove = append(imagesToRemove, svcSpec.Image)
				}
			}
		}
	}

	// 5. Remove the plugin version directory; if the parent dir (~/.griffino/plugins/{id}) becomes empty,
	//    clean it up too. os.Remove only succeeds on empty dirs, so this is a safe no-op when other versions exist / 5. 删除插件版本目录，父目录变空则一并清掉
	if instance.PluginDir != "" {
		if err := os.RemoveAll(instance.PluginDir); err != nil {
			slog.Warn("failed to remove plugin directory", "plugin", pluginID, "error", err)
		} else {
			_ = os.Remove(filepath.Dir(instance.PluginDir))
		}
	}

	// 6. Remove images / 6. 删除镜像
	for _, img := range imagesToRemove {
		if _, err := s.dockerCli.ImageRemove(ctx, img, image.RemoveOptions{
			Force:         false,
			PruneChildren: true,
		}); err != nil {
			slog.Warn("failed to remove image", "plugin", pluginID, "image", img, "error", err)
		}
	}

	return nil
}

// StartPluginFromInstance starts from an already-held instance, avoiding a re-read from DB
// that could cause state inconsistency (used when starting immediately after config save) / 直接使用调用方已持有的 instance 启动，避免重新读 DB 导致状态不一致.
func (s *PluginService) StartPluginFromInstance(instance *store.PluginInstance) error {
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": instance.ID}))
	}
	pkg, err := manifest.Load(instance.PluginDir)
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginLoadManifest) + ": " + err.Error())
	}
	slog.Info("checking image whitelist", "plugin", instance.ID)
	if err := imagecheck.CheckBootSpec(pkg.BootSpec); err != nil {
		return err
	}
	go func() {
		ctx := context.Background()
		if _, err := s.startCore(ctx, instance, pkg); err != nil {
			slog.Error("async plugin start failed", "plugin", instance.ID, "error", err)
		}
	}()
	return nil
}

// UpgradePlugin switches an installed plugin to the version at newDir.
// newDir must already be downloaded and validated (manifest + image whitelist) by the calling handler / 切换已安装插件到 newDir 指向的新版本目录，调用方保证 manifest 校验和白名单检查.
//
// Flow: stop if running → switch directory (preserving AdminConfig) → clean up old dir and images used only by the old version
// → decide whether to auto-restart based on config compatibility and original running state.
// If config is incompatible (new version added a required field with no default that AdminConfig lacks),
// the plugin stays at ready with ConfigDirty=true, waiting for user review — no auto-restart / 运行中先停 → 切目录保留 AdminConfig → 清理旧目录和镜像 → 视配置兼容性决定是否自动重启.
func (s *PluginService) UpgradePlugin(ctx context.Context, pluginID, newDir string, newPkg *manifest.PluginPackage) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": pluginID}))
	}

	oldDir := instance.PluginDir
	priorStatus := instance.Status
	wasRunning := priorStatus == store.StatusRunning

	// 1. If running, do a full stop (containers/RabbitMQ/Redis ACL); stopCore persists as Stopped / 1. 运行中先走完整停止流程，stopCore 落库为 Stopped
	if wasRunning {
		if err := s.stopCore(ctx, instance); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginUpgradeStop) + ": " + err.Error())
		}
		instance, err = s.store.GetPlugin(pluginID)
		if err != nil || instance == nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginReadStatus))
		}
	}

	// 2. Before switching dirs, compute the old image set from the old manifest for later diff cleanup / 2. 切目录前用旧 manifest 算出旧镜像集合，便于差量清理
	oldImages := map[string]struct{}{}
	if oldDir != "" {
		if oldPkg, err := manifest.Load(oldDir); err == nil {
			for _, svc := range oldPkg.BootSpec.Services {
				if svc.Image != "" {
					oldImages[svc.Image] = struct{}{}
				}
			}
		}
	}

	// 3. Config compatibility: does the new version add required fields with no default that the existing AdminConfig lacks? / 3. 配置兼容性：新版本是否新增了必填且无默认值、而 AdminConfig 缺失的配置项
	configCompatible := !missingRequiredConfig(instance.AdminConfig, newPkg.BootConfig)

	// 4. Switch directory, preserve AdminConfig, clear previous failure info / 4. 切目录、保留 AdminConfig、清除上次失败信息
	instance.PluginDir = newDir
	instance.FailReason = ""
	instance.FailStage = ""
	switch {
	case !configCompatible:
		instance.ConfigDirty = true
		instance.Status = store.StatusReady
	case wasRunning:
		// About to restart — persist a stable intermediate state first / 即将重启，先落一个稳定中间态
		instance.ConfigDirty = false
		instance.Status = store.StatusStopped
	default:
		// Not running: preserve original state (failed means needs reconfiguration) / 未运行：尽量保留原状态（failed 视为需重新配置）
		instance.ConfigDirty = false
		if priorStatus == store.StatusFailed {
			instance.Status = store.StatusReady
		} else {
			instance.Status = priorStatus
		}
	}
	if err := s.store.SavePlugin(instance); err != nil {
		// Roll back the directory pointer: keep the old dir, delete the new download dir / 回滚目录指针，保留旧目录，删掉新下载目录
		instance.PluginDir = oldDir
		_ = s.store.SavePlugin(instance)
		if newDir != "" && newDir != oldDir {
			_ = os.RemoveAll(newDir)
		}
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginUpgradeSave) + ": " + err.Error())
	}

	// 5. Clean up old dir and images used only by the old version (shared images are kept) / 5. 清理旧目录与仅旧版本使用的镜像，共用镜像保留
	if oldDir != "" && oldDir != newDir {
		if err := os.RemoveAll(oldDir); err != nil {
			slog.Warn("failed to remove old plugin directory after upgrade", "plugin", pluginID, "dir", oldDir, "error", err)
		}
	}
	for img := range oldImages {
		stillUsed := false
		for _, svc := range newPkg.BootSpec.Services {
			if svc.Image == img {
				stillUsed = true
				break
			}
		}
		if stillUsed {
			continue
		}
		if _, err := s.dockerCli.ImageRemove(ctx, img, image.RemoveOptions{Force: false, PruneChildren: true}); err != nil {
			slog.Warn("failed to remove old image after upgrade", "plugin", pluginID, "image", img, "error", err)
		}
	}

	// 6. If originally running and config is compatible, auto-restart / 6. 原本在运行且配置兼容则自动重启
	if wasRunning && configCompatible {
		if err := s.StartPluginFromInstance(instance); err != nil {
			slog.Error("failed to restart plugin after upgrade", "plugin", pluginID, "error", err)
		}
	}
	return nil
}

// missingRequiredConfig checks whether newCfg has any required field with no default that the existing AdminConfig lacks.
// Any missing field means config is incompatible; user review is needed after upgrade / 判断 newCfg 中是否存在必填且无默认值而 AdminConfig 缺失的配置项
func missingRequiredConfig(adminConfig map[string]map[string]string, newCfg *manifest.BootConfig) bool {
	if newCfg == nil {
		return false
	}
	for _, svc := range newCfg.Services {
		for _, p := range svc.Configs {
			if p.Optional || p.Default != nil {
				continue
			}
			if v, ok := adminConfig[svc.ID][p.Key]; ok && v != "" {
				continue
			}
			return true
		}
	}
	return false
}

// StopThenStartFromInstance stops then restarts asynchronously, using an already-held instance / 异步停止后重启，直接使用已有 instance.
func (s *PluginService) StopThenStartFromInstance(instance *store.PluginInstance) error {
	if instance.IsDevPlugin {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginIsDevPlugin, map[string]interface{}{"ID": instance.ID}))
	}
	pluginID := instance.ID
	go func() {
		ctx := context.Background()
		if err := s.stopCore(ctx, instance); err != nil {
			slog.Error("async stop failed before restart", "plugin", pluginID, "error", err)
			return
		}
		updated, err := s.store.GetPlugin(pluginID)
		if err != nil || updated == nil {
			slog.Error("async restart: failed to reload plugin after stop", "plugin", pluginID)
			return
		}
		pkg, err := manifest.Load(updated.PluginDir)
		if err != nil {
			s.setFailed(pluginID, "restart", err.Error())
			return
		}
		if _, err := s.startCore(ctx, updated, pkg); err != nil {
			slog.Error("async restart failed", "plugin", pluginID, "error", err)
		}
	}()
	return nil
}
