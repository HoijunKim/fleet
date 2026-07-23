// Package buildinfo carries the version a binary was cut from.
//
// The values are injected at link time by release.yml:
//
//	-X github.com/hoijun/fleet/internal/buildinfo.version=v0.1.0
//
// They live here rather than in a main package because both mains need them -
// the desktop app and cmd/fleetd - and -X can only target one symbol path.
// Every accessor degrades to something printable, so an unstamped `go build`
// still produces a binary that can answer "which build are you".
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Set by -ldflags at release time; empty in a plain `go build`.
var (
	version string
	commit  string
	date    string
)

// shortSHALen is git's own default abbreviation width.
const shortSHALen = 7

// Version is the label form: a single bare token, no spaces or parentheses,
// because it goes into the fleet_build_info Prometheus label.
func Version() string {
	if version == "" {
		return "dev"
	}
	return version
}

// Commit is the abbreviated commit the binary was built from: the stamped value
// if there is one, else the revision Go records automatically when building
// inside a git work tree, else empty.
func Commit() string {
	c := commit
	if c == "" {
		c, _ = vcs()
	}
	if len(c) > shortSHALen {
		return c[:shortSHALen]
	}
	return c
}

// Date is the release build timestamp, empty unless stamped. Nothing surfaces it
// yet; it is carried so a stamped binary can answer "when" without a new release.
func Date() string { return date }

// String is the human form for the UI and the boot log: "v0.1.0 (a1b2c3d)",
// "dev (a1b2c3d, dirty)" for a build off a modified work tree, or bare "dev"
// when there is no VCS information at all (go test, or -buildvcs=false).
func String() string {
	var b strings.Builder
	b.WriteString(Version())
	c := Commit()
	if c == "" {
		return b.String()
	}
	b.WriteString(" (")
	b.WriteString(c)
	// "dirty" describes the work tree a local build came from, so it is only
	// meaningful when the commit came from the VCS rather than from -ldflags:
	// a release binary is stamped from a clean tag by definition.
	if _, modified := vcs(); modified && commit == "" {
		b.WriteString(", dirty")
	}
	b.WriteString(")")
	return b.String()
}

// vcs reports the revision and dirty flag Go stamped into the binary. Both are
// absent for a test binary built with -buildvcs=false and for any build outside
// a work tree, in which case this returns ("", false).
func vcs() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return revision, modified
}
