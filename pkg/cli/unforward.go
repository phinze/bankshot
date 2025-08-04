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
	unforwardHost       string
	unforwardConnection string
)

func newUnforwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unforward <remote-port>",
		Short: "Remove a port forward",
		Long:  `Removes an existing port forward managed by the daemon.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var remotePort int
			if _, err := fmt.Sscanf(args[0], "%d", &remotePort); err != nil {
				return fmt.Errorf("invalid port: %s", args[0])
			}

			connectionInfo := unforwardConnection
			if connectionInfo == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("failed to get hostname: %w", err)
				}
				connectionInfo = hostname
			}

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

	cmd.Flags().StringVarP(&unforwardHost, "host", "H", "localhost", "Remote host")
	cmd.Flags().StringVarP(&unforwardConnection, "connection", "c", "", "SSH connection identifier")

	return cmd
}
