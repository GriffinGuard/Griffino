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

//go:build !gui

package cli

import (
	"fmt"

	"github.com/GriffinGuard/Griffino/internal/config"
)

// runTray is the headless-build stub. The system tray is a GUI feature compiled
// only into `-tags gui` builds (shipped by the Windows/macOS graphical
// installers). This binary is headless-only, so direct the user to the daemon.
// Keeping the stub here means the default cross-platform build stays CGO-free
// and never links the tray/Cocoa/dbus dependencies.
func runTray(_ *config.Config, _ bool) error {
	return fmt.Errorf("this build of griffino is headless and has no system tray; " +
		"run `griffino daemon`, or install the GUI package (Windows MSI/MSIX or macOS app)")
}
