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

//go:build !windows

package cli

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
)

// noopProgram satisfies service.Interface. We never run Griffino *through*
// kardianos' service runner — the installed launchd/systemd unit simply execs
// `griffino daemon` — but service.New requires an Interface to build the handle
// used for Install/Start/Stop/Status.
type noopProgram struct{}

func (noopProgram) Start(service.Service) error { return nil }
func (noopProgram) Stop(service.Service) error  { return nil }

// newDaemonService builds a per-user service handle that runs `griffino daemon`
// with the given extra arguments. UserService installs a launchd LaunchAgent
// (~/Library/LaunchAgents) on macOS and a `systemctl --user` unit on Linux, so
// it runs inside the logged-in session where Docker Desktop is reachable.
func newDaemonService(daemonArgs []string) (service.Service, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	cfg := &service.Config{
		Name:        serviceName,
		DisplayName: "Griffino",
		Description: "Griffino plugin orchestration daemon.",
		Executable:  exe,
		Arguments:   append([]string{"daemon"}, daemonArgs...),
		Option:      service.KeyValue{"UserService": true},
	}
	return service.New(noopProgram{}, cfg)
}

// serviceManagedExternally reports whether autostart is owned by the OS/package
// rather than by these subcommands. On Unix the daemon is always managed by the
// kardianos-installed launchd/systemd unit, so this is always false.
func serviceManagedExternally() bool { return false }

func installService(daemonArgs []string) error {
	s, err := newDaemonService(daemonArgs)
	if err != nil {
		return err
	}
	return s.Install()
}

func uninstallService() error {
	// Arguments are irrelevant for uninstall; the unit is found by name.
	s, err := newDaemonService(nil)
	if err != nil {
		return err
	}
	return s.Uninstall()
}

func controlService(action string) error {
	s, err := newDaemonService(nil)
	if err != nil {
		return err
	}
	switch action {
	case "start":
		return s.Start()
	case "stop":
		return s.Stop()
	case "restart":
		return s.Restart()
	default:
		return fmt.Errorf("unknown service action: %s", action)
	}
}

func serviceStatus() (string, error) {
	s, err := newDaemonService(nil)
	if err != nil {
		return "", err
	}
	st, err := s.Status()
	if err != nil {
		if err == service.ErrNotInstalled {
			return "not installed", nil
		}
		return "", err
	}
	switch st {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}
