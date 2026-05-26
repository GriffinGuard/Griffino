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