package smtp

import (
	"bufio"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"
)

type slowCloseConn struct {
	net.Conn
	closeCalled chan struct{}
	once        sync.Once
}

func (c *slowCloseConn) Close() error {
	c.once.Do(func() {
		close(c.closeCalled)
		// Hold the Close call open so handleStartTLS must finish its
		// handshake and connection swap while Close is in flight.
		time.Sleep(50 * time.Millisecond)
	})
	return c.Conn.Close()
}

// TestStartTLSCloseRace exercises the STARTTLS upgrade and Conn.Close
// concurrently. Both must agree on the underlying connection: Close must not
// read the old connection while handleStartTLS swaps it for the TLS one.
func TestStartTLSCloseRace(t *testing.T) {
	keypair, err := tls.X509KeyPair(localhostCert, localhostKey)
	if err != nil {
		t.Fatalf("tls.X509KeyPair() = %v", err)
	}

	serverRaw, clientRaw := net.Pipe()
	defer clientRaw.Close()

	wrapped := &slowCloseConn{
		Conn:        serverRaw,
		closeCalled: make(chan struct{}),
	}
	s := &Server{
		Domain:    "localhost",
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{keypair}},
	}
	conn := newConn(wrapped, s)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.handleStartTLS()
	}()

	r := bufio.NewReader(clientRaw)
	if line, err := r.ReadString('\n'); err != nil {
		t.Fatalf("read 220 reply: %v", err)
	} else if len(line) < 3 || line[:3] != "220" {
		t.Fatalf("unexpected reply: %q", line)
	}

	clientTLS := tls.Client(clientRaw, &tls.Config{InsecureSkipVerify: true})
	handshakeDone := make(chan error, 1)
	go func() { handshakeDone <- clientTLS.Handshake() }()

	// Close the connection while the handshake is in progress. The result of
	// the handshake does not matter; what matters is that both goroutines
	// agree on the value of c.conn.
	closed := make(chan error, 1)
	go func() { closed <- conn.Close() }()
	select {
	case <-wrapped.closeCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("Conn.Close did not reach the underlying connection")
	}

	select {
	case <-handshakeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("TLS handshake did not finish")
	}

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Conn.Close did not return")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleStartTLS did not return")
	}
}
