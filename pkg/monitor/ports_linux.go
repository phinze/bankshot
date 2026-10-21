//go:build linux

package monitor

// GetListeningPorts returns all ports in LISTEN state from procfs, with the
// owning process resolved where it can be.
func GetListeningPorts() ([]Port, error) {
	ports, err := getListeningPortsFromProc("/proc/net/tcp", "/proc/net/tcp6")
	resolvePortOwners("/proc", ports)
	return ports, err
}
