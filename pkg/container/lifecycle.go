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

package container

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/imagecheck"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

// pullEvent 对应 Docker ImagePull 返回的 JSON stream 每行结构
type pullEvent struct {
	Status         string          `json:"status"`
	ID             string          `json:"id"`
	ProgressDetail *progressDetail `json:"progressDetail,omitempty"`
}

type progressDetail struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

// ContainerName 根据插件ID和服务ID生成容器名称
func ContainerName(pluginID, serviceID string) string {
	safeID := strings.ReplaceAll(pluginID, ".", "_")
	return fmt.Sprintf("griffino_%s_%s", safeID, serviceID)
}

// PullImages 拉取插件所有服务的镜像（跳过本地已有的）。
func PullImages(
    ctx context.Context,
    cli *client.Client,
    pkg *manifest.PluginPackage,
    pluginID string,
) error {
    mainServiceImage := pkg.BootSpec.Services[pkg.BootSpec.MainServiceID].Image
    for serviceID, svcSpec := range pkg.BootSpec.Services {
        if svcSpec.Image == "" {
            continue
        }
        progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerCheckingImage, map[string]interface{}{
            "Image":   svcSpec.Image,
            "Service": serviceID,
        }))
        slog.Info("checking image", "image", svcSpec.Image, "service", serviceID)
        if err := ensureImage(ctx, cli, svcSpec.Image, mainServiceImage, serviceID, pluginID); err != nil {
            return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerImagePrep, map[string]interface{}{
                "Service": serviceID,
            }) + ": " + err.Error())
        }
    }
    return nil
}

// StartContainers 按依赖顺序创建并启动插件的所有容器。
func StartContainers(
	ctx context.Context,
	cli *client.Client,
	pkg *manifest.PluginPackage,
	envMap map[string][]string,
	networkName string,
    pluginID string,
) (map[string]string, error) {
	order, err := topoSort(pkg.BootSpec)
	if err != nil {
		return nil, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerTopoSort) + ": " + err.Error())
	}

	containers := make(map[string]string)

	for _, serviceID := range order {
		svcSpec := pkg.BootSpec.Services[serviceID]
		containerName := ContainerName(pkg.Manifest.ID, serviceID)

		progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerStartingService, map[string]interface{}{
			"Service": serviceID,
		}))
		slog.Info("starting service", "service", serviceID, "container", containerName)

		existingID, existingState, err := findContainer(ctx, cli, containerName)
		if err != nil {
			return containers, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerFindContainer, map[string]interface{}{
				"Container": containerName,
			}) + ": " + err.Error())
		}

		if existingID != "" {
			if existingState == "running" {
				progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerAlreadyRunning, map[string]interface{}{
					"Container": containerName,
				}))
				slog.Info("container already running, skipping", "container", containerName)
				containers[serviceID] = containerName
				continue
			}

			if isGriffinoManaged(ctx, cli, existingID) {
				progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerRestarting, map[string]interface{}{
					"Container": containerName,
				}))
				slog.Info("restarting stopped container", "container", containerName)
				if err := cli.ContainerStart(ctx, existingID, container.StartOptions{}); err != nil {
					return containers, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerRestart, map[string]interface{}{
						"Container": containerName,
					}) + ": " + err.Error())
				}
				containers[serviceID] = containerName
				continue
			}

			return containers, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerNameConflict, map[string]interface{}{
				"Container": containerName,
				"ID":        existingID[:12],
			}))
		}

		var mounts []mount.Mount
		for _, vol := range svcSpec.Volumes {
			volumeName := fmt.Sprintf("griffino_%s_%s",
				strings.ReplaceAll(pkg.Manifest.ID, ".", "_"),
				vol.Name,
			)
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   vol.MountPath,
				ReadOnly: vol.ReadOnly,
			})
		}

		capDrop := []string{"ALL"}
		capAdd := []string{}
		if serviceID != pkg.BootSpec.MainServiceID {
			capAdd = []string{"CHOWN", "SETUID", "SETGID"}
		}

		resp, err := cli.ContainerCreate(
			ctx,
			&container.Config{
				Image: svcSpec.Image,
				Env:   envMap[serviceID],
				Labels: map[string]string{
					"griffino.plugin.id":  pkg.Manifest.ID,
					"griffino.service.id": serviceID,
					"griffino.managed":    "true",
				},
			},
			&container.HostConfig{
				Mounts:      mounts,
				NetworkMode: container.NetworkMode(networkName),
				RestartPolicy: container.RestartPolicy{
					Name: "unless-stopped",
				},
				CapDrop:     capDrop,
				CapAdd:      capAdd,
				Privileged:  false,
				SecurityOpt: []string{
					"no-new-privileges:true",
				},
			},
			nil,
			nil,
			containerName,
		)
		if err != nil {
			return containers, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerCreate, map[string]interface{}{
				"Container": containerName,
			}) + ": " + err.Error())
		}

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return containers, errors.New(griffinoi18n.T(griffinoi18n.ErrContainerStart, map[string]interface{}{
				"Container": containerName,
			}) + ": " + err.Error())
		}

		containers[serviceID] = containerName
	}

	return containers, nil
}

