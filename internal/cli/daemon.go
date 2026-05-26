package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/GriffinGuard/Griffino/internal/api"
	"github.com/GriffinGuard/Griffino/internal/config"
	"github.com/GriffinGuard/Griffino/internal/devdaemon"
	"github.com/GriffinGuard/Griffino/internal/health"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/logger"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/router"
	"github.com/GriffinGuard/Griffino/internal/service"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/system"
	"github.com/GriffinGuard/Griffino/internal/util"
	"github.com/GriffinGuard/Griffino/pkg/broker"
	"github.com/GriffinGuard/Griffino/pkg/container"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
)

func checkNotRoot() error {
	u, err := user.Current()
	if err != nil {
		return errors.New(griffinoi18n.T(griffinoi18n.MsgErrGetCurrentUser,
			map[string]interface{}{"Error": err.Error()}))
	}
	if u.Uid == "0" {
		return errors.New(griffinoi18n.T(griffinoi18n.MsgErrRootNotAllowed))
	}
	return nil
}

func NewDaemonCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start Griffino daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkNotRoot(); err != nil {
				return err
			}

			// 1. 初始化 logger
			homeDir, _ := os.UserHomeDir()
			logDir := filepath.Join(homeDir, ".griffino", "logs")
			devMode := os.Getenv("GRIFFINO_DEV") == "1"
			if err := logger.Init(logDir, devMode); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
			}

			// 2. 尽早启动 bubbletea，后续所有输出都走 progress
			progress.Init()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			slog.Info("daemon starting")
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDaemonStarting))

			// 3. 打开数据库
			s, err := store.New(cfg.DatabasePath)
			if err != nil {
				slog.Error("failed to open database", "error", err)
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrOpenDatabase,
					map[string]interface{}{"Error": err.Error()}))
			}
			defer s.Close()

			// 4. 连接 Docker
			dockerCli, err := container.NewDockerClient()
			if err != nil {
				slog.Error("failed to connect to docker", "error", err)
				return err
			}

			// 5. 启动系统级基础服务（带冲突处理）
			sysMgr := system.NewManager(dockerCli, s)
			if err := bootstrapWithConflictHandling(ctx, dockerCli, sysMgr); err != nil {
				slog.Error("bootstrap failed", "error", err)
				return err
			}

			// 6. 从 store 读取自动生成的系统配置
			sysState, err := sysMgr.GetSystemState()
			if err != nil {
				slog.Error("failed to get system state", "error", err)
				return err
			}

			// 7. 等待 RabbitMQ 就绪
			slog.Info("waiting for system services")
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDaemonWaitingServices))
			if err := waitForRabbitMQ(sysState.RabbitMQManagementPort, sysState.RabbitMQAdminUser, sysState.RabbitMQAdminPassword); err != nil {
				slog.Error("timed out waiting for rabbitmq", "error", err)
				return fmt.Errorf("timed out waiting for RabbitMQ: %w", err)
			}

			pluginSvc := service.NewPluginService(cfg, s, sysMgr, dockerCli)

			// 8. 启动 dev socket server
			devSrv := devdaemon.NewServer(config.SocketPath(), s, pluginSvc)
			go func() {
				if err := devSrv.Start(ctx); err != nil {
					slog.Error("devdaemon failed to start", "error", err)
				}
			}()

			// 9. 更新运行时 config
			cfg.RabbitMQ.Host = "localhost"
			cfg.RabbitMQ.ContainerHost = system.RabbitMQContainerName
			cfg.RabbitMQ.Port = sysState.RabbitMQPort
			cfg.RabbitMQ.ManagementPort = sysState.RabbitMQManagementPort
			cfg.RabbitMQ.AdminUser = sysState.RabbitMQAdminUser
			cfg.RabbitMQ.AdminPassword = sysState.RabbitMQAdminPassword

			// 10. 声明系统级共享 Exchange
			brokerClient := broker.NewClient(
				"localhost",
				sysState.RabbitMQManagementPort,
				sysState.RabbitMQAdminUser,
				sysState.RabbitMQAdminPassword,
			)
			if err := broker.Bootstrap(brokerClient, "/"); err != nil {
				slog.Error("broker bootstrap failed", "error", err)
				return fmt.Errorf("broker bootstrap failed: %w", err)
			}

			// 11. 初始化 Task 调度器 + 启动消息路由
			amqpURL := fmt.Sprintf("amqp://%s:%s@localhost:%d/",
				sysState.RabbitMQAdminUser,
				sysState.RabbitMQAdminPassword,
				sysState.RabbitMQPort,
			)
			redisAddr := fmt.Sprintf("localhost:%d", sysState.RedisPort)

			schedulerConn, err := amqp.Dial(amqpURL)
			if err != nil {
				slog.Error("failed to connect to RabbitMQ for scheduler", "error", err)
				return fmt.Errorf("scheduler amqp dial failed: %w", err)
			}
			defer schedulerConn.Close()

			rdb := redis.NewClient(&redis.Options{
				Addr:     redisAddr,
				Password: sysState.RedisPassword,
			})
			defer rdb.Close()

			bpStore := taskscheduler.NewBlueprintStore(s.DB())
			taskStore := taskscheduler.NewTaskStore(rdb)
			scheduler, err := taskscheduler.NewScheduler(bpStore, taskStore, schedulerConn)
			if err != nil {
				slog.Error("failed to create scheduler", "error", err)
				return fmt.Errorf("scheduler init failed: %w", err)
			}
			defer scheduler.Stop()

			r := router.New(redisAddr, sysState.RedisPassword, scheduler)
			if err := r.Start(amqpURL); err != nil {
				slog.Error("router failed to start", "error", err)
			} else {
				defer r.Stop()
			}

			mqContainer := sysState.RabbitMQContainerName
			if mqContainer == "" {
				mqContainer = system.RabbitMQContainerName
			}
			rdContainer := sysState.RedisContainerName
			if rdContainer == "" {
				rdContainer = system.RedisContainerName
			}

			slog.Info("daemon started",
				"rabbitmq", fmt.Sprintf("%s:127.0.0.1:%d", mqContainer, sysState.RabbitMQPort),
				"redis", fmt.Sprintf("%s:127.0.0.1:%d", rdContainer, sysState.RedisPort),
			)
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDaemonStarted, map[string]interface{}{
				"RabbitMQ": fmt.Sprintf("%s (127.0.0.1:%d)", mqContainer, sysState.RabbitMQPort),
				"Redis":    fmt.Sprintf("%s (127.0.0.1:%d)", rdContainer, sysState.RedisPort),
			}))

			// 12. 启动健康检查器
			checker := health.NewChecker(dockerCli, s, scheduler)
			checker.Start(ctx)

			// 13. 重置中断状态 + 恢复上次运行中的插件
			restoreCtx, restoreCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer restoreCancel()

			plugins, err := s.ListPlugins()
			if err == nil {
				for _, p := range plugins {
					// 重置 pulling/starting 状态（daemon 重启导致中断）
					if p.Status == store.StatusPulling || p.Status == store.StatusStarting {
						slog.Warn("resetting interrupted plugin status",
							"plugin", p.ID, "status", p.Status)
						progress.Warn("", griffinoi18n.T(griffinoi18n.MsgPluginInterrupted,
							map[string]interface{}{"ID": p.ID, "Stage": string(p.Status)}))
						_ = s.UpdateStatus(p.ID, store.StatusFailed)
						continue
					}

					if p.Status != store.StatusRunning || p.IsDevPlugin {
						continue
					}

					slog.Info("restoring plugin", "plugin", p.ID)
					progress.Log("", griffinoi18n.T(griffinoi18n.MsgPluginRestoring,
						map[string]interface{}{"ID": p.ID}))
					_ = s.UpdateStatus(p.ID, store.StatusStopped)

					// 异步恢复，不阻塞 daemon 启动
					pluginID := p.ID
					go func() {
						if err := pluginSvc.StartPluginAsync(pluginID); err != nil {
							slog.Error("failed to restore plugin", "plugin", pluginID, "error", err)
							progress.Error("", griffinoi18n.T(griffinoi18n.MsgPluginRestoreFailed,
								map[string]interface{}{"ID": pluginID, "Error": err.Error()}))
							_ = s.UpdateStatus(pluginID, store.StatusFailed)
						} else {
							slog.Info("plugin restore scheduled", "plugin", pluginID)
							progress.Log("", griffinoi18n.T(griffinoi18n.MsgPluginRestored,
								map[string]interface{}{"ID": pluginID}))
						}
					}()
				}
			}
			_ = restoreCtx

			// 14. 首次启动时自动创建 admin 账号
			hasUser, _ := s.HasAnyUser()
			if !hasUser {
				password := util.GenerateRandomPassword()
				if _, err := s.CreateUser("admin", password, store.RoleAdmin, true); err != nil {
					slog.Error("failed to create admin user", "error", err)
					return err
				}
				slog.Info("admin user created")
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDaemonAdminCreated,
					map[string]interface{}{"Password": password}))
			}

			// 15. 启动 API 服务器
			apiServer := api.NewServer(cfg, s, sysMgr, dockerCli, pluginSvc, r)
			go func() {
				if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
					slog.Error("API server exited unexpectedly", "error", err)
				}
			}()

			// 16. 监听退出信号，优雅关闭
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit

			slog.Info("daemon shutting down")
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDaemonStopping))
			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer shutdownCancel()
			apiServer.Shutdown(shutdownCtx)
			pluginSvc.StopAll(shutdownCtx)
			sysMgr.Shutdown(shutdownCtx)

			slog.Info("daemon stopped")
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDaemonStopped))

			// 最后才退出 bubbletea，确保上面所有输出都能渲染
			progress.Shutdown()
			return nil
		},
	}

	return cmd
}

