package tailnet

import (
	"io"
	"net"
)

// Splice copies bytes both ways between a and b until both directions finish,
// passing each side's EOF along as a half-close. aReader and bReader read
// from a and b; pass a bufio.Reader that already consumed a handshake so its
// buffered bytes aren't lost, or the conn itself.
func Splice(a net.Conn, aReader io.Reader, b net.Conn, bReader io.Reader) {
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(b, aReader)
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
