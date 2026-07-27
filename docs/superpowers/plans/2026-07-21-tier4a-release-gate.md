# Tier 4a - Release Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Never publish a binary no test has touched, never let a second launch destroy the first launch's data, never boot a server whose token signing key is guessable.

**Architecture:** A new reusable GitHub Actions workflow (`desktop.yml`) runs the desktop Go suite on Windows and Linux plus the frontend suite on Linux, with no path filter; `release.yml` calls it through `workflow_call` and gates its build job on it. Two small Go changes close the remaining holes: a wails `SingleInstanceLock` in `main.go` and a pure `validateSigningKey` function in `cmd/fleetd`. The README is corrected to match what the project actually ships.

**Tech Stack:** Go 1.22, wails v2.12.0, GitHub Actions, Node 20 / npm / vitest / svelte-check.

## Global Constraints

- Go version everywhere: `1.22` (matches `go.mod` and both existing workflows).
- Node version in CI: `20` (matches `release.yml:23-24`).
- wails version: `v2.12.0` (`go.mod`). `options.SingleInstanceLock` exists in this version.
- The Linux Go build needs `libgtk-3-dev libwebkit2gtk-4.1-dev` and the build tag `webkit2_41`; Windows needs neither.
- `frontend/dist/` is gitignored, and `main.go:15-16` embeds it. Every Go command that compiles package `main` on a fresh checkout must be preceded by a frontend build.
- Do NOT run `gofmt -w` locally. `gofmt -l .` reports 67 files because the author's working tree is CRLF (`core.autocrlf=true`); `.gitattributes` forces LF on a fresh checkout, so CI is clean. Reformatting would produce a 67-file line-ending-only diff.
- Commit messages follow Conventional Commits as used in this repo (`feat(...)`, `docs(...)`, `ci(...)`), no trailers.
- Spec: `docs/superpowers/specs/2026-07-21-tier4a-release-gate-design.md`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `.github/workflows/desktop.yml` (create) | The desktop test suite: Go matrix job + frontend job. Reusable via `workflow_call`. |
| `.github/workflows/release.yml` (modify) | Adds a `test` job that calls `desktop.yml`, and `needs: test` on `build`. |
| `main.go` (modify) | Wires `SingleInstanceLock` into the wails options. |
| `app.go` (modify) | Adds `App.focus()`, the second-instance window-raise callback. |
| `app_test.go` (modify) | Test that `focus()` is a no-op with a nil context. |
| `cmd/fleetd/main.go` (modify) | Adds `validateSigningKey` and calls it in `run()`. |
| `cmd/fleetd/main_test.go` (modify) | Table test for `validateSigningKey`. |
| `README.md` (modify) | Platform-neutral run instructions, correct build commands, per-OS config path, `claude` CLI prerequisite, signing key requirement. |

---

### Task 1: Desktop CI workflow + release gate

**Files:**
- Create: `.github/workflows/desktop.yml`
- Modify: `.github/workflows/release.yml:7-8` (insert a `test` job before `build`, add `needs: test`)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: a workflow named `desktop` with jobs `go` (matrix `windows-latest`, `ubuntu-latest`) and `frontend`, callable as `./.github/workflows/desktop.yml`.

- [ ] **Step 1: Write the workflow file**

Create `.github/workflows/desktop.yml` with exactly this content:

```yaml
name: desktop
on:
  push:
    branches: ["**"]
  pull_request:
  workflow_call:

jobs:
  go:
    strategy:
      fail-fast: false
      matrix:
        os: [windows-latest, ubuntu-latest]
    runs-on: ${{ matrix.os }}
    env:
      # Package main embeds frontend/dist and links wails, which is cgo/GTK on
      # Linux. The tag matches the one release.yml builds with.
      GOFLAGS: ${{ matrix.os == 'ubuntu-latest' && '-tags=webkit2_41' || '' }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - uses: actions/setup-node@v4
        with: { node-version: "20" }
      - name: Install Linux webkit deps
        if: matrix.os == 'ubuntu-latest'
        run: sudo apt-get update && sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
      - name: Build frontend
        # main.go embeds frontend/dist, which is gitignored: without this every
        # Go command that compiles package main fails on a fresh checkout.
        working-directory: frontend
        run: npm ci && npm run build
      - name: Build
        run: go build ./...
      - name: Vet
        run: go vet ./...
      - name: Check gofmt
        if: matrix.os == 'ubuntu-latest'
        run: |
          fmt_out=$(gofmt -l .)
          if [ -n "$fmt_out" ]; then
            echo "The following files are not gofmt'd:"
            echo "$fmt_out"
            exit 1
          fi
      - name: Test (race)
        if: matrix.os == 'ubuntu-latest'
        run: go test -race ./...
      - name: Test
        if: matrix.os == 'windows-latest'
        run: go test ./...

  frontend:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "20" }
      - name: Install
        run: npm ci
      - name: Type check
        run: npm run check
      - name: Test
        run: npm test
      - name: Build
        run: npm run build
```

