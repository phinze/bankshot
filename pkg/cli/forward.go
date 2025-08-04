package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

var (
	forwardHost       string
	forwardConnection string
)

func newForwardCmd() *cobra.Command {
	cmd := &cobra.Command{
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

			connectionInfo := forwardConnection
			if connectionInfo == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("failed to get hostname: %w", err)
				}
				connectionInfo = hostname
			}

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

	cmd.Flags().StringVarP(&forwardHost, "host", "H", "localhost", "Remote host to forward from")
	cmd.Flags().StringVarP(&forwardConnection, "connection", "c", "", "SSH connection identifier (e.g., hostname used in ssh command)")

	return cmd
}
