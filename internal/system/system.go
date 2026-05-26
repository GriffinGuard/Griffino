package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	SystemNetwork         = "griffino_system"
	RabbitMQContainerName = "griffino_system_rabbitmq"
	RedisContainerName    = "griffino_system_redis"
	RabbitMQImage         = "rabbitmq:3-management-alpine"
	RedisImage            = "redis:7-alpine"

	rabbitMQPortStart     = 15672
	rabbitMQMgmtPortStart = 25672
	redisPortStart        = 16379
)

type Manager struct {
	docker *client.Client
	store  *store.Store
}

func NewManager(docker *client.Client, store *store.Store) *Manager {
	return &Manager{docker: docker, store: store}
}

// Shutdown 停止系统级容器（daemon 退出时调用）
func (m *Manager) Shutdown(ctx context.Context) {
	for _, name := range []string{RabbitMQContainerName, RedisContainerName} {
		slog.Info("stopping system container", "name", name)
		progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemStoppingContainer,
			map[string]interface{}{"Name": name}))

		timeout := 10
		if err := m.docker.ContainerStop(ctx, name, dockercontainer.StopOptions{
			Timeout: &timeout,
		}); err != nil {
			slog.Warn("failed to stop system container", "name", name, "error", err)
			progress.Warn("", griffinoi18n.T(griffinoi18n.MsgSystemStopContainerFailed,
				map[string]interface{}{"Name": name, "Error": err}))
		} else {
			slog.Info("system container stopped", "name", name)
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgSystemContainerStopped,
				map[string]interface{}{"Name": name}))
		}
	}
}

// Bootstrap 确保所有系统级基础服务正常运行
func (m *Manager) Bootstrap(ctx context.Context) error {
	slog.Info("checking system network")
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemCheckingNetwork))
	if err := m.ensureSystemNetwork(ctx); err != nil {
		return fmt.Errorf("%s: %w", griffinoi18n.T(griffinoi18n.ErrSystemNetworkInit), err)
	}

	state, err := m.ensureSystemConfig()
	if err != nil {
		return fmt.Errorf("%s: %w", griffinoi18n.T(griffinoi18n.ErrSystemConfigInit), err)
	}

	slog.Info("checking rabbitmq container")
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemCheckingRabbitMQ))
	if err := m.ensureRabbitMQ(ctx, state); err != nil {
		return fmt.Errorf("%s: %w", griffinoi18n.T(griffinoi18n.ErrSystemRabbitMQStart), err)
	}

	slog.Info("checking redis container")
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemCheckingRedis))
	if err := m.ensureRedis(ctx, state); err != nil {
		return fmt.Errorf("%s: %w", griffinoi18n.T(griffinoi18n.ErrSystemRedisStart), err)
	}

	slog.Info("all system services ready")
	progress.Success("", griffinoi18n.T(griffinoi18n.MsgSystemAllServicesReady))
	return nil
}

func (m *Manager) ensureSystemNetwork(ctx context.Context) error {
	networks, err := m.docker.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", SystemNetwork)),
	})
	if err != nil {
		return err
	}
	for _, n := range networks {
		if n.Name == SystemNetwork {
			return nil
		}
	}
	_, err = m.docker.NetworkCreate(ctx, SystemNetwork, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"griffino.managed": "true",
			"griffino.system":  "true",
		},
	})
	return err
}

