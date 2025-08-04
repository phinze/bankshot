package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
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
}
