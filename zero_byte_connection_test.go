package smtp_test

import (
	"bytes"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

// TestZeroByteConnection verifies that health check connections that
// connect and immediately disconnect don't generate error logs
func TestZeroByteConnection(t *testing.T) {
	// Capture error logs
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "test: ", 0)

	be := &backend{}
	s := smtp.NewServer(be)
	s.ErrorLog = logger
	defer s.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Simulate health check connections
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		// Immediately close without sending any data
		conn.Close()
	}

	// Give server time to process connections
	time.Sleep(100 * time.Millisecond)

	// Check logs
	logs := logBuf.String()
	if strings.Contains(logs, "error handling") {
		t.Errorf("Zero-byte connections should not generate error logs, but got:\n%s", logs)
	}
}

// TestNormalConnectionError verifies that real connection errors are still logged
func TestNormalConnectionError(t *testing.T) {
	// Capture error logs
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "test: ", 0)

	be := &backend{}
	s := smtp.NewServer(be)
	s.ErrorLog = logger
	s.ReadTimeout = 50 * time.Millisecond // Short timeout to force error
	defer s.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and send partial command
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	buf := make([]byte, 1024)
	conn.Read(buf)

	// Send partial command and wait for timeout
	conn.Write([]byte("HELO"))
	time.Sleep(100 * time.Millisecond)

	// Check that timeout error was logged
	logs := logBuf.String()
	if !strings.Contains(logs, "error handling") {
		t.Error("Connection timeouts should still be logged")
	}
}