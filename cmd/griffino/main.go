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
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/GriffinGuard/Griffino/internal/cli"
	"github.com/GriffinGuard/Griffino/internal/config"
	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/progress"
	"github.com/spf13/cobra"
)

// version/date are set at build time via -ldflags "-X main.version=... -X main.date=...".
// version defaults to "dev" for source builds; date is empty unless injected by the release build.
var (
	version = "dev"
	date    = ""
)

// versionString renders the version line shown by `griffino -v` / `griffino version`.
//   - Release build (version != "dev"): "griffino <version>", plus "(<date>)" when injected.
//   - Dev build: "griffino dev (built <binary mtime>)" so two local builds can be told apart.
func versionString() string {
	if version != "dev" {
		if date != "" {
			return fmt.Sprintf("griffino %s (%s)", version, date)
		}
		return fmt.Sprintf("griffino %s", version)
	}
	if built := devBuildDate(); built != "" {
		return fmt.Sprintf("griffino dev (built %s)", built)
	}
	return "griffino dev"
}

// devBuildDate reports the binary's build date for source builds: the executable's
// modification time (the literal "build date"), falling back to the VCS commit time
// embedded by the Go toolchain when the executable can't be stat'd.
func devBuildDate() string {
	if exe, err := os.Executable(); err == nil {
		if info, err := os.Stat(exe); err == nil {
			return info.ModTime().Format("2006-01-02 15:04:05")
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" {
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					return t.Local().Format("2006-01-02 15:04:05")
				}
				return s.Value
			}
		}
	}
	return ""
}

var rootCmd = &cobra.Command{
	Use:     "griffino",
	Short:   "Griffino - Plugin System Management Tool",
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		lang, _ := cmd.Flags().GetString("lang")
		return griffinoi18n.Init(lang)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Griffino version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionString())
	},
}

func main() {
	rootCmd.PersistentFlags().String("lang", "", "Language override (e.g. zh_CN, en)")

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		progress.Error("", "Failed to load config: "+err.Error())
		os.Exit(1)
	}

	// `-v` / `--version` outputs the same clean version string as `griffino version`
	// (dev builds include the build date) / `-v`/`--version` 输出与 `griffino version` 一致的干净版本串（dev 下含 build 日期）.
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	// Disable Cobra's auto-generated `completion` command — it shells out a script
	// not meant for direct display / 禁用 Cobra 自动生成的 completion 命令（输出 eval 用脚本，不宜直接展示）.
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(cli.NewDaemonCmd(cfg, version))
	rootCmd.AddCommand(cli.NewDevCmd(cfg))
	rootCmd.AddCommand(cli.NewAdminCmd(cfg))
	rootCmd.AddCommand(cli.NewServiceCmd())
	rootCmd.AddCommand(cli.NewDoctorCmd(cfg))
	rootCmd.AddCommand(cli.NewTrayCmd(cfg))

	if err := rootCmd.Execute(); err != nil {
		progress.Error("", err.Error())
		os.Exit(1)
	}
}
