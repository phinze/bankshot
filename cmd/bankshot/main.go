package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/process"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/version"
	"github.com/spf13/cobra"
)

var (
	socketPath string
	quiet      bool
	verbose    bool
)

var rootCmd = &cobra.Command{
	Use:   "bankshot",
	Short: "Bankshot client - opens URLs and manages port forwards",
	Long: `Bankshot client communicates with the bankshot daemon to:
- Open URLs in your local browser from remote sessions
- Manage SSH port forwards dynamically
- Check daemon status`,
	Version:      version.GetFullVersion(),
	SilenceUsage: true,
}

var openCmd = &cobra.Command{
	Use:   "open [url]",
	Short: "Open a URL in the local browser",
	Long:  `Opens the specified URL in the default browser on the local machine.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]

		// Create open request
		openReq := protocol.OpenRequest{URL: url}
		payload, err := json.Marshal(openReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req := protocol.Request{
			ID:      uuid.New().String(),
			Type:    protocol.CommandOpen,
			Payload: payload,
		}

		resp, err := sendRequest(&req)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("failed to open URL: %s", resp.Error)
		}

		if !quiet {
			fmt.Println("URL opened successfully")
		}
		return nil
	},
}

var (
	forwardHost       string
	forwardConnection string
)

var forwardCmd = &cobra.Command{
	Use:   "forward <remote-port> [local-port]",
	Short: "Request a port forward",
	Long: `Requests the daemon to forward a port from the remote machine to the local machine.
If local-port is not specified, it defaults to the same as remote-port.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var remotePort, localPort int
		if _, err := fmt.Sscanf(args[0], "%d", &remotePort); err != nil {
			return fmt.Errorf("invalid remote port: %s", args[0])
		}

		if len(args) > 1 {
			if _, err := fmt.Sscanf(args[1], "%d", &localPort); err != nil {
				return fmt.Errorf("invalid local port: %s", args[1])
			}
		} else {
			localPort = remotePort
		}

		// Get connection info
		connectionInfo := forwardConnection
		if connectionInfo == "" {
			// Try to use hostname
			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("failed to get hostname: %w", err)
			}
			connectionInfo = hostname
		}

		// Get host
		host := forwardHost
		if host == "" {
			host = "localhost"
		}

		forwardReq := protocol.ForwardRequest{
			RemotePort:     remotePort,
			LocalPort:      localPort,
			Host:           host,
			ConnectionInfo: connectionInfo,
		}

		payload, err := json.Marshal(forwardReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req := protocol.Request{
			ID:      uuid.New().String(),
			Type:    protocol.CommandForward,
			Payload: payload,
		}

		resp, err := sendRequest(&req)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("failed to create forward: %s", resp.Error)
		}

		if !quiet {
			fmt.Printf("Port forward created: %d -> %d\n", remotePort, localPort)
		}
		return nil
	},
}

var (
	unforwardHost       string
	unforwardConnection string
)

var unforwardCmd = &cobra.Command{
	Use:   "unforward <remote-port>",
	Short: "Remove a port forward",
	Long:  `Removes an existing port forward managed by the daemon.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var remotePort int
		if _, err := fmt.Sscanf(args[0], "%d", &remotePort); err != nil {
			return fmt.Errorf("invalid port: %s", args[0])
		}

		// Get connection info
		connectionInfo := unforwardConnection
		if connectionInfo == "" {
			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("failed to get hostname: %w", err)
			}
			connectionInfo = hostname
		}

		// Get host
		host := unforwardHost
		if host == "" {
			host = "localhost"
		}

		unforwardReq := protocol.UnforwardRequest{
			RemotePort:     remotePort,
			Host:           host,
			ConnectionInfo: connectionInfo,
		}

		payload, err := json.Marshal(unforwardReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req := protocol.Request{
			ID:      uuid.New().String(),
			Type:    protocol.CommandUnforward,
			Payload: payload,
		}

		resp, err := sendRequest(&req)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("failed to remove forward: %s", resp.Error)
		}

		if !quiet {
			fmt.Printf("Port forward removed: %d\n", remotePort)
		}
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get daemon status",
	Long:  `Retrieves the current status of the bankshot daemon.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := protocol.Request{
			ID:   uuid.New().String(),
			Type: protocol.CommandStatus,
		}

		resp, err := sendRequest(&req)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("failed to get status: %s", resp.Error)
		}

		var status protocol.StatusResponse
		if err := json.Unmarshal(resp.Data, &status); err != nil {
			return fmt.Errorf("failed to parse status: %w", err)
		}

		fmt.Printf("Daemon Status:\n")
		fmt.Printf("  Version: %s\n", status.Version)
		fmt.Printf("  Uptime: %s\n", status.Uptime)
		fmt.Printf("  Active Forwards: %d\n", status.ActiveForwards)

		if len(status.Connections) > 0 {
			fmt.Printf("\nActive Connections:\n")
			for _, conn := range status.Connections {
				fmt.Printf("  %s: %d forwards (last activity: %s)\n",
					conn.ConnectionInfo, conn.ForwardCount, conn.LastActivity)
			}
		}

		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List active port forwards",
	Long:  `Lists all currently active port forwards managed by the daemon.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := protocol.Request{
			ID:   uuid.New().String(),
			Type: protocol.CommandList,
		}

		resp, err := sendRequest(&req)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("failed to list forwards: %s", resp.Error)
		}

		var list protocol.ListResponse
		if err := json.Unmarshal(resp.Data, &list); err != nil {
			return fmt.Errorf("failed to parse list: %w", err)
		}

		if len(list.Forwards) == 0 {
			fmt.Println("No active port forwards")
			return nil
		}

		fmt.Println("Active Port Forwards:")
		// Group by connection
		byConnection := make(map[string][]protocol.ForwardInfo)
		for _, fw := range list.Forwards {
			byConnection[fw.ConnectionInfo] = append(byConnection[fw.ConnectionInfo], fw)
		}

		for conn, forwards := range byConnection {
			fmt.Printf("\n  Connection: %s\n", conn)
			for _, fw := range forwards {
				fmt.Printf("    %s:%d -> localhost:%d (created: %s)\n",
					fw.Host, fw.RemotePort, fw.LocalPort, fw.CreatedAt)
			}
		}

		return nil
	},
}

var (
	monitorInterval int
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor port forward status continuously",
	Long:  `Continuously monitors and displays the status of port forwards.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Clear screen escape sequence
		clearScreen := "\033[2J\033[H"

		for {
			// Clear screen
			fmt.Print(clearScreen)

			// Get status
			req := protocol.Request{
				ID:   uuid.New().String(),
				Type: protocol.CommandStatus,
			}

			resp, err := sendRequest(&req)
			if err != nil {
				fmt.Printf("Error getting status: %v\n", err)
			} else if !resp.Success {
				fmt.Printf("Failed to get status: %s\n", resp.Error)
			} else {
				var status protocol.StatusResponse
				if err := json.Unmarshal(resp.Data, &status); err == nil {
					fmt.Printf("Bankshot Monitor - %s\n", time.Now().Format("15:04:05"))
					fmt.Printf("════════════════════════════════════════\n")
					fmt.Printf("Daemon Uptime: %s | Active Forwards: %d\n", status.Uptime, status.ActiveForwards)

					if len(status.Connections) > 0 {
						fmt.Printf("\nActive Connections:\n")
						for _, conn := range status.Connections {
							fmt.Printf("  • %s: %d forwards\n", conn.ConnectionInfo, conn.ForwardCount)
						}
					}
				}
			}

			// Get list of forwards
			req = protocol.Request{
				ID:   uuid.New().String(),
				Type: protocol.CommandList,
			}

			resp, err = sendRequest(&req)
			if err == nil && resp.Success {
				var list protocol.ListResponse
				if err := json.Unmarshal(resp.Data, &list); err == nil && len(list.Forwards) > 0 {
					fmt.Printf("\nPort Forwards:\n")
					for _, fw := range list.Forwards {
						fmt.Printf("  • [%s] %s:%d → localhost:%d\n",
							fw.ConnectionInfo, fw.Host, fw.RemotePort, fw.LocalPort)
					}
				}
			}

			fmt.Printf("\nPress Ctrl+C to exit")

			// Sleep for interval
			time.Sleep(time.Duration(monitorInterval) * time.Second)
		}
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display daemon configuration",
	Long:  `Displays the current daemon configuration including socket path and settings.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Bankshot Configuration:")
		fmt.Printf("  Network: %s\n", cfg.Network)
		fmt.Printf("  Address: %s\n", cfg.Address)
		fmt.Printf("  SSH Command: %s\n", cfg.SSHCommand)
		fmt.Printf("  Log Level: %s\n", cfg.LogLevel)

		// Show actual socket path after expansion
		if cfg.Network == "unix" {
			expanded, err := homedir.Expand(cfg.Address)
			if err == nil && expanded != cfg.Address {
				fmt.Printf("  Expanded Path: %s\n", expanded)
			}

			// Check if socket exists
			if _, err := os.Stat(expanded); err == nil {
				fmt.Printf("  Socket Status: Active\n")
			} else if os.IsNotExist(err) {
				fmt.Printf("  Socket Status: Not found (daemon may not be running)\n")
			}
		}

		// Show config file location
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

var (
	wrapConnection      string
	wrapMonitorInterval int
)

var wrapCmd = &cobra.Command{
	Use:   "wrap [flags] -- <command> [args...]",
	Short: "Wrap a command and auto-forward its ports",
	Long: `Wraps a command and automatically forwards any ports it binds via SSH.
