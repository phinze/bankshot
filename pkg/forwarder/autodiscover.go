package forwarder

import (
	"bufio"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

// SSHForward represents a discovered SSH port forward
type SSHForward struct {
	PID            int
	LocalPort      int
	RemotePort     int
	RemoteHost     string
	ConnectionInfo string
	SocketPath     string
}

// DiscoverActiveForwards finds all active SSH port forwards on the system
func DiscoverActiveForwards(logger *slog.Logger) ([]SSHForward, error) {
	var forwards []SSHForward

	// Find all SSH processes with control master sockets
	sshProcesses, err := findSSHControlMasterProcesses(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to find SSH processes: %w", err)
	}

	// For each SSH process, find its listening ports
	for _, proc := range sshProcesses {
		processForwards, err := discoverProcessForwards(logger, proc)
		if err != nil {
			logger.Warn("Failed to discover forwards for process",
				"pid", proc.PID,
				"error", err)
			continue
		}
		forwards = append(forwards, processForwards...)
	}

	return forwards, nil
}

type sshProcess struct {
	PID            int
	Command        string
	ConnectionInfo string
	SocketPath     string
}

// findSSHControlMasterProcesses finds all SSH processes that are control masters
func findSSHControlMasterProcesses(logger *slog.Logger) ([]sshProcess, error) {
	var processes []sshProcess

	// Use ps to find SSH processes with [mux] in their command
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Look for SSH mux processes
		if strings.Contains(line, "ssh:") && strings.Contains(line, "[mux]") {
			fields := strings.Fields(line)
			if len(fields) < 11 {
				continue
			}

			pid, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}

			// Extract connection info from the command
			// Format is typically: ssh: /path/to/socket_host_port_user [mux]
			command := strings.Join(fields[10:], " ")
			
			// Parse socket path from command
			socketPath := ""
			if idx := strings.Index(command, "ssh: "); idx >= 0 {
				start := idx + 5
				end := strings.Index(command[start:], " ")
				if end > 0 {
					socketPath = command[start : start+end]
				}
			}

			// Try to extract connection info from socket path
			connectionInfo := extractConnectionInfo(socketPath)

			processes = append(processes, sshProcess{
				PID:            pid,
				Command:        command,
				ConnectionInfo: connectionInfo,
				SocketPath:     socketPath,
			})

			logger.Debug("Found SSH control master process",
				"pid", pid,
				"socketPath", socketPath,
				"connectionInfo", connectionInfo)
		}
	}

	return processes, nil
}

// extractConnectionInfo tries to extract connection info from socket path
func extractConnectionInfo(socketPath string) string {
	// Socket paths often contain the connection info
	// Common patterns: 
	// - /tmp/ssh_mux_hostname_port_user
	// - ~/.ssh/master-user@hostname:port
	
	parts := strings.Split(socketPath, "/")
	if len(parts) == 0 {
		return ""
	}

	filename := parts[len(parts)-1]
	
	// Try to parse ssh_mux_* pattern
	if strings.HasPrefix(filename, "ssh_mux_") {
		components := strings.Split(strings.TrimPrefix(filename, "ssh_mux_"), "_")
		if len(components) >= 1 {
			// Return the hostname part
			return components[0]
		}
	}

	// Try to parse master-* pattern
	if strings.HasPrefix(filename, "master-") {
		return strings.TrimPrefix(filename, "master-")
	}

	// If we can't parse it, return the filename as-is
	return filename
}

// discoverProcessForwards discovers port forwards for a specific SSH process
func discoverProcessForwards(logger *slog.Logger, proc sshProcess) ([]SSHForward, error) {
	var forwards []SSHForward

	// Use lsof to find listening ports for this process
	cmd := exec.Command("lsof", "-p", strconv.Itoa(proc.PID), "-n", "-P")
	output, err := cmd.Output()
	if err != nil {
		// Process might have exited
		return nil, fmt.Errorf("failed to run lsof: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Look for LISTEN lines on localhost
		if !strings.Contains(line, "LISTEN") || !strings.Contains(line, "127.0.0.1") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		// Parse the address field (e.g., "127.0.0.1:8080")
		addrField := fields[8]
		if !strings.HasPrefix(addrField, "127.0.0.1:") {
			continue
		}

		portStr := strings.TrimPrefix(addrField, "127.0.0.1:")
		localPort, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}

		// For discovered forwards, we don't know the remote port initially
		// We'll need to query the SSH connection or make educated guesses
		forward := SSHForward{
			PID:            proc.PID,
			LocalPort:      localPort,
			RemotePort:     localPort, // Assume same port for now
			RemoteHost:     "localhost",
			ConnectionInfo: proc.ConnectionInfo,
			SocketPath:     proc.SocketPath,
		}

		forwards = append(forwards, forward)

		logger.Debug("Found port forward",
			"pid", proc.PID,
			"localPort", localPort,
			"connectionInfo", proc.ConnectionInfo)
	}

	return forwards, nil
}

// QuerySSHForwards uses SSH control commands to query active forwards
func QuerySSHForwards(logger *slog.Logger, connectionInfo string) ([]SSHForward, error) {
	// First check if the connection is active
	checkCmd := exec.Command("ssh", "-O", "check", connectionInfo)
	if err := checkCmd.Run(); err != nil {
		return nil, fmt.Errorf("no active SSH connection to %s", connectionInfo)
	}

	// Unfortunately, SSH doesn't provide a direct way to list active forwards
	// We would need to enhance this with platform-specific methods or
	// maintain our own state tracking
	
	logger.Debug("SSH connection is active",
		"connectionInfo", connectionInfo)

	return nil, nil
}