package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/process"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

var (
	wrapConnection      string
	wrapMonitorInterval int
)

func newWrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <command> [args...]",
		Short: "Wrap a command and auto-forward its ports",
		Long: `Wraps a command and automatically forwards any ports it binds via SSH.
The wrapped process will be monitored for port bindings, and those ports
will be automatically forwarded through the bankshot daemon.

Examples:
  bankshot wrap -- npm run dev
  bankshot wrap -- python -m http.server 8080
  bankshot wrap -c myserver -- ./myapp --port 3000`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				fmt.Printf("Starting wrapped process: %s\n", strings.Join(args, " "))
			}

			connectionInfo := wrapConnection
			if connectionInfo == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("failed to get hostname: %w", err)
				}
				connectionInfo = hostname
			}

			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable path: %w", err)
			}

			execPath, err = filepath.EvalSymlinks(execPath)
			if err != nil {
				return fmt.Errorf("failed to resolve executable path: %w", err)
			}

			extraEnv := map[string]string{
				"BROWSER": fmt.Sprintf("%s open", execPath),
				"DISPLAY": "1",
			}

			pm := process.New(args[0], args[1:], extraEnv)
			if err := pm.Start(); err != nil {
				return fmt.Errorf("failed to start process: %w", err)
			}

			if verbose {
				fmt.Printf("Process started with PID: %d\n", pm.PID())
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))
			if verbose {
				logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				}))
			}

			portMon := monitor.New(pm.PID(), logger)
			if err := portMon.Start(ctx); err != nil {
				return fmt.Errorf("failed to start port monitor: %w", err)
			}

			// Get existing forwards before we start
			existingPorts := make(map[int]bool)
			listReq := protocol.Request{
				ID:   uuid.New().String(),
				Type: protocol.CommandList,
			}
			if resp, err := sendRequest(&listReq); err == nil && resp.Success {
				var list protocol.ListResponse
				if err := json.Unmarshal(resp.Data, &list); err == nil {
					for _, fw := range list.Forwards {
						if fw.ConnectionInfo == connectionInfo {
							existingPorts[fw.RemotePort] = true
						}
					}
				}
			}

			ourForwardedPorts := make(map[int]bool)

			go func() {
				for event := range portMon.Events() {
					switch event.EventType {
					case monitor.PortOpened:
						// Skip if port was already forwarded before wrap started
						if existingPorts[event.Port.Port] {
							if verbose {
								fmt.Printf("Port %d already forwarded, skipping\n", event.Port.Port)
							}
							continue
						}

						// Skip if we already forwarded this port
						if ourForwardedPorts[event.Port.Port] {
							continue
						}

						req := createForwardRequest(event.Port.Port, event.Port.Port, connectionInfo)
						resp, err := sendRequest(&req)
						if err != nil {
							if verbose {
								fmt.Fprintf(os.Stderr, "Failed to forward port %d: %v\n", event.Port.Port, err)
							}
						} else if resp.Success {
							ourForwardedPorts[event.Port.Port] = true
							if verbose {
								fmt.Printf("Auto-forwarded port %d\n", event.Port.Port)
							}
						}
					case monitor.PortClosed:
						// We don't need to track port closes, we'll clean up at the end
					}
				}
			}()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			defer signal.Stop(sigChan)

			done := make(chan struct{})
			var exitCode int

			go func() {
				code, _ := pm.Wait()
				exitCode = code
				close(done)
			}()

			select {
			case <-done:
			case sig := <-sigChan:
				if verbose {
					fmt.Printf("Received signal: %s\n", sig)
				}
				if err := pm.Signal(sig); err != nil {
					if verbose {
						fmt.Printf("Failed to signal process: %v\n", err)
					}
				}

				select {
				case <-done:
				case <-time.After(5 * time.Second):
					if err := pm.Stop(context.Background()); err != nil {
						if verbose {
							fmt.Printf("Failed to stop process: %v\n", err)
						}
					}
					<-done
				}
			}

			cancel()

			if verbose {
				fmt.Printf("Process exited with code: %d\n", exitCode)
			}

			// Unforward only the ports we created
			for port := range ourForwardedPorts {
				unforwardReq := protocol.UnforwardRequest{
					RemotePort:     port,
					Host:           "localhost",
					ConnectionInfo: connectionInfo,
				}

				payload, _ := json.Marshal(unforwardReq)
				req := protocol.Request{
					ID:      uuid.New().String(),
					Type:    protocol.CommandUnforward,
					Payload: payload,
				}

				if resp, err := sendRequest(&req); err == nil && resp.Success {
					if verbose {
						fmt.Printf("Unforwarded port %d\n", port)
					}
				} else if verbose {
					if err != nil {
						fmt.Printf("Failed to unforward port %d: %v\n", port, err)
					} else {
						fmt.Printf("Failed to unforward port %d: %s\n", port, resp.Error)
					}
				}
			}

			os.Exit(exitCode)
			return nil
		},
	}

	cmd.Flags().StringVarP(&wrapConnection, "connection", "c", "", "SSH connection identifier")
	cmd.Flags().IntVarP(&wrapMonitorInterval, "poll-interval", "p", 500, "Port monitoring interval in milliseconds")

	return cmd
}

func createForwardRequest(remotePort, localPort int, connectionInfo string) protocol.Request {
	forwardReq := protocol.ForwardRequest{
		RemotePort:     remotePort,
		LocalPort:      localPort,
		Host:           "localhost",
		ConnectionInfo: connectionInfo,
	}

	payload, _ := json.Marshal(forwardReq)

	return protocol.Request{
		ID:      uuid.New().String(),
		Type:    protocol.CommandForward,
		Payload: payload,
	}
}
