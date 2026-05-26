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
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/config"
	"github.com/GriffinGuard/Griffino/internal/devdaemon"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/util"
	"github.com/spf13/cobra"
)

func NewDevCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Developer tools (requires daemon)",
		Long: `griffino dev command group for managing local dev plugins.

All dev subcommands require griffino daemon to be running.
Dev plugins have the following restrictions:
  · Can only be started/stopped via griffino dev start / dev stop
  · Not automatically restored after daemon restart
  · Allowed to use images not on the approved allowlist`,
	}
	cmd.AddCommand(newDevInstallCmd(cfg))
	cmd.AddCommand(newDevStartCmd(cfg))
	cmd.AddCommand(newDevStopCmd(cfg))
	cmd.AddCommand(newDevUninstallCmd(cfg))
	return cmd
}

func mustDaemonClient() *devdaemon.Client {
	c := devdaemon.NewClient(config.SocketPath())
	if !c.IsDaemonRunning() {
		slog.Warn("daemon is not running")
		progress.Error("", griffinoi18n.T(griffinoi18n.MsgDevDaemonNotRunning))
		progress.Error("", griffinoi18n.T(griffinoi18n.MsgDevDaemonStartHint))
		os.Exit(1)
	}
	return c
}

func newDevInstallCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "install <plugin-dir>",
		Short: "Install a local dev plugin (skips allowlist, requires daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			absDir, err := filepath.Abs(args[0])
			if err != nil {
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrInvalidPath,
					map[string]interface{}{"Error": err.Error()}))
			}
			if err := util.ValidatePluginDir(absDir); err != nil {
				return err
			}

			c := mustDaemonClient()
			data, err := c.DevInstall(absDir)
			if err != nil {
				slog.Error("dev install failed", "dir", absDir, "error", err)
				return err
			}

			slog.Info("dev plugin installed", "id", data.ID, "version", data.PluginVersion)
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDevInstallSuccess,
				map[string]interface{}{"ID": data.ID, "Name": data.Name, "Version": data.PluginVersion}))
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgDevInstallNextStep,
				map[string]interface{}{"ID": data.ID}))
			return nil
		},
	}
}

func newDevStartCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "start <plugin-id>",
		Short: "Start a dev plugin (requires daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := args[0]
			c := mustDaemonClient()
			data, err := c.DevStart(pluginID)
			if err != nil {
				slog.Error("dev start failed", "plugin", pluginID, "error", err)
				return err
			}

			slog.Info("dev plugin started", "plugin", pluginID)
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDevStartSuccess,
				map[string]interface{}{"ID": pluginID}))
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgStartSuccessNetwork,
				map[string]interface{}{"Network": data.Network}))
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgStartSuccessMQUser,
				map[string]interface{}{"MQUser": data.RabbitMQUser}))
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgStartSuccessContainers))
			for svcID, name := range data.Containers {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgStartSuccessContainer,
					map[string]interface{}{"ServiceID": svcID, "Container": name}))
			}
			return nil
		},
	}
}

func newDevStopCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <plugin-id>",
		Short: "Stop a dev plugin (requires daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := args[0]
			c := mustDaemonClient()
			if err := c.DevStop(pluginID); err != nil {
				slog.Error("dev stop failed", "plugin", pluginID, "error", err)
				return err
			}

			slog.Info("dev plugin stopped", "plugin", pluginID)
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgDevStopSuccess,
				map[string]interface{}{"ID": pluginID}))
			return nil
		},
	}
}

func newDevUninstallCmd(cfg *config.Config) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "uninstall <plugin-id>",
		Short: "Uninstall a dev plugin (requires daemon)",
		Long: `Uninstall a dev plugin: removes DB record and cleans up containers.
Does not delete the local plugin source directory.

If the plugin is running, use -r to force stop before uninstalling:
  griffino dev uninstall -r <plugin-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := args[0]
			c := mustDaemonClient()
			if err := c.DevUninstall(pluginID, force); err != nil {
				slog.Error("dev uninstall failed", "plugin", pluginID, "error", err)
				return err
			}

			slog.Info("dev plugin uninstalled", "plugin", pluginID, "force", force)
			if force {
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDevUninstallForceSuccess,
					map[string]interface{}{"ID": pluginID}))
			} else {
				progress.Success("", griffinoi18n.T(griffinoi18n.MsgDevUninstallSuccess,
					map[string]interface{}{"ID": pluginID}))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "r", false, "Force stop before uninstalling")
	return cmd
}