func bootstrapWithConflictHandling(ctx context.Context, dockerCli *client.Client, sysMgr *system.Manager) error {
	for {
		err := sysMgr.Bootstrap(ctx)
		if err == nil {
			return nil
		}
		var conflictErr *system.ContainerConflictError
		if !errors.As(err, &conflictErr) {
			return err
		}
		newName, resolveErr := resolveContainerConflict(ctx, dockerCli, sysMgr, conflictErr)
		if resolveErr != nil {
			return resolveErr
		}
		if newName != conflictErr.Name {
			if err := sysMgr.UpdateContainerName(ctx, conflictErr.Name, newName); err != nil {
				return err
			}
		}
	}
}

func resolveContainerConflict(
	ctx context.Context,
	dockerCli *client.Client,
	sysMgr *system.Manager,
	conflictErr *system.ContainerConflictError,
) (string, error) {
	// 容器冲突处理在 bubbletea 启动前可能触发，用 fmt 直接输出
	fmt.Printf("\n⚠️  Container name conflict: %s\n", conflictErr.Error())

	var choice string
	prompt := &survey.Select{
		Message: "How would you like to resolve this?",
		Options: []string{
			"Stop and remove the conflicting container, use default name",
			"Use a custom name for Griffino system containers",
			"Exit",
		},
	}
	if err := survey.AskOne(prompt, &choice); err != nil {
		return "", err
	}

	switch choice {
	case "Stop and remove the conflicting container, use default name":
		timeout := 10
		_ = dockerCli.ContainerStop(ctx, conflictErr.ID, dockercontainer.StopOptions{Timeout: &timeout})
		if err := dockerCli.ContainerRemove(ctx, conflictErr.ID, dockercontainer.RemoveOptions{}); err != nil {
			return "", errors.New(griffinoi18n.T(griffinoi18n.MsgErrRemoveContainer,
				map[string]interface{}{"Error": err.Error()}))
		}
		slog.Info("removed conflicting container", "id", conflictErr.ID)
		fmt.Printf("✓ Removed conflicting container (ID: %s)\n", conflictErr.ID)
		return conflictErr.Name, nil

	case "Use a custom name for Griffino system containers":
		var newName string
		namePrompt := &survey.Input{
			Message: fmt.Sprintf("Enter new container name (current default: %s):", conflictErr.Name),
		}
		if err := survey.AskOne(namePrompt, &newName); err != nil {
			return "", err
		}
		if newName == "" {
			return "", errors.New(griffinoi18n.T(griffinoi18n.MsgErrContainerNameEmpty))
		}
		slog.Info("using custom container name", "name", newName)
		fmt.Printf("✓ Will use custom container name: %s\n", newName)
		return newName, nil

	default:
		os.Exit(0)
		return "", nil
	}
}

func waitForRabbitMQ(mgmtPort int, user, password string) error {
	c := broker.NewClient("localhost", mgmtPort, user, password)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.HealthCheck(); err == nil {
			slog.Info("RabbitMQ is ready")
			return nil
		}
		slog.Info("waiting for RabbitMQ...")
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after 60s")
}