# fleet Desktop Client + GUI (Plan B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub sign-in and offline-first cross-device sync of project-management data to the fleet Wails desktop app, consuming Plan A's HTTP API, with a polished dark-theme GUI for auth and sync status.

**Architecture:** A new `internal/cloud` package speaks the v0 REST contract (auth + sync) and stores the refresh token in the Windows Credential Manager; a new `internal/syncengine` package runs an offline-first push/pull/LWW loop against a local `sync.json`; `store.Record` gains an `UpdatedAt` LWW timestamp; `git.RemoteURL`/`git.NormalizeRemote` derive stable cross-machine doc ids; `app.go` (via a new `authsync.go`) exposes `AuthStart/AuthStatus/SignOut/SyncNow/SyncState` bindings plus a background sync goroutine that emits Wails events; Svelte components (SignIn, AccountChip, SyncPill) render into the existing top bar.

**Tech Stack:** Go 1.22 (stdlib + `github.com/zalando/go-keyring`), Wails v2 runtime events, Svelte + TypeScript, `net/http`/`httptest` for tests.

## Global Constraints

- **Go version:** `go 1.22.0` (module `github.com/hoijun/fleet`). Do not raise the floor.
- **Desktop dependency policy:** stdlib only, EXCEPT the single new dependency `github.com/zalando/go-keyring` (OS credential storage for the refresh token). No other new desktop deps. (Server-only deps like pgx/chi/jwt/oauth2/migrate belong to Plan A and MUST NOT be imported by any desktop package.)
- **ASCII-only source** for all desktop Go files.
- **`gofmt` clean and `go vet ./...` clean** for every task. Existing desktop tests stay green.
- **LWW rule (client side):** apply a pulled doc locally only if `parse(pulled.updated_at)` is strictly newer than the local record's `UpdatedAt` (or the doc is absent locally); tombstones (`deleted:true`) delete locally. A push that the server rejects (stale) is not an error - the following pull reconciles it.
- **Config dir:** `sync.json` lives beside `projects.json` in the fleet config dir (`%APPDATA%\fleet` on Windows), resolved from `filepath.Dir(cfgPath)` where `cfgPath` comes from `config.Load()`.
- **Backend base URL:** constant `defaultAPIURL = "https://fleet-api.fly.dev"`, overridable by env `FLEET_API_URL`.
- **Contract types/endpoints/event names are fixed** - use them verbatim (see each task's Interfaces block). Event names: `auth:changed`, `sync:changed` (both required). One supplementary event `sync:remoteEdit` (no payload) drives the "updated on another device" toast.

---

### Task 1: store.Record.UpdatedAt + central stamp + migration

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing (leaf change).
- Produces: `store.Record.UpdatedAt string` (json `updatedAt`); `store.Update` stamps `r.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)` after the mutator runs; `migrate` backfills empty `UpdatedAt` once at load.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/store_test.go` (add `"time"` to its imports):

```go
func TestOpenStampsMissingUpdatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	// legacy file: record has no updatedAt
	raw := `{"p1":{"manual":true,"name":"legacy","tasks":[]}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := s.Get("p1")
	if !ok {
		t.Fatal("expected record p1")
	}
	if rec.UpdatedAt == "" {
		t.Error("migrate should stamp a missing UpdatedAt")
	}
	if _, perr := time.Parse(time.RFC3339Nano, rec.UpdatedAt); perr != nil {
		t.Errorf("stamped UpdatedAt not RFC3339Nano: %q (%v)", rec.UpdatedAt, perr)
	}
}

func TestUpdateStampsUpdatedAt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	if err := s.Update("m-1", func(r *Record) { r.Manual = true; r.Name = "n" }); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("m-1")
	if got.UpdatedAt == "" {
		t.Fatal("Update must stamp UpdatedAt")
	}
	ts, perr := time.Parse(time.RFC3339Nano, got.UpdatedAt)
	if perr != nil {
		t.Fatalf("UpdatedAt not RFC3339Nano: %q (%v)", got.UpdatedAt, perr)
	}
	if time.Since(ts) > time.Minute {
		t.Errorf("stamp not recent: %v", ts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run "UpdatedAt" -v`
Expected: FAIL - compile error `rec.UpdatedAt undefined (type Record has no field or method UpdatedAt)`.

- [ ] **Step 3: Add the field**

In `internal/store/store.go`, add the field to `Record` (after `Tasks`):

```go
type Record struct {
	Manual    bool     `json:"manual"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Priority  int      `json:"priority"`
	Deadline  string   `json:"deadline"`
	Notes     string   `json:"notes"`
	Tags      []string `json:"tags"`
	Tasks     []Task   `json:"tasks"`
	UpdatedAt string   `json:"updatedAt"`
}
```

- [ ] **Step 4: Add the `time` import and stamp in `Update` + `migrate`**

In `internal/store/store.go` add `"time"` to the import block. Replace the body of `Update` so it stamps after `fn`:

```go
func (s *Store) Update(id string, fn func(*Record)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := cloneRecord(s.records[id])
	fn(&rec)
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.records[id] = rec
	return s.saveLocked()
}
```

Replace `migrate` to backfill an empty `UpdatedAt` once at load (keep the existing task-status loop unchanged):

```go
func migrate(recs map[string]Record) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for k, record := range recs {
		for i, t := range record.Tasks {
			if t.Status == "" {
				if t.Done {
					t.Status = "done"
				} else {
					t.Status = "todo"
				}
			}
			t.Done = t.Status == "done"
			record.Tasks[i] = t
		}
		if record.UpdatedAt == "" {
			record.UpdatedAt = now
		}
		recs[k] = record
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS (all store tests, including the two new ones).

- [ ] **Step 6: Verify formatting/vet**

Run: `gofmt -l internal/store/store.go` (expect no output) and `go vet ./internal/store/` (expect no output).

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): Record에 UpdatedAt 추가, Update 중앙 스탬프와 마이그레이션"
```

---

### Task 2: git.RemoteURL + git.NormalizeRemote

**Files:**
- Create: `internal/git/remote.go`
- Test: `internal/git/remote_test.go`

**Interfaces:**
- Consumes: `git.Runner` (`Run(dir string, args ...string) (string, error)`) from `internal/git/runner.go`.
- Produces:
  - `func NormalizeRemote(remote string) string` - strip scheme+creds, lowercase host+path, strip trailing `.git`.
  - `func RemoteURL(r Runner, dir string) (string, error)` - runs `git remote get-url origin`, trims.

- [ ] **Step 1: Write the failing tests**

Create `internal/git/remote_test.go`:

```go
package git

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Owner/Repo.git":               "github.com/owner/repo",
		"https://github.com/Owner/Repo.git":           "github.com/owner/repo",
		"https://github.com/Owner/Repo":               "github.com/owner/repo",
		"https://user:pass@github.com/Owner/Repo.git": "github.com/owner/repo",
		"ssh://git@github.com/Owner/Repo.git":         "github.com/owner/repo",
		"":                                            "",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteURL(t *testing.T) {
	f := &opFake{out: map[string]string{
		"remote get-url": "git@github.com:o/r.git\n",
	}}
	got, err := RemoteURL(f, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if got != "git@github.com:o/r.git" {
		t.Errorf("RemoteURL = %q", got)
	}
}
```

(`opFake` is defined in `internal/git/ops_test.go` in the same package; its key for `remote get-url origin` is `args[0]+" "+args[1]` = `"remote get-url"`, which matches the test's map key.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run "NormalizeRemote|RemoteURL" -v`
Expected: FAIL - `undefined: NormalizeRemote` / `undefined: RemoteURL`.

- [ ] **Step 3: Implement**

Create `internal/git/remote.go`:

```go
package git

import "strings"

// NormalizeRemote reduces a git remote URL to a stable, machine-independent
// identity: scheme and credentials removed, host+path lowercased, trailing
// ".git" stripped. It is the basis of a code project's sync doc_id.
func NormalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@"):
		rest := strings.TrimPrefix(remote, "git@")
		rest = strings.Replace(rest, ":", "/", 1) // host:owner/repo -> host/owner/repo
		remote = rest
	case strings.HasPrefix(remote, "ssh://"):
		rest := strings.TrimPrefix(remote, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:] // strip user@ credentials
		}
		remote = rest
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		rest := remote[strings.Index(remote, "://")+3:]
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:] // strip user:pass@ credentials
		}
		remote = rest
	}
	return strings.ToLower(remote)
}

// RemoteURL returns the raw origin remote URL for a repo, or an error when the
// repo has no origin (or is not a git repo). Uses the standard Runner seam.
func RemoteURL(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: PASS (all git tests, including the two new ones).

- [ ] **Step 5: Verify formatting/vet**

Run: `gofmt -l internal/git/remote.go` (no output) and `go vet ./internal/git/` (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/git/remote.go internal/git/remote_test.go
git commit -m "feat(git): RemoteURL 헬퍼와 NormalizeRemote 순수 함수 추가"
```

---

### Task 3: cloud package - contract types + Client methods

**Files:**
- Create: `internal/cloud/cloud.go`
- Test: `internal/cloud/cloud_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (exact contract):
  - `type Doc struct { Kind string ` + "`json:\"kind\"`" + `; DocID string ` + "`json:\"doc_id\"`" + `; Payload json.RawMessage ` + "`json:\"payload\"`" + `; UpdatedAt string ` + "`json:\"updated_at\"`" + `; Deleted bool ` + "`json:\"deleted\"`" + `; Version int64 ` + "`json:\"version\"`" + ` }`
  - `type Tokens struct { Access string; Refresh string }`
  - `type User struct { ID string; Login string; AvatarURL string }`
  - `type PushResult struct { DocID string ` + "`json:\"doc_id\"`" + `; Kind string ` + "`json:\"kind\"`" + `; Accepted bool ` + "`json:\"accepted\"`" + `; Version int64 ` + "`json:\"version\"`" + ` }`
  - `type Client struct { BaseURL string; HTTP *http.Client }`
  - `var ErrUnauthorized error`
  - `func New(baseURL string) *Client`
  - `func (c *Client) Exchange(linkCode, verifier string) (Tokens, User, error)`
  - `func (c *Client) Refresh(refresh string) (Tokens, error)`
  - `func (c *Client) Logout(refresh string) error`
  - `func (c *Client) Pull(since int64, access string) ([]Doc, int64, error)`
  - `func (c *Client) Push(docs []Doc, access string) ([]PushResult, int64, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cloud/cloud_test.go`:

```go
package cloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushThenPull(t *testing.T) {
	var stored []Doc
	var cur int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Docs []Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			var results []PushResult
			for _, d := range body.Docs {
				cur++
				d.Version = cur
				stored = append(stored, d)
				results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: cur})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "cursor": cur})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": stored, "cursor": cur})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	res, cursor, err := c.Push([]Doc{{Kind: "project", DocID: "m-1", Payload: json.RawMessage(`{"name":"a"}`), UpdatedAt: "2026-07-09T00:00:00Z"}}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("push result: %+v cursor=%d", res, cursor)
	}
	docs, cur2, err := c.Pull(0, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].DocID != "m-1" || cur2 != 1 {
		t.Fatalf("pull docs=%+v cursor=%d", docs, cur2)
	}
}

func TestPullUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()
	c := New(ts.URL)
	if _, _, err := c.Pull(0, "bad"); err != ErrUnauthorized {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestExchangeParsesUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "\"link_code\":\"lc\"") || !strings.Contains(string(b), "\"code_verifier\":\"ver\"") {
			t.Errorf("bad exchange body: %s", b)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "acc",
			"refresh_token": "ref",
			"user":          map[string]any{"id": "u1", "login": "octo", "avatar_url": "http://a/x.png"},
		})
	}))
	defer ts.Close()
	c := New(ts.URL)
	tok, user, err := c.Exchange("lc", "ver")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "acc" || tok.Refresh != "ref" {
		t.Errorf("tokens: %+v", tok)
	}
	if user.ID != "u1" || user.Login != "octo" || user.AvatarURL != "http://a/x.png" {
		t.Errorf("user: %+v", user)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cloud/ -v`
Expected: FAIL - package/`New` undefined (build error).

- [ ] **Step 3: Implement the package**

Create `internal/cloud/cloud.go`:

```go
// Package cloud is fleet's client for the v0 backend spine: GitHub-native auth
// (exchange/refresh/logout) and per-user document sync (pull/push). It is
// stdlib-only and Wails-free so the sync engine can use it without pulling in
// the desktop UI.
package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Doc is one synced document (v0 kind is always "project").
type Doc struct {
	Kind      string          `json:"kind"`
	DocID     string          `json:"doc_id"`
	Payload   json.RawMessage `json:"payload"`
	UpdatedAt string          `json:"updated_at"`
	Deleted   bool            `json:"deleted"`
	Version   int64           `json:"version"`
}

// Tokens is a fleet session token pair.
type Tokens struct {
	Access  string
	Refresh string
}

// User is the signed-in account identity.
type User struct {
	ID        string
	Login     string
	AvatarURL string
}

// PushResult is the server's per-doc verdict for a push.
type PushResult struct {
	DocID    string `json:"doc_id"`
	Kind     string `json:"kind"`
	Accepted bool   `json:"accepted"`
	Version  int64  `json:"version"`
}

// Client talks to the backend over JSON+HTTPS.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// ErrUnauthorized is returned by Pull/Push when the server responds 401, so a
// Session can refresh the access token and retry.
var ErrUnauthorized = errors.New("cloud: unauthorized")

// New builds a Client for baseURL with a sane timeout.
func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Exchange trades a one-time link_code + PKCE verifier for session tokens and
// the account identity.
func (c *Client) Exchange(linkCode, verifier string) (Tokens, User, error) {
	body, err := json.Marshal(map[string]string{"link_code": linkCode, "code_verifier": verifier})
	if err != nil {
		return Tokens{}, User{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/exchange", bytes.NewReader(body))
	if err != nil {
		return Tokens{}, User{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Tokens{}, User{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, User{}, fmt.Errorf("exchange: status %d", resp.StatusCode)
	}
	var out struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		User    struct {
			ID        string `json:"id"`
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Tokens{}, User{}, err
	}
	return Tokens{Access: out.Access, Refresh: out.Refresh},
		User{ID: out.User.ID, Login: out.User.Login, AvatarURL: out.User.AvatarURL}, nil
}

// Refresh rotates the refresh token and returns a fresh pair.
func (c *Client) Refresh(refresh string) (Tokens, error) {
	body, err := json.Marshal(map[string]string{"refresh_token": refresh})
	if err != nil {
		return Tokens{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, fmt.Errorf("refresh: status %d", resp.StatusCode)
	}
	var out struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: out.Access, Refresh: out.Refresh}, nil
}

// Logout revokes the refresh token (best effort; a 204 is success).
func (c *Client) Logout(refresh string) error {
	body, err := json.Marshal(map[string]string{"refresh_token": refresh})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/logout", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("logout: status %d", resp.StatusCode)
	}
	return nil
}

// Pull returns documents with version > since plus the new cursor.
func (c *Client) Pull(since int64, access string) ([]Doc, int64, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/sync?since="+strconv.FormatInt(since, 10), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, 0, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("pull: status %d", resp.StatusCode)
	}
	var out struct {
		Docs   []Doc `json:"docs"`
		Cursor int64 `json:"cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	return out.Docs, out.Cursor, nil
}

// Push uploads docs and returns per-doc results plus the new cursor.
func (c *Client) Push(docs []Doc, access string) ([]PushResult, int64, error) {
	body, err := json.Marshal(struct {
		Docs []Doc `json:"docs"`
	}{Docs: docs})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/sync", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, 0, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("push: status %d", resp.StatusCode)
	}
	var out struct {
		Results []PushResult `json:"results"`
		Cursor  int64        `json:"cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	return out.Results, out.Cursor, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cloud/ -v`
Expected: PASS (`TestPushThenPull`, `TestPullUnauthorized`, `TestExchangeParsesUser`).

- [ ] **Step 5: Verify formatting/vet**

Run: `gofmt -l internal/cloud/cloud.go` (no output) and `go vet ./internal/cloud/` (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/cloud/cloud.go internal/cloud/cloud_test.go
git commit -m "feat(cloud): 계약 타입과 Client(exchange/refresh/logout/pull/push) 구현"
```

---

### Task 4: cloud.Session - refresh-on-401 wrapper

**Files:**
- Create: `internal/cloud/session.go`
- Test: `internal/cloud/session_test.go`

**Interfaces:**
- Consumes: `*Client`, `ErrUnauthorized`, `Tokens`, `Doc` from Task 3.
- Produces:
  - `type Session struct { Client *Client; ... }`
  - `func NewSession(c *Client, access, refresh string, onRotate func(Tokens)) *Session`
  - `func (s *Session) Access() string`
  - `func (s *Session) WithAccess(fn func(access string) error) error` - runs `fn` with the current access token; on `ErrUnauthorized`, refreshes (rotating tokens and invoking `onRotate`) and retries `fn` exactly once.

- [ ] **Step 1: Write the failing test**

Create `internal/cloud/session_test.go`:

```go
package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionRefreshesOn401(t *testing.T) {
	var rotated Tokens
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync":
			if r.Header.Get("Authorization") != "Bearer new" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []Doc{}, "cursor": 0})
		case "/auth/refresh":
			var body struct {
				Refresh string `json:"refresh_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Refresh != "r0" {
				t.Errorf("refresh sent %q, want r0", body.Refresh)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new", "refresh_token": "r1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	s := NewSession(c, "old", "r0", func(t Tokens) { rotated = t })

	calls := 0
	err := s.WithAccess(func(access string) error {
		calls++
		_, _, e := c.Pull(0, access)
		return e
	})
	if err != nil {
		t.Fatalf("WithAccess err: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected fn called twice (401 then retry), got %d", calls)
	}
	if rotated.Access != "new" || rotated.Refresh != "r1" {
		t.Errorf("onRotate got %+v", rotated)
	}
	if s.Access() != "new" {
		t.Errorf("session access not updated: %q", s.Access())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cloud/ -run TestSessionRefreshesOn401 -v`
Expected: FAIL - `undefined: NewSession`.

- [ ] **Step 3: Implement**

Create `internal/cloud/session.go`:

```go
package cloud

import (
	"errors"
	"sync"
)

// Session holds the in-memory access token and the (rotating) refresh token and
// transparently refreshes on a 401. The refresh token is never written to disk
// by this type; onRotate lets the caller persist it (e.g. to the OS keychain).
type Session struct {
	Client   *Client
	mu       sync.Mutex
	access   string
	refresh  string
	onRotate func(Tokens)
}

// NewSession builds a Session. onRotate may be nil.
func NewSession(c *Client, access, refresh string, onRotate func(Tokens)) *Session {
	return &Session{Client: c, access: access, refresh: refresh, onRotate: onRotate}
}

// Access returns the current access token.
func (s *Session) Access() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access
}

// WithAccess runs fn with the current access token. If fn returns
// ErrUnauthorized, it refreshes the token pair (invoking onRotate with the new
// tokens) and retries fn exactly once. Any other error is returned as-is.
func (s *Session) WithAccess(fn func(access string) error) error {
	s.mu.Lock()
	access := s.access
	s.mu.Unlock()

	err := fn(access)
	if !errors.Is(err, ErrUnauthorized) {
		return err
	}

	s.mu.Lock()
	refresh := s.refresh
	s.mu.Unlock()

	tok, rerr := s.Client.Refresh(refresh)
	if rerr != nil {
		return rerr
	}

	s.mu.Lock()
	s.access = tok.Access
	s.refresh = tok.Refresh
	s.mu.Unlock()
	if s.onRotate != nil {
		s.onRotate(tok)
	}
	return fn(tok.Access)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cloud/ -run TestSessionRefreshesOn401 -v`
Expected: PASS.

- [ ] **Step 5: Verify formatting/vet**

Run: `gofmt -l internal/cloud/session.go` (no output) and `go vet ./internal/cloud/` (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/cloud/session.go internal/cloud/session_test.go
git commit -m "feat(cloud): 401 시 자동 리프레시하는 Session 래퍼 추가"
```

---

### Task 5: cloud.CredStore - keychain refresh-token storage

**Files:**
- Create: `internal/cloud/credstore.go`
- Test: `internal/cloud/credstore_test.go`
- Modify: `go.mod`, `go.sum` (add `github.com/zalando/go-keyring`)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type CredStore interface { SaveRefresh(token string) error; LoadRefresh() (string, error); DeleteRefresh() error }`
  - `type KeyringStore struct { Service string; User string }` implementing `CredStore` via the OS keychain (missing entry -> `("", nil)`).
  - `type MemCredStore struct { ... }` - an in-memory `CredStore` for tests/other packages.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/zalando/go-keyring@v0.2.6`
Expected: `go.mod` gains `github.com/zalando/go-keyring v0.2.6` in the require block (it may land as `// indirect` until first import; Step 3 imports it).

- [ ] **Step 2: Write the failing test**

Create `internal/cloud/credstore_test.go`:

```go
package cloud

import "testing"

// Compile-time guarantee the keychain impl satisfies the interface.
var _ CredStore = KeyringStore{}
var _ CredStore = (*MemCredStore)(nil)

func TestMemCredStoreRoundTrip(t *testing.T) {
	var s MemCredStore
	if got, err := s.LoadRefresh(); err != nil || got != "" {
		t.Fatalf("empty load: %q %v", got, err)
	}
	if err := s.SaveRefresh("r1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRefresh(); got != "r1" {
		t.Fatalf("load after save = %q", got)
	}
	if err := s.DeleteRefresh(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRefresh(); got != "" {
		t.Fatalf("load after delete = %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/cloud/ -run TestMemCredStoreRoundTrip -v`
Expected: FAIL - `undefined: CredStore` / `undefined: KeyringStore`.

- [ ] **Step 4: Implement**

Create `internal/cloud/credstore.go`:

```go
package cloud

import (
	"sync"

	keyring "github.com/zalando/go-keyring"
)

// CredStore persists the long-lived refresh token. The access token stays in
// process memory only and is never given to a CredStore.
type CredStore interface {
	SaveRefresh(token string) error
	LoadRefresh() (string, error)
	DeleteRefresh() error
}

// KeyringStore stores the refresh token in the OS credential store (Windows
// Credential Manager). A missing entry reads back as an empty string with no
// error, so callers can treat "no token" and "signed out" uniformly.
type KeyringStore struct {
	Service string
	User    string
}

// SaveRefresh writes token to the OS keychain.
func (k KeyringStore) SaveRefresh(token string) error {
	return keyring.Set(k.Service, k.User, token)
}

// LoadRefresh reads the token; a missing entry yields ("", nil).
func (k KeyringStore) LoadRefresh() (string, error) {
	v, err := keyring.Get(k.Service, k.User)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// DeleteRefresh removes the token; a missing entry is not an error.
func (k KeyringStore) DeleteRefresh() error {
	err := keyring.Delete(k.Service, k.User)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// MemCredStore is an in-memory CredStore for tests and headless use.
type MemCredStore struct {
	mu    sync.Mutex
	token string
}

// SaveRefresh stores token in memory.
func (m *MemCredStore) SaveRefresh(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = token
	return nil
}

// LoadRefresh returns the in-memory token ("" when unset).
func (m *MemCredStore) LoadRefresh() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token, nil
}

// DeleteRefresh clears the in-memory token.
func (m *MemCredStore) DeleteRefresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = ""
	return nil
}
```

- [ ] **Step 5: Tidy modules and run test to verify it passes**

Run: `go mod tidy` then `go test ./internal/cloud/ -v`
Expected: `go.mod` shows `github.com/zalando/go-keyring v0.2.6` as a direct require; all cloud tests PASS.

- [ ] **Step 6: Verify formatting/vet**

Run: `gofmt -l internal/cloud/credstore.go` (no output) and `go vet ./internal/cloud/` (no output).

- [ ] **Step 7: Commit**

```bash
git add internal/cloud/credstore.go internal/cloud/credstore_test.go go.mod go.sum
git commit -m "feat(cloud): CredStore 인터페이스와 go-keyring 자격증명 저장 구현"
```

---

### Task 6: syncengine - state persistence, doc_id, hashing, backoff (pure)

**Files:**
- Create: `internal/syncengine/state.go`
- Test: `internal/syncengine/state_test.go`

**Interfaces:**
- Consumes: `store.Record` (Task 1), `git.NormalizeRemote` (Task 2).
- Produces:
  - `type State struct { Cursor int64 ` + "`json:\"cursor\"`" + `; Docs map[string]DocState ` + "`json:\"docs\"`" + ` }`
  - `type DocState struct { LocalID string ` + "`json:\"localId\"`" + `; Hash string ` + "`json:\"hash\"`" + `; UpdatedAt string ` + "`json:\"updatedAt\"`" + `; Deleted bool ` + "`json:\"deleted\"`" + ` }`
  - `func loadState(path string) (State, error)` / `func saveState(path string, s State) error`
  - `func DocID(localID string, rec store.Record, remote string) string`
  - `func payloadHash(b []byte) string` / `func shortHash(s string) string` / `func newer(a, b string) bool`
  - `func NextBackoff(cur, base, max time.Duration) time.Duration`

- [ ] **Step 1: Write the failing tests**

Create `internal/syncengine/state_test.go`:

```go
package syncengine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/store"
)

func TestDocID(t *testing.T) {
	if got := DocID("m-9", store.Record{Manual: true}, ""); got != "m-9" {
		t.Errorf("manual doc_id = %q, want m-9", got)
	}
	got := DocID("C:/repos/app", store.Record{Manual: false}, "git@github.com:O/App.git")
	if got != "git:github.com/o/app" {
		t.Errorf("code doc_id = %q, want git:github.com/o/app", got)
	}
	noRemote := DocID("C:/repos/app", store.Record{Manual: false}, "")
	if noRemote[:6] != "local:" || len(noRemote) != 6+12 {
		t.Errorf("no-remote doc_id = %q, want local:<12hex>", noRemote)
	}
}

func TestNewer(t *testing.T) {
	a := "2026-07-09T00:00:01Z"
	b := "2026-07-09T00:00:00Z"
	if !newer(a, b) {
		t.Error("a should be newer than b")
	}
	if newer(b, a) {
		t.Error("b should not be newer than a")
	}
	if !newer(a, "") {
		t.Error("any time is newer than empty")
	}
	if newer("", "") {
		t.Error("empty is not newer than empty")
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sync.json")
	got, err := loadState(p) // missing file
	if err != nil || got.Cursor != 0 || got.Docs == nil {
		t.Fatalf("missing load: %+v %v", got, err)
	}
	got.Cursor = 7
	got.Docs["m-1"] = DocState{LocalID: "m-1", Hash: "h", UpdatedAt: "u", Deleted: false}
	if err := saveState(p, got); err != nil {
		t.Fatal(err)
	}
	back, err := loadState(p)
	if err != nil || back.Cursor != 7 || back.Docs["m-1"].Hash != "h" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}

func TestNextBackoff(t *testing.T) {
	base, max := 5*time.Second, time.Minute
	if got := NextBackoff(0, base, max); got != base {
		t.Errorf("from 0 -> %v, want %v", got, base)
	}
	if got := NextBackoff(10*time.Second, base, max); got != 20*time.Second {
		t.Errorf("doubling -> %v, want 20s", got)
	}
	if got := NextBackoff(40*time.Second, base, max); got != max {
		t.Errorf("cap -> %v, want %v", got, max)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/syncengine/ -v`
Expected: FAIL - package does not compile (`undefined: DocID`).

- [ ] **Step 3: Implement**

Create `internal/syncengine/state.go`:

```go
// Package syncengine runs fleet's offline-first PM sync: it derives stable
// document ids, tracks dirty documents against a local sync.json, and applies
// last-write-wins on pull. It is Wails-free and depends only on internal/store,
// internal/cloud, and internal/git.
package syncengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/store"
)

// State is the persisted sync bookkeeping (sync.json), keyed by doc_id.
type State struct {
	Cursor int64               `json:"cursor"`
	Docs   map[string]DocState `json:"docs"`
}

// DocState records what the engine last synced for one doc_id.
type DocState struct {
	LocalID   string `json:"localId"`
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updatedAt"`
	Deleted   bool   `json:"deleted"`
}

// loadState reads sync.json; a missing file yields an empty (usable) State.
func loadState(path string) (State, error) {
	s := State{Docs: map[string]DocState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{Docs: map[string]DocState{}}, err
	}
	if s.Docs == nil {
		s.Docs = map[string]DocState{}
	}
	return s, nil
}

// saveState writes sync.json atomically (temp file + rename).
func saveState(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DocID derives the sync doc_id for a local record:
//   - manual project -> its opaque local id (already portable)
//   - code project with a remote -> "git:" + NormalizeRemote(remote)
//   - code project with no remote -> "local:" + shortHash(base(localID))
func DocID(localID string, rec store.Record, remote string) string {
	if rec.Manual {
		return localID
	}
	if remote != "" {
		return "git:" + git.NormalizeRemote(remote)
	}
	return "local:" + shortHash(filepath.Base(localID))
}

// payloadHash is the dirty-detection fingerprint of a doc payload.
func payloadHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// shortHash is a 12-hex-char digest, used for no-remote doc ids.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// newer reports whether RFC3339Nano time a is strictly after b. An empty b is
// treated as "older than anything"; an empty/unparsable a is not newer.
func newer(a, b string) bool {
	if b == "" {
		return a != ""
	}
	ta, ea := time.Parse(time.RFC3339Nano, a)
	if ea != nil {
		return false
	}
	tb, eb := time.Parse(time.RFC3339Nano, b)
	if eb != nil {
		return true
	}
	return ta.After(tb)
}

// NextBackoff returns the next capped exponential backoff delay: base when cur
// is below base, otherwise min(cur*2, max).
func NextBackoff(cur, base, max time.Duration) time.Duration {
	if cur < base {
		return base
	}
	n := cur * 2
	if n > max {
		return max
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/syncengine/ -v`
Expected: PASS (`TestDocID`, `TestNewer`, `TestStateRoundTrip`, `TestNextBackoff`).

- [ ] **Step 5: Verify formatting/vet**

Run: `gofmt -l internal/syncengine/state.go` (no output) and `go vet ./internal/syncengine/` (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/syncengine/state.go internal/syncengine/state_test.go
git commit -m "feat(syncengine): 상태 저장, doc_id 파생, 해시, 백오프 순수 함수"
```

---

### Task 7: syncengine.Engine - SyncOnce (push/pull/LWW/persist)

**Files:**
- Create: `internal/syncengine/engine.go`
- Test: `internal/syncengine/engine_test.go`

**Interfaces:**
- Consumes: `*store.Store` (Snapshot/Put/Delete/Get), `*cloud.Client` (Pull/Push), `cloud.Doc`, `cloud.PushResult`, `DocID`, `payloadHash`, `newer`, `loadState`, `saveState` (Task 6).
- Produces:
  - `type Engine struct { ... }`
  - `func New(st *store.Store, client *cloud.Client, statePath string, remoteOf func(path string) string) *Engine`
  - `func (e *Engine) SyncOnce(access string) error`
  - `func (e *Engine) TookRemoteEdit() bool` - returns and clears whether the last sync overwrote a local edit (drives the "updated on another device" toast).

- [ ] **Step 1: Write the failing tests**

Create `internal/syncengine/engine_test.go`:

```go
package syncengine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/store"
)

// fakeSrv is an in-process stand-in for the backend /sync endpoints with the
// same per-user LWW + monotonic version semantics.
type fakeSrv struct {
	mu     sync.Mutex
	docs   map[string]cloud.Doc
	cur    int64
	pushes int
}

func newFake() *fakeSrv { return &fakeSrv{docs: map[string]cloud.Doc{}} }

// keys is a deterministic-free helper to report the server's doc_id set on a
// test failure.
func keys(m map[string]cloud.Doc) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeSrv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		var out []cloud.Doc
		for _, d := range f.docs {
			if d.Version > since {
				out = append(out, d)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
		cursor := since
		for _, d := range out {
			if d.Version > cursor {
				cursor = d.Version
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": out, "cursor": cursor})
	case http.MethodPost:
		f.pushes++
		var body struct {
			Docs []cloud.Doc `json:"docs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var results []cloud.PushResult
		for _, d := range body.Docs {
			stored, ok := f.docs[d.DocID]
			accept := !ok || newer(d.UpdatedAt, stored.UpdatedAt)
			if accept {
				f.cur++
				d.Version = f.cur
				f.docs[d.DocID] = d
				results = append(results, cloud.PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: d.Version})
			} else {
				results = append(results, cloud.PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: false, Version: stored.Version})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "cursor": f.cur})
	}
}

func newEngine(t *testing.T, url string) (*Engine, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	statePath := filepath.Join(dir, "sync.json")
	e := New(st, cloud.New(url), statePath, func(string) string { return "" })
	return e, st, statePath
}

func TestSyncPushesDirtyOnce(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()
	e, st, statePath := newEngine(t, ts.URL)

	_ = st.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "a" })
	if err := e.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if f.pushes != 1 || len(f.docs) != 1 {
		t.Fatalf("first sync: pushes=%d docs=%d", f.pushes, len(f.docs))
	}
	// cursor persisted
	back, _ := loadState(statePath)
	if back.Cursor != 1 {
		t.Errorf("cursor not persisted: %d", back.Cursor)
	}
	// second sync with no local change must not push again
	if err := e.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if f.pushes != 1 {
		t.Errorf("clean re-sync pushed again: pushes=%d", f.pushes)
	}
}

func TestSyncPullAppliesLWW(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	// device A creates and syncs a manual project
	eA, stA, _ := newEngine(t, ts.URL)
	_ = stA.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "fromA" })
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}

	// device B pulls it into an empty store
	eB, stB, _ := newEngine(t, ts.URL)
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	rec, ok := stB.Get("m-1")
	if !ok || rec.Name != "fromA" {
		t.Fatalf("device B did not receive doc: %+v ok=%v", rec, ok)
	}
}

func TestSyncTombstoneDeletes(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	eA, stA, _ := newEngine(t, ts.URL)
	_ = stA.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "x" })
	_ = eA.SyncOnce("tok")

	eB, stB, _ := newEngine(t, ts.URL)
	_ = eB.SyncOnce("tok") // B now has m-1
	if _, ok := stB.Get("m-1"); !ok {
		t.Fatal("precondition: B should have m-1")
	}

	// A deletes locally and syncs -> pushes a tombstone
	_ = stA.Delete("m-1")
	// ensure the tombstone timestamp is strictly newer than the create
	time.Sleep(2 * time.Millisecond)
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}

	// B syncs and must delete locally
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stB.Get("m-1"); ok {
		t.Error("device B still has the deleted doc")
	}
	if !eB.TookRemoteEdit() {
		t.Error("expected remote-edit flag after a tombstone overwrote local")
	}
}

func TestSyncOfflineNoCorruption(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	url := ts.URL
	ts.Close() // server is now unreachable

	e, st, statePath := newEngine(t, url)
	_ = st.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "a" })
	if err := e.SyncOnce("tok"); err == nil {
		t.Fatal("expected an error while offline")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("sync.json must not be written on an offline failure (stat err=%v)", err)
	}
}

// TestSyncDetachedCodeDocNotRepushed guards the "detached record" rule: a pulled
// code-project doc with no local repo on this machine is retained under its own
// doc_id and must NOT, on a subsequent sync, be re-pushed under a fresh
// "local:" id nor tombstoned under its original "git:" id (spec: detached
// records are retained, never dropped or duplicated).
func TestSyncDetachedCodeDocNotRepushed(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	// device A has a code project WITH a git remote and syncs it.
	dirA := t.TempDir()
	stA, _ := store.Open(filepath.Join(dirA, "projects.json"))
	eA := New(stA, cloud.New(ts.URL), filepath.Join(dirA, "sync.json"),
		func(string) string { return "git@github.com:o/app.git" })
	_ = stA.Update("C:/repos/app", func(r *store.Record) { r.Manual = false; r.Name = "app" })
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.docs["git:github.com/o/app"]; !ok {
		t.Fatalf("server missing the git doc: %v", keys(f.docs))
	}

	// device B has NO such repo (remoteOf returns "") and pulls a detached doc.
	dirB := t.TempDir()
	stB, _ := store.Open(filepath.Join(dirB, "projects.json"))
	eB := New(stB, cloud.New(ts.URL), filepath.Join(dirB, "sync.json"),
		func(string) string { return "" })
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stB.Get("git:github.com/o/app"); !ok {
		t.Fatal("device B should hold the detached record under its doc_id")
	}
	pushesAfterFirst := f.pushes
	docsAfterFirst := len(f.docs)

	// a SECOND B sync must not grow the server's doc set nor re-push.
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != docsAfterFirst {
		t.Errorf("detached doc grew server doc set: %d -> %d (%v)", docsAfterFirst, len(f.docs), keys(f.docs))
	}
	if f.pushes != pushesAfterFirst {
		t.Errorf("second B sync re-pushed the detached doc: pushes %d -> %d", pushesAfterFirst, f.pushes)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/syncengine/ -run "TestSync" -v`
Expected: FAIL - `undefined: New` / `undefined: (*Engine).SyncOnce`.

- [ ] **Step 3: Implement**

Create `internal/syncengine/engine.go`:

```go
package syncengine

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/store"
)

// Engine syncs the local PM store against the backend. All local domain logic
// stays here; the server is a dumb versioned document store.
type Engine struct {
	store     *store.Store
	client    *cloud.Client
	statePath string
	remoteOf  func(path string) string

	mu           sync.Mutex
	state        State
	loaded       bool
	lastConflict bool
}

// New builds an Engine. remoteOf resolves a code project's git remote URL from
// its local id (repo path); return "" when there is no remote.
func New(st *store.Store, client *cloud.Client, statePath string, remoteOf func(string) string) *Engine {
	return &Engine{
		store:     st,
		client:    client,
		statePath: statePath,
		remoteOf:  remoteOf,
		state:     State{Docs: map[string]DocState{}},
	}
}

// TookRemoteEdit returns (and clears) whether the last sync applied a remote
// change over an existing local edit.
func (e *Engine) TookRemoteEdit() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.lastConflict
	e.lastConflict = false
	return v
}

// SyncOnce performs one push/pull cycle: push dirty docs (and tombstones for
// locally deleted docs), pull since the cursor, apply LWW, then persist state.
// A network error returns without writing sync.json, so local data is never
// corrupted by a failed sync.
func (e *Engine) SyncOnce(access string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.loaded {
		st, err := loadState(e.statePath)
		if err != nil {
			return err
		}
		e.state = st
		e.loaded = true
	}

	snap := e.store.Snapshot()

	// Build local docs; a doc is dirty when its payload hash changed.
	var dirty []cloud.Doc
	live := map[string]bool{}
	for localID, rec := range snap {
		remote := ""
		if !rec.Manual && e.remoteOf != nil {
			remote = e.remoteOf(localID)
		}
		id := DocID(localID, rec, remote)
		// A detached record (a pulled code-project doc with no local repo on
		// this machine) is stored under its own doc_id as the local key. Keep
		// that identity instead of re-deriving one, so it is neither re-pushed
		// under a fresh "local:" id nor wrongly tombstoned under its original
		// id - the spec requires detached records be retained, never dropped
		// or duplicated.
		if ds, ok := e.state.Docs[localID]; ok && ds.LocalID == localID {
			id = localID
		}
		live[id] = true
		payload, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		h := payloadHash(payload)
		prev, ok := e.state.Docs[id]
		if ok && prev.Hash == h && !prev.Deleted {
			continue
		}
		dirty = append(dirty, cloud.Doc{Kind: "project", DocID: id, Payload: payload, UpdatedAt: rec.UpdatedAt, Deleted: false})
		prev.LocalID = localID // remember mapping for accepted pushes
		e.state.Docs[id] = prev
	}

	// Tombstones: docs we tracked that have vanished from the store.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for id, ds := range e.state.Docs {
		if live[id] || ds.Deleted {
			continue
		}
		dirty = append(dirty, cloud.Doc{Kind: "project", DocID: id, Payload: json.RawMessage("{}"), UpdatedAt: now, Deleted: true})
	}

	if len(dirty) > 0 {
		results, cursor, err := e.client.Push(dirty, access)
		if err != nil {
			return err
		}
		byID := make(map[string]cloud.Doc, len(dirty))
		for _, d := range dirty {
			byID[d.DocID] = d
		}
		for _, r := range results {
			if !r.Accepted {
				continue // stale push; the pull below reconciles it
			}
			d := byID[r.DocID]
			ds := e.state.Docs[r.DocID]
			ds.Hash = payloadHash(d.Payload)
			ds.UpdatedAt = d.UpdatedAt
			ds.Deleted = d.Deleted
			e.state.Docs[r.DocID] = ds
		}
		if cursor > e.state.Cursor {
			e.state.Cursor = cursor
		}
	}

	docs, cursor, err := e.client.Pull(e.state.Cursor, access)
	if err != nil {
		return err
	}
	for _, d := range docs {
		localID := e.localIDForDoc(d)
		localUpdated := ""
		if rec, ok := snap[localID]; ok {
			localUpdated = rec.UpdatedAt
		} else if ds, ok := e.state.Docs[d.DocID]; ok {
			localUpdated = ds.UpdatedAt
		}
		if !newer(d.UpdatedAt, localUpdated) {
			continue // local is newer or equal: LWW keeps local
		}
		if d.Deleted {
			if err := e.store.Delete(localID); err != nil {
				return err
			}
		} else {
			var rec store.Record
			if err := json.Unmarshal(d.Payload, &rec); err != nil {
				return err
			}
			if err := e.store.Put(localID, rec); err != nil {
				return err
			}
		}
		if localUpdated != "" {
			e.lastConflict = true // overwrote an existing local edit
		}
		e.state.Docs[d.DocID] = DocState{LocalID: localID, Hash: payloadHash(d.Payload), UpdatedAt: d.UpdatedAt, Deleted: d.Deleted}
	}
	if cursor > e.state.Cursor {
		e.state.Cursor = cursor
	}

	return saveState(e.statePath, e.state)
}

// localIDForDoc maps a doc_id back to a local store key. A known mapping wins;
// a manual doc_id is itself the local id; an unmatched code doc is retained
// (detached) under its doc_id until a scan reconciles it.
func (e *Engine) localIDForDoc(d cloud.Doc) string {
	if ds, ok := e.state.Docs[d.DocID]; ok && ds.LocalID != "" {
		return ds.LocalID
	}
	if !strings.HasPrefix(d.DocID, "git:") && !strings.HasPrefix(d.DocID, "local:") {
		return d.DocID
	}
	return d.DocID
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/syncengine/ -v`
Expected: PASS (`TestSyncPushesDirtyOnce`, `TestSyncPullAppliesLWW`, `TestSyncTombstoneDeletes`, `TestSyncOfflineNoCorruption`, `TestSyncDetachedCodeDocNotRepushed`, plus Task 6's pure tests).

- [ ] **Step 5: Verify formatting/vet**

Run: `gofmt -l internal/syncengine/engine.go` (no output) and `go vet ./internal/syncengine/` (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/syncengine/engine.go internal/syncengine/engine_test.go
git commit -m "feat(syncengine): SyncOnce 푸시/풀/LWW/영속화와 오프라인 안전성"
```

---

### Task 8: app.go Wails bindings + background sync loop

**Files:**
- Create: `authsync.go` (package `main`)
- Modify: `app.go` (imports, `App` struct fields, `NewApp`, `startup`)

**Interfaces:**
- Consumes: `cloud.New`, `cloud.KeyringStore`, `cloud.Session`, `cloud.NewSession`, `cloud.Tokens`, `cloud.User`, `cloud.CredStore`, `cloud.Client` (Tasks 3-5); `syncengine.New`, `(*Engine).SyncOnce`, `(*Engine).TookRemoteEdit`, `syncengine.NextBackoff` (Tasks 6-7); `git.RemoteURL`, `git.ExecRunner` (Task 2); `wruntime.EventsEmit`, `wruntime.BrowserOpenURL`.
- Produces (bound methods + view types, exact contract):
  - `type AuthStatusView struct { SignedIn bool ` + "`json:\"signedIn\"`" + `; Login string ` + "`json:\"login\"`" + `; AvatarURL string ` + "`json:\"avatarUrl\"`" + ` }`
  - `type SyncStateView struct { State string ` + "`json:\"state\"`" + `; LastSyncedUnix int64 ` + "`json:\"lastSyncedUnix\"`" + `; Error string ` + "`json:\"error\"`" + ` }`
  - `func (a *App) AuthStart() string`
  - `func (a *App) AuthStatus() AuthStatusView`
  - `func (a *App) SignOut() string`
  - `func (a *App) SyncNow() string`
  - `func (a *App) SyncState() SyncStateView`
  - Events emitted: `auth:changed` (AuthStatusView), `sync:changed` (SyncStateView), `sync:remoteEdit` (nil).

- [ ] **Step 1: Add imports and struct fields to app.go**

In `app.go`, add to the import block (alongside the existing `internal/*` imports):

```go
	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/syncengine"
```

Add these fields to the `App` struct (immediately after `aiGen int`):

```go
	// cloud sync + auth
	cloudClient *cloud.Client
	creds       cloud.CredStore
	engine      *syncengine.Engine
	authMu      sync.Mutex
	access      string
	user        cloud.User
	signedIn    bool
	session     *cloud.Session
	syncMu      sync.Mutex
	syncView    SyncStateView
	syncTrigger chan struct{}
```

(`sync` is already imported by `app.go`, so `sync.Mutex` needs no new import.)

- [ ] **Step 2: Replace NewApp to wire the cloud client, creds, and engine**

Replace the whole `NewApp` function in `app.go` with:

```go
// NewApp builds the App with the real git runner, loaded config, and the cloud
// sync stack (API client, keychain-backed credential store, sync engine).
func NewApp() *App {
	cfg, cfgPath, _ := config.Load()
	dir := filepath.Dir(cfgPath)
	storePath := filepath.Join(dir, "projects.json")
	st, _ := store.Open(storePath) // empty store on error; UI still works
	edgesPath := filepath.Join(dir, "edges.json")
	ed, _ := edges.Open(edgesPath) // empty store on error; UI still works

	cl := cloud.New(apiURL())
	creds := cloud.KeyringStore{Service: "fleet", User: "refresh"}
	syncPath := filepath.Join(dir, "sync.json")
	eng := syncengine.New(st, cl, syncPath, func(path string) string {
		u, _ := git.RemoteURL(git.ExecRunner{}, path)
		return u
	})

	return &App{
		cfg: cfg, runner: git.ExecRunner{}, store: st,
		ghRunner: gh.ExecRunner{}, ghCache: map[string]ghEntry{}, edges: ed,
		symCache: map[string]symEntry{},
		aiRunner: ai.New(cfg.AIProvider, cfg.AIModel, cfg.OpenAIKey, cfg.GeminiKey),
		cloudClient: cl, creds: creds, engine: eng,
		syncTrigger: make(chan struct{}, 1),
		syncView:    SyncStateView{State: "signedout"},
	}
}
```

- [ ] **Step 3: Replace startup to launch the sync stack**

Replace the `startup` method in `app.go` with:

```go
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startSync(ctx)
}
```

- [ ] **Step 4: Create authsync.go with the bindings and loop**

Create `authsync.go`:

```go
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/syncengine"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// defaultAPIURL is the production backend; FLEET_API_URL overrides it in dev.
const defaultAPIURL = "https://fleet-api.fly.dev"

func apiURL() string {
	if v := os.Getenv("FLEET_API_URL"); v != "" {
		return v
	}
	return defaultAPIURL
}

// AuthStatusView is the JS-facing sign-in state.
type AuthStatusView struct {
	SignedIn  bool   `json:"signedIn"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

// SyncStateView is the JS-facing sync-status pill state. State is one of
// offline, syncing, synced, error, signedout.
type SyncStateView struct {
	State          string `json:"state"`
	LastSyncedUnix int64  `json:"lastSyncedUnix"`
	Error          string `json:"error"`
}

var errNotSignedIn = errors.New("not signed in")

// randB64 returns n random bytes as base64url (no padding).
func randB64(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkce generates a PKCE verifier and its S256 challenge.
func pkce() (verifier, challenge string) {
	verifier = randB64(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// AuthStart runs the RFC 8252 native GitHub OAuth flow: a loopback listener on
// 127.0.0.1:<ephemeral>, PKCE, the system browser, capture of the link_code,
// token exchange, and refresh-token storage in the OS keychain. Returns "" on
// success or an error message.
func (a *App) AuthStart() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "cannot open loopback listener: " + err.Error()
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	verifier, challenge := pkce()
	state := randB64(16)
	login := apiURL() + "/auth/github/login?" + url.Values{
		"state":          {state},
		"code_challenge": {challenge},
		"redirect":       {redirect},
	}.Encode()

	type capture struct {
		code, state string
	}
	ch := make(chan capture, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body style=\"font-family:sans-serif;background:#0e1116;color:#e6edf3;text-align:center;padding-top:80px\"><h2>fleet</h2><p>Signed in. You can close this window.</p></body></html>"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		ch <- capture{code: q.Get("link_code"), state: q.Get("state")}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	wruntime.BrowserOpenURL(a.ctx, login)

	var got capture
	select {
	case got = <-ch:
	case <-time.After(3 * time.Minute):
		return "sign-in timed out"
	}
	if got.state != state {
		return "state mismatch (possible CSRF)"
	}
	if got.code == "" {
		return "no link code returned"
	}

	tokens, user, err := a.cloudClient.Exchange(got.code, verifier)
	if err != nil {
		return "exchange failed: " + err.Error()
	}
	if err := a.creds.SaveRefresh(tokens.Refresh); err != nil {
		return "keychain save failed: " + err.Error()
	}

	a.authMu.Lock()
	a.access = tokens.Access
	a.user = user
	a.signedIn = true
	a.session = cloud.NewSession(a.cloudClient, tokens.Access, tokens.Refresh, func(t cloud.Tokens) {
		_ = a.creds.SaveRefresh(t.Refresh)
	})
	a.authMu.Unlock()

	a.emitAuth()
	a.triggerSync()
	return ""
}

// AuthStatus returns the current sign-in state.
func (a *App) AuthStatus() AuthStatusView {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return AuthStatusView{SignedIn: a.signedIn, Login: a.user.Login, AvatarURL: a.user.AvatarURL}
}

// SignOut revokes the refresh token, clears local session state, and drops the
// pill to signed-out. Local PM data is untouched.
func (a *App) SignOut() string {
	if refresh, _ := a.creds.LoadRefresh(); refresh != "" {
		_ = a.cloudClient.Logout(refresh)
	}
	_ = a.creds.DeleteRefresh()

	a.authMu.Lock()
	a.access = ""
	a.user = cloud.User{}
	a.signedIn = false
	a.session = nil
	a.authMu.Unlock()

	a.emitAuth()
	a.setSyncState("signedout", "")
	return ""
}

// SyncNow runs one sync immediately and returns "" or an error message.
func (a *App) SyncNow() string {
	if err := a.runSync(); err != nil {
		if errors.Is(err, errNotSignedIn) {
			return "not signed in"
		}
		return err.Error()
	}
	return ""
}

// SyncState returns the current pill state.
func (a *App) SyncState() SyncStateView {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	return a.syncView
}

// emitAuth broadcasts the auth state to the frontend.
func (a *App) emitAuth() {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "auth:changed", a.AuthStatus())
}

// setSyncState records and broadcasts the pill state.
func (a *App) setSyncState(state, errText string) {
	a.syncMu.Lock()
	a.syncView.State = state
	a.syncView.Error = errText
	if state == "synced" {
		a.syncView.LastSyncedUnix = time.Now().Unix()
	}
	v := a.syncView
	a.syncMu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "sync:changed", v)
	}
}

// triggerSync nudges the background loop to sync now (non-blocking).
func (a *App) triggerSync() {
	select {
	case a.syncTrigger <- struct{}{}:
	default:
	}
}

// isOffline reports whether err is a network/transport failure (as opposed to
// an application error), so the pill can show Offline rather than Error.
func isOffline(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var ue *url.Error
	return errors.As(err, &ue)
}

// runSync performs one guarded sync and updates the pill state.
func (a *App) runSync() error {
	a.authMu.Lock()
	signedIn := a.signedIn
	sess := a.session
	a.authMu.Unlock()
	if !signedIn || sess == nil {
		return errNotSignedIn
	}
	a.setSyncState("syncing", "")
	if err := sess.WithAccess(a.engine.SyncOnce); err != nil {
		if isOffline(err) {
			a.setSyncState("offline", err.Error())
		} else {
			a.setSyncState("error", err.Error())
		}
		return err
	}
	a.setSyncState("synced", "")
	if a.engine.TookRemoteEdit() && a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "sync:remoteEdit", nil)
	}
	return nil
}

// startSync restores a stored session (silent sign-in) and starts the loop.
func (a *App) startSync(ctx context.Context) {
	go func() {
		if refresh, _ := a.creds.LoadRefresh(); refresh != "" {
			if tok, err := a.cloudClient.Refresh(refresh); err == nil {
				_ = a.creds.SaveRefresh(tok.Refresh)
				a.authMu.Lock()
				a.access = tok.Access
				a.signedIn = true
				a.session = cloud.NewSession(a.cloudClient, tok.Access, tok.Refresh, func(t cloud.Tokens) {
					_ = a.creds.SaveRefresh(t.Refresh)
				})
				a.authMu.Unlock()
				a.emitAuth()
				a.triggerSync()
			}
		}
	}()
	go a.syncLoop(ctx)
}

// syncLoop syncs on an interval, on demand (triggerSync), and retries failures
// with capped exponential backoff.
func (a *App) syncLoop(ctx context.Context) {
	const interval = 60 * time.Second
	base, max := 5*time.Second, 5*time.Minute
	backoff := base
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.syncTrigger:
		case <-timer.C:
		}
		a.authMu.Lock()
		signedIn := a.signedIn
		a.authMu.Unlock()
		if !signedIn {
			timer.Reset(interval)
			continue
		}
		if err := a.runSync(); err != nil && !errors.Is(err, errNotSignedIn) {
			timer.Reset(backoff)
			backoff = NextBackoffDelay(backoff, base, max)
			continue
		}
		backoff = base
		timer.Reset(interval)
	}
}

// NextBackoffDelay is the loop's capped exponential backoff, delegating to the
// tested syncengine helper so the backoff has a single source of truth.
func NextBackoffDelay(cur, base, max time.Duration) time.Duration {
	return syncengine.NextBackoff(cur, base, max)
}
```

- [ ] **Step 5: Build and vet the whole module**

Run: `go build ./... && go vet ./...`
Expected: no output (compiles and vets clean).

- [ ] **Step 6: Run the full Go test suite**

Run: `go test ./...`
Expected: PASS across all packages (no regressions in existing desktop tests).

- [ ] **Step 7: Verify formatting**

Run: `gofmt -l app.go authsync.go`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add app.go authsync.go
git commit -m "feat(app): AuthStart/AuthStatus/SignOut/SyncNow/SyncState 바인딩과 백그라운드 동기화 루프"
```

---

### Task 9: GUI - SignIn, AccountChip, SyncPill wired into the top bar

> Use the frontend-design skill at implementation time for visual craft; the code below is complete and functional and matches the existing dark-theme CSS variables (`--bg`, `--surface`, `--raised`, `--text`, `--muted`, `--faint`, `--border`, `--accent`, `--accent-soft`, `--accent-line`, `--ok`, `--err`, `--err-line`, `--r-pill`, `--r-btn`, `--t`).

**Files:**
- Create: `frontend/src/lib/SyncPill.svelte`
- Create: `frontend/src/lib/AccountChip.svelte`
- Create: `frontend/src/lib/SignIn.svelte`
- Modify: `frontend/src/lib/Toolbar.svelte`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes: bound methods `AuthStart, AuthStatus, SignOut, SyncNow, SyncState` from `../wailsjs/go/main/App`; `EventsOn` from `../wailsjs/runtime/runtime`; `toastError, toastInfo` (already imported by `App.svelte` from `./lib/toasts`).
- Produces: three Svelte components and new Toolbar props (`authSignedIn`, `authLogin`, `authAvatar`, `authBusy`, `syncState`, `onSignIn`, `onSignOut`, `onSyncNow`, `onRetrySync`).

- [ ] **Step 1: Regenerate the Wails bindings for the new Go methods**

Run: `wails generate module`
Expected: `frontend/wailsjs/go/main/App.js` and `App.d.ts` now export `AuthStart`, `AuthStatus`, `SignOut`, `SyncNow`, `SyncState`; `frontend/wailsjs/go/models.ts` gains `main.AuthStatusView` and `main.SyncStateView`.

- [ ] **Step 2: Create SyncPill.svelte**

Create `frontend/src/lib/SyncPill.svelte`:

```svelte
<script lang="ts">
  import { onMount, onDestroy } from "svelte";

  // State: offline | syncing | synced | error | signedout
  export let state: string = "signedout";
  export let lastSyncedUnix: number = 0;
  export let error: string = "";
  export let onRetry: () => void = () => {};
  export let onSignIn: () => void = () => {};

  let now = Date.now();
  let timer: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    timer = setInterval(() => (now = Date.now()), 1000);
  });
  onDestroy(() => timer && clearInterval(timer));

  function ago(unix: number): string {
    if (!unix) return "just now";
    const s = Math.max(0, Math.floor(now / 1000 - unix));
    if (s < 60) return s + "s ago";
    const m = Math.floor(s / 60);
    if (m < 60) return m + "m ago";
    const h = Math.floor(m / 60);
    return h + "h ago";
  }
</script>

<div class="pill pill-{state}" title={error || state}>
  {#if state === "syncing"}
    <span class="spinner"></span><span class="pill-text">Syncing...</span>
  {:else if state === "synced"}
    <span class="dot dot-ok"></span><span class="pill-text">Synced {ago(lastSyncedUnix)}</span>
  {:else if state === "offline"}
    <span class="dot dot-warn"></span><span class="pill-text">Offline</span>
  {:else if state === "error"}
    <span class="dot dot-err"></span><span class="pill-text">Sync error</span>
    <button class="pill-action" on:click={onRetry}>Retry</button>
  {:else}
    <span class="dot dot-idle"></span>
    <button class="pill-action" on:click={onSignIn}>Sign in to sync</button>
  {/if}
</div>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    min-width: 140px; /* stable width: no layout shift across states */
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    background: var(--surface);
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .pill-text { font-variant-numeric: tabular-nums; }
  .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
  .dot-ok { background: var(--ok); }
  .dot-warn { background: var(--muted); }
  .dot-err { background: var(--err); }
  .dot-idle { background: var(--faint); }
  .pill-error { border-color: var(--err-line); color: var(--err); }
  .pill-action {
    font: inherit;
    font-size: 11.5px;
    color: var(--accent);
    background: transparent;
    border: none;
    padding: 0;
    cursor: pointer;
  }
  .pill-action:hover { text-decoration: underline; }
</style>
```

- [ ] **Step 3: Create AccountChip.svelte**

Create `frontend/src/lib/AccountChip.svelte`:

```svelte
<script lang="ts">
  export let login: string = "";
  export let avatarUrl: string = "";
  export let onSignOut: () => void;
  export let onSyncNow: () => void;

  let open = false;
  let imgOk = true;
  function toggle() {
    open = !open;
  }
  function onWindowClick(e: MouseEvent) {
    if (!open) return;
    const t = e.target as HTMLElement;
    if (!t.closest(".acct")) open = false;
  }
  function initial(): string {
    return (login || "?").slice(0, 1).toUpperCase();
  }
</script>

<svelte:window on:click={onWindowClick} />

<div class="acct">
  <button class="acct-btn" on:click|stopPropagation={toggle} aria-expanded={open} title={login || "Account"}>
    {#if avatarUrl && imgOk}
      <img class="acct-av" src={avatarUrl} alt="" on:error={() => (imgOk = false)} />
    {:else}
      <span class="acct-av acct-av-fallback">{initial()}</span>
    {/if}
    <span class="acct-login">{login || "Account"}</span>
    <span class="acct-caret"></span>
  </button>
  {#if open}
    <div class="acct-menu">
      <button class="acct-item" on:click={() => { open = false; onSyncNow(); }}>Sync now</button>
      <div class="acct-div"></div>
      <button class="acct-item" on:click={() => { open = false; onSignOut(); }}>Sign out</button>
    </div>
  {/if}
</div>

<style>
  .acct { position: relative; }
  .acct-btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    padding: 0 8px 0 4px;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .acct-btn:hover { border-color: var(--accent-line); background: var(--accent-soft); }
  .acct-av { width: 20px; height: 20px; border-radius: 50%; flex: none; object-fit: cover; }
  .acct-av-fallback {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11px;
    font-weight: 700;
  }
  .acct-login { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
  .acct-caret {
    width: 0; height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 4px solid var(--muted);
  }
  .acct-menu {
    position: absolute;
    top: 34px;
    right: 0;
    min-width: 140px;
    background: var(--raised);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    padding: 4px;
    z-index: 50;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  }
  .acct-item {
    display: block;
    width: 100%;
    text-align: left;
    font: inherit;
    font-size: 13px;
    color: var(--text);
    background: transparent;
    border: none;
    border-radius: 6px;
    padding: 7px 10px;
    cursor: pointer;
  }
  .acct-item:hover { background: var(--accent-soft); color: var(--accent); }
  .acct-div { height: 1px; background: var(--border); margin: 4px 2px; }
</style>
```

- [ ] **Step 4: Create SignIn.svelte**

Create `frontend/src/lib/SignIn.svelte`:

```svelte
<script lang="ts">
  export let onSignIn: () => void;
  export let busy: boolean = false;
</script>

<button
  class="signin"
  on:click={onSignIn}
  disabled={busy}
  title="Works without signing in; sign in to sync across devices"
>
  {busy ? "Opening browser..." : "Sign in with GitHub"}
</button>

<style>
  .signin {
    display: inline-flex;
    align-items: center;
    height: 28px;
    padding: 0 12px;
    border: 1px solid var(--accent-line);
    border-radius: var(--r-pill);
    background: var(--accent-soft);
    color: var(--accent);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--t), border-color var(--t);
  }
  .signin:hover:not(:disabled) { background: rgba(110, 168, 254, 0.22); }
  .signin:disabled { opacity: 0.6; cursor: default; }
</style>
```

- [ ] **Step 5: Wire the account cluster into Toolbar.svelte**

In `frontend/src/lib/Toolbar.svelte`, add imports + props at the top of the `<script>` block (after the existing `export let` lines is fine; imports must be before use):

```ts
  import SyncPill from "./SyncPill.svelte";
  import AccountChip from "./AccountChip.svelte";
  import SignIn from "./SignIn.svelte";

  export let authSignedIn: boolean = false;
  export let authLogin: string = "";
  export let authAvatar: string = "";
  export let authBusy: boolean = false;
  export let syncState: { state: string; lastSyncedUnix: number; error: string } = {
    state: "signedout",
    lastSyncedUnix: 0,
    error: "",
  };
  export let onSignIn: () => void = () => {};
  export let onSignOut: () => void = () => {};
  export let onSyncNow: () => void = () => {};
  export let onRetrySync: () => void = () => {};
```

Then, inside `<div class="toolbar-right">`, insert the cluster immediately BEFORE the `<!-- Global utility: jump, search, settings - always at the far right -->` comment (i.e. before the palette icon button):

```svelte
    <div class="acct-cluster">
      <SyncPill
        state={syncState.state}
        lastSyncedUnix={syncState.lastSyncedUnix}
        error={syncState.error}
        onRetry={onRetrySync}
        {onSignIn}
      />
      {#if authSignedIn}
        <AccountChip login={authLogin} avatarUrl={authAvatar} {onSignOut} {onSyncNow} />
      {:else}
        <SignIn {onSignIn} busy={authBusy} />
      {/if}
    </div>
    <span class="toolbar-div"></span>
```

Add to the Toolbar's `<style>` block:

```css
  .acct-cluster { display: inline-flex; align-items: center; gap: 8px; }
```

- [ ] **Step 6: Wire state, events, and handlers into App.svelte**

In `frontend/src/App.svelte`, extend the first App import to add the auth/sync bindings and add the events import:

```ts
  import { ListProjects, LoadRepo, Fetch, Pull, Push, DeleteProject, GetConfig, GetProject, AuthStart, AuthStatus, SignOut, SyncNow, SyncState } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";
```

(`toastError`, `toastInfo` are already imported at the top of `App.svelte` from `./lib/toasts`, so no new toast import is needed.)

Add reactive auth/sync state (near the other `let` declarations, e.g. after `let view: ... = "today";`):

```ts
  // ---- cloud auth + sync ---------------------------------------------------
  let auth = { signedIn: false, login: "", avatarUrl: "" };
  let sync = { state: "signedout", lastSyncedUnix: 0, error: "" };
  let authBusy = false;
  let unsubs: Array<() => void> = [];

  async function signIn() {
    if (authBusy) return;
    authBusy = true;
    try {
      const err = await AuthStart();
      if (err) toastError("Sign in: " + err);
    } catch (e) {
      toastError("Sign in: " + errText(e));
    } finally {
      authBusy = false;
    }
  }
  async function signOut() {
    const err = await SignOut();
    if (err) toastError("Sign out: " + err);
  }
  async function syncNow() {
    const err = await SyncNow();
    if (err) toastError("Sync: " + err);
  }
```

In the existing `onMount(async () => { ... })`, after `window.addEventListener("keydown", onKey);`, add:

```ts
    try {
      auth = await AuthStatus();
      sync = await SyncState();
    } catch {
      /* offline-first: ignore */
    }
    unsubs.push(EventsOn("auth:changed", (v: any) => { if (v) auth = v; }));
    unsubs.push(EventsOn("sync:changed", (v: any) => {
      if (v && v.state === "error" && sync.state !== "error") {
        toastError("Sync failed: " + (v.error || "unknown error"));
      }
      if (v) sync = v;
    }));
    unsubs.push(EventsOn("sync:remoteEdit", () => toastInfo("Updated on another device")));
```

In the existing `onDestroy(() => { ... })`, add:

```ts
    for (const off of unsubs) off();
```

Finally, pass the new props to `<Toolbar ... />` (add these attributes to the existing Toolbar usage):

```svelte
  authSignedIn={auth.signedIn}
  authLogin={auth.login}
  authAvatar={auth.avatarUrl}
  {authBusy}
  syncState={sync}
  onSignIn={signIn}
  onSignOut={signOut}
  onSyncNow={syncNow}
  onRetrySync={syncNow}
```

- [ ] **Step 7: Build the app (frontend + Go) to verify it compiles**

Run: `wails build`
Expected: build completes successfully (frontend compiles with the new components and bindings; Go binary links). No TypeScript or Svelte errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/SyncPill.svelte frontend/src/lib/AccountChip.svelte frontend/src/lib/SignIn.svelte frontend/src/lib/Toolbar.svelte frontend/src/App.svelte frontend/wailsjs
git commit -m "feat(gui): 로그인/계정칩/동기화 상태 필과 이벤트 연동 UI"
```

---

## Self-Review

**Spec coverage:**
- store `UpdatedAt` + central stamp + migration -> Task 1.
- `git.RemoteURL` + `NormalizeRemote` -> Task 2.
- `internal/cloud` contract types + methods + refresh-on-401 + keychain refresh storage -> Tasks 3, 4, 5.
- `internal/syncengine` state/doc_id/hash/backoff + SyncOnce (push/pull/LWW/tombstone/dirty/cursor/offline) + detached-record retention -> Tasks 6, 7.
- app.go bindings (AuthStart loopback+PKCE+browser+exchange+keychain+emit, AuthStatus, SignOut, SyncNow, SyncState) + background loop with capped backoff + events -> Task 8.
- GUI SignIn / AccountChip / SyncPill / toasts / no layout shift / event subscription / top-bar wiring -> Task 9.
- Tests required by the brief (store migration, normalizeRemote, cloud refresh-on-401, syncengine LWW/offline/detached, wails build) are all present in Tasks 1, 2, 4, 7, 9.
- Server-side work (cmd/fleetd, internal/server, pgstore, migrations, Fly deploy) is Plan A and is out of scope for this desktop-client plan, which only consumes Plan A's HTTP API (per the shared contract).

**Type consistency:** `Doc`, `Tokens`, `User`, `PushResult`, `Client`, `Session`, `CredStore`, `State`, `DocState`, `Engine`, `AuthStatusView`, `SyncStateView` and all method signatures match the shared contract verbatim and are used consistently across tasks (e.g. `SyncOnce(access string) error` consumed by `Session.WithAccess` in Task 8; `DocID`/`newer`/`payloadHash` produced in Task 6 and consumed in Task 7; `NextBackoff` produced in Task 6 and delegated to by `NextBackoffDelay` in Task 8).

**Detached-record correctness:** A pulled code-project doc with no matching local repo is retained under its `git:`/`local:` doc_id as the local store key. Task 7's dirty loop keeps that identity (guard: `if ds, ok := e.state.Docs[localID]; ok && ds.LocalID == localID { id = localID }`), so a detached record is never re-derived into a new `local:` doc (which would duplicate it on the server) nor spuriously tombstoned under its original id. Reconciliation-on-scan (re-attaching a detached doc to a freshly discovered local repo with a matching stable id) is deferred to a later slice per the spec; until then the record is retained and its identity preserved.

**Known v0 limitations (in-scope per spec):** silent sign-in from a stored refresh restores the session and access token but not `login`/`avatarUrl` (the contract's `/auth/refresh` returns no identity), so the chip shows a generic "Account" until the next `AuthStart`.