package tailnet

import (
	"io"
	"net"
)

// Splice copies bytes both ways between a and b until both directions finish,
// passing each side's EOF along as a half-close. bReader reads from b, and
// may hold bytes already buffered from it.
func Splice(a, b net.Conn, bReader io.Reader) {
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(b, a)
		closeWrite(b)
		close(done)
	}()
	_, _ = io.Copy(a, bReader)
	closeWrite(a)
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}
