package ssh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Connection represents SSH connection information
type Connection struct {
	User         string
	Host         string
	Port         string
	ControlPath  string
	IsSSHSession bool
}

// DetectSSHConnection checks if we're in an SSH session and extracts connection info
func DetectSSHConnection() (*Connection, error) {
	// Check SSH_CONNECTION environment variable
	sshConn := os.Getenv("SSH_CONNECTION")
	if sshConn == "" {
		return nil, fmt.Errorf("not in an SSH session (SSH_CONNECTION not set)")
	}
	
	// Parse SSH_CONNECTION: "client_ip client_port server_ip server_port"
	parts := strings.Fields(sshConn)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid SSH_CONNECTION format")
	}
	
	conn := &Connection{
		IsSSHSession: true,
		Port:         parts[3], // We'll use the server port for matching
	}
	
	// Try to get more info from SSH_CLIENT if available
	if sshClient := os.Getenv("SSH_CLIENT"); sshClient != "" {
		// SSH_CLIENT format: "client_ip client_port server_port"
		clientParts := strings.Fields(sshClient)
		if len(clientParts) >= 3 {
			conn.Port = clientParts[2]
		}
	}
	
	// Try to determine user and host
	if tty := os.Getenv("SSH_TTY"); tty != "" {
		// We have a TTY, good indication of interactive SSH
		// Try to get username
		conn.User = os.Getenv("USER")
		if conn.User == "" {
			conn.User = os.Getenv("LOGNAME")
		}
	}
	
	// Try to find the control socket
	controlPath, err := findControlSocket(conn)
	if err != nil {
		return conn, fmt.Errorf("SSH session detected but no ControlMaster found: %w", err)
	}
	
	conn.ControlPath = controlPath
	
	// Extract host from control path if possible
	conn.Host = extractHostFromPath(controlPath)
	
	return conn, nil
}

// findControlSocket searches for SSH ControlMaster socket
func findControlSocket(conn *Connection) (string, error) {
	// Check environment variable first
	if path := os.Getenv("BANKSHOT_SSH_SOCKET"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	
	// Common ControlPath patterns
	patterns := []string{
		"/tmp/ssh-*@*:%s",
		"/tmp/ssh-*@*:%s.sock",
		"/tmp/ssh_mux_*_%s",
		"~/.ssh/cm-*@*:%s",
		"~/.ssh/master-*@*:%s",
		"~/.ssh/control-*@*:%s",
		"~/.ssh/sockets/*@*:%s",
	}
	
	// Also check without port
	patternsNoPort := []string{
		"/tmp/ssh-*@*",
		"/tmp/ssh_mux_*",
		"~/.ssh/cm-*@*",
		"~/.ssh/master-*@*",
		"~/.ssh/control-*@*",
		"~/.ssh/sockets/*@*",
	}
	
	// Try patterns with port first
	for _, pattern := range patterns {
		path := fmt.Sprintf(pattern, conn.Port)
		path = expandPath(path)
		
		matches, err := filepath.Glob(path)
		if err == nil && len(matches) > 0 {
			// Check if it's a socket
			for _, match := range matches {
				if isSocket(match) {
					return match, nil
				}
			}
		}
	}
	
	// Try patterns without port
	for _, pattern := range patternsNoPort {
		path := expandPath(pattern)
		
		matches, err := filepath.Glob(path)
		if err == nil && len(matches) > 0 {
			// Check if it's a socket
			for _, match := range matches {
				if isSocket(match) {
					return match, nil
				}
			}
		}
	}
	
	// Try to find any socket in common locations
	searchDirs := []string{
		"/tmp",
		expandPath("~/.ssh"),
		expandPath("~/.ssh/sockets"),
		"/var/run/user/" + os.Getenv("UID"),
	}
	
	for _, dir := range searchDirs {
		if sockets := findSocketsInDir(dir); len(sockets) > 0 {
			// Try to validate each socket
			for _, socket := range sockets {
				if strings.Contains(socket, "ssh") {
					return socket, nil
				}
			}
		}
	}
	
	return "", fmt.Errorf("no ControlMaster socket found")
}

// isSocket checks if a path is a Unix socket
func isSocket(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

// findSocketsInDir finds all Unix sockets in a directory
func findSocketsInDir(dir string) []string {
	var sockets []string
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sockets
	}
	
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if isSocket(path) {
			sockets = append(sockets, path)
		}
	}
	
	return sockets
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return path
}

// extractHostFromPath tries to extract hostname from control socket path
func extractHostFromPath(path string) string {
	base := filepath.Base(path)
	
	// Common patterns: user@host:port or user@host
	if idx := strings.Index(base, "@"); idx > 0 {
		rest := base[idx+1:]
		if colonIdx := strings.Index(rest, ":"); colonIdx > 0 {
			return rest[:colonIdx]
		}
		// Remove any file extensions
		if dotIdx := strings.Index(rest, "."); dotIdx > 0 {
			return rest[:dotIdx]
		}
		return rest
	}
	
	return ""
}