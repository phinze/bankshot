package main

import (
	"os"
	"path/filepath"

	"github.com/phinze/bankshot/pkg/cli"
)

func main() {
	// Check if called as 'open' or 'xdg-open' for compatibility mode
	baseName := filepath.Base(os.Args[0])
	if (baseName == "open" || baseName == "xdg-open") && len(os.Args) >= 2 {
		// Compatibility mode: bankshot open <url>
		os.Args = append([]string{"bankshot", "open"}, os.Args[1:]...)
	}

	rootCmd := cli.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