func (m *Manager) ensureSystemConfig() (*store.SystemState, error) {
	sys, err := m.store.GetSystemState()
	if err != nil {
		return nil, err
	}

	if sys != nil && sys.RabbitMQAdminUser != "" && sys.RabbitMQPort != 0 {
		if sys.RedisPassword == "" {
			redisPassword, err := generateSecret(16)
			if err != nil {
				return nil, err
			}
			sys.RedisPassword = redisPassword
			if err := m.store.SaveSystemState(sys); err != nil {
				return nil, err
			}
			slog.Warn("generated redis password for database upgrade")
			progress.Warn("", griffinoi18n.T(griffinoi18n.MsgSystemRedisPasswordUpgrade))
			progress.Warn("", griffinoi18n.T(griffinoi18n.MsgSystemRedisManualDelete,
				map[string]interface{}{"Name": RedisContainerName}))
		}
		return sys, nil
	}

	mqPassword, err := generateSecret(16)
	if err != nil {
		return nil, err
	}
	redisPassword, err := generateSecret(16)
	if err != nil {
		return nil, err
	}
	mqPort, err := findAvailablePort(rabbitMQPortStart)
	if err != nil {
		return nil, err
	}
	mqMgmtPort, err := findAvailablePort(rabbitMQMgmtPortStart)
	if err != nil {
		return nil, err
	}
	redisPort, err := findAvailablePort(redisPortStart)
	if err != nil {
		return nil, err
	}

	state := &store.SystemState{
		RabbitMQAdminUser:      "griffino_admin",
		RabbitMQAdminPassword:  mqPassword,
		RabbitMQPort:           mqPort,
		RabbitMQManagementPort: mqMgmtPort,
		RedisPort:              redisPort,
		RedisPassword:          redisPassword,
	}
	if err := m.store.SaveSystemState(state); err != nil {
		return nil, err
	}

	slog.Info("system config generated",
		"mq_port", mqPort, "mq_mgmt_port", mqMgmtPort, "redis_port", redisPort)
	progress.Success("", griffinoi18n.T(griffinoi18n.MsgSystemConfigGenerated,
		map[string]interface{}{
			"MQPort":     mqPort,
			"MQMgmtPort": mqMgmtPort,
			"RedisPort":  redisPort,
		}))
	return state, nil
}

func (m *Manager) ensureRabbitMQ(ctx context.Context, state *store.SystemState) error {
	exists, err := m.ensureContainerStarted(ctx, RabbitMQContainerName)
	if err != nil {
		return err
	}
	if exists {
		slog.Info("rabbitmq already running", "port", state.RabbitMQPort)
		progress.Success("", griffinoi18n.T(griffinoi18n.MsgSystemRabbitMQRunning,
			map[string]interface{}{"Port": state.RabbitMQPort}))
		_ = m.ensureInSystemNetwork(ctx, RabbitMQContainerName)
		return nil
	}

	if err := m.pullImageIfNotExists(ctx, RabbitMQImage); err != nil {
		return err
	}

	slog.Info("starting rabbitmq container", "port", state.RabbitMQPort)
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemStartingRabbitMQ,
		map[string]interface{}{"Port": state.RabbitMQPort}))

	resp, err := m.docker.ContainerCreate(ctx,
		&dockercontainer.Config{
			Image: RabbitMQImage,
			Env: []string{
				fmt.Sprintf("RABBITMQ_DEFAULT_USER=%s", state.RabbitMQAdminUser),
				fmt.Sprintf("RABBITMQ_DEFAULT_PASS=%s", state.RabbitMQAdminPassword),
			},
			Labels: map[string]string{
				"griffino.managed": "true",
				"griffino.system":  "true",
			},
		},
		&dockercontainer.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: "griffino_system_rabbitmq_data",
					Target: "/var/lib/rabbitmq",
				},
			},
			PortBindings: nat.PortMap{
				"5672/tcp":  []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", state.RabbitMQPort)}},
				"15672/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", state.RabbitMQManagementPort)}},
			},
			RestartPolicy: dockercontainer.RestartPolicy{Name: "unless-stopped"},
		},
		nil, nil, RabbitMQContainerName,
	)
	if err != nil {
		return err
	}
	if err := m.docker.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		return err
	}
	return m.docker.NetworkConnect(ctx, SystemNetwork, resp.ID, nil)
}

func (m *Manager) ensureRedis(ctx context.Context, state *store.SystemState) error {
	exists, err := m.ensureContainerStarted(ctx, RedisContainerName)
	if err != nil {
		return err
	}
	if exists {
		slog.Info("redis already running", "port", state.RedisPort)
		progress.Success("", griffinoi18n.T(griffinoi18n.MsgSystemRedisRunning,
			map[string]interface{}{"Port": state.RedisPort}))
		_ = m.ensureInSystemNetwork(ctx, RedisContainerName)
		return nil
	}

	if err := m.pullImageIfNotExists(ctx, RedisImage); err != nil {
		return err
	}

	slog.Info("starting redis container", "port", state.RedisPort)
	progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemStartingRedis,
		map[string]interface{}{"Port": state.RedisPort}))

	resp, err := m.docker.ContainerCreate(ctx,
		&dockercontainer.Config{
			Image: RedisImage,
			Cmd: []string{
				"redis-server",
				"--save", "60", "1",
				"--loglevel", "warning",
				"--requirepass", state.RedisPassword,
			},
			Labels: map[string]string{
				"griffino.managed": "true",
				"griffino.system":  "true",
			},
		},
		&dockercontainer.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: "griffino_system_redis_data",
					Target: "/data",
				},
			},
			PortBindings: nat.PortMap{
				"6379/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", state.RedisPort)}},
			},
			RestartPolicy: dockercontainer.RestartPolicy{Name: "unless-stopped"},
		},
		nil, nil, RedisContainerName,
	)
	if err != nil {
		return err
	}
	if err := m.docker.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		return err
	}
	return m.docker.NetworkConnect(ctx, SystemNetwork, resp.ID, nil)
}

