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
)

// pullEvent corresponds to each line of the JSON stream returned by Docker ImagePull / Docker ImagePull 返回的 JSON stream 每行结构
type pullEvent struct {
	Status         string          `json:"status"`
	ID             string          `json:"id"`
	ProgressDetail *progressDetail `json:"progressDetail,omitempty"`
}

type progressDetail struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

// ContainerName generates a container name from the plugin ID and service ID / 根据插件ID和服务ID生成容器名称
func ContainerName(pluginID, serviceID string) string {
	safeID := strings.ReplaceAll(pluginID, ".", "_")
	return fmt.Sprintf("griffino_%s_%s", safeID, serviceID)
}

// PullImages pulls images for all plugin services (skipping locally available ones).
// When allowUnapproved is true (Dev plugin), image whitelist validation is skipped / 拉取插件所有服务的镜像，跳过本地已有的，Dev 插件跳过白名单校验.
func PullImages(
	ctx context.Context,
	cli DockerAPI,
	pkg *manifest.PluginPackage,
	pluginID string,
	allowUnapproved bool,
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
		if err := ensureImage(ctx, cli, svcSpec.Image, mainServiceImage, serviceID, pluginID, allowUnapproved); err != nil {
			return errors.New(griffinoi18n.T(griffinoi18n.ErrContainerImagePrep, map[string]interface{}{
				"Service": serviceID,
			}) + ": " + err.Error())
		}
	}
	return nil
}

// StartContainers creates and starts all plugin containers in dependency order / 按依赖顺序创建并启动插件的所有容器.
// Platform default container resource caps, applied when a service does not declare
// its own limits, so no plugin runs unbounded / 平台默认容器资源上限，未声明时回退.
const (
	defaultMemoryMB  = 512
	defaultCPUs      = 1.0
	defaultPidsLimit = 512
)

// resolveResources builds the Docker resource limits for a service, preferring the
// manifest-declared caps and falling back to the platform defaults / 解析容器资源上限.
func resolveResources(spec *manifest.ResourceLimits) container.Resources {
	memMB := defaultMemoryMB
	cpus := defaultCPUs
	pids := defaultPidsLimit
	if spec != nil {
		if spec.MemoryMB > 0 {
			memMB = spec.MemoryMB
		}
		if spec.CPUs > 0 {
			cpus = spec.CPUs
		}
		if spec.PidsLimit > 0 {
			pids = spec.PidsLimit
		}
	}
	pidsLimit := int64(pids)
	return container.Resources{
		Memory:    int64(memMB) * 1024 * 1024,
		NanoCPUs:  int64(cpus * 1e9),
		PidsLimit: &pidsLimit,
	}
}

func StartContainers(
	ctx context.Context,
	cli DockerAPI,
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
				Resources:  resolveResources(svcSpec.Resources),
				CapDrop:    capDrop,
				CapAdd:     capAdd,
				Privileged: false,
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

// StartPlugin pulls images and starts all containers / 拉取镜像并启动所有容器.
func StartPlugin(
	ctx context.Context,
	cli DockerAPI,
	pkg *manifest.PluginPackage,
	envMap map[string][]string,
	networkName string,
	pluginID string,
	allowUnapproved bool,
) (map[string]string, error) {
	if err := PullImages(ctx, cli, pkg, pluginID, allowUnapproved); err != nil {
		return nil, err
	}
	return StartContainers(ctx, cli, pkg, envMap, networkName, pluginID)
}

// StopPlugin stops and removes all plugin containers / 停止并删除插件的所有容器
func StopPlugin(ctx context.Context, cli DockerAPI, pluginID string) error {
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

// StopPluginContainers stops containers without removing them (called on daemon exit, preserves containers for next recovery) / 只停止容器不删除，daemon 退出时调用保留容器供下次恢复
func StopPluginContainers(ctx context.Context, cli DockerAPI, pluginID string) error {
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

// findContainer finds a container, returning (id, state, error) / 查找容器，返回 (id, state, error)
func findContainer(ctx context.Context, cli DockerAPI, name string) (string, string, error) {
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

// isGriffinoManaged checks whether a container has the griffino.managed=true label / 检查容器是否有 griffino.managed=true 标签
func isGriffinoManaged(ctx context.Context, cli DockerAPI, containerID string) bool {
	info, _, err := cli.ContainerInspectWithRaw(ctx, containerID, false)
	if err != nil {
		return false
	}
	return info.Config.Labels["griffino.managed"] == "true"
}

// topoSort topologically sorts services by depends_on, returning start order / 对服务按 depends_on 拓扑排序，返回启动顺序
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

// ensureImage ensures an image exists; if not local, auto-pulls after validation.
// When allowUnapproved is true (Dev plugin), image whitelist validation is skipped, allowing any image / 确保镜像存在，本地没有则在校验通过后自动拉取，Dev 插件跳过白名单校验
func ensureImage(ctx context.Context, cli DockerAPI, imageName, mainServiceImage, serviceID, pluginID string, allowUnapproved bool) error {
	_, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		slog.Info("image already exists locally", "image", imageName)
		return nil
	}

	// Dev plugins can use images not on the whitelist, skipping validation and pulling directly / Dev 插件可使用未列入白名单的镜像，跳过校验直接拉取
	if !allowUnapproved {
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
