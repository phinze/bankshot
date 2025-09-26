package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
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

	// Parse durations
	pollDuration, err := time.ParseDuration(pollInterval)
	if err != nil {
		return fmt.Errorf("invalid poll interval: %w", err)
	}

	graceDuration, err := time.ParseDuration(gracePeriod)
	if err != nil {
		return fmt.Errorf("invalid grace period: %w", err)
	}

	// Parse port ranges from environment
	portRangesJSON := os.Getenv("BANKSHOT_MONITOR_PORT_RANGES")
	var portRanges []monitor.PortRange
	if portRangesJSON != "" {
		if err := json.Unmarshal([]byte(portRangesJSON), &portRanges); err != nil {
			return fmt.Errorf("failed to parse port ranges: %w", err)
		}
	} else {
		// Default port ranges
		portRanges = []monitor.PortRange{
			{Start: 3000, End: 9999},
		}
	}

	// Parse ignore list from environment
	ignoreList := []string{"sshd", "systemd", "ssh-agent"}
	ignoreEnv := os.Getenv("BANKSHOT_MONITOR_IGNORE")
	if ignoreEnv != "" {
		ignoreList = strings.Split(ignoreEnv, ",")
	}

	// Set up logger
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Create daemon client
	daemonClient := &cliDaemonClient{
		logger: logger,
	}

	// Create session monitor
	sessionMonitor, err := monitor.NewSessionMonitor(monitor.SessionConfig{
		SessionID:       sessionID,
		DaemonClient:    daemonClient,
		PortRanges:      portRanges,
		IgnoreProcesses: ignoreList,
		PollInterval:    pollDuration,
		GracePeriod:     graceDuration,
		Logger:          logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create session monitor: %w", err)
	}

	logger.Info("Starting monitor",
		"session", sessionID,
		"pollInterval", pollInterval,
		"gracePeriod", gracePeriod,
		"portRanges", portRanges,
		"ignoreProcesses", ignoreList)

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Monitor received shutdown signal")
		cancel()
	}()

	// Start monitoring
	if err := sessionMonitor.Start(ctx); err != nil {
		return fmt.Errorf("monitor failed: %w", err)
	}

	logger.Info("Monitor shutdown complete")
	return nil
}

// cliDaemonClient implements the DaemonClient interface for CLI
type cliDaemonClient struct {
	logger *slog.Logger
}

func (c *cliDaemonClient) SendRequest(req *protocol.Request) (*protocol.Response, error) {
	return sendRequest(req)
}
