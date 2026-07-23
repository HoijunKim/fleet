# Tier 4b - Release Metadata Implementation Plan

**Goal:** every fleet binary can name itself, and every release says what changed.

**Architecture:** one new package `internal/buildinfo` holds the ldflags targets,
because both `main` packages (desktop, `cmd/fleetd`) need the value. The desktop
app exposes it through a binding shown in the Settings footer; `fleetd` uses it
as the default for the metric label it already emits. `release.yml` stamps the
tag into the binaries and pulls that tag's `CHANGELOG.md` section into the
release body.

**Tech Stack:** Go 1.22, wails v2.12.0, Svelte 5, GitHub Actions.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-tier4b-release-metadata-design.md`.
- Do NOT run `gofmt -w` across the tree - the working copy is CRLF and it would
  produce a 67-file line-ending diff. Format only the files you touch, and check
  them the way CI does: `git show HEAD:<file> | gofmt -l` on the LF blob.
- `frontend/dist/` is gitignored and `main.go` embeds it: build the frontend once
  before any Go command that compiles package `main`.
- Conventional Commits, no trailers.
- CI (`desktop.yml`) runs `gofmt -l .`, `go vet`, `go test -race`, svelte-check
  and vitest on every push. Keep the branch green.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/buildinfo/buildinfo.go` (create) | ldflags targets + `Version`/`Commit`/`String`. |
| `internal/buildinfo/buildinfo_test.go` (create) | Table tests for the three shapes. |
| `app.go` (modify) | `BuildVersion()` binding. |
| `app_test.go` (modify) | Binding works on a zero-value `App`. |
| `frontend/src/lib/SettingsModal.svelte` (modify) | Version in the modal footer. |
| `cmd/fleetd/main.go` (modify) | Default the metric label and log the version. |
| `CHANGELOG.md` (create) | Keep a Changelog, `[Unreleased]` only. |
| `.github/workflows/release.yml` (modify) | Stamp ldflags; extract release notes. |

---

### Task 1: `internal/buildinfo`

- [ ] **Step 1: Write the failing test** - `internal/buildinfo/buildinfo_test.go`

Cover: `Version()` = `dev` when unstamped and the stamped value when set;
`Commit()` truncates a 40-char SHA to 7; `String()` renders `v0.1.0 (a1b2c3d)`,
`dev (a1b2c3d)`, and bare `dev`. Save and restore the package vars with
`t.Cleanup` so the cases do not leak into each other.

The `dirty` suffix comes from `debug.ReadBuildInfo`, which a test cannot set, so
it is verified by hand in Task 5 rather than asserted here.

- [ ] **Step 2: Run it, watch it fail to build** (`undefined: buildinfo.Version`)

`go test ./internal/buildinfo/`

- [ ] **Step 3: Implement `buildinfo.go`** per spec section 1 - three unexported
vars, three accessors, `debug.ReadBuildInfo` fallback for `vcs.revision` and
`vcs.modified`.

- [ ] **Step 4: `go test ./internal/buildinfo/ -v`** - all cases pass.

- [ ] **Step 5: Commit** - `feat(buildinfo): version/commit stamping with a dev fallback`

### Task 2: Surface it in the desktop app

- [ ] **Step 1: Failing test** - add `TestBuildVersionOnZeroApp` to `app_test.go`:
`(&App{}).BuildVersion()` returns a non-empty string. It must not touch config,
store or ctx, so a zero-value `App` is the whole point of the test.

- [ ] **Step 2: Run it** - fails to build, `a.BuildVersion undefined`.

- [ ] **Step 3: Implement** - `func (a *App) BuildVersion() string { return buildinfo.String() }`
next to `GetConfig` (`app.go:258`), plus the import.

- [ ] **Step 4: Run it** - passes.

- [ ] **Step 5: Settings footer** - in `SettingsModal.svelte`, call the binding in
the existing `onMount` load path, hold it in a `let version = ""`, and render it
in `.modal-foot` before the buttons:

```svelte
    <div class="modal-foot">
      <span class="set-version mono" title="Build version">{version}</span>
      <button class="btn btn-secondary" on:click={onClose}>Cancel</button>
```

with `.modal-foot` gaining `margin-right: auto` on `.set-version` so the buttons
stay right-aligned. Style: `font-size: 11.5px; color: var(--muted);` matching
`.ai-radio-note`.

- [ ] **Step 6: Frontend check** - `cd frontend && npm run check && npm test`.
Both must stay green; no new frontend test (the binding is mocked-runtime
territory and the string is verified by hand in Task 5).

- [ ] **Step 7: Commit** - `feat(app): report the build version in Settings`

### Task 3: `cmd/fleetd`

- [ ] **Step 1: Change the metric default** - `cmd/fleetd/main.go:71`,
`envOr("FLEET_VERSION", "dev")` becomes `envOr("FLEET_VERSION", buildinfo.Version())`.
An explicit env var still wins - the Docker image builds without a `.git` dir,
so the env var stays its supported path.

- [ ] **Step 2: Log it at boot** - add `"version", buildinfo.Version()` to the
`slog.Info("listening", ...)` call at `:116`.

- [ ] **Step 3: Verify** - `go build ./cmd/fleetd && go vet ./cmd/fleetd/... && go test ./cmd/fleetd/...`

- [ ] **Step 4: Commit** - `feat(server): default the build-info label to the stamped version`

### Task 4: CHANGELOG + release wiring

- [ ] **Step 1: Write `CHANGELOG.md`** - Keep a Changelog header, one
`## [Unreleased]` section grouping what is on `master` today (Added / Changed /
Fixed / Security), derived from `git log --no-merges`. No versioned section: no
tag has been cut.

- [ ] **Step 2: Stamp the build** - in `release.yml`, extend the `wails build`
step with the three `-X` flags from spec section 2.

- [ ] **Step 3: Release notes** - add the awk extraction step (Linux leg only,
`if: matrix.os == 'ubuntu-latest'`) and give `softprops/action-gh-release@v2`
`body_path: RELEASE_NOTES.md` and `generate_release_notes: true`.

Guard: `body_path` on a file that does not exist fails the action, so the Windows
and macOS legs must not receive it. Since all three legs run the same `uses:`
step, the value is conditional:
`body_path: ${{ matrix.os == 'ubuntu-latest' && 'RELEASE_NOTES.md' || '' }}`.

- [ ] **Step 4: Verify the awk locally** against the committed CHANGELOG with a
fake version, since a workflow cannot be dry-run:

```bash
printf '## [0.1.0]\nhello\n## [0.0.9]\nold\n' > /tmp/cl.md
awk -v ver="0.1.0" '$0 ~ "^## \\[" ver "\\]" {p=1; next} p && /^## \[/ {exit} p {print}' /tmp/cl.md
```

Expected: `hello` and nothing else.

- [ ] **Step 5: Verify the workflow shape** - `node -e` assertion that
`release.yml` contains `buildinfo.version=`, `body_path`, and still has
`needs: test` (the tier 4a gate must survive this edit).

- [ ] **Step 6: Commit** - `ci(release): stamp the tag into the binaries and use the CHANGELOG as release notes`

### Task 5: Whole-suite verification

- [ ] **Step 1** - `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...`
- [ ] **Step 2** - gofmt the LF blobs of every touched Go file; expect no output.
- [ ] **Step 3** - `wails build -ldflags "-X github.com/hoijun/fleet/internal/buildinfo.version=v9.9.9-test -X github.com/hoijun/fleet/internal/buildinfo.commit=abcdef1234567890"`,
launch, open Settings: the footer must read `v9.9.9-test (abcdef1)`. Then an
unstamped `wails build` must read `dev (<7-char sha>)` - and `, dirty` when the
tree has uncommitted changes.
- [ ] **Step 4** - push, confirm the three `desktop` checks are green, open a PR.
