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

//go:build windows

// Command griffino-startup is the MSIX GUI entry point — both the app's tile
// (Application Executable) and its logon startup task point here.
//
// The packaged manifest can only launch an executable with no arguments, but the
// graphical experience is `griffino tray` (a menu-bar/tray icon that supervises a
// headless daemon and opens the WebUI). This tiny launcher bridges that gap: it
// locates the GUI griffino.exe next to itself inside the package and execs the
// tray. (The tray in turn starts `griffino daemon --admin-init=web`.)
//
// It is built and shipped ONLY inside the MSIX package (see scripts/build-msix.sh)
// and is not part of the GoReleaser build, the MSI, or any other distribution
// channel — so it has no effect on non-Store builds or other platforms.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	griffino := filepath.Join(filepath.Dir(self), "griffino.exe")

	cmd := exec.Command(griffino, "tray")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Run (not just Start) so the host keeps the tray (and its daemon child)
	// alive for the logon session; the tray handles its own shutdown.
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
