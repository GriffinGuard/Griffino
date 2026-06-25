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
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/spf13/cobra"
)

// serviceName is the OS-level identifier for the Griffino background service
// (launchd label / systemd unit / Windows scheduled task).
const serviceName = "griffino"

// NewServiceCmd manages Griffino as a per-user background service that starts on
// login and runs `griffino daemon`. Platform specifics live in service_unix.go
// (launchd LaunchAgent / systemd --user via kardianos) and service_windows.go
// (a logon-triggered scheduled task). The per-user model is deliberate: Docker
// Desktop only runs inside a logged-in session, so a pre-login system service
// would have no container runtime to talk to.
func NewServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage Griffino as a background service (autostart on login)",
		Long: "Install, remove, and control the Griffino daemon as a per-user " +
			"background service that starts automatically on login.\n\n" +
			"Griffino orchestrates plugins as Docker containers, so Docker (or a " +
			"compatible runtime) must be installed and running for the daemon to work.",
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceStartCmd(),
		newServiceStopCmd(),
		newServiceRestartCmd(),
		newServiceStatusCmd(),
	)
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register Griffino to start on login",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			var daemonArgs []string
			if webSetup, _ := cmd.Flags().GetBool("web-setup"); webSetup {
				// GUI-installer flow: no admin is created on the CLI; the first
				// admin is created in the browser setup wizard.
				daemonArgs = append(daemonArgs, "--admin-init=web")
			}
			if err := installService(daemonArgs); err != nil {
				return err
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgServiceInstalled))
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgServiceStartHint))
			return nil
		},
	}
	cmd.Flags().Bool("web-setup", false,
		"Create the first admin in the browser setup wizard instead of on the CLI (for GUI installers)")
	return cmd
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Griffino login service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			// Best-effort stop so we don't leave a running daemon behind.
			_ = controlService("stop")
			if err := uninstallService(); err != nil {
				return err
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgServiceUninstalled))
			return nil
		},
	}
}

func newServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Griffino service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			if err := controlService("start"); err != nil {
				return err
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgServiceStarted))
			return nil
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Griffino service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			if err := controlService("stop"); err != nil {
				return err
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgServiceStopped))
			return nil
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the Griffino service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			if err := controlService("restart"); err != nil {
				return err
			}
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgServiceRestarted))
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the Griffino service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceManagedExternally() {
				progress.Log("", griffinoi18n.T(griffinoi18n.MsgServicePackaged))
				return nil
			}
			status, err := serviceStatus()
			if err != nil {
				return err
			}
			progress.Log("", griffinoi18n.T(griffinoi18n.MsgServiceStatus,
				map[string]interface{}{"Status": status}))
			return nil
		},
	}
}
