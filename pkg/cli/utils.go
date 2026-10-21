package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/protocol"
)

func getSocketPath() (string, error) {
	if socketPath != "" {
		expanded, err := homedir.Expand(socketPath)
		if err != nil {
			return "", fmt.Errorf("failed to expand socket path: %w", err)
		}
		return expanded, nil
	}

	cfg, err := config.Load("")
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid config: %w", err)
	}

	// On a tailnet host the CLI talks to the local monitor, which vouches
	// for it to the daemon.
	if cfg.Tailnet.Enabled {
		return homedir.Expand(cfg.Tailnet.LocalSocket)
	}

	return cfg.Address, nil
}

func sendRequest(req *protocol.Request) (*protocol.Response, error) {
	sockPath, err := getSocketPath()
	if err != nil {
		return nil, err
	}

	network := "unix"
	if strings.Contains(sockPath, ":") {
		network = "tcp"
	}

	conn, err := net.Dial(network, sockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if verbose {
		fmt.Printf("Sending request: %s\n", string(reqData))
	}

	reqData = append(reqData, '\n')

	if _, err := conn.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

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
