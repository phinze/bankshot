package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	sessionID    string
	pollInterval string
	gracePeriod  string
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor local processes and request port forwards",
		Long: `Monitor all processes owned by the current user and automatically
request port forwards when they bind to ports. This command is typically
started automatically by the shell integration in SSH sessions.`,
		RunE: runMonitor,
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID for this monitor instance")
	cmd.Flags().StringVar(&pollInterval, "poll-interval", "1s", "Polling interval for process discovery")
	cmd.Flags().StringVar(&gracePeriod, "grace-period", "30s", "Grace period before removing forwards")

	return cmd
}

func runMonitor(cmd *cobra.Command, args []string) error {
	// Get session ID from flag or environment
	if sessionID == "" {
		sessionID = os.Getenv("BANKSHOT_SESSION")
	}
	if sessionID == "" {
		return fmt.Errorf("session ID required (use --session or set BANKSHOT_SESSION)")
	}

	// TODO: Implement monitor logic
	// For now, just print a message to show it's working
	fmt.Printf("Starting monitor for session: %s\n", sessionID)
	fmt.Printf("Poll interval: %s\n", pollInterval)
	fmt.Printf("Grace period: %s\n", gracePeriod)

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		if verbose {
			fmt.Fprintln(os.Stderr, "Monitor received shutdown signal")
		}
		cancel()
	}()

	// TODO: Implement the actual monitoring logic
	// This will:
	// 1. Discover all processes owned by the current user
	// 2. Monitor their port bindings
	// 3. Request forwards from the daemon when new ports are detected
	// 4. Clean up forwards when processes exit

	// For now, just wait for context cancellation
	<-ctx.Done()

	fmt.Println("Monitor shutting down")
	return nil
}