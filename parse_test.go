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

	validForwardPaths := []struct {
		raw, path, after string
	}{
		{"<root@nsa.gov>", "root@nsa.gov", ""},
		{"<Postmaster>", "Postmaster", ""},
		{"<postmaster> NOTIFY=NEVER", "postmaster", " NOTIFY=NEVER"},
		{"<Postmaster@nsa.gov>", "Postmaster@nsa.gov", ""},
	}
	for _, tc := range validForwardPaths {
		p := parser{tc.raw}
		path, err := p.parseForwardPath()
		if err != nil {
			t.Errorf("parser.parseForwardPath(%q) = %v", tc.raw, err)
		} else if path != tc.path {
			t.Errorf("parser.parseForwardPath(%q) = %q, want %q", tc.raw, path, tc.path)
		} else if p.s != tc.after {
			t.Errorf("parser.parseForwardPath(%q): got after = %q, want %q", tc.raw, p.s, tc.after)
		}
	}

	invalidForwardPaths := []string{
		"",
		"<>",
		"<root>",
		"<postmasterx>",
		"<Postmaster",
	}
	for _, tc := range invalidForwardPaths {
		p := parser{tc}
		if path, err := p.parseForwardPath(); err == nil {
			t.Errorf("parser.parseForwardPath(%q) = %q, want error", tc, path)
		}
	}

	p := parser{"<Postmaster>"}
	if path, err := p.parseReversePath(); err == nil {
		t.Errorf("parser.parseReversePath(%q) = %q, want error: only a forward-path may omit the domain", "<Postmaster>", path)
	}
}

func TestParseCmd(t *testing.T) {
	valid := []struct {
		line, cmd, arg string
	}{
		{"NOOP", "NOOP", ""},
		{"noop", "NOOP", ""},
		{"MAIL FROM:<root@nsa.gov>", "MAIL", "FROM:<root@nsa.gov>"},
		{"rcpt TO:<root@nsa.gov>", "RCPT", "TO:<root@nsa.gov>"},
		{"STARTTLS", "STARTTLS", ""},
		{"starttls", "STARTTLS", ""},
	}
	for _, tc := range valid {
		cmd, arg, err := parseCmd(tc.line)
		if err != nil || cmd != tc.cmd || arg != tc.arg {
			t.Errorf("parseCmd(%q) = %q, %q, %v, want %q, %q", tc.line, cmd, arg, err, tc.cmd, tc.arg)
		}
	}

	// Verbs are ASCII: a non-ASCII letter that strings.ToUpper maps to an
	// ASCII one ("ſ" to "S", "ı" to "I") does not make a verb.
	for line, verb := range map[string]string{"ſTARTTLS": "STARTTLS", "ſtarttls": "STARTTLS", "ıı": "II"} {
		if cmd, _, _ := parseCmd(line); cmd == verb {
			t.Errorf("parseCmd(%q) = %q, want no verb", line, cmd)
		}
	}
}
