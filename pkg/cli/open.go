package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [url]",
		Short: "Open a URL in the local browser",
		Long:  `Opens the specified URL in the default browser on the local machine.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]

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

			if verbose {
				fmt.Println("URL opened successfully")
			}
			return nil
		},
	}
}