Why each choice, so a reviewer can check it:
- `branches: ["**"]` matches branch pushes but not tags. Without it a `v*` tag would run this workflow twice: once on its own and once through `release.yml`'s `workflow_call`.
- No `paths:` filter anywhere. That is the entire point of the task - `server.yml:4-19` has one, which is why the desktop tree has never been tested by CI.
- `-race` on Linux only: the Windows race detector needs a cgo toolchain, and `internal/git` already takes ~14s there because it shells out to real git.
- `gofmt` on Linux only: gofmt is platform-independent, so a second run buys nothing.
- `fail-fast: false`: a Windows-only failure must not hide a Linux-only one.

- [ ] **Step 2: Verify the file is valid YAML and has the expected shape**

Run (no dependencies beyond the Node already required to build the frontend):

```bash
node -e "const s=require('fs').readFileSync('.github/workflows/desktop.yml','utf8');const need=['name: desktop','workflow_call:','windows-latest','ubuntu-latest','npm ci && npm run build','go test -race ./...','npm run check'];const miss=need.filter(n=>!s.includes(n));console.log(miss.length?'MISSING: '+miss.join(', '):'OK');console.log(/paths:/.test(s)?'FAIL: a path filter crept in':'no path filter, good');"
```

Expected: two lines, `OK` and `no path filter, good`.

- [ ] **Step 3: Gate the release on it**

In `.github/workflows/release.yml`, replace lines 7-8:

```yaml
jobs:
  build:
```

with:

```yaml
jobs:
  test:
    uses: ./.github/workflows/desktop.yml

  build:
    needs: test
```

Nothing else in `release.yml` changes. `permissions: contents: write` stays at the top level; the called workflow only reads.

- [ ] **Step 4: Verify the gate is wired**

Run:

```bash
node -e "const s=require('fs').readFileSync('.github/workflows/release.yml','utf8');const ok=s.includes('uses: ./.github/workflows/desktop.yml')&&s.includes('needs: test')&&s.includes('softprops/action-gh-release@v2');console.log(ok?'OK':'FAIL');"
```

Expected: `OK`

- [ ] **Step 5: Verify the Go suite actually passes the way CI will run it**

CI cannot be run locally, so run its Go half by hand on this machine (Windows):

```bash
cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...
```

Expected: `npm run build` emits `dist/index.html` plus one JS and one CSS asset; `go build` and `go vet` print nothing; `go test ./...` prints `ok` for 25 packages and `no test files` for `internal/winhide`, with no `FAIL`.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/desktop.yml .github/workflows/release.yml
git commit -m "ci(desktop): test the desktop tree on every push and gate releases on it"
```

---

### Task 2: Single-instance lock

**Files:**
- Modify: `app.go` (add `focus` method next to the other `wruntime` callers)
- Modify: `main.go:29-38` (add the `SingleInstanceLock` option)
- Test: `app_test.go` (add `TestFocusNilContextIsNoop`)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `func (a *App) focus()` - raises and shows the main window; no-op when `a.ctx` is nil.

Background a reviewer needs: two fleet processes hold divergent full in-memory copies of every local store and rewrite each file whole on mutation (`app.go:118-131`), so the last writer silently destroys the other's projects, tasks, tags and deadlines. wails' `SingleInstanceLock` makes the second process exit inside `SetupSingleInstance`, called at the top of `Frontend.Run` - before `OnStartup` is dispatched, so the second process never starts the 60s sync loop.

- [ ] **Step 1: Write the failing test**

Add to `app_test.go`:

```go
func TestFocusNilContextIsNoop(t *testing.T) {
	// OnSecondInstanceLaunch can fire before startup has assigned a.ctx.
	// focus must return instead of calling into a runtime with no window.
	a := &App{}
	a.focus()
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestFocusNilContextIsNoop ./...`

Expected: FAIL to build, with `a.focus undefined (type *App has no field or method focus)`.

- [ ] **Step 3: Implement `focus`**

Add to `app.go`, immediately after the `startup` method (which ends at `app.go:160`):

```go
// focus raises the existing window. It is the OnSecondInstanceLaunch callback:
// a second launch hands its intent to the running instance and exits, rather
// than opening a second window whose divergent in-memory stores would overwrite
// this one's on the next save. The nil check is load-bearing - the callback can
// fire before startup has assigned ctx.
func (a *App) focus() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowUnminimise(a.ctx)
	wruntime.Show(a.ctx)
}
```

`wruntime` is already imported in `app.go:34` as `wruntime "github.com/wailsapp/wails/v2/pkg/runtime"`. No new import.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -run TestFocusNilContextIsNoop ./...`

