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

package health

import (
	"context"
	"time"
	"log/slog"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// TaskScheduler 是 taskscheduler 包的抽象，避免循环依赖
type TaskScheduler interface {
	FailTasksByPlugin(pluginID string)
}

type Checker struct {
	docker   *client.Client
	store    *store.Store
	scheduler TaskScheduler  // 可为 nil，未初始化时跳过 Task 通知
	interval time.Duration
}

func NewChecker(docker *client.Client, store *store.Store, scheduler TaskScheduler) *Checker {
	return &Checker{
		docker:    docker,
		store:     store,
		scheduler: scheduler,
		interval:  30 * time.Second,
	}
}

// Start 启动后台健康检查，每 30 秒检查一次所有插件的容器状态
func (c *Checker) Start(ctx context.Context) {
	go func() {
		// 启动时先检查一次
		c.check(ctx)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.check(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Checker) check(ctx context.Context) {
	plugins, err := c.store.ListPlugins()
	if err != nil {
		return
	}

	for _, plugin := range plugins {
		if plugin.Status != store.StatusRunning && plugin.Status != store.StatusFailed {
			continue
		}
		if plugin.RuntimeInfo == nil || len(plugin.RuntimeInfo.Containers) == 0 {
			continue
		}

		allRunning := true
		for _, containerName := range plugin.RuntimeInfo.Containers {
			if !c.isContainerRunning(ctx, containerName) {
				allRunning = false
				break
			}
		}

		switch {
		case allRunning && plugin.Status == store.StatusFailed:
			// 容器已恢复，状态从 failed 更新为 running
			slog.Info("plugin containers recovered, updating status to running",
				"plugin", plugin.ID)
			progress.Log(plugin.ID, griffinoi18n.T(griffinoi18n.MsgHealthPluginRecovered,
				map[string]interface{}{"ID": plugin.ID}))
			instance, err := c.store.GetPlugin(plugin.ID)
			if err == nil && instance != nil {
				instance.Status = store.StatusRunning
				instance.FailStage = ""
				instance.FailReason = ""
				_ = c.store.SavePlugin(instance)
			}

		case !allRunning && plugin.Status == store.StatusRunning:
			// 容器意外挂掉，状态从 running 更新为 failed
			slog.Warn("plugin containers down, marking as failed", "plugin", plugin.ID)
			instance, err := c.store.GetPlugin(plugin.ID)
			if err == nil && instance != nil {
				instance.Status = store.StatusFailed
				instance.FailStage = "restart"
				instance.FailReason = "containers unexpectedly stopped"
				_ = c.store.SavePlugin(instance)
			}
			// 通知 Task 调度器：该插件相关的进行中 Task 全部失败
			if c.scheduler != nil {
				go c.scheduler.FailTasksByPlugin(plugin.ID)
			}
		}
	}
}

func (c *Checker) isContainerRunning(ctx context.Context, name string) bool {
	containers, err := c.docker.ContainerList(ctx, dockercontainer.ListOptions{
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