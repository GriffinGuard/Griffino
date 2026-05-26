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

package main

import (
	"os"

	"github.com/GriffinGuard/Griffino/internal/cli"
	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "griffino",
	Short: "Griffino - Plugin System Management Tool",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		lang, _ := cmd.Flags().GetString("lang")
		return griffinoi18n.Init(lang)
	},
}

func main() {
	rootCmd.PersistentFlags().String("lang", "", "Language override (e.g. zh_CN, en)")

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		progress.Error("", "Failed to load config: "+err.Error())
		os.Exit(1)
	}

	rootCmd.AddCommand(cli.NewDaemonCmd(cfg))
	rootCmd.AddCommand(cli.NewDevCmd(cfg))
	rootCmd.AddCommand(cli.NewAdminCmd(cfg))

	if err := rootCmd.Execute(); err != nil {
		progress.Error("", err.Error())
		os.Exit(1)
	}
}