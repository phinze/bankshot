//go:build linux

package daemon

import (
	"fmt"
	"net"
	"syscall"
)

// peerCred returns the pid and uid of the process on the other end of a
// unix socket.
func peerCred(c *net.UnixConn) (pid, uid int, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, fmt.Errorf("SO_PEERCRED: %w", credErr)
	}
	return int(cred.Pid), int(cred.Uid), nil
}
