package cli

import (
	"fmt"
	"os"

	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Display daemon configuration",
		Long:  `Displays the current daemon configuration including socket path and settings.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			fmt.Println("Bankshot Configuration:")
			fmt.Printf("  Network: %s\n", cfg.Network)
			fmt.Printf("  Address: %s\n", cfg.Address)
			fmt.Printf("  SSH Command: %s\n", cfg.SSHCommand)
			fmt.Printf("  Log Level: %s\n", cfg.LogLevel)

			if cfg.Network == "unix" {
				expanded, err := homedir.Expand(cfg.Address)
				if err == nil && expanded != cfg.Address {
					fmt.Printf("  Expanded Path: %s\n", expanded)
				}

				if _, err := os.Stat(expanded); err == nil {
					fmt.Printf("  Socket Status: Active\n")
				} else if os.IsNotExist(err) {
					fmt.Printf("  Socket Status: Not found (daemon may not be running)\n")
				}
			}

			configPaths := []string{
				"~/.config/bankshot/config.yaml",
				"/etc/bankshot/config.yaml",
			}

			fmt.Printf("\nConfig Search Paths:\n")
			for _, path := range configPaths {
				expanded, _ := homedir.Expand(path)
				if _, err := os.Stat(expanded); err == nil {
					fmt.Printf("  %s (found)\n", path)
				} else {
					fmt.Printf("  %s\n", path)
				}
			}

			return nil
		},
	}
}
