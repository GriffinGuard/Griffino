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
	"strings"
	"sync"

	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/imagecheck"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/internal/logger"
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
	cfg       *config.Config
	store     *store.Store
	sysMgr    *system.Manager
	dockerCli *client.Client
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
		cfg:          cfg,
		store:        store,
		sysMgr:       sysMgr,
		dockerCli:    dockerCli,
		streamCancels: make(map[string]context.CancelFunc),
		streamCtx:    ctx,
		streamCancel: cancel,
	}
}

// StartPlugin 通过 API/Web-UI 启动插件。
// Dev 插件被拒绝——必须通过 griffino dev start。
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

// StartPluginAsync 异步启动插件，立即返回，后台执行 startCore。
// 前置校验同步完成，校验失败同步返回错误。
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

// StopThenStartAsync 异步停止后重启，用于 save_and_restart 场景。
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

// StartDevPlugin 通过 griffino dev start 启动 Dev 插件。
// 跳过 IsDevPlugin 拦截和镜像白名单检查。
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

// StopPlugin 通过 API/Web-UI 停止插件。
// Dev 插件被拒绝——必须通过 griffino dev stop。
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

// StopDevPlugin 通过 griffino dev stop 停止 Dev 插件。
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

// startCore 核心启动流程，Dev/非Dev 共用。
func (s *PluginService) startCore(ctx context.Context, instance *store.PluginInstance, pkg *manifest.PluginPackage) (*store.PluginInstance, error) {
	pluginID := instance.ID

	// startCore 开始
	slog.Info("starting plugin", "plugin", pluginID)
	progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgPluginStarting, map[string]interface{}{"ID": pluginID}))

	// 获取系统状态
	sysState, err := s.sysMgr.GetSystemState()
	if err != nil {
		return nil, err
	}

	// 创建 RabbitMQ 资源
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

	// 创建 Redis ACL 用户
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

	// 创建 Docker 网络
	slog.Info("creating Docker network", "plugin", pluginID)
	networkName := container.NetworkName(pluginID)
	if _, err := container.CreateNetwork(ctx, s.dockerCli, pluginID); err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNetworkCreate) + ": " + err.Error())
	}

	// 构建模板解析上下文
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

	// 组装环境变量
	slog.Info("building environment variables", "plugin", pluginID)
	envMap, err := env.Build(pkg.BootSpec, instance.AdminConfig, resolveCtx)
	if err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginEnvBuild) + ": " + err.Error())
	}

	// 拉取镜像
	slog.Info("pulling images", "plugin", pluginID)
	_ = s.store.UpdateStatus(pluginID, store.StatusPulling)
	if err := container.PullImages(ctx, s.dockerCli, pkg, pluginID); err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		s.setFailed(pluginID, "pull", err.Error())
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginContainerStart) + ": " + err.Error())
	}

	// 启动容器
	slog.Info("starting containers", "plugin", pluginID)
	_ = s.store.UpdateStatus(pluginID, store.StatusStarting)
	containers, err := container.StartContainers(ctx, s.dockerCli, pkg, envMap, networkName, pluginID)
	if err != nil {
		s.cleanupProvisionedResources(ctx, pluginID, redisACL, provisioner, resources)
		_ = container.StopPlugin(ctx, s.dockerCli, pluginID,)
		_ = container.RemoveNetwork(ctx, s.dockerCli, pluginID)
		s.setFailed(pluginID, "start", err.Error())
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrPluginContainerStart) + ": " + err.Error())
	}

	// 将容器加入系统网络
	for _, containerName := range containers {
		if err := s.sysMgr.ConnectPluginToSystemNetwork(ctx, containerName); err != nil {
			if !strings.Contains(err.Error(), "already exists in network") {
				slog.Warn("failed to connect container to system network", "container", containerName, "error", err)
			}
		}
	}

	// 保存运行时信息
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

	// 为每个容器启动日志 stream goroutine
	pluginCtx, pluginCancel := context.WithCancel(s.streamCtx)
	s.streamMu.Lock()
	s.streamCancels[pluginID] = pluginCancel
	s.streamMu.Unlock()
	for serviceID, containerName := range containers {
		go streamPluginLogs(pluginCtx, s.dockerCli, pluginID, serviceID, containerName)
	}

	// startCore 结束（containers 启动完成后）
	slog.Info("plugin started", "plugin", pluginID)
	progress.Success(pluginID, griffinoi18n.T(griffinoi18n.MsgPluginStarted, map[string]interface{}{"ID": pluginID}))

	return instance, nil
}

// setFailed 将插件标记为失败状态，记录失败阶段和原因。
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

// cleanupProvisionedResources 回滚已创建的 RabbitMQ 和 Redis ACL 资源。
// 仅在启动流程中途失败时调用，正常 stop 走 stopCore。
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

// stopCore 核心停止流程，Dev/非Dev 共用。
func (s *PluginService) stopCore(ctx context.Context, instance *store.PluginInstance) error {
	pluginID := instance.ID

	slog.Info("stopping containers", "plugin", pluginID)

	// 停止日志采集
	s.streamMu.Lock()
	if cancel, ok := s.streamCancels[pluginID]; ok {
		cancel()
		delete(s.streamCancels, pluginID)
	}
	s.streamMu.Unlock()
	logger.ClosePluginLogger(pluginID)

	if err := container.StopPlugin(ctx, s.dockerCli, pluginID,); err != nil {
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

// StopAll daemon 退出时调用。
// 只停容器，不清理 ACL，不改变状态，下次启动时自动恢复（Dev 插件除外）。
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
		if err := container.StopPluginContainers(ctx, s.dockerCli, p.ID,); err != nil {
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

// UninstallPlugin 完整卸载流程：清理资源 → 删除 DB → 删除本地文件和镜像。
// Dev 插件不删除本地代码目录，也不删除镜像（开发者自己管理）。
func (s *PluginService) UninstallPlugin(ctx context.Context, pluginID string) error {
	instance, err := s.store.GetPlugin(pluginID)
	if err != nil {
		return err
	}
	if instance == nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginNotInstalled, map[string]interface{}{"ID": pluginID}))
	}

	// 1. 如果正在运行，先走完整 stop 流程（清理容器、RabbitMQ、Redis ACL）
	if instance.Status == store.StatusRunning {
		if err := s.stopCore(ctx, instance); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginStopBeforeUninstall) + ": " + err.Error())
		}
		instance, err = s.store.GetPlugin(pluginID)
		if err != nil || instance == nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginReadStatus) + ": " + err.Error())
		}
	}

	// 2. 删除 DB 记录
	if err := s.store.DeletePlugin(pluginID); err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrPluginDeleteRecord) + ": " + err.Error())
	}

	// 3. Dev 插件到此结束，不删代码目录和镜像
	if instance.IsDevPlugin {
		return nil
	}

	// 4. 先读 manifest 获取镜像列表（目录删除前）
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

	// 5. 删除插件目录
	if instance.PluginDir != "" {
		if err := os.RemoveAll(instance.PluginDir); err != nil {
			slog.Warn("failed to remove plugin directory", "plugin", pluginID, "error", err)
		}
	}

	// 6. 删除镜像
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

// StartPluginFromInstance 直接使用调用方已持有的 instance 启动，
// 避免重新读 DB 导致状态不一致（用于 config 保存后立即启动的场景）。
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

// StopThenStartFromInstance 异步停止后重启，直接使用已有 instance。
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