The wrapped process will be monitored for port bindings, and those ports
will be automatically forwarded through the bankshot daemon.

Examples:
  bankshot wrap -- npm run dev
  bankshot wrap -- python -m http.server 8080
  bankshot wrap -c myserver -- ./myapp --port 3000`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !quiet {
			fmt.Printf("Starting wrapped process: %s\n", strings.Join(args, " "))
		}

		// Get connection info
		connectionInfo := wrapConnection
		if connectionInfo == "" {
			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("failed to get hostname: %w", err)
			}
			connectionInfo = hostname
		}

		// Create and start process
		pm := process.New(args[0], args[1:])
		if err := pm.Start(); err != nil {
			return fmt.Errorf("failed to start process: %w", err)
		}

		if verbose {
			fmt.Printf("Process started with PID: %d\n", pm.PID())
		}

		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create logger for monitor
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelError, // Only show errors by default
		}))
		if verbose {
			logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}))
		}

		// Start port monitoring
		portMon := monitor.New(pm.PID(), logger)
		if err := portMon.Start(ctx); err != nil {
			return fmt.Errorf("failed to start port monitor: %w", err)
		}

		// Track forwarded ports for cleanup
		forwardedPorts := make(map[int]bool)

		// Handle port events in background
		go func() {
			for event := range portMon.Events() {
				switch event.EventType {
				case monitor.PortOpened:
					if !forwardedPorts[event.Port.Port] {
						// Request port forward from daemon
						req := createForwardRequest(event.Port.Port, event.Port.Port, connectionInfo)
						resp, err := sendRequest(&req)
						if err != nil {
							if !quiet {
								fmt.Fprintf(os.Stderr, "Failed to forward port %d: %v\n", event.Port.Port, err)
							}
						} else if resp.Success {
							forwardedPorts[event.Port.Port] = true
							if !quiet {
								fmt.Printf("Auto-forwarded port %d\n", event.Port.Port)
							}
						}
					}
				case monitor.PortClosed:
					// Port closed events are handled during cleanup
					delete(forwardedPorts, event.Port.Port)
				}
			}
		}()

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		defer signal.Stop(sigChan)

		// Wait for process to complete or signal
		done := make(chan struct{})
		var exitCode int

		go func() {
			code, _ := pm.Wait()
			exitCode = code
			close(done)
		}()

		select {
		case <-done:
			// Process exited normally
		case sig := <-sigChan:
			// Forward signal to process
			if verbose {
				fmt.Printf("Received signal: %s\n", sig)
			}
			pm.Signal(sig)

			// Wait for graceful shutdown
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				pm.Stop(context.Background())
				<-done
			}
		}

		// Cleanup
		cancel() // Stop port monitoring

		if !quiet {
			fmt.Printf("Process exited with code: %d\n", exitCode)
		}

		// Clean up port forwards
		for port := range forwardedPorts {
			// Note: In a production implementation, we might want to
			// remove these forwards, but for now we'll leave them
			// as they might still be useful
			if verbose {
				fmt.Printf("Port %d forward remains active\n", port)
			}
		}

		os.Exit(exitCode)
		return nil
	},
}

func createForwardRequest(remotePort, localPort int, connectionInfo string) protocol.Request {
	forwardReq := protocol.ForwardRequest{
		RemotePort:     remotePort,
		LocalPort:      localPort,
		Host:           "localhost",
		ConnectionInfo: connectionInfo,
	}

	payload, _ := json.Marshal(forwardReq)

	return protocol.Request{
		ID:      uuid.New().String(),
		Type:    protocol.CommandForward,
		Payload: payload,
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", "", "Path to bankshot socket")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	forwardCmd.Flags().StringVarP(&forwardHost, "host", "H", "localhost", "Remote host to forward from")
	forwardCmd.Flags().StringVarP(&forwardConnection, "connection", "c", "", "SSH connection identifier (e.g., hostname used in ssh command)")
	
	unforwardCmd.Flags().StringVarP(&unforwardHost, "host", "H", "localhost", "Remote host")
	unforwardCmd.Flags().StringVarP(&unforwardConnection, "connection", "c", "", "SSH connection identifier")

	monitorCmd.Flags().IntVarP(&monitorInterval, "interval", "i", 2, "Update interval in seconds")

	wrapCmd.Flags().StringVarP(&wrapConnection, "connection", "c", "", "SSH connection identifier")
	wrapCmd.Flags().IntVarP(&wrapMonitorInterval, "poll-interval", "p", 500, "Port monitoring interval in milliseconds")

	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(forwardCmd)
	rootCmd.AddCommand(unforwardCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(monitorCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(wrapCmd)
}

func main() {
	// Check if called as 'open' for compatibility mode
	if filepath.Base(os.Args[0]) == "open" && len(os.Args) >= 2 {
		// Compatibility mode: bankshot open <url>
		os.Args = append([]string{"bankshot", "open"}, os.Args[1:]...)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getSocketPath() (string, error) {
	// If explicitly specified, use that
	if socketPath != "" {
		expanded, err := homedir.Expand(socketPath)
		if err != nil {
			return "", fmt.Errorf("failed to expand socket path: %w", err)
		}
		return expanded, nil
	}

	// Try to load from config
	cfg, err := config.Load("")
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid config: %w", err)
	}

	// For TCP, we need host:port format
	if cfg.Network == "tcp" {
		return cfg.Address, nil
	}

	// For Unix socket, ensure it's expanded
	return cfg.Address, nil
}

func sendRequest(req *protocol.Request) (*protocol.Response, error) {
	// Get socket path
	sockPath, err := getSocketPath()
	if err != nil {
		return nil, err
	}

	// Determine network type
	network := "unix"
	if strings.Contains(sockPath, ":") {
		network = "tcp"
	}

	// Connect to daemon
	conn, err := net.Dial(network, sockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	// Send request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if verbose {
		fmt.Printf("Sending request: %s\n", string(reqData))
	}

	// Add newline to request as daemon expects line-based protocol
	reqData = append(reqData, '\n')

	if _, err := conn.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp protocol.Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if verbose {
		respData, _ := json.Marshal(resp)
		fmt.Printf("Received response: %s\n", string(respData))
	}

	return &resp, nil
}
