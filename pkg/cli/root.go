package cli

import (
	"github.com/phinze/bankshot/version"
	"github.com/spf13/cobra"
)

var (
	socketPath string
	verbose    bool
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "bankshot",
		Short: "Bankshot client - opens URLs and manages port forwards",
		Long: `Bankshot client communicates with the bankshot daemon to:
- Open URLs in your local browser from remote sessions
- Manage SSH port forwards dynamically
- Check daemon status`,
		Version:      version.GetFullVersion(),
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", "", "Path to bankshot socket")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	rootCmd.AddCommand(newOpenCmd())
	rootCmd.AddCommand(newForwardCmd())
	rootCmd.AddCommand(newUnforwardCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newMonitorCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newWrapCmd())

	return rootCmd
}
