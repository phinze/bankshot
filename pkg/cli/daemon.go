package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/phinze/bankshot/pkg/daemon"
	"github.com/spf13/cobra"
)

var (
	systemdMode bool
	logLevel    string
	pidFile     string
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the bankshotd daemon (used by systemd)",
		Long: `Run the bankshotd daemon process. This command is typically called by systemd
on remote servers to automatically detect and forward ports.

For manual control, use systemctl:
  systemctl --user start bankshotd    # Start daemon
  systemctl --user stop bankshotd     # Stop daemon
  systemctl --user status bankshotd   # Check status
  systemctl --user restart bankshotd  # Restart daemon
  journalctl --user -u bankshotd      # View logs`,
	}

	cmd.AddCommand(newDaemonRunCmd())

	return cmd
}

func newDaemonRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon directly (used by systemd)",
		Long:  `Run the bankshot daemon process directly. This is typically called by systemd.`,
		RunE:  runDaemon,
	}

	cmd.Flags().BoolVar(&systemdMode, "systemd", false, "Run in systemd mode with sd_notify support")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "Path to PID file")

	return cmd
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Create daemon configuration
	cfg := daemon.Config{
		SystemdMode: systemdMode,
		LogLevel:    logLevel,
		PIDFile:     pidFile,
	}

	// Create and initialize daemon
	d, err := daemon.NewWithConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		if verbose {
			fmt.Fprintln(os.Stderr, "Received shutdown signal")
		}
		cancel()
	}()

	// Start the daemon
	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("daemon failed: %w", err)
	}

	return nil
}