package monitor

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Port represents a network port binding
type Port struct {
	Port     int
	Protocol string // "tcp" or "tcp6"
	State    string // Connection state
	BindAddr string // Bind address (e.g. "0.0.0.0", "127.0.0.1", "::1")
}

// parseProcNet parses /proc/net/tcp or /proc/net/tcp6 files
func parseProcNet(path string, protocol string) ([]Port, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var ports []Port
	scanner := bufio.NewScanner(file)

	// Skip header line
	// Header: sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
	scanner.Scan()

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

		// Parse hex IP address
		addrHex := parts[0]
		bindAddr := parseHexAddr(addrHex, protocol)

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
				BindAddr: bindAddr,
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

// parseHexAddr decodes the hex IP address from /proc/net/tcp{,6} format.
// IPv4 (/proc/net/tcp): 8 hex chars, little-endian 32-bit integer.
// IPv6 (/proc/net/tcp6): 32 hex chars, four little-endian 32-bit words.
func parseHexAddr(hexStr string, protocol string) string {
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}

	if protocol == "tcp" && len(b) == 4 {
		// IPv4: stored as little-endian 32-bit, so bytes are reversed
		ip := net.IPv4(b[3], b[2], b[1], b[0])
		return ip.String()
	}

	if protocol == "tcp6" && len(b) == 16 {
		// IPv6: four groups of little-endian 32-bit words
		ip := make(net.IP, 16)
		for i := 0; i < 4; i++ {
			off := i * 4
			ip[off] = b[off+3]
			ip[off+1] = b[off+2]
			ip[off+2] = b[off+1]
			ip[off+3] = b[off]
		}
		return ip.String()
	}

	return ""
}

// IsLocalAddr returns true if the address is a wildcard or loopback address
// that should be considered for port forwarding.
func IsLocalAddr(addr string) bool {
	switch addr {
	case "0.0.0.0", "127.0.0.1", "::", "::1":
		return true
	}
	return false
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
		// This will cause duplicates but they'll be filtered at a higher level
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
