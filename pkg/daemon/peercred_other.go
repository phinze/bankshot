//go:build !linux

package daemon

import (
	"net"
	"os"
)

// peerCred can't ask the kernel here, so it vouches for the socket's own
// user and leaves the gate to the socket's 0600 permissions.
func peerCred(*net.UnixConn) (pid, uid int, err error) {
	return 0, os.Getuid(), nil
}
