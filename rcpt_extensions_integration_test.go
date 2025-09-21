package smtp

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

// Helper function to read a line from bufio.Reader and convert to string
func readLine(reader *bufio.Reader) (string, error) {
	line, _, err := reader.ReadLine()
	return string(line), err
}

// Test backend that captures RCPT options with extensions
type TestBackendWithExtensions struct {
	rcptOptions []*RcptOptions
	mailFrom    string
	dataContent string
}

func (b *TestBackendWithExtensions) NewSession(c *Conn) (Session, error) {
	return &TestSessionWithExtensions{backend: b}, nil
}

type TestSessionWithExtensions struct {
	backend *TestBackendWithExtensions
}

func (s *TestSessionWithExtensions) Mail(from string, opts *MailOptions) error {
	s.backend.mailFrom = from
	return nil
}

func (s *TestSessionWithExtensions) Rcpt(to string, opts *RcptOptions) error {
	// Store a copy of the options to verify later
	optsCopy := &RcptOptions{
		Notify:                     opts.Notify,
		OriginalRecipientType:      opts.OriginalRecipientType,
		OriginalRecipient:          opts.OriginalRecipient,
		RequireRecipientValidSince: opts.RequireRecipientValidSince,
		DeliverBy:                  opts.DeliverBy,
		MTPriority:                 opts.MTPriority,
	}
	if opts.Extensions != nil {
		optsCopy.Extensions = make(map[string]string)
		for k, v := range opts.Extensions {
			optsCopy.Extensions[k] = v
		}
	}
	s.backend.rcptOptions = append(s.backend.rcptOptions, optsCopy)
	return nil
}

func (s *TestSessionWithExtensions) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.backend.dataContent = string(data)
	return nil
}

func (s *TestSessionWithExtensions) Reset() {
	s.backend.rcptOptions = nil
	s.backend.mailFrom = ""
	s.backend.dataContent = ""
}

func (s *TestSessionWithExtensions) Logout() error {
	return nil
}

// Integration test for RCPT with custom extensions
func TestServer_RcptWithExtensions(t *testing.T) {
	backend := &TestBackendWithExtensions{}
	server := NewServer(backend)
	server.Domain = "example.com"
	server.AllowInsecureAuth = true
	server.EnableDSN = true
	server.EnableRCPTExtensions = true

	// Create a pipe for testing
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Run server handler in background
	go func() {
		conn := newConn(serverConn, server)
		server.handleConn(conn)
	}()

	// Client side
	client := bufio.NewReader(clientConn)

	// Read greeting
	greeting, err := readLine(client)
	if err != nil {
		t.Fatalf("Failed to read greeting: %v", err)
	}
	if !strings.HasPrefix(greeting, "220") {
		t.Fatalf("Expected 220 greeting, got: %s", greeting)
	}

	// Send EHLO (required for extensions)
	clientConn.Write([]byte("EHLO client.example.com\r\n"))
	// Read EHLO response (multiple lines)
	for {
		line, _ := readLine(client)
		if strings.HasPrefix(line, "250 ") { // Last line starts with "250 "
			break
		}
	}

	// Send MAIL FROM
	clientConn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	response, _ := readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("MAIL FROM failed: %s", response)
	}

	// Test 1: RCPT with standard parameters only
	clientConn.Write([]byte("RCPT TO:<user1@example.com> NOTIFY=SUCCESS\r\n"))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("RCPT with standard params failed: %s", response)
	}

	// Test 2: RCPT with XRCPTFORWARD extension
	xrcptData := base64.StdEncoding.EncodeToString([]byte("user=john\\tsmith\tsession=12345\tip=192.168.1.100"))
	rcptCmd := fmt.Sprintf("RCPT TO:<user2@example.com> XRCPTFORWARD=%s\r\n", xrcptData)
	clientConn.Write([]byte(rcptCmd))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("RCPT with XRCPTFORWARD failed: %s", response)
	}

	// Test 3: RCPT with mixed standard and custom parameters
	customCmd := fmt.Sprintf("RCPT TO:<user3@example.com> NOTIFY=FAILURE XRCPTFORWARD=%s CUSTOM=value\r\n", xrcptData)
	clientConn.Write([]byte(customCmd))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("RCPT with mixed params failed: %s", response)
	}

	// Verify backend received the data correctly BEFORE sending DATA (which triggers reset)
	if len(backend.rcptOptions) != 3 {
		t.Fatalf("Expected 3 RCPT options, got %d", len(backend.rcptOptions))
	}

	// Test 1 verification: Standard parameters only
	opts1 := backend.rcptOptions[0]
	if len(opts1.Notify) != 1 || opts1.Notify[0] != DSNNotifySuccess {
		t.Error("Standard NOTIFY parameter not handled correctly")
	}
	if opts1.Extensions != nil {
		t.Error("Extensions should be nil for standard-only parameters")
	}

	// Test 2 verification: XRCPTFORWARD extension
	opts2 := backend.rcptOptions[1]
	if opts2.Extensions == nil {
		t.Fatal("Extensions should not be nil for XRCPTFORWARD")
	}
	if opts2.Extensions["XRCPTFORWARD"] != xrcptData {
		t.Error("XRCPTFORWARD data not stored correctly")
	}

	// Parse the XRCPTFORWARD data
	parsedData, err := ParseXRCPTFORWARD(opts2.Extensions["XRCPTFORWARD"])
	if err != nil {
		t.Fatalf("Failed to parse XRCPTFORWARD data: %v", err)
	}
	if parsedData["user"] != "john\tsmith" {
		t.Errorf("XRCPTFORWARD user data incorrect: got %q", parsedData["user"])
	}
	if parsedData["session"] != "12345" {
		t.Errorf("XRCPTFORWARD session data incorrect: got %q", parsedData["session"])
	}

	// Test 3 verification: Mixed parameters
	opts3 := backend.rcptOptions[2]
	if len(opts3.Notify) != 1 || opts3.Notify[0] != DSNNotifyFailure {
		t.Error("Mixed standard parameter NOTIFY not handled correctly")
	}
	if opts3.Extensions == nil {
		t.Fatal("Extensions should not be nil for mixed parameters")
	}
	if opts3.Extensions["XRCPTFORWARD"] != xrcptData {
		t.Error("Mixed XRCPTFORWARD data not stored correctly")
	}
	if opts3.Extensions["CUSTOM"] != "value" {
		t.Error("Mixed custom parameter not stored correctly")
	}

	// Send DATA
	clientConn.Write([]byte("DATA\r\n"))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "354") {
		t.Fatalf("DATA failed: %s", response)
	}

	// Send message content
	clientConn.Write([]byte("Subject: Test\r\n\r\nTest message\r\n.\r\n"))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("Message send failed: %s", response)
	}

	// Send QUIT
	clientConn.Write([]byte("QUIT\r\n"))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "221") {
		t.Fatalf("QUIT failed: %s", response)
	}
}

// Test malformed XRCPTFORWARD handling
func TestServer_RcptWithMalformedXRCPTFORWARD(t *testing.T) {
	backend := &TestBackendWithExtensions{}
	server := NewServer(backend)
	server.Domain = "example.com"
	server.EnableDSN = true
	server.EnableRCPTExtensions = true

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		conn := newConn(serverConn, server)
		server.handleConn(conn)
	}()

	client := bufio.NewReader(clientConn)

	// Read greeting and send EHLO/MAIL
	readLine(client) // greeting
	clientConn.Write([]byte("EHLO client.example.com\r\n"))
	// Read EHLO response (multiple lines)
	for {
		line, _ := readLine(client)
		if strings.HasPrefix(line, "250 ") { // Last line starts with "250 "
			break
		}
	}
	clientConn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	readLine(client)

	// Test malformed XRCPTFORWARD (invalid base64)
	clientConn.Write([]byte("RCPT TO:<user@example.com> XRCPTFORWARD=invalid-base64!\r\n"))
	response, _ := readLine(client)
	if !strings.HasPrefix(response, "501") {
		t.Fatalf("Expected 501 error for invalid base64, got: %s", response)
	}

	// Test XRCPTFORWARD with content too large
	largeData := make([]byte, 1000) // > 900 bytes
	for i := range largeData {
		largeData[i] = 'a'
	}
	largeEncoded := base64.StdEncoding.EncodeToString(largeData)
	clientConn.Write([]byte(fmt.Sprintf("RCPT TO:<user@example.com> XRCPTFORWARD=%s\r\n", largeEncoded)))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "501") {
		t.Fatalf("Expected 501 error for too large content, got: %s", response)
	}

	// Cleanup
	clientConn.Write([]byte("QUIT\r\n"))
	readLine(client)
}

// Test backward compatibility - ensure old code still works
func TestServer_BackwardCompatibilityRcpt(t *testing.T) {
	backend := &TestBackendWithExtensions{}
	server := NewServer(backend)
	server.Domain = "example.com"
	server.EnableDSN = true // Enable DSN for testing standard parameters

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		conn := newConn(serverConn, server)
		server.handleConn(conn)
	}()

	client := bufio.NewReader(clientConn)

	// Complete SMTP transaction with only standard parameters
	readLine(client) // greeting
	clientConn.Write([]byte("EHLO client.example.com\r\n"))

	// Read EHLO response (multiple lines)
	for {
		line, _ := readLine(client)
		if strings.HasPrefix(line, "250 ") { // Last line starts with "250 "
			break
		}
	}

	clientConn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	readLine(client)

	// Send RCPT with standard DSN parameters only
	clientConn.Write([]byte("RCPT TO:<user@example.com> NOTIFY=SUCCESS,FAILURE ORCPT=rfc822;user@example.com\r\n"))
	response, _ := readLine(client)
	if !strings.HasPrefix(response, "250") {
		t.Fatalf("RCPT with DSN params failed: %s", response)
	}

	// Verify backward compatibility BEFORE DATA (which triggers reset)
	if len(backend.rcptOptions) != 1 {
		t.Fatalf("Expected 1 RCPT option, got %d", len(backend.rcptOptions))
	}

	opts := backend.rcptOptions[0]

	// Standard parameters should work as before
	if len(opts.Notify) != 2 {
		t.Errorf("Expected 2 notify options, got %d", len(opts.Notify))
	}

	if opts.OriginalRecipient != "user@example.com" {
		t.Errorf("ORCPT not handled correctly: got %q", opts.OriginalRecipient)
	}

	// Extensions should be nil for standard-only parameters
	if opts.Extensions != nil {
		t.Error("Extensions should be nil when only standard parameters are used")
	}

	clientConn.Write([]byte("DATA\r\n"))
	readLine(client)
	clientConn.Write([]byte("Test message\r\n.\r\n"))
	readLine(client)
	clientConn.Write([]byte("QUIT\r\n"))
	readLine(client)
}

// Test that unknown RCPT parameters return error 500 when extensions are disabled
func TestServer_RcptExtensionsDisabled(t *testing.T) {
	backend := &TestBackendWithExtensions{}
	server := NewServer(backend)
	server.Domain = "example.com"
	server.EnableDSN = true
	// Note: EnableRCPTExtensions is false by default

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		conn := newConn(serverConn, server)
		server.handleConn(conn)
	}()

	client := bufio.NewReader(clientConn)

	// Read greeting and send EHLO/MAIL
	readLine(client) // greeting
	clientConn.Write([]byte("EHLO client.example.com\r\n"))
	// Read EHLO response (multiple lines)
	for {
		line, _ := readLine(client)
		if strings.HasPrefix(line, "250 ") { // Last line starts with "250 "
			break
		}
	}
	clientConn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	readLine(client)

	// Test that unknown RCPT parameter returns error 500
	clientConn.Write([]byte("RCPT TO:<user@example.com> UNKNOWNPARAM=value\r\n"))
	response, _ := readLine(client)
	if !strings.HasPrefix(response, "500") {
		t.Fatalf("Expected 500 error for unknown parameter when extensions disabled, got: %s", response)
	}

	// Test that XRCPTFORWARD also returns error 500 when extensions disabled
	xrcptData := base64.StdEncoding.EncodeToString([]byte("user=john\tsession=12345"))
	rcptCmd := fmt.Sprintf("RCPT TO:<user@example.com> XRCPTFORWARD=%s\r\n", xrcptData)
	clientConn.Write([]byte(rcptCmd))
	response, _ = readLine(client)
	if !strings.HasPrefix(response, "500") {
		t.Fatalf("Expected 500 error for XRCPTFORWARD when extensions disabled, got: %s", response)
	}

	// Cleanup
	clientConn.Write([]byte("QUIT\r\n"))
	readLine(client)
}
