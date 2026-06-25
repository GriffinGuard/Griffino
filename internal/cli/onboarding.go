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
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/util"
)

// bootstrapAdmin creates the first admin account on a fresh install, with the
// strategy chosen by --admin-init. It is a no-op once any user exists.
//
//   - "cli" (default): create admin with a random password and print it. Best
//     for terminal installs where the operator reads the password.
//   - "web": create no account; the browser setup wizard creates the first
//     admin. Best for GUI installers (.pkg/.msi) where there is no console.
//   - "unattended": no printed password. Uses GRIFFINO_ADMIN_PASSWORD (and
//     optional GRIFFINO_ADMIN_USER) if set; otherwise creates a random,
//     must-change password silently (set one later via `griffino admin
//     reset-password`). Best for automation.
func bootstrapAdmin(s *store.Store, mode string) error {
	hasUser, err := s.HasAnyUser()
	if err != nil {
		return err
	}
	if hasUser {
		return nil
	}

	switch mode {
	case "web":
		slog.Info("no admin account; deferring creation to the web setup wizard")
		progress.Log("", "No admin account yet — create one in the web console at http://localhost:7070")
		return nil

	case "unattended":
		username := envOr("GRIFFINO_ADMIN_USER", "admin")
		if password := os.Getenv("GRIFFINO_ADMIN_PASSWORD"); password != "" {
			if _, err := s.CreateUser(username, password, store.RoleAdmin, false); err != nil {
				return err
			}
			slog.Info("admin account created from environment", "user", username)
			return nil
		}
		if _, err := s.CreateUser(username, util.GenerateRandomPassword(), store.RoleAdmin, true); err != nil {
			return err
		}
		slog.Info("admin account created without a shown password")
		progress.Log("", "Admin created without a shown password. Set one with: griffino admin reset-password")
		return nil

	default: // "cli"
		password := util.GenerateRandomPassword()
		if _, err := s.CreateUser("admin", password, store.RoleAdmin, true); err != nil {
			return err
		}
		slog.Info("admin account created")
		progress.Success("", griffinoi18n.T(griffinoi18n.MsgDaemonAdminCreated,
			map[string]interface{}{"Password": password}))
		return nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// openBrowser best-effort opens a URL in the user's default browser.
func openBrowser(url string) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if err := exec.Command(name, args...).Start(); err != nil {
		slog.Debug("failed to open browser", "error", err)
	}
}
