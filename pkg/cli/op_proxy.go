package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newOpProxyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "op-proxy [-- args...]",
		Short: "Proxy 1Password CLI requests to the local machine",
		Long: `Proxies op CLI invocations over the bankshot socket to the local machine,
where the real op binary can use native app integration (Touch ID, biometric unlock).

All arguments after -- are forwarded as op CLI arguments.`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Strip leading "--" if present
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}

			if len(args) == 0 {
				return fmt.Errorf("no op arguments provided")
			}

			opReq := protocol.OpProxyRequest{Args: args}
			payload, err := json.Marshal(opReq)
			if err != nil {
				return fmt.Errorf("failed to marshal request: %w", err)
			}

			req := protocol.Request{
				ID:      uuid.New().String(),
				Type:    protocol.CommandOpProxy,
				Payload: payload,
			}

			resp, err := sendRequest(&req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bankshot op-proxy: %s\n", err)
				os.Exit(1)
			}

			if !resp.Success {
				fmt.Fprintf(os.Stderr, "bankshot op-proxy: %s\n", resp.Error)
				os.Exit(1)
			}

			var opResp protocol.OpProxyResponse
			if err := json.Unmarshal(resp.Data, &opResp); err != nil {
				fmt.Fprintf(os.Stderr, "bankshot op-proxy: failed to parse response: %s\n", err)
				os.Exit(1)
			}

			if opResp.Stdout != "" {
				fmt.Fprint(os.Stdout, opResp.Stdout)
			}
			if opResp.Stderr != "" {
				fmt.Fprint(os.Stderr, opResp.Stderr)
			}
			os.Exit(opResp.ExitCode)

			return nil // unreachable
		},
	}
}