func (m *Manager) ConnectPluginToSystemNetwork(ctx context.Context, containerName string) error {
	return m.docker.NetworkConnect(ctx, SystemNetwork, containerName, &network.EndpointSettings{
		Aliases: []string{containerName},
	})
}

func (m *Manager) GetSystemState() (*store.SystemState, error) {
	sys, err := m.store.GetSystemState()
	if err != nil || sys == nil {
		return nil, fmt.Errorf("%s", griffinoi18n.T(griffinoi18n.ErrSystemStateNotInitialized))
	}
	return sys, nil
}

func (m *Manager) pullImageIfNotExists(ctx context.Context, imageName string) error {
	_, _, err := m.docker.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		slog.Info("image already exists locally", "image", imageName)
		return nil
	}

	slog.Info("pulling system image", "image", imageName)
	progress.PullStart("", "system", imageName)

	reader, err := m.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	type pullEvent struct {
		Status         string `json:"status"`
		ID             string `json:"id"`
		ProgressDetail *struct {
			Current int64 `json:"current"`
			Total   int64 `json:"total"`
		} `json:"progressDetail,omitempty"`
	}

	decoder := json.NewDecoder(reader)
	for {
		var event pullEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		if event.ID == "" {
			continue
		}
		switch event.Status {
		case "Downloading":
			var cur, tot int64
			if event.ProgressDetail != nil {
				cur = event.ProgressDetail.Current
				tot = event.ProgressDetail.Total
			}
			progress.PullLayerUpdate("", "system", event.ID, cur, tot)
		case "Pull complete", "Already exists", "Download complete":
			progress.PullLayerDone("", "system", event.ID)
		}
	}

	progress.PullDone("", "system")
	return nil
}

func (m *Manager) ensureInSystemNetwork(ctx context.Context, containerName string) error {
	err := m.docker.NetworkConnect(ctx, SystemNetwork, containerName, &network.EndpointSettings{
		Aliases: []string{containerName},
	})
	if err != nil && !strings.Contains(err.Error(), "already exists in network") {
		slog.Warn("container failed to join system network", "name", containerName, "error", err)
		progress.Warn("", griffinoi18n.T(griffinoi18n.MsgSystemNetworkJoinFailed,
			map[string]interface{}{"Name": containerName, "Error": err}))
	}
	return nil
}

func (m *Manager) ensureContainerStarted(ctx context.Context, name string) (bool, error) {
	containers, err := m.docker.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "/"+name)),
	})
	if err != nil {
		return false, err
	}

	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name {
				if c.Labels["griffino.system"] == "true" {
					if c.State == "running" {
						return true, nil
					}
					slog.Info("restarting stopped system container", "name", name)
					progress.Log("", griffinoi18n.T(griffinoi18n.MsgSystemRestartingContainer,
						map[string]interface{}{"Name": name}))
					if err := m.docker.ContainerStart(ctx, c.ID, dockercontainer.StartOptions{}); err != nil {
						return false, err
					}
					return true, nil
				}
				return false, &ContainerConflictError{Name: name, ID: c.ID[:12]}
			}
		}
	}
	return false, nil
}

func generateSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) effectiveContainerName(stored, defaultName string) string {
	if stored != "" {
		return stored
	}
	return defaultName
}

func (m *Manager) UpdateContainerName(ctx context.Context, oldName, newName string) error {
	sys, err := m.store.GetSystemState()
	if err != nil {
		return err
	}
	switch oldName {
	case RabbitMQContainerName:
		sys.RabbitMQContainerName = newName
	case RedisContainerName:
		sys.RedisContainerName = newName
	}
	return m.store.SaveSystemState(sys)
}