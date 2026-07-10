package agent

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in       string
		maj, min int
		ok       bool
	}{
		{"2.1.4 (Claude Code)", 2, 1, true},
		{"claude 2.3.0", 2, 3, true},
		{"v2.10.1", 2, 10, true},
		{"no version here", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		maj, min, ok := ParseVersion(c.in)
		if ok != c.ok || maj != c.maj || min != c.min {
			t.Errorf("ParseVersion(%q) = %d,%d,%v want %d,%d,%v", c.in, maj, min, ok, c.maj, c.min, c.ok)
		}
	}
}

func TestMinVersionMet(t *testing.T) {
	cases := []struct {
		maj, min int
		want     bool
	}{
		{2, 1, true}, {2, 3, true}, {3, 0, true}, {2, 0, false}, {1, 9, false},
	}
	for _, c := range cases {
		if got := MinVersionMet(c.maj, c.min); got != c.want {
			t.Errorf("MinVersionMet(%d,%d) = %v want %v", c.maj, c.min, got, c.want)
		}
	}
}
