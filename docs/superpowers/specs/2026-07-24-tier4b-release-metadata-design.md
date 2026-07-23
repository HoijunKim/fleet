# Tier 4b - Release Metadata Design

**Goal:** every fleet binary can name itself, and every release says what changed.

Today neither is true. There is no `-ldflags` anywhere in the tree, so a built
binary carries no version; `git tag` is empty, so the first tag is still ahead of
us; and there is no `CHANGELOG.md`. A bug report today cannot name a build, and
tier 4a's release pipeline publishes three platform binaries with an empty body.

Backlog item 1 (`docs/superpowers/BACKLOG.md`). Code signing, notarization and an
auto-update channel stay out - see "Out of scope".

## 1. `internal/buildinfo` (new package)

One package, because two `main` packages need the value: the desktop app and
`cmd/fleetd`. `-X main.version=...` only reaches one of them, so the vars live in
a shared package instead.

```go
package buildinfo

// Set by -ldflags at release time; empty in a plain `go build`.
var (
	version string
	commit  string
	date    string
)
```

Three accessors:

- `Version() string` - the ldflags value, else `"dev"`. This is the label form:
  it goes into `fleet_build_info{version=...}`, so it stays a single bare token
  with no spaces or parentheses.
- `Commit() string` - the ldflags value, else the VCS revision that
  `runtime/debug.ReadBuildInfo` records (`vcs.revision`), truncated to 7 chars,
  else `""`. Go stamps `vcs.revision`/`vcs.modified` automatically when building
  inside a git work tree, which covers every local dev build for free.
- `String() string` - the human form for the UI and the boot log:
  `v0.1.0 (a1b2c3d)`, `dev (a1b2c3d, dirty)` when the tree was modified, plain
  `dev` when there is no VCS info at all (the `go test` case, and any build with
  `-buildvcs=false`).

`date` is carried for the release build only and is not surfaced anywhere yet;
it exists so a stamped binary can answer "when" without another release.

The accessors are pure functions of package state, so the tests set the vars
directly rather than trying to exercise ldflags.

## 2. Stamping the release build

`release.yml` builds through wails, which forwards `-ldflags` to the Go build:

```yaml
      - name: Build
        run: >
          wails build -tags webkit2_41 -platform ${{ matrix.platform }}
          -ldflags "-X github.com/hoijun/fleet/internal/buildinfo.version=${{ github.ref_name }}
          -X github.com/hoijun/fleet/internal/buildinfo.commit=${{ github.sha }}
          -X github.com/hoijun/fleet/internal/buildinfo.date=${{ github.event.repository.updated_at }}"
```

`github.ref_name` on a tag push is the tag itself (`v0.1.0`), which is the whole
point: the version a binary reports is the tag it was cut from, with no second
place to bump. `commit` is stamped explicitly rather than left to `vcs.revision`
because `Commit()` truncates to 7 chars either way and an explicit value cannot
be disabled by a `-buildvcs=false` that creeps in later.

`desktop.yml` does not stamp. Its binaries are never published, and an unstamped
build is exactly what exercises the `dev` fallback path.

## 3. Surfacing it in the app

A binding beside the other zero-argument getters:

```go
func (a *App) BuildVersion() string { return buildinfo.String() }
```

The Settings modal footer shows it, muted and monospaced, left of the
Cancel/Save buttons (`SettingsModal.svelte:443-448`). Settings is where a user
already goes when something is wrong, and the footer is the only spot in that
modal that is not tab-scoped, so the version is visible from all three tabs.

Nothing else in the UI changes. No About dialog: it would be a second surface
for one string.

## 4. `cmd/fleetd`

`cmd/fleetd/main.go:71` already reads `envOr("FLEET_VERSION", "dev")` for the
`fleet_build_info` metric label. The literal `"dev"` becomes
`buildinfo.Version()`, so a stamped server binary reports its build even when the
deploy forgets the env var; an explicit `FLEET_VERSION` still wins, which is what
the Docker image path relies on (the image builds without a `.git` directory).

The boot line at `:116` gains `"version", buildinfo.Version()`, so a running
server's first log line identifies the build.

## 5. `CHANGELOG.md` and release notes

Keep a Changelog format, newest first, with a `## [Unreleased]` section at the
top. Since no tag exists yet, the initial file has exactly one section:
`[Unreleased]`, describing what is on `master` now - the tiers that have shipped
into it.

`release.yml` extracts the tag's section into the release body:

```yaml
      - name: Extract release notes
        shell: bash
        run: |
          awk -v ver="${GITHUB_REF_NAME#v}" '
            $0 ~ "^## \\[" ver "\\]" {p=1; next}
            p && /^## \[/ {exit}
            p {print}
          ' CHANGELOG.md > RELEASE_NOTES.md
      - uses: softprops/action-gh-release@v2
        with:
          files: build/bin/*
          body_path: RELEASE_NOTES.md
          generate_release_notes: true
```

`generate_release_notes: true` stays on: GitHub appends its commit/PR list below
the body, so a tag whose section is missing still gets a non-empty release rather
than a silent blank. The extraction runs on the Linux leg only - the three
matrix legs all upload to the same release, and running the same awk three times
on three OSes buys nothing but a Windows shell to debug.

## Testing

- **Go unit** (`internal/buildinfo/buildinfo_test.go`): `Version()` falls back to
  `dev`; `Version()` returns the stamped value; `String()` renders each of the
  three shapes (stamped, dev+commit, bare dev); `Commit()` truncates a 40-char
  SHA to 7. Tests set the package vars and restore them with `t.Cleanup`.
- **Go unit** (`app_test.go`): `BuildVersion()` returns a non-empty string on a
  zero-value `App` - it must not touch config, store, or ctx.
- **By hand**: `go run .` is not enough (the binding is only reachable from the
  UI), so `wails build` with an explicit `-ldflags` and confirm the Settings
  footer shows the stamped version.
- **CI**: the awk extraction is verified locally against the committed
  `CHANGELOG.md` before the workflow change lands, since a workflow cannot be
  dry-run.

## Out of scope

Code signing and notarization (needs a paid Apple Developer certificate and a
Windows code-signing cert - a purchasing decision, not a code one), an
auto-update channel, and a version check against a remote endpoint. Also: no
`--version` CLI flag on the desktop binary. It is a GUI app whose only non-GUI
entry point is the agent hook, and adding a flag means teaching `main` to
parse args before the hook sentinel check, which is the one ordering in `main`
that must not move.
