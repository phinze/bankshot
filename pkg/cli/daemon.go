package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
		Short: "Manage the bankshot daemon",
		Long:  `Start, stop, and manage the bankshot daemon process`,
	}

	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonStatusCmd())
	cmd.AddCommand(newDaemonRestartCmd())
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

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the bankshot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try to use systemctl if available
			if isSystemdAvailable() {
				return runSystemctl("start", "bankshot-daemon")
			}
			// Fall back to manual start
			return fmt.Errorf("systemd not available, use 'bankshot daemon run' to start manually")
		},
	}
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the bankshot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if isSystemdAvailable() {
				return runSystemctl("stop", "bankshot-daemon")
			}
			// Fall back to PID file based stop
			return stopDaemonByPID()
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show bankshot daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if isSystemdAvailable() {
				return runSystemctl("status", "bankshot-daemon")
			}
			// Fall back to checking PID file
			return checkDaemonStatus()
		},
	}
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the bankshot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if isSystemdAvailable() {
				return runSystemctl("restart", "bankshot-daemon")
			}
			// Fall back to stop and start
			if err := stopDaemonByPID(); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}
			return fmt.Errorf("please start daemon manually with 'bankshot daemon run'")
		},
	}
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

func isSystemdAvailable() bool {
	// Check if we're running under systemd
	if os.Getenv("INVOCATION_ID") != "" {
		return true
	}

	// Check if systemctl is available
	_, err := exec.LookPath("systemctl")
	if err != nil {
		return false
	}

	// Check if user systemd is running
	cmd := exec.Command("systemctl", "--user", "is-system-running")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

func runSystemctl(action string, service string) error {
	cmd := exec.Command("systemctl", "--user", action, service)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopDaemonByPID() error {
	// TODO: Implement PID file based stopping
	return fmt.Errorf("PID file based stopping not yet implemented")
}

func checkDaemonStatus() error {
	// TODO: Implement PID file based status checking
	return fmt.Errorf("PID file based status checking not yet implemented")
}