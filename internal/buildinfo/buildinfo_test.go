package buildinfo

import "testing"

// stamp sets the ldflags-backed vars for one test and restores them after, so
// cases cannot leak into each other or into the package's real (unstamped) state.
func stamp(t *testing.T, v, c string) {
	t.Helper()
	oldV, oldC := version, commit
	version, commit = v, c
	t.Cleanup(func() { version, commit = oldV, oldC })
}

func TestVersionFallsBackToDev(t *testing.T) {
	stamp(t, "", "")
	if got := Version(); got != "dev" {
		t.Errorf("Version() = %q, want dev", got)
	}
}

func TestVersionUsesStampedValue(t *testing.T) {
	stamp(t, "v0.1.0", "")
	if got := Version(); got != "v0.1.0" {
		t.Errorf("Version() = %q, want v0.1.0", got)
	}
}

func TestCommitTruncatesToShortSHA(t *testing.T) {
	stamp(t, "", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678")
	if got := Commit(); got != "a1b2c3d" {
		t.Errorf("Commit() = %q, want a1b2c3d", got)
	}
}

func TestCommitKeepsAlreadyShortValue(t *testing.T) {
	stamp(t, "", "abc12")
	if got := Commit(); got != "abc12" {
		t.Errorf("Commit() = %q, want abc12", got)
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{"stamped release", "v0.1.0", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "v0.1.0 (a1b2c3d)"},
		{"dev with a commit", "", "a1b2c3d4e5f6", "dev (a1b2c3d)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stamp(t, tc.version, tc.commit)
			if got := String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// With no stamped commit, String falls back to whatever the build recorded. Under
// `go test` that is the test binary's own VCS info, which is present in a git
// work tree and absent with -buildvcs=false - so assert the shape, not the value.
func TestStringWithoutStampedCommit(t *testing.T) {
	stamp(t, "v0.1.0", "")
	got := String()
	if got != "v0.1.0" && !hasCommitSuffix(got, "v0.1.0") {
		t.Errorf("String() = %q, want %q or %q with a (commit) suffix", got, "v0.1.0", "v0.1.0")
	}
}

func hasCommitSuffix(s, prefix string) bool {
	return len(s) > len(prefix)+3 && s[:len(prefix)+2] == prefix+" (" && s[len(s)-1] == ')'
}
