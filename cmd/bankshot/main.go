package main

import (
	"os"
	"path/filepath"

	"github.com/phinze/bankshot/pkg/cli"
)

func main() {
	// Check if called as 'open' for compatibility mode
	if filepath.Base(os.Args[0]) == "open" && len(os.Args) >= 2 {
		// Compatibility mode: bankshot open <url>
		os.Args = append([]string{"bankshot", "open"}, os.Args[1:]...)
	}

	rootCmd := cli.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
