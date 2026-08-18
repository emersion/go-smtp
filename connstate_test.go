package smtp_test

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

type stateTracker struct {
	mu     sync.Mutex
	states []smtp.ConnState
}

func (st *stateTracker) record(conn net.Conn, state smtp.ConnState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.states = append(st.states, state)
}

func (st *stateTracker) getStates() []smtp.ConnState {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]smtp.ConnState(nil), st.states...)
}

func (st *stateTracker) reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.states = nil
}

func TestConnStateBasicLifecycle(t *testing.T) {
	be := &backend{}
	s := smtp.NewServer(be)
	defer s.Close()

	tracker := &stateTracker{}
	s.ConnState = tracker.record

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and immediately disconnect
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// Wait for states to be recorded
	time.Sleep(100 * time.Millisecond)

	states := tracker.getStates()
	if len(states) < 2 {
		t.Fatalf("Expected at least 2 states, got %d: %v", len(states), states)
	}

	// Should see New -> Active -> Closed
	if states[0] != smtp.StateNew {
		t.Errorf("Expected first state to be StateNew, got %v", states[0])
	}
	if states[1] != smtp.StateActive {
		t.Errorf("Expected second state to be StateActive, got %v", states[1])
	}
	if states[len(states)-1] != smtp.StateClosed {
		t.Errorf("Expected last state to be StateClosed, got %v", states[len(states)-1])
	}
}

func TestConnStateAuth(t *testing.T) {
	be := &backend{
		authDisabled: false, // Ensure auth is enabled
	}
	s := smtp.NewServer(be)
	defer s.Close()

	tracker := &stateTracker{}
	s.ConnState = tracker.record

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and perform AUTH
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	readResponse(t, conn)

	// Send EHLO
	writeCommand(t, conn, "EHLO test.example.com")
	readResponse(t, conn)

	// Send AUTH command
	writeCommand(t, conn, "AUTH PLAIN")
	readResponse(t, conn)

	// Send credentials (base64 encoded "\0username\0password")
	writeCommand(t, conn, "AHVzZXJuYW1lAHBhc3N3b3Jk")
	readResponse(t, conn)

	// Send QUIT
	writeCommand(t, conn, "QUIT")
	readResponse(t, conn)

	conn.Close()

	// Wait for states to be recorded
	time.Sleep(100 * time.Millisecond)

	states := tracker.getStates()
	
	// Debug: Print all states to understand what's happening
	t.Logf("All states: %v", states)
	
	// Should see New -> Active -> Auth -> (Active or Error) -> Closed
	// The auth might fail, but we want to test that StateAuth is triggered
	
	if len(states) < 4 {
		t.Fatalf("Expected at least 4 states, got %d: %v", len(states), states)
	}

	// Check key states are present
	if states[0] != smtp.StateNew {
		t.Errorf("Expected first state to be StateNew, got %v", states[0])
	}
	if states[1] != smtp.StateActive {
		t.Errorf("Expected second state to be StateActive, got %v", states[1])
	}
	if states[2] != smtp.StateAuth {
		t.Errorf("Expected third state to be StateAuth, got %v", states[2])
	}
	
	// Fourth state should be either StateActive (success) or StateError (failure)
	fourthState := states[3]
	if fourthState != smtp.StateActive && fourthState != smtp.StateError {
		t.Errorf("Expected fourth state to be StateActive or StateError, got %v", fourthState)
	}
	
	if states[len(states)-1] != smtp.StateClosed {
		t.Errorf("Expected last state to be StateClosed, got %v", states[len(states)-1])
	}
}

func TestConnStateData(t *testing.T) {
	be := &backend{}
	s := smtp.NewServer(be)
	defer s.Close()

	tracker := &stateTracker{}
	s.ConnState = tracker.record

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and send email
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	readResponse(t, conn)

	// Send EHLO
	writeCommand(t, conn, "EHLO test.example.com")
	readResponse(t, conn)

	// Send MAIL FROM
	writeCommand(t, conn, "MAIL FROM:<test@example.com>")
	readResponse(t, conn)

	// Send RCPT TO
	writeCommand(t, conn, "RCPT TO:<dest@example.com>")
	readResponse(t, conn)

	// Send DATA
	writeCommand(t, conn, "DATA")
	readResponse(t, conn)

	// Send message
	writeCommand(t, conn, "Subject: Test\r\n\r\nTest message\r\n.")
	readResponse(t, conn)

	// Send QUIT
	writeCommand(t, conn, "QUIT")
	readResponse(t, conn)

	conn.Close()

	// Wait for states to be recorded
	time.Sleep(100 * time.Millisecond)

	states := tracker.getStates()
	
	// Should see New -> Active -> Data -> Active -> Closed
	expectedStates := []smtp.ConnState{
		smtp.StateNew,
		smtp.StateActive,
		smtp.StateData,
		smtp.StateActive,
		smtp.StateClosed,
	}

	if len(states) != len(expectedStates) {
		t.Fatalf("Expected %d states, got %d: %v", len(expectedStates), len(states), states)
	}

	for i, expected := range expectedStates {
		if states[i] != expected {
			t.Errorf("State %d: expected %v, got %v", i, expected, states[i])
		}
	}
}

func TestConnStateReset(t *testing.T) {
	be := &backend{}
	s := smtp.NewServer(be)
	defer s.Close()

	tracker := &stateTracker{}
	s.ConnState = tracker.record

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and send RSET
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	readResponse(t, conn)

	// Send EHLO
	writeCommand(t, conn, "EHLO test.example.com")
	readResponse(t, conn)

	// Send RSET
	writeCommand(t, conn, "RSET")
	readResponse(t, conn)

	// Send QUIT
	writeCommand(t, conn, "QUIT")
	readResponse(t, conn)

	conn.Close()

	// Wait for states to be recorded
	time.Sleep(100 * time.Millisecond)

	states := tracker.getStates()
	
	// Should see New -> Active -> Reset -> Active -> Closed
	expectedStates := []smtp.ConnState{
		smtp.StateNew,
		smtp.StateActive,
		smtp.StateReset,
		smtp.StateActive,
		smtp.StateClosed,
	}

	if len(states) != len(expectedStates) {
		t.Fatalf("Expected %d states, got %d: %v", len(expectedStates), len(states), states)
	}

	for i, expected := range expectedStates {
		if states[i] != expected {
			t.Errorf("State %d: expected %v, got %v", i, expected, states[i])
		}
	}
}

func TestConnStateAuthError(t *testing.T) {
	be := &backend{}
	s := smtp.NewServer(be)
	defer s.Close()

	tracker := &stateTracker{}
	s.ConnState = tracker.record

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and perform failed AUTH
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	readResponse(t, conn)

	// Send EHLO
	writeCommand(t, conn, "EHLO test.example.com")
	readResponse(t, conn)

	// Send AUTH command with invalid credentials
	writeCommand(t, conn, "AUTH PLAIN")
	readResponse(t, conn)

	// Send invalid credentials
	writeCommand(t, conn, "invalid_base64")
	readResponse(t, conn)

	// Send QUIT
	writeCommand(t, conn, "QUIT")
	readResponse(t, conn)

	conn.Close()

	// Wait for states to be recorded
	time.Sleep(100 * time.Millisecond)

	states := tracker.getStates()
	
	// Should see New -> Active -> Auth -> Error -> Closed
	expectedStates := []smtp.ConnState{
		smtp.StateNew,
		smtp.StateActive,
		smtp.StateAuth,
		smtp.StateError,
		smtp.StateClosed,
	}

	if len(states) != len(expectedStates) {
		t.Fatalf("Expected %d states, got %d: %v", len(expectedStates), len(states), states)
	}

	for i, expected := range expectedStates {
		if states[i] != expected {
			t.Errorf("State %d: expected %v, got %v", i, expected, states[i])
		}
	}
}

func TestConnStateNoCallback(t *testing.T) {
	be := &backend{}
	s := smtp.NewServer(be)
	defer s.Close()

	// Don't set ConnState callback - should work normally
	s.ConnState = nil

	// Start server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go s.Serve(l)

	// Connect and send simple command
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read greeting
	readResponse(t, conn)

	// Send EHLO
	writeCommand(t, conn, "EHLO test.example.com")
	readResponse(t, conn)

	// Send QUIT
	writeCommand(t, conn, "QUIT")
	readResponse(t, conn)

	conn.Close()

	// Test passes if no panic occurred
}

// Helper functions

func writeCommand(t *testing.T, conn net.Conn, cmd string) {
	if !strings.HasSuffix(cmd, "\r\n") {
		cmd += "\r\n"
	}
	_, err := conn.Write([]byte(cmd))
	if err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, conn net.Conn) string {
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf[:n])
}