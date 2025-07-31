package monitor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Port represents a network port binding
type Port struct {
	Port     int
	Protocol string // "tcp" or "tcp6"
	State    string // Connection state
}

// parseProcNet parses /proc/net/tcp or /proc/net/tcp6 files
func parseProcNet(path string, protocol string) ([]Port, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ports []Port
	scanner := bufio.NewScanner(file)
	
	// Skip header line
	if scanner.Scan() {
		// Header: sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
	}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// local_address is in format: "00000000:1F90" (IP:Port in hex)
		localAddr := fields[1]
		parts := strings.Split(localAddr, ":")
		if len(parts) != 2 {
			continue
		}

		// Parse hex port
		portHex := parts[1]
		portNum, err := strconv.ParseInt(portHex, 16, 64)
		if err != nil {
			continue
		}

		// Parse state (01 = ESTABLISHED, 0A = LISTEN, etc)
		stateHex := fields[3]
		state := parseState(stateHex)

		// We're only interested in LISTEN state for port forwarding
		if state == "LISTEN" {
			ports = append(ports, Port{
				Port:     int(portNum),
				Protocol: protocol,
				State:    state,
			})
		}
	}

	return ports, scanner.Err()
}

// parseState converts hex state to readable string
func parseState(hexState string) string {
	states := map[string]string{
		"01": "ESTABLISHED",
		"02": "SYN_SENT",
		"03": "SYN_RECV",
		"04": "FIN_WAIT1",
		"05": "FIN_WAIT2",
		"06": "TIME_WAIT",
		"07": "CLOSE",
		"08": "CLOSE_WAIT",
		"09": "LAST_ACK",
		"0A": "LISTEN",
		"0B": "CLOSING",
	}
	
	if state, ok := states[hexState]; ok {
		return state
	}
	return "UNKNOWN"
}

// GetListeningPorts returns all ports in LISTEN state
func GetListeningPorts() ([]Port, error) {
	var allPorts []Port

	// Parse TCP ports
	tcpPorts, err := parseProcNet("/proc/net/tcp", "tcp")
	if err == nil {
		allPorts = append(allPorts, tcpPorts...)
	}

	// Parse TCP6 ports
	tcp6Ports, err := parseProcNet("/proc/net/tcp6", "tcp6")
	if err == nil {
		allPorts = append(allPorts, tcp6Ports...)
	}

	return allPorts, nil
}

// GetProcessListeningPorts returns ports for a specific process
func GetProcessListeningPorts(pid int) ([]Port, error) {
	var allPorts []Port

	// Try process-specific network namespace
	tcpPath := fmt.Sprintf("/proc/%d/net/tcp", pid)
	tcpPorts, err := parseProcNet(tcpPath, "tcp")
	if err == nil {
		allPorts = append(allPorts, tcpPorts...)
	} else {
		// Fallback to system-wide if process-specific fails
		tcpPorts, _ = parseProcNet("/proc/net/tcp", "tcp")
		allPorts = append(allPorts, tcpPorts...)
	}

	// Same for TCP6
	tcp6Path := fmt.Sprintf("/proc/%d/net/tcp6", pid)
	tcp6Ports, err := parseProcNet(tcp6Path, "tcp6")
	if err == nil {
		allPorts = append(allPorts, tcp6Ports...)
	} else {
		tcp6Ports, _ = parseProcNet("/proc/net/tcp6", "tcp6")
		allPorts = append(allPorts, tcp6Ports...)
	}

	return allPorts, nil
}