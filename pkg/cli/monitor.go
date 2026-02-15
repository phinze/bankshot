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
		Long: `Run the bankshot monitor process. This command is typically called by systemd
on remote servers to automatically detect and forward ports.

For manual control, use systemctl:
  systemctl --user start bankshot-monitor    # Start monitor
  systemctl --user stop bankshot-monitor     # Stop monitor
  systemctl --user status bankshot-monitor   # Check status
  systemctl --user restart bankshot-monitor  # Restart monitor
  journalctl --user -u bankshot-monitor      # View logs`,
	}

	cmd.AddCommand(newMonitorRunCmd())
	cmd.AddCommand(newMonitorReconcileCmd())

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

func newMonitorReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Run a one-shot reconciliation of port forwards",
		Long: `Reconcile queries the laptop daemon for existing forwards and compares
them with ports actually listening on this VM. It then:
- Requests forwards for VM ports that aren't forwarded
- Removes forwards for ports that aren't listening on the VM

This is useful to run after SSH reconnection to restore forwards.

Example SSH config to run on connect:
  Host your-vm
    RemoteCommand bankshot monitor reconcile 2>/dev/null || true
`,
		RunE: runMonitorReconcile,
	}

	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

func runMonitorReconcile(cmd *cobra.Command, args []string) error {
	// Create monitor configuration
	cfg := daemon.Config{
		LogLevel: logLevel,
	}

	// Create monitor instance
	d, err := daemon.NewMonitor(cfg)
	if err != nil {
		return fmt.Errorf("failed to create monitor: %w", err)
	}

	// Run reconciliation
	if err := d.Reconcile(); err != nil {
		return fmt.Errorf("reconciliation failed: %w", err)
	}

	fmt.Println("Reconciliation completed successfully")
	return nil
}
