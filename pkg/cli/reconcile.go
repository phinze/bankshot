package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Trigger immediate forward reconciliation",
		Long: `Triggers the daemon to immediately reconcile port forwards by:
- Checking if tracked forwards still have listening ports
- Re-establishing forwards when SSH connections are alive
- Removing forwards for dead SSH connections

This is useful after SSH reconnection to restore forwards without waiting
for the periodic reconciliation cycle.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := protocol.Request{
				ID:   uuid.New().String(),
				Type: protocol.CommandReconcile,
			}

			resp, err := sendRequest(&req)
			if err != nil {
				return err
			}

			if !resp.Success {
				return fmt.Errorf("reconciliation failed: %s", resp.Error)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if msg, ok := result["message"].(string); ok {
				fmt.Println(msg)
			}

			return nil
		},
	}
}
