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
	"github.com/GriffinGuard/Griffino/internal/config"
	"github.com/spf13/cobra"
)

// NewTrayCmd runs Griffino with a system tray / menu-bar icon: it supervises a
// headless `griffino daemon` in the background and offers a menu to open the web
// console or quit. This is the entry point the graphical installers (Windows
// MSI/MSIX, macOS .app) launch by default, so a GUI install "feels like an app":
// a tray icon whose menu opens the WebUI.
//
// Package-manager installs (Homebrew, apt, …) ship the headless CLI and run via
// `griffino daemon`, with no tray. The tray itself is compiled only into GUI
// builds (-tags gui); in headless builds this command reports that and exits.
func NewTrayCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tray",
		Short: "Run Griffino with a system tray icon (GUI builds)",
		Long: "Start the Griffino daemon in the background and show a system tray / " +
			"menu-bar icon whose menu opens the web console. Used by the graphical " +
			"installers; package-manager installs run headless via `griffino daemon`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			open, _ := cmd.Flags().GetBool("open")
			return runTray(cfg, open)
		},
	}
	cmd.Flags().Bool("open", true, "Open the web console in a browser once the daemon is ready")
	return cmd
}
