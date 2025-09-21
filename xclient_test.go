package smtp

import (
	"net"
	"testing"
	"time"
)

func TestXCLIENTTrustedNetworks(t *testing.T) {
	// Test trusted network checking
	_, network, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		EnableXCLIENT:      true,
		XCLIENTTrustedNets: []*net.IPNet{network},
	}

	// Mock connection from trusted IP
	trustedAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")
	untrustedAddr, _ := net.ResolveTCPAddr("tcp", "10.0.0.1:12345")

	tests := []struct {
		name     string
		addr     net.Addr
		expected bool
	}{
		{"trusted IP", trustedAddr, true},
		{"untrusted IP", untrustedAddr, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockConn{addr: tt.addr}
			c := &Conn{
				conn:   conn,
				server: server,
			}

			result := c.isXCLIENTTrusted()
			if result != tt.expected {
				t.Errorf("isXCLIENTTrusted() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestParseXCLIENTArgs(t *testing.T) {
	conn := &Conn{}

	tests := []struct {
		name     string
		arg      string
		expected map[string]string
		hasError bool
	}{
		{
			name: "valid attributes",
			arg:  "ADDR=192.168.1.1 PORT=25 PROTO=ESMTP",
			expected: map[string]string{
				"ADDR":  "192.168.1.1",
				"PORT":  "25",
				"PROTO": "ESMTP",
			},
		},
		{
			name: "special values",
			arg:  "ADDR=[UNAVAILABLE] LOGIN=[TEMPUNAVAIL]",
			expected: map[string]string{
				"ADDR":  "[UNAVAILABLE]",
				"LOGIN": "[TEMPUNAVAIL]",
			},
		},
		{
			name:     "invalid format",
			arg:      "INVALID_FORMAT",
			hasError: true,
		},
		{
			name:     "empty args",
			arg:      "",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := conn.parseXCLIENTArgs(tt.arg)
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
					t.Errorf("result[%s] = %s, expected %s", k, result[k], v)
				}
			}
		})
	}
}

func TestValidateXCLIENTAttrs(t *testing.T) {
	conn := &Conn{}

	tests := []struct {
		name     string
		attrs    map[string]string
		hasError bool
	}{
		{
			name: "valid attributes",
			attrs: map[string]string{
				"ADDR":  "192.168.1.1",
				"PORT":  "25",
				"PROTO": "ESMTP",
				"HELO":  "example.com",
			},
		},
		{
			name: "valid IPv6 address with prefix",
			attrs: map[string]string{
				"ADDR": "ipv6:2001:db8::1",
			},
		},
		{
			name: "special values",
			attrs: map[string]string{
				"ADDR":  "[UNAVAILABLE]",
				"LOGIN": "[TEMPUNAVAIL]",
			},
		},
		{
			name: "invalid attribute name",
			attrs: map[string]string{
				"INVALID": "value",
			},
			hasError: true,
		},
		{
			name: "invalid IP address",
			attrs: map[string]string{
				"ADDR": "invalid-ip",
			},
			hasError: true,
		},
		{
			name: "invalid empty ADDR",
			attrs: map[string]string{
				"ADDR": "",
			},
			hasError: true,
		},
		{
			name: "invalid port",
			attrs: map[string]string{
				"PORT": "99999",
			},
			hasError: true,
		},
		{
			name: "invalid empty PORT",
			attrs: map[string]string{
				"PORT": "",
			},
			hasError: true,
		},
		{
			name: "invalid protocol",
			attrs: map[string]string{
				"PROTO": "HTTP",
			},
			hasError: true,
		},
		{
			name: "empty HELO",
			attrs: map[string]string{
				"HELO": "",
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conn.validateXCLIENTAttrs(tt.attrs)
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

// Mock connection for testing
type mockConn struct {
	addr net.Addr
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	return m.addr
}

func (m *mockConn) RemoteAddr() net.Addr {
	return m.addr
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}
