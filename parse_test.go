package smtp

import (
	"testing"
)

func TestParser(t *testing.T) {
	validReversePaths := []struct {
		raw, path, after string
	}{
		{"<>", "", ""},
		{"<root@nsa.gov>", "root@nsa.gov", ""},
		{"root@nsa.gov", "root@nsa.gov", ""},
		{"<root@nsa.gov> AUTH=asdf@example.org", "root@nsa.gov", " AUTH=asdf@example.org"},
		{"root@nsa.gov AUTH=asdf@example.org", "root@nsa.gov", " AUTH=asdf@example.org"},
	}
	for _, tc := range validReversePaths {
		p := parser{tc.raw}
		path, err := p.parseReversePath()
		if err != nil {
			t.Errorf("parser.parseReversePath(%q) = %v", tc.raw, err)
		} else if path != tc.path {
			t.Errorf("parser.parseReversePath(%q) = %q, want %q", tc.raw, path, tc.path)
		} else if p.s != tc.after {
			t.Errorf("parser.parseReversePath(%q): got after = %q, want %q", tc.raw, p.s, tc.after)
		}
	}

	invalidReversePaths := []string{
		"",
		" ",
		"asdf",
		"<Foo Bar <root@nsa.gov>>",
		" BODY=8BITMIME SIZE=12345",
		"a:b:c@example.org",
		"<root@nsa.gov",
	}
	for _, tc := range invalidReversePaths {
		p := parser{tc}
		if path, err := p.parseReversePath(); err == nil {
			t.Errorf("parser.parseReversePath(%q) = %q, want error", tc, path)
		}
	}
}

func TestParseCmd(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedCmd string
		expectedArg string
		shouldError bool
	}{
		{
			name:        "XCLIENT command with arguments",
			input:       "XCLIENT ADDR=127.0.0.1 PORT=55804 PROTO=ESMTP",
			expectedCmd: "XCLIENT",
			expectedArg: "ADDR=127.0.0.1 PORT=55804 PROTO=ESMTP",
			shouldError: false,
		},
		{
			name:        "MAIL command",
			input:       "MAIL FROM:<test@example.com>",
			expectedCmd: "MAIL",
			expectedArg: "FROM:<test@example.com>",
			shouldError: false,
		},
		{
			name:        "STARTTLS command (special case)",
			input:       "STARTTLS",
			expectedCmd: "STARTTLS",
			expectedArg: "",
			shouldError: false,
		},
		{
			name:        "QUIT command no args",
			input:       "QUIT",
			expectedCmd: "QUIT",
			expectedArg: "",
			shouldError: false,
		},
		{
			name:        "EHLO command",
			input:       "EHLO localhost",
			expectedCmd: "EHLO",
			expectedArg: "localhost",
			shouldError: false,
		},
		{
			name:        "LHLO command",
			input:       "LHLO localhost",
			expectedCmd: "LHLO",
			expectedArg: "localhost",
			shouldError: false,
		},
		{
			name:        "Long command name",
			input:       "TESTCOMMAND args here",
			expectedCmd: "TESTCOMMAND",
			expectedArg: "args here",
			shouldError: false,
		},
		{
			name:        "Command too short",
			input:       "HI",
			expectedCmd: "",
			expectedArg: "",
			shouldError: true,
		},
		{
			name:        "Command with CRLF",
			input:       "XCLIENT ADDR=127.0.0.1\r\n",
			expectedCmd: "XCLIENT",
			expectedArg: "ADDR=127.0.0.1",
			shouldError: false,
		},
		{
			name:        "Empty line",
			input:       "",
			expectedCmd: "",
			expectedArg: "",
			shouldError: false,
		},
		{
			name:        "Case insensitive command",
			input:       "xclient addr=127.0.0.1",
			expectedCmd: "XCLIENT",
			expectedArg: "addr=127.0.0.1",
			shouldError: false,
		},
		{
			name:        "Very short command with space",
			input:       "AB args",
			expectedCmd: "",
			expectedArg: "",
			shouldError: true,
		},
		{
			name:        "Minimum valid command with args",
			input:       "TEST arg",
			expectedCmd: "TEST",
			expectedArg: "arg",
			shouldError: false,
		},
		{
			name:        "Command with extra spaces in args",
			input:       "RCPT  TO:<user@example.com>  ",
			expectedCmd: "RCPT",
			expectedArg: "TO:<user@example.com>",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, arg, err := parseCmd(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if cmd != tt.expectedCmd {
				t.Errorf("Expected command %q, got %q", tt.expectedCmd, cmd)
			}

			if arg != tt.expectedArg {
				t.Errorf("Expected argument %q, got %q", tt.expectedArg, arg)
			}
		})
	}
}
