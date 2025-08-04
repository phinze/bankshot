package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
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
}
