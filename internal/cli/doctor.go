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

package cli

import (
	"context"
	"time"

	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/pkg/broker"
	"github.com/GriffinGuard/Griffino/pkg/container"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

func NewDoctorCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Docker environment and system dependency status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			allOK := true

			// 1. Docker connectivity
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorCheckDocker))
			dockerCli, err := container.NewDockerClient()
			if err != nil {
				progress.Error("", griffinoi18n.T(griffinoi18n.ErrDoctorDockerFail,
					map[string]interface{}{"Error": err.Error()}))
				progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorHasIssues))
				return nil
			}
			if _, err := dockerCli.Ping(ctx); err != nil {
				progress.Error("", griffinoi18n.T(griffinoi18n.ErrDoctorDockerFail,
					map[string]interface{}{"Error": err.Error()}))
				progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorHasIssues))
				return nil
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorDockerOK))

			// 2. Service installation (info only, does not affect allOK).
			// Use the platform-agnostic serviceStatus() so this compiles on
			// Windows too (newDaemonService is unix-only / kardianos-specific).
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorCheckService))
			if status, err := serviceStatus(); err == nil {
				if status == "not installed" {
					progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorServiceNotInstalled))
				} else {
					progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorServiceInstalled))
				}
			}

			// 3. Read persisted system state for container names and credentials.
			// When daemon is running it holds the BoltDB exclusive lock; store.New
			// will time out. Silently fall back to default container names so the
			// container checks still run without crashing.
			mqContainer := system.RabbitMQContainerName
			rdContainer := system.RedisContainerName
			var mqMgmtPort int
			var mqAdminUser, mqAdminPassword string

			if s, dbErr := store.New(cfg.DatabasePath); dbErr == nil {
				defer s.Close()
				if sysState, err := s.GetSystemState(); err == nil && sysState.RabbitMQPort != 0 {
					if sysState.RabbitMQContainerName != "" {
						mqContainer = sysState.RabbitMQContainerName
					}
					if sysState.RedisContainerName != "" {
						rdContainer = sysState.RedisContainerName
					}
					mqMgmtPort = sysState.RabbitMQManagementPort
					mqAdminUser = sysState.RabbitMQAdminUser
					mqAdminPassword = sysState.RabbitMQAdminPassword
				}
			}

			// 4. RabbitMQ container
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorCheckContainer,
				map[string]interface{}{"Name": mqContainer}))
			if doctorContainerRunning(ctx, dockerCli, mqContainer) {
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorContainerOK,
					map[string]interface{}{"Name": mqContainer}))

				// 5. RabbitMQ internal health via Management API (only when credentials available)
				if mqMgmtPort != 0 {
					progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorCheckRabbitMQHealth))
					mqClient := broker.NewClient("localhost", mqMgmtPort, mqAdminUser, mqAdminPassword)
					if err := mqClient.HealthCheck(); err != nil {
						progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorRabbitMQHealthFail,
							map[string]interface{}{"Error": err.Error()}))
						allOK = false
					} else {
						progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorRabbitMQHealthOK))
					}
				}
			} else {
				progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorContainerFail,
					map[string]interface{}{"Name": mqContainer}))
				allOK = false
			}

			// 6. Redis container
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDoctorCheckContainer,
				map[string]interface{}{"Name": rdContainer}))
			if doctorContainerRunning(ctx, dockerCli, rdContainer) {
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorContainerOK,
					map[string]interface{}{"Name": rdContainer}))
			} else {
				progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorContainerFail,
					map[string]interface{}{"Name": rdContainer}))
				allOK = false
			}

			if allOK {
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDoctorAllOK))
			} else {
				progress.Error("", griffinoi18n.T(griffinoi18n.MsgDoctorHasIssues))
			}
			return nil
		},
	}
}

// doctorContainerRunning checks whether a container with the given name is running.
// ContainerList without All only returns running containers.
func doctorContainerRunning(ctx context.Context, dockerCli *client.Client, name string) bool {
	containers, err := dockerCli.ContainerList(ctx, dockercontainer.ListOptions{
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
