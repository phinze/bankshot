package monitor

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Port represents a network port binding
type Port struct {
	Port     int
	Protocol string // "tcp" or "tcp6"
	State    string // Connection state
	BindAddr string // Bind address (e.g. "0.0.0.0", "127.0.0.1", "::1")
	Inode    uint64 // Socket inode, from procfs; 0 where unknown
	PID      int    // Owning process, where it could be resolved; 0 otherwise
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
			var inode uint64
			if len(fields) > 9 {
				inode, _ = strconv.ParseUint(fields[9], 10, 64)
			}
			ports = append(ports, Port{
				Port:     int(portNum),
				Protocol: protocol,
				State:    state,
				BindAddr: bindAddr,
				Inode:    inode,
			})
		}
	}

	return ports, scanner.Err()
}

// resolvePortOwners fills in PID for each port whose socket inode some
// process under procRoot holds open. It walks every /proc/PID/fd, so it's for
// occasional scans, not per-event use. Processes it can't read (gone, or
// another user's without CAP_DAC_READ_SEARCH) are skipped.
func resolvePortOwners(procRoot string, ports []Port) {
	want := make(map[string][]int) // "socket:[inode]" -> indexes into ports
	for i, p := range ports {
		if p.Inode != 0 {
			link := fmt.Sprintf("socket:[%d]", p.Inode)
			want[link] = append(want[link], i)
		}
	}
	if len(want) == 0 {
		return
	}
	procs, err := os.ReadDir(procRoot)
	if err != nil {
		return
	}
	for _, proc := range procs {
		pid, err := strconv.Atoi(proc.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(procRoot, proc.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			for _, i := range want[link] {
				ports[i].PID = pid
			}
			delete(want, link)
			if len(want) == 0 {
				return
			}
		}
	}
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

func getListeningPortsFromProc(tcpPath, tcp6Path string) ([]Port, error) {
	var allPorts []Port
	var readErrors []error

	// Parse TCP ports
	tcpPorts, err := parseProcNet(tcpPath, "tcp")
	if err == nil {
		allPorts = append(allPorts, tcpPorts...)
	} else {
		readErrors = append(readErrors, fmt.Errorf("read %s: %w", tcpPath, err))
	}

	// Parse TCP6 ports
	tcp6Ports, err := parseProcNet(tcp6Path, "tcp6")
	if err == nil {
		allPorts = append(allPorts, tcp6Ports...)
	} else {
		readErrors = append(readErrors, fmt.Errorf("read %s: %w", tcp6Path, err))
	}

	if len(readErrors) == 2 {
		return nil, fmt.Errorf("failed to read TCP socket tables: %w", errors.Join(readErrors...))
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

// ResolveProcessName returns the process name for a given PID.
// It reads /proc/<pid>/cmdline first to get the full (untruncated) argv[0]
// basename, falling back to /proc/<pid>/comm (which the kernel truncates
// to 15 characters). Returns empty string if the process is gone or unreadable.
func ResolveProcessName(pid int) string {
	// Try cmdline first for the full name
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil && len(data) > 0 {
		// cmdline is NUL-delimited; argv[0] is everything before the first NUL
		argv0 := string(data)
		if i := strings.IndexByte(argv0, 0); i >= 0 {
			argv0 = argv0[:i]
		}
		if argv0 != "" {
			return filepath.Base(argv0)
		}
	}
	// Fall back to comm (truncated to 15 chars)
	data, err = os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ResolveParentPID returns the parent PID for a given PID by reading PPid from
// /proc/<pid>/status. Returns 0 if the process is gone, unreadable, or at init.
func ResolveParentPID(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				ppid, err := strconv.Atoi(fields[1])
				if err != nil {
					return 0
				}
				return ppid
			}
		}
	}
	return 0
}

// ResolveProcessCwd reads /proc/<pid>/cwd symlink and returns the working directory.
// Returns empty string if the process is gone or unreadable.
func ResolveProcessCwd(pid int) string {
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return ""
	}
	return cwd
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
