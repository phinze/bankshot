package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/protocol"
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
		for _, fw := range list.Forwards {
			fmt.Printf("  %s:%d -> localhost:%d (created: %s)\n",
				fw.Host, fw.RemotePort, fw.LocalPort, fw.CreatedAt)
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", "", "Path to bankshot socket")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	forwardCmd.Flags().StringVarP(&forwardHost, "host", "H", "localhost", "Remote host to forward from")
	forwardCmd.Flags().StringVarP(&forwardConnection, "connection", "c", "", "SSH connection identifier (e.g., hostname used in ssh command)")

	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(forwardCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
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