// StartPlugin 拉取镜像并启动所有容器。
func StartPlugin(
    ctx context.Context,
    cli *client.Client,
    pkg *manifest.PluginPackage,
    envMap map[string][]string,
    networkName string,
    pluginID string,   // 新增
) (map[string]string, error) {
    if err := PullImages(ctx, cli, pkg, pluginID); err != nil {
        return nil, err
    }
    return StartContainers(ctx, cli, pkg, envMap, networkName, pluginID)
}

// StopPlugin 停止并删除插件的所有容器
func StopPlugin(ctx context.Context, cli *client.Client, pluginID string,) error {
	filterArgs := filters.NewArgs(
		filters.Arg("label", fmt.Sprintf("griffino.plugin.id=%s", pluginID)),
		filters.Arg("label", "griffino.managed=true"),
	)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerList) + ": " + err.Error())
	}

	for _, c := range containers {
		progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerStopping, map[string]interface{}{
			"Container": c.Names[0],
		}))
		slog.Info("stopping container", "container", c.Names[0])

		timeout := 10
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerStop, map[string]interface{}{
				"Container": c.Names[0],
			}) + ": " + err.Error())
		}
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{}); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerRemove, map[string]interface{}{
				"Container": c.Names[0],
			}) + ": " + err.Error())
		}
	}

	return nil
}

// StopPluginContainers 只停止容器，不删除（daemon 退出时调用，保留容器供下次恢复）
func StopPluginContainers(ctx context.Context, cli *client.Client, pluginID string,) error {
	filterArgs := filters.NewArgs(
		filters.Arg("label", fmt.Sprintf("griffino.plugin.id=%s", pluginID)),
		filters.Arg("label", "griffino.managed=true"),
	)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerList) + ": " + err.Error())
	}

	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerStopping, map[string]interface{}{
			"Container": c.Names[0],
		}))
		slog.Info("stopping container", "container", c.Names[0])
		timeout := 10
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			slog.Warn("failed to stop container", "container", c.Names[0], "error", err)
		}
	}
	return nil
}

// findContainer 查找容器，返回 (id, state, error)
func findContainer(ctx context.Context, cli *client.Client, name string) (string, string, error) {
	list, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "/"+name)),
	})
	if err != nil {
		return "", "", err
	}
	for _, c := range list {
		for _, n := range c.Names {
			if n == "/"+name {
				return c.ID, c.State, nil
			}
		}
	}
	return "", "", nil
}

// isGriffinoManaged 检查容器是否有 griffino.managed=true 标签
func isGriffinoManaged(ctx context.Context, cli *client.Client, containerID string) bool {
	info, _, err := cli.ContainerInspectWithRaw(ctx, containerID, false)
	if err != nil {
		return false
	}
	return info.Config.Labels["griffino.managed"] == "true"
}

// topoSort 对服务按 depends_on 进行拓扑排序，返回启动顺序
func topoSort(bootSpec *manifest.BootSpec) ([]string, error) {
	visited := make(map[string]bool)
	result := []string{}

	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true

		svc, ok := bootSpec.Services[id]
		if !ok {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerServiceNotFound, map[string]interface{}{
				"Service": id,
			}))
		}

		for _, dep := range svc.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}

		result = append(result, id)
		return nil
	}

	if err := visit(bootSpec.MainServiceID); err != nil {
		return nil, err
	}

	for id := range bootSpec.Services {
		if err := visit(id); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// ensureImage 确保镜像存在，本地没有则在校验通过后自动拉取。
func ensureImage(ctx context.Context, cli *client.Client, imageName, mainServiceImage, serviceID, pluginID string) error {
	_, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		slog.Info("image already exists locally", "image", imageName)
		return nil
	}

	allowed, err := imagecheck.IsAllowedToPull(imageName, mainServiceImage)
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerWhitelistCheck) + ": " + err.Error())
	}
	if !allowed {
		if imageName == mainServiceImage {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerImageNotOfficial, map[string]interface{}{
				"Image": imageName,
			}))
		}
		return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerImageNotAllowed, map[string]interface{}{
			"Image": imageName,
		}))
	}

	progress.Log(pluginID, griffinoi18n.T(griffinoi18n.MsgContainerPullingImage, map[string]interface{}{
		"Image": imageName,
	}))
	slog.Info("pulling image", "image", imageName)

	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerPullFailed, map[string]interface{}{
			"Image": imageName,
		}) + ": " + err.Error())
	}
	defer reader.Close()

	progress.PullStart(pluginID, serviceID, imageName)

	decoder := json.NewDecoder(reader)
	for {
		var event pullEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			slog.Warn("failed to decode pull event", "error", err)
			break
		}

		if event.ID == "" {
			continue
		}

		switch event.Status {
		case "Downloading":
			var current, total int64
			if event.ProgressDetail != nil {
				current = event.ProgressDetail.Current
				total = event.ProgressDetail.Total
			}
			progress.PullLayerUpdate(pluginID, serviceID, event.ID, current, total)

		case "Pull complete", "Already exists", "Download complete":
			progress.PullLayerDone(pluginID, serviceID, event.ID)
		}
	}

	progress.PullDone(pluginID, serviceID)
	return nil
}