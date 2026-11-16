package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get daemon status",
		Long:  `Retrieves the current status of the bankshot daemon and monitor if available.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Always check monitor status first (if systemctl is available)
			if err := showMonitorStatus(); err != nil {
				// Don't fail if monitor isn't available, just note it
				if verbose {
					fmt.Fprintf(os.Stderr, "Monitor: %v\n", err)
				}
			}

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

	return cmd
}

const monitorUnit = "bankshot-monitor.service"

// monitorScope reports which systemctl scope owns the monitor unit: the NixOS
// module runs it as a system service, the home-manager module as a user
// service. Look in both rather than assume.
// Falling back to the user scope keeps an absent unit reporting as "not
// running" rather than as an error.
func monitorScope() string {
	for _, scope := range []string{"--user", "--system"} {
		out, err := exec.Command("systemctl", scope, "show", "-p", "LoadState", "--value", monitorUnit).Output()
		if err == nil && strings.TrimSpace(string(out)) == "loaded" {
			return scope
		}
	}
	return "--user"
}

// showMonitorStatus displays the status of the bankshot-monitor systemd service
func showMonitorStatus() error {
	// Check if systemctl exists
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not available")
	}

	scope := monitorScope()

	// Get bankshot-monitor service status
	cmd := exec.Command("systemctl", scope, "is-active", monitorUnit)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	isActive := cmd.Run() == nil
	status := strings.TrimSpace(out.String())

	// Get more detailed status
	cmd = exec.Command("systemctl", scope, "status", monitorUnit, "--no-pager", "-n", "0")
	out.Reset()
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Run() // Ignore error as it returns non-zero for inactive services

	statusOutput := out.String()

	// Parse the output to get key information
	var uptime, memory, cpu string
	lines := strings.Split(statusOutput, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Active:") {
			if strings.Contains(line, "active (running)") {
				// Extract uptime from the Active line
				if idx := strings.Index(line, "since"); idx > 0 {
					uptime = strings.TrimSpace(line[idx+5:])
					if semi := strings.Index(uptime, ";"); semi > 0 {
						uptime = uptime[:semi]
					}
				}
			}
		} else if strings.Contains(line, "Memory:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				memory = parts[1]
			}
		} else if strings.Contains(line, "CPU:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				cpu = parts[1]
			}
		}
	}

	// Display monitor status
	fmt.Printf("Monitor Status:\n")
	if isActive && status == "active" {
		fmt.Printf("  State: \033[32m●\033[0m Running\n")
		if uptime != "" {
			fmt.Printf("  Since: %s\n", uptime)
		}
		if memory != "" {
			fmt.Printf("  Memory: %s\n", memory)
		}
		if cpu != "" {
			fmt.Printf("  CPU: %s\n", cpu)
		}
	} else if status == "inactive" || status == "dead" {
		fmt.Printf("  State: \033[90m○\033[0m Not running\n")
	} else if status == "failed" {
		fmt.Printf("  State: \033[31m×\033[0m Failed\n")
	} else {
		fmt.Printf("  State: \033[33m?\033[0m %s\n", status)
	}

	// Check for any active monitor sessions
	cmd = exec.Command("systemctl", scope, "list-units", "bankshot-monitor@*.service", "--no-legend", "--no-pager")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		monitors := strings.TrimSpace(out.String())
		if monitors != "" {
			monitorLines := strings.Split(monitors, "\n")
			activeMonitors := 0
			for _, line := range monitorLines {
				if strings.Contains(line, "active") || strings.Contains(line, "running") {
					activeMonitors++
				}
			}
			if activeMonitors > 0 {
				fmt.Printf("  Active Monitors: %d\n", activeMonitors)
			}
		}
	}

	fmt.Println() // Empty line separator
	return nil
}
