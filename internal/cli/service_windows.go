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

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the daemon must run inside the logged-in user's session (that is
// where Docker Desktop lives), so we register a logon-triggered Scheduled Task
// rather than a classic LocalSystem Windows Service, which would start before
// login with no container runtime to reach.
const taskName = serviceName

// appmodelErrorNoPackage is the Win32 status (APPMODEL_ERROR_NO_PACKAGE)
// returned by GetCurrentPackageFullName when the process has no package
// identity, i.e. it is running unpackaged (the MSI/zip builds).
const appmodelErrorNoPackage = 15700

var (
	modkernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentPackageFullName = modkernel32.NewProc("GetCurrentPackageFullName")
)

// isPackaged reports whether griffino.exe is running with MSIX package identity
// (i.e. the Microsoft Store build) rather than as a plain executable. Inside a
// package, runtime schtasks management is both unavailable and unnecessary —
// autostart is declared by the package's windows.startupTask extension and
// toggled in Windows Settings → Startup apps.
func isPackaged() bool {
	var length uint32
	// First probe with a nil buffer: returns APPMODEL_ERROR_NO_PACKAGE when
	// unpackaged, or ERROR_INSUFFICIENT_BUFFER (with the needed length) when
	// packaged. Any result other than "no package" means we have identity.
	r, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
	return r != appmodelErrorNoPackage
}

// serviceManagedExternally reports whether autostart is owned by the OS/package
// rather than by these subcommands. True only for the MSIX/Store build, where
// the scheduled-task model does not apply.
func serviceManagedExternally() bool { return isPackaged() }

func daemonCommand(daemonArgs []string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	cmd := fmt.Sprintf(`"%s" daemon`, exe)
	for _, a := range daemonArgs {
		cmd += " " + a
	}
	return cmd, nil
}

func runSchtasks(args ...string) error {
	out, err := exec.Command("schtasks", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installService(daemonArgs []string) error {
	tr, err := daemonCommand(daemonArgs)
	if err != nil {
		return err
	}
	// ONLOGON trigger, runs in the current user's session at limited integrity.
	return runSchtasks("/Create", "/F", "/SC", "ONLOGON", "/TN", taskName, "/TR", tr, "/RL", "LIMITED")
}

func uninstallService() error {
	return runSchtasks("/Delete", "/F", "/TN", taskName)
}

func controlService(action string) error {
	switch action {
	case "start":
		return runSchtasks("/Run", "/TN", taskName)
	case "stop":
		return runSchtasks("/End", "/TN", taskName)
	case "restart":
		_ = runSchtasks("/End", "/TN", taskName)
		return runSchtasks("/Run", "/TN", taskName)
	default:
		return fmt.Errorf("unknown service action: %s", action)
	}
}

func serviceStatus() (string, error) {
	out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		// schtasks returns non-zero when the task does not exist.
		return "not installed", nil
	}
	s := string(out)
	switch {
	case strings.Contains(s, "Running"):
		return "running", nil
	case strings.Contains(s, "Ready"), strings.Contains(s, "Disabled"):
		return "stopped", nil
	default:
		return "unknown", nil
	}
}
