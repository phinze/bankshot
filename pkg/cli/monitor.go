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

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Run the bankshot monitor (used by systemd)",
		Long: `Run the bankshot monitor process. It runs on remote servers, usually under
systemd, and carries this host's requests to the laptop daemon over the
tailnet: opening URLs, paste, the op proxy, the ssh agent, and the ports
bankshot forward and bankshot wrap ask for.

For manual control, use systemctl:
  systemctl --user start bankshot-monitor    # Start monitor
  systemctl --user stop bankshot-monitor     # Stop monitor
  systemctl --user status bankshot-monitor   # Check status
  systemctl --user restart bankshot-monitor  # Restart monitor
  journalctl --user -u bankshot-monitor      # View logs`,
	}

	cmd.AddCommand(newMonitorRunCmd())

	return cmd
}

func newMonitorRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the monitor directly (used by systemd)",
		Long:  `Run the bankshot monitor process directly. This is typically called by systemd.`,
		RunE:  runMonitor,
	}

	cmd.Flags().BoolVar(&systemdMode, "systemd", false, "Run in systemd mode with sd_notify support")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "Path to PID file")

	return cmd
}

func runMonitor(cmd *cobra.Command, args []string) error {
	// Create monitor configuration
	cfg := daemon.Config{
		SystemdMode: systemdMode,
		LogLevel:    logLevel,
		PIDFile:     pidFile,
	}

	// Create and initialize monitor
	d, err := daemon.NewMonitor(cfg)
	if err != nil {
		return fmt.Errorf("failed to create monitor: %w", err)
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

	// Start monitor
	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("monitor failed: %w", err)
	}

	return nil
}
