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
