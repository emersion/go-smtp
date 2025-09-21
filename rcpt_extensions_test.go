package smtp

import (
	"encoding/base64"
	"testing"
)

func TestRcptOptions_Extensions(t *testing.T) {
	// Test that RcptOptions can hold custom extensions
	opts := &RcptOptions{
		Extensions: map[string]string{
			"XRCPTFORWARD": "dXNlcj1qb2huCXNlc3Npb249MTIzNDU=",
			"CUSTOM":       "value",
		},
	}

	if opts.Extensions["XRCPTFORWARD"] != "dXNlcj1qb2huCXNlc3Npb249MTIzNDU=" {
		t.Error("XRCPTFORWARD not stored correctly")
	}

	if opts.Extensions["CUSTOM"] != "value" {
		t.Error("Custom extension not stored correctly")
	}
}

func TestParseXRCPTFORWARD(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
		hasError bool
	}{
		{
			name:  "valid simple data",
			input: base64.StdEncoding.EncodeToString([]byte("user=john\tsession=12345")),
			expected: map[string]string{
				"user":    "john",
				"session": "12345",
			},
		},
		{
			name:  "with escaped characters in values",
			input: base64.StdEncoding.EncodeToString([]byte("name=john\\tsmith\tpath=/var\\nmailbox")),
			expected: map[string]string{
				"name": "john\tsmith",
				"path": "/var\nmailbox",
			},
		},
		{
			name:  "empty value",
			input: base64.StdEncoding.EncodeToString([]byte("user=\tactive=true")),
			expected: map[string]string{
				"user":   "",
				"active": "true",
			},
		},
		{
			name:  "single key-value pair",
			input: base64.StdEncoding.EncodeToString([]byte("forwarded=true")),
			expected: map[string]string{
				"forwarded": "true",
			},
		},
		{
			name:     "empty input",
			input:    "",
			hasError: true,
		},
		{
			name:     "invalid base64",
			input:    "invalid-base64!",
			hasError: true,
		},
		{
			name:  "empty pairs ignored",
			input: base64.StdEncoding.EncodeToString([]byte("user=john\t\tactive=true")),
			expected: map[string]string{
				"user":   "john",
				"active": "true",
			},
		},
		{
			name:     "invalid key=value format",
			input:    base64.StdEncoding.EncodeToString([]byte("invalidformat\tuser=john")),
			hasError: true,
		},
		{
			name:     "empty key",
			input:    base64.StdEncoding.EncodeToString([]byte("=value\tuser=john")),
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseXRCPTFORWARD(tt.input)

			if tt.hasError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("result length %d, expected %d", len(result), len(tt.expected))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("result[%s] = %q, expected %q", k, result[k], v)
				}
			}
		})
	}
}

func TestConn_validateXRCPTFORWARD(t *testing.T) {
	conn := &Conn{}

	tests := []struct {
		name     string
		input    string
		hasError bool
	}{
		{
			name:  "valid XRCPTFORWARD",
			input: base64.StdEncoding.EncodeToString([]byte("user=john\tsession=12345")),
		},
		{
			name:  "with escaped characters",
			input: base64.StdEncoding.EncodeToString([]byte("path=/var\\tmailbox\nowner=user")),
		},
		{
			name:     "empty value",
			input:    "",
			hasError: true,
		},
		{
			name:     "invalid base64",
			input:    "not-base64!",
			hasError: true,
		},
		{
			name:     "too large content",
			input:    base64.StdEncoding.EncodeToString(make([]byte, 1000)), // > 900 bytes
			hasError: true,
		},
		{
			name:     "invalid key=value format",
			input:    base64.StdEncoding.EncodeToString([]byte("invalidformat\tuser=john")),
			hasError: true,
		},
		{
			name:     "empty key",
			input:    base64.StdEncoding.EncodeToString([]byte("=value\tuser=john")),
			hasError: true,
		},
		{
			name:     "empty base64 string not allowed",
			input:    base64.StdEncoding.EncodeToString([]byte("")), // This produces ""
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conn.validateXRCPTFORWARD(tt.input)

			if tt.hasError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUnescapeXRCPTFORWARD(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "user=john\\tsmith",
			expected: "user=john\tsmith",
		},
		{
			input:    "path=/var\\nmailbox",
			expected: "path=/var\nmailbox",
		},
		{
			input:    "data=line1\\r\\nline2",
			expected: "data=line1\r\nline2",
		},
		{
			input:    "escaped=\\\\backslash",
			expected: "escaped=\\backslash",
		},
		{
			input:    "mixed=\\t\\n\\r\\\\test",
			expected: "mixed=\t\n\r\\test",
		},
		{
			input:    "noescapes=normaltext",
			expected: "noescapes=normaltext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := unescapeXRCPTFORWARD(tt.input)
			if result != tt.expected {
				t.Errorf("unescapeXRCPTFORWARD(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRcptOptions_BackwardCompatibility(t *testing.T) {
	// Test that existing code still works with nil Extensions
	opts := &RcptOptions{
		Notify: []DSNNotify{DSNNotifySuccess},
	}

	// Extensions should be nil by default
	if opts.Extensions != nil {
		t.Error("Extensions should be nil by default")
	}

	// Test that we can safely read from nil Extensions
	if opts.Extensions["NONEXISTENT"] != "" {
		t.Error("Reading from nil Extensions should return empty string")
	}
}

// Test integration with actual RCPT command parsing
func TestRcptExtensionsParsing(t *testing.T) {
	// This would be an integration test showing how the RCPT command
	// with extensions gets parsed. Since it requires a server setup,
	// we'll create a more focused unit test for the argument parsing logic.

	// Test that parseArgs works with custom parameters
	// This simulates what happens in handleRcpt when custom parameters are present

	// Mock the parseArgs function behavior (this would need to be extracted to test properly)
	// For now, we verify the structure is correct for the Extensions field

	opts := &RcptOptions{}

	// Simulate what the RCPT handler would do with custom parameters
	if opts.Extensions == nil {
		opts.Extensions = make(map[string]string)
	}

	// Add XRCPTFORWARD
	opts.Extensions["XRCPTFORWARD"] = base64.StdEncoding.EncodeToString([]byte("user=john\tsession=12345"))

	// Add other custom parameter
	opts.Extensions["CUSTOM"] = "value"

	// Verify standard parameters still work
	opts.Notify = []DSNNotify{DSNNotifySuccess}

	// Check that both standard and custom parameters coexist
	if len(opts.Extensions) != 2 {
		t.Errorf("Expected 2 extensions, got %d", len(opts.Extensions))
	}

	if len(opts.Notify) != 1 {
		t.Errorf("Expected 1 notify option, got %d", len(opts.Notify))
	}

	// Test parsing the XRCPTFORWARD data
	data, err := ParseXRCPTFORWARD(opts.Extensions["XRCPTFORWARD"])
	if err != nil {
		t.Errorf("Failed to parse XRCPTFORWARD: %v", err)
	}

	if data["user"] != "john" || data["session"] != "12345" {
		t.Error("XRCPTFORWARD data not parsed correctly")
	}
}
