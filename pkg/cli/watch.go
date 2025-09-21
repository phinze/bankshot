package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

var monitorInterval int

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch port forward status continuously",
		Long:  `Continuously monitors and displays the status of port forwards.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clearScreen := "\033[2J\033[H"

			for {
				fmt.Print(clearScreen)

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
						fmt.Printf("Bankshot Watch - %s\n", time.Now().Format("15:04:05"))
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

				time.Sleep(time.Duration(monitorInterval) * time.Second)
			}
		},
	}

	cmd.Flags().IntVarP(&monitorInterval, "interval", "i", 2, "Update interval in seconds")

	return cmd
}
