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

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/AlecAivazis/survey/v2"
	"github.com/GriffinGuard/Griffino/internal/config"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

func NewAdminCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin tools",
	}
	cmd.AddCommand(newResetPasswordCmd(cfg))
	return cmd
}

func newResetPasswordCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "reset-password",
		Short: "Reset admin password",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.New(cfg.DatabasePath)
			if err != nil {
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrOpenDatabase,
					map[string]interface{}{"Error": err.Error()}))
			}
			defer s.Close()

			user, err := s.GetUserByUsername("admin")
			if err != nil || user == nil {
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrAdminNotFound))
			}

			var newPassword string
			if err := survey.AskOne(&survey.Password{
				Message: griffinoi18n.T(griffinoi18n.MsgAdminPasswordPrompt),
			}, &newPassword); err != nil {
				return err
			}

			if len(newPassword) < 8 {
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrPasswordTooShort))
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
			if err != nil {
				return err
			}

			user.PasswordHash = string(hash)
			user.MustChange = false
			if err := s.UpdateUser(user); err != nil {
				return errors.New(griffinoi18n.T(griffinoi18n.MsgErrUpdatePassword,
					map[string]interface{}{"Error": err.Error()}))
			}

			slog.Info("admin password reset")
			progress.Success("", griffinoi18n.T(griffinoi18n.MsgAdminPasswordReset))
			return nil
		},
	}
}