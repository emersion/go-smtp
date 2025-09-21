package smtp

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestClientXCLIENT(t *testing.T) {
	xclientServer := "220 hello world\n" +
		// Response to first EHLO
		"250-mx.google.com at your service\n" +
		"250-XCLIENT ADDR PORT PROTO HELO LOGIN NAME\n" +
		"250 PIPELINING\n" +
		// Response to XCLIENT is a new greeting
		"220 new greeting\n" +
		// Response to second EHLO
		"250-mx.google.com at your service\n" +
		"250 PIPELINING\n" +
		// Response to QUIT
		"221 goodbye"

	xclientClient := "EHLO localhost\n" +
		"XCLIENT ADDR=192.168.1.100 PROTO=ESMTP\n" +
		"EHLO localhost\n" + // After XCLIENT, session is reset, must say EHLO again
		"QUIT"

	server := strings.Join(strings.Split(xclientServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(xclientClient, "\n"), "\r\n")

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}

	c := NewClient(fake)

	err := c.Hello("localhost")
	if err != nil {
		t.Fatalf("Hello failed: %v", err)
	}

	if !c.SupportsXCLIENT() {
		t.Fatal("Expected server to support XCLIENT")
	}

	attrs := map[string]string{
		"ADDR":  "192.168.1.100",
		"PROTO": "ESMTP",
	}

	err = c.XCLIENT(attrs)
	if err != nil {
		t.Fatalf("XCLIENT failed: %v", err)
	}

	// Quit will trigger a new EHLO because the session was reset
	if err := c.Quit(); err != nil {
		t.Fatalf("Quit failed: %v", err)
	}

	actualcmds := wrote.String()

	// Split into lines and verify each command
	actualLines := strings.Split(strings.TrimSpace(actualcmds), "\r\n")
	expectedLines := strings.Split(strings.TrimSpace(client), "\r\n")

	if len(actualLines) != len(expectedLines) {
		t.Fatalf("Got %d lines, expected %d lines.\nGot:\n%s\nExpected:\n%s", len(actualLines), len(expectedLines), actualcmds, client)
	}

	for i, actualLine := range actualLines {
		expectedLine := expectedLines[i]

		// For XCLIENT command, check that both lines are XCLIENT and contain the same attributes
		if strings.HasPrefix(actualLine, "XCLIENT") && strings.HasPrefix(expectedLine, "XCLIENT") {
			// Parse attributes from both lines
			actualAttrs := parseXCLIENTLine(actualLine)
			expectedAttrs := parseXCLIENTLine(expectedLine)

			if len(actualAttrs) != len(expectedAttrs) {
				t.Fatalf("XCLIENT line %d: got %d attributes, expected %d", i, len(actualAttrs), len(expectedAttrs))
			}

			for k, v := range expectedAttrs {
				if actualAttrs[k] != v {
					t.Fatalf("XCLIENT line %d: attribute %s got %q, expected %q", i, k, actualAttrs[k], v)
				}
			}
		} else if actualLine != expectedLine {
			t.Fatalf("Line %d: got %q, expected %q", i+1, actualLine, expectedLine)
		}
	}
}

// Helper function to parse XCLIENT attributes from a command line
func parseXCLIENTLine(line string) map[string]string {
	attrs := make(map[string]string)
	parts := strings.Fields(line)
	if len(parts) > 1 && parts[0] == "XCLIENT" {
		for _, part := range parts[1:] {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				attrs[kv[0]] = kv[1]
			}
		}
	}
	return attrs
}

func TestClientXCLIENT_NotSupported(t *testing.T) {
	server := "220 hello world\r\n" +
		"250-mx.google.com at your service\r\n" +
		"250 PIPELINING\r\n"

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}

	c := NewClient(fake)

	err := c.Hello("localhost")
	if err != nil {
		t.Fatalf("Hello failed: %v", err)
	}

	if c.SupportsXCLIENT() {
		t.Fatal("Expected server to not support XCLIENT")
	}

	attrs := map[string]string{
		"ADDR": "192.168.1.100",
	}

	err = c.XCLIENT(attrs)
	if err == nil {
		t.Fatal("Expected XCLIENT to fail when not supported")
	}

	expectedError := "smtp: server doesn't support XCLIENT"
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err.Error())
	}
}

func TestClientXCLIENT_EmptyAttributes(t *testing.T) {
	server := "220 hello world\r\n" +
		"250-mx.google.com at your service\r\n" +
		"250-XCLIENT ADDR PORT PROTO HELO LOGIN NAME\r\n" +
		"250 PIPELINING\r\n"

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}

	c := NewClient(fake)

	err := c.Hello("localhost")
	if err != nil {
		t.Fatalf("Hello failed: %v", err)
	}

	err = c.XCLIENT(map[string]string{})
	if err == nil {
		t.Fatal("Expected XCLIENT to fail with empty attributes")
	}

	expectedError := "smtp: XCLIENT requires at least one attribute"
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err.Error())
	}
}