Expected: `ok  github.com/hoijun/fleet` (and `no test files` / `ok` lines for the other packages).

- [ ] **Step 5: Wire the lock into wails**

In `main.go`, change the `wails.Run` call (currently `main.go:29-38`) to:

```go
	err := wails.Run(&options.App{
		Title:  "Fleet",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "fleet-desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.focus() },
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
```

The agent-hook path (`main.go:22-26`) returns before this, so a hook invocation never takes the lock.

- [ ] **Step 6: Verify it builds and the whole suite still passes**

Run: `go build ./... && go vet ./... && go test ./...`

Expected: no output from build/vet; `go test` shows no `FAIL`.

- [ ] **Step 7: Verify the behavior by hand**

Run `wails build` then start `build/bin/fleet.exe` twice.

Expected: the second launch does not open a window; the first window comes to the front (and un-minimizes if it was minimized). Close it and confirm a subsequent single launch still opens normally.

- [ ] **Step 8: Commit**

```bash
git add app.go app_test.go main.go
git commit -m "feat(app): single-instance lock so a second launch cannot clobber local data"
```

---

### Task 3: Reject a weak `JWT_SIGNING_KEY`

**Files:**
- Modify: `cmd/fleetd/main.go:49` (call the check) and add `validateSigningKey` near `mustEnv` (`cmd/fleetd/main.go:163-170`)
- Test: `cmd/fleetd/main_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1-2.
- Produces: `func validateSigningKey(key []byte) error` - nil when the key is at least 32 bytes and has at least 8 distinct byte values.

Background: a short or dictionary secret boots cleanly and looks healthy forever, and every legitimately issued 15-minute token is then an offline cracking oracle for minting `sub=<any user id>`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/fleetd/main_test.go`:

```go
func TestValidateSigningKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty", "", true},
		{"short but random", "k9#Lq2vXz7!Pw4Rt6Ym1Bn8Cd3Fg5Hj", true},           // 31 bytes
		{"long but one repeated byte", strings.Repeat("a", 64), true},
		{"long but too few distinct bytes", strings.Repeat("abcdefg", 10), true}, // 70 bytes, 7 distinct
		{"exactly 32 bytes, enough variety", "k9#Lq2vXz7!Pw4Rt6Ym1Bn8Cd3Fg5HjK", false},
		{"long random", "8f3c1e9a7b2d4f60c5a8e13b7d92f4a6c0b3e857d192f4a6", false},
	}
	for _, tc := range cases {
		err := validateSigningKey([]byte(tc.key))
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
```

Add `"strings"` to the import block of `cmd/fleetd/main_test.go` (it currently imports only `"os"` and `"testing"`).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/fleetd/ -run TestValidateSigningKey`

Expected: FAIL to build, with `undefined: validateSigningKey`.

- [ ] **Step 3: Implement it**

Add to `cmd/fleetd/main.go`, immediately after `mustEnv` (which ends at `:170`):

```go
// signingKeyMinLen and signingKeyMinDistinct are the floor for JWT_SIGNING_KEY.
// 32 bytes matches the HS256 output width; the distinct-byte floor rejects
// keys that pass a length check while carrying almost no entropy ("aaaa...",
// a repeated word). Both are cheap guards against a key that would make every
// issued token an offline cracking oracle for minting sub=<any user id>.
const (
	signingKeyMinLen      = 32
	signingKeyMinDistinct = 8
)

func validateSigningKey(key []byte) error {
	if len(key) < signingKeyMinLen {
		return fmt.Errorf("JWT_SIGNING_KEY must be at least %d bytes, got %d", signingKeyMinLen, len(key))
	}
	var seen [256]bool
	distinct := 0
	for _, b := range key {
		if !seen[b] {
			seen[b] = true
			distinct++
		}
	}
	if distinct < signingKeyMinDistinct {
		return fmt.Errorf("JWT_SIGNING_KEY has too little entropy: %d distinct bytes, need %d", distinct, signingKeyMinDistinct)
	}
	return nil
}
```

`fmt` is already imported by `cmd/fleetd/main.go` (used by `fmt.Errorf("migrate: %w", err)` at `:57`).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/fleetd/ -run TestValidateSigningKey -v`

