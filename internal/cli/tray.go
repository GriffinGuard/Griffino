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

//go:build gui

package cli

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/systray"

	"github.com/GriffinGuard/Griffino/internal/config"
)

// The tray glyph is format-specific: the Windows shell wants an .ico, while the
// macOS menu bar takes a PNG. Both are embedded; trayIconBytes picks one.
//
//go:embed trayicon.png
var trayIconPNG []byte

//go:embed trayicon.ico
var trayIconICO []byte

func trayIconBytes() []byte {
	if runtime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
}

// consoleURL builds the web console URL from the configured listen address,
// falling back to the localhost defaults.
func consoleURL(cfg *config.Config) string {
	host := cfg.Server.ListenHost
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := cfg.Server.ListenPort
	if port == 0 {
		port = 7070
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// runTray supervises a headless `griffino daemon` child process and shows a
// tray / menu-bar icon over it. Keeping the daemon a separate process means the
// tray is a thin GUI shell, decoupled from the daemon's internals (bubbletea UI,
// container orchestration, graceful shutdown).
func runTray(cfg *config.Config, open bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Headless daemon, with the web setup flow (first admin created in-browser).
	daemon := exec.Command(exe, "daemon", "--admin-init=web")
	daemon.Stdout = os.Stdout
	daemon.Stderr = os.Stderr
	if err := daemon.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	url := consoleURL(cfg)

	onReady := func() {
		systray.SetIcon(trayIconBytes())
		systray.SetTooltip("Griffino")
		mOpen := systray.AddMenuItem("Open Griffino Console", "Open the web console in your browser")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit Griffino", "Stop the daemon and exit")

		if open {
			// Give the daemon a moment to bind :7070 before the first open.
			time.AfterFunc(1500*time.Millisecond, func() { openBrowser(url) })
		}

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openBrowser(url)
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	// onExit runs after systray.Quit(): stop the daemon child, gracefully then
	// forcefully.
	onExit := func() {
		if daemon.Process == nil {
			return
		}
		_ = daemon.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = daemon.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = daemon.Process.Kill()
		}
	}

	// systray.Run blocks and drives the platform UI loop; it must own the main
	// goroutine (required on macOS).
	systray.Run(onReady, onExit)
	return nil
}