Expected: `--- PASS: TestValidateSigningKey`.

- [ ] **Step 5: Call it from `run`**

In `cmd/fleetd/main.go`, immediately after the `mustEnv` block (i.e. after `metricsToken := envOr("METRICS_TOKEN", "")` at `:55`, before the `pgstore.Migrate` call at `:57`), insert:

```go
	if err := validateSigningKey(signingKey); err != nil {
		return err
	}
```

Return the error rather than calling `os.Exit`: `run` returns errors so failures propagate to `main` for a single exit point (`cmd/fleetd/main.go:44-46`). Placing it before `Migrate` means a weak key cannot serve a request or touch the database.

- [ ] **Step 6: Verify the package still builds and passes**

Run: `go build ./cmd/fleetd && go vet ./cmd/fleetd/... && go test ./cmd/fleetd/...`

Expected: no build/vet output; `ok  github.com/hoijun/fleet/cmd/fleetd`.

- [ ] **Step 7: Commit**

```bash
git add cmd/fleetd/main.go cmd/fleetd/main_test.go
git commit -m "feat(server): refuse to boot on a short or low-entropy JWT_SIGNING_KEY"
```

---

### Task 4: README corrections

**Files:**
- Modify: `README.md:7-22` and `README.md:44`

**Interfaces:**
- Consumes: the signing-key floor from Task 3 (32 bytes) - the env table must state the same number.
- Produces: nothing other tasks depend on.

- [ ] **Step 1: Replace the run and build sections**

Replace `README.md:7-22` (from `## Run (Windows)` through the `go build ./cmd/fleet` line) with:

```markdown
## Run

Download the binary for your platform from Releases - `fleet.exe` (Windows),
`fleet-macos.zip` (macOS), `fleet` (Linux) - or build from source:

    wails build
    ./build/bin/fleet.exe

First run writes a config and creates the data directory next to it:

| OS | Path |
| --- | --- |
| Windows | `%APPDATA%\fleet\config.toml` |
| macOS / Linux | `$XDG_CONFIG_HOME/fleet/config.toml`, else `~/.config/fleet/config.toml` |

`roots` defaults to `~/Projects`. Edit it and restart.

## Build from source

Requires Go 1.22+, Node 18+, and Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).
On Linux also install `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

    wails build            # desktop app -> build/bin/fleet.exe

The AI brief, repo chat and agent features shell out to the `claude` CLI, which
must be on `PATH` (it is the default AI provider). The OpenAI and Gemini
providers are configured in Settings instead and need no CLI.
```

Three facts this encodes: `release.yml:9-17` ships all three platforms; `cmd/fleet` was deleted in bd6f51e so the second build command was dead; `internal/ai/ai.go:52` resolves the provider with `exec.LookPath("claude")` and `internal/config/config.go:59` makes `"claude"` the default.

- [ ] **Step 2: Update the signing-key row in the server env table**

Replace `README.md:44`:

```markdown
| `JWT_SIGNING_KEY` | HS256 secret for access tokens |
```

with:

```markdown
| `JWT_SIGNING_KEY` | HS256 secret for access tokens. At least 32 bytes with real entropy - the server refuses to boot otherwise. Generate with `openssl rand -base64 48` |
```

- [ ] **Step 3: Verify the dead command is gone and the claims hold**

Run:

```bash
grep -n "cmd/fleet\b" README.md; ls cmd/; grep -n "32 bytes" README.md
```

Expected: the first `grep` prints nothing (no reference to the deleted directory); `ls cmd/` prints `fleet-hook` and `fleetd`; the second `grep` prints the `JWT_SIGNING_KEY` row.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(readme): all three platforms, per-OS config path, claude CLI, signing key floor"
```

---

## Post-merge verification

These cannot be checked before the workflows exist on the default branch. Do them once the branch lands:

- [ ] Push and confirm three checks appear and go green: `desktop / go (windows-latest)`, `desktop / go (ubuntu-latest)`, `desktop / frontend`.
- [ ] Confirm the Linux Go leg does not fail on `gofmt` - if it does, do NOT run `gofmt -w`; check whether the failure is line endings (`.gitattributes` should have forced LF) before touching any file.
- [ ] Push a throwaway tag (`git tag v0.0.0-test && git push origin v0.0.0-test`) and confirm the release run shows `test` completing before `build` starts. Delete the tag and its draft release afterwards.
