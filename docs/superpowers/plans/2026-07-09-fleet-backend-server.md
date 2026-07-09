# fleet Backend Spine (accounts + PM sync) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the fleet cloud backend server: GitHub-OAuth identity, JWT sessions with rotating refresh tokens, and a per-user versioned document store with Last-Write-Wins sync, deployable to Fly.io on Neon Postgres.

**Architecture:** A second `main` (`cmd/fleetd`) in the existing single Go module, importing only new Wails-free `internal/server/*` packages. A chi router exposes `/healthz`, GitHub OAuth endpoints, and an authenticated `/sync` REST API. `pgstore` (pgx) holds users, rotating refresh tokens, and documents; the LWW + per-user monotonic cursor logic runs in one Postgres transaction. GitHub API access sits behind an interface so tests use a fake; the store is an interface so handler tests use a fake. golang-migrate runs embedded SQL migrations at startup.

**Tech Stack:** Go 1.22, `github.com/go-chi/chi/v5`, `github.com/golang-jwt/jwt/v5`, `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`, `github.com/google/uuid`, stdlib `net/http`/`log/slog`; Docker (distroless) + Fly.io + Neon Postgres; GitHub Actions.

## Global Constraints

Every task's requirements implicitly include this section.

- **Go 1.22** (`go 1.22.0` in `go.mod`; CI and Docker use Go 1.22).
- **Desktop packages stay stdlib-only and ASCII-only source; gofmt-clean.** Do not add third-party imports to any package under `internal/` that the desktop (`app.go`) imports, and do not add non-ASCII bytes to desktop source. The new `internal/server/*` and `cmd/fleetd` packages are server-only (never imported by `app.go`) and MAY use the allowed server deps below.
- **`go vet` clean and gofmt-clean** for all new server packages: `go vet ./cmd/fleetd/... ./internal/server/...` must pass.
- **Allowed NEW server dependencies (only these):** `github.com/jackc/pgx/v5`, `github.com/golang-jwt/jwt/v5`, `golang.org/x/oauth2` (permitted; not required — the GitHub client is stdlib `net/http` behind an interface), `github.com/go-chi/chi/v5`, `github.com/golang-migrate/migrate/v4`, plus `github.com/google/uuid` (already an indirect dep, promoted to direct).
- **Allowed NEW desktop dependency:** `github.com/zalando/go-keyring` ONLY (out of scope for this plan — desktop is Plan B).
- **LWW rule (server), verbatim:** accept a pushed doc iff `parse(updated_at) > stored.updated_at` OR the doc is absent. On accept: `user_versions.current += 1` (same tx); `documents.version = new current`; store payload/updated_at/deleted. On reject: `accepted=false`, `version=stored.version`. Pull returns docs where `version > since`, ordered by `version asc`; `cursor = max version returned` (or the given `since` if none).
- **Per-user isolation:** every server query is scoped by `user_id`.
- **Tokens:** access = HS256 JWT, claim `sub=user_id`, exp ~15m, signed with `JWT_SIGNING_KEY`. refresh = 32 random bytes base64url; stored as sha256 hex hash; TTL ~30d; rotating (rotate revokes old, issues new).

## File Map

- `cmd/fleetd/main.go` — server entrypoint: read env, run migrations, wire deps, `ListenAndServe`.
- `internal/server/pgstore/store.go` — shared domain types (`GitHubIdentity`, `User`, `Doc`, `PushResult`) + the `Store` interface.
- `internal/server/pgstore/pg.go` — pgx pool + user/refresh-token repositories.
- `internal/server/pgstore/documents.go` — `Push`/`Pull` (LWW + cursor, one tx).
- `internal/server/pgstore/migrate.go` + `internal/server/pgstore/migrations/*.sql` — embedded golang-migrate migrations.
- `internal/server/auth/token.go` — JWT issue/verify.
- `internal/server/auth/refresh.go` — refresh token generation + hashing.
- `internal/server/auth/pkce.go` — PKCE S256 verify.
- `internal/server/auth/ttlstore.go` — generic one-time TTL store (pending-auth + link-code).
- `internal/server/auth/github.go` — GitHub client interface + real `net/http` impl.
- `internal/server/auth/handlers.go` — OAuth handlers (login/callback/exchange/refresh/logout).
- `internal/server/http/router.go` — chi router + `/healthz` + wiring.
- `internal/server/http/middleware.go` — slog logging, Bearer-JWT auth, rate limit, context.
- `internal/server/http/sync.go` — `GET`/`POST /sync` handlers.
- `Dockerfile`, `fly.toml`, `README.md` (notes), `.github/workflows/server.yml`.

---

### Task 1: Server module dependencies, `/healthz`, request logging

**Files:**
- Modify: `go.mod` (add `github.com/go-chi/chi/v5`; promote `github.com/google/uuid` to direct later)
- Create: `internal/server/http/router.go`
- Create: `internal/server/http/middleware.go`
- Test: `internal/server/http/router_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `func NewRouter(opts Options) http.Handler` (package `httpapi`, dir `internal/server/http`)
  - `type Options struct{}` (empty here; extended in Task 9)
  - `func LogRequests(next http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test**

Create `internal/server/http/router_test.go`:

```go
package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzOK(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Options{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want %q", string(body), "ok")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/http/ -run TestHealthzOK`
Expected: FAIL — build error `undefined: NewRouter` / `undefined: Options`.

- [ ] **Step 3: Add the chi dependency**

Run: `go get github.com/go-chi/chi/v5@v5.1.0`
Expected: `go.mod` gains `github.com/go-chi/chi/v5 v5.1.0`.

- [ ] **Step 4: Write the logging middleware**

Create `internal/server/http/middleware.go`:

```go
// Package httpapi is the fleet backend HTTP layer: router, middleware, and
// handlers. It imports the server-only auth and pgstore packages (never Wails).
package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// statusWriter captures the response status for structured request logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// LogRequests logs one structured line per request via slog.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}
```

- [ ] **Step 5: Write the router**

Create `internal/server/http/router.go`:

```go
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Options carries the router's dependencies. It is empty in the healthz-only
// build and gains fields as later tasks add auth and sync routes.
type Options struct{}

// NewRouter builds the HTTP handler. /healthz is unauthenticated and returns
// the literal text "ok".
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(LogRequests)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return r
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/server/http/ -run TestHealthzOK`
Expected: PASS (`ok  github.com/hoijun/fleet/internal/server/http`).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/server/http/router.go internal/server/http/middleware.go internal/server/http/router_test.go
git commit -m "feat(server): chi router with /healthz and slog request logging"
```

---

### Task 2: pgstore types, Store interface, migrations, users + refresh-token repos

**Files:**
- Create: `internal/server/pgstore/store.go`
- Create: `internal/server/pgstore/migrations/0001_init.up.sql`
- Create: `internal/server/pgstore/migrations/0001_init.down.sql`
- Create: `internal/server/pgstore/migrate.go`
- Create: `internal/server/pgstore/pg.go`
- Test: `internal/server/pgstore/helper_test.go`
- Test: `internal/server/pgstore/pg_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type GitHubIdentity struct { GitHubID int64; Login, Email, AvatarURL string }`
  - `type User struct { ID string; GitHubID int64; Login, Email, AvatarURL string }`
  - `type Doc struct { Kind string; DocID string; Payload json.RawMessage; UpdatedAt string; Deleted bool; Version int64 }` (json tags: `kind,doc_id,payload,updated_at,deleted,version`)
  - `type PushResult struct { DocID, Kind string; Accepted bool; Version int64 }` (json tags: `doc_id,kind,accepted,version`)
  - `type Store interface { UpsertUserByGitHub(ctx, GitHubIdentity) (User, error); CreateRefreshToken(ctx, userID, tokenHash string, expiresAt time.Time) error; RotateRefreshToken(ctx, oldHash, newHash string, expiresAt time.Time) (string, error); RevokeRefreshToken(ctx, tokenHash string) error; Pull(ctx, userID string, since int64) ([]Doc, int64, error); Push(ctx, userID string, docs []Doc) ([]PushResult, int64, error) }`
  - `func New(ctx context.Context, databaseURL string) (*Pg, error)`; `func (p *Pg) Close()`
  - `func Migrate(databaseURL string) error`

- [ ] **Step 1: Add pgx, migrate, and uuid dependencies**

Run: `go get github.com/jackc/pgx/v5@v5.7.1 github.com/golang-migrate/migrate/v4@v4.18.1 github.com/google/uuid@v1.6.0`
Expected: `go.mod` gains those requires (uuid promoted from indirect to direct).

- [ ] **Step 2: Write the embedded migration SQL**

Create `internal/server/pgstore/migrations/0001_init.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS users (
    id         uuid PRIMARY KEY,
    github_id  bigint UNIQUE NOT NULL,
    login      text NOT NULL,
    email      text NOT NULL DEFAULT '',
    avatar_url text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id),
    token_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked    boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS refresh_tokens_hash_idx ON refresh_tokens(token_hash);

CREATE TABLE IF NOT EXISTS documents (
    user_id    uuid NOT NULL REFERENCES users(id),
    kind       text NOT NULL,
    doc_id     text NOT NULL,
    payload    jsonb NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted    boolean NOT NULL DEFAULT false,
    version    bigint NOT NULL,
    PRIMARY KEY (user_id, kind, doc_id)
);
CREATE INDEX IF NOT EXISTS documents_user_version_idx ON documents(user_id, version);

CREATE TABLE IF NOT EXISTS user_versions (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    current bigint NOT NULL DEFAULT 0
);
```

Create `internal/server/pgstore/migrations/0001_init.down.sql`:

```sql
DROP TABLE IF EXISTS user_versions;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
```

- [ ] **Step 3: Write the types + Store interface**

Create `internal/server/pgstore/store.go`:

```go
// Package pgstore is the Postgres-backed server store: identity (users,
// refresh tokens) and the per-user versioned document store with LWW sync.
// The Store interface lets handler tests substitute a fake.
package pgstore

import (
	"context"
	"encoding/json"
	"time"
)

// GitHubIdentity is the subset of a GitHub profile used to upsert a user.
type GitHubIdentity struct {
	GitHubID  int64
	Login     string
	Email     string
	AvatarURL string
}

// User is a fleet account row.
type User struct {
	ID        string
	GitHubID  int64
	Login     string
	Email     string
	AvatarURL string
}

// Doc is one synced document. The JSON shape is the shared v0 sync contract.
type Doc struct {
	Kind      string          `json:"kind"`
	DocID     string          `json:"doc_id"`
	Payload   json.RawMessage `json:"payload"`
	UpdatedAt string          `json:"updated_at"`
	Deleted   bool            `json:"deleted"`
	Version   int64           `json:"version"`
}

// PushResult reports the outcome of one pushed document.
type PushResult struct {
	DocID    string `json:"doc_id"`
	Kind     string `json:"kind"`
	Accepted bool   `json:"accepted"`
	Version  int64  `json:"version"`
}

// Store is the server persistence seam consumed by the HTTP and auth layers.
type Store interface {
	UpsertUserByGitHub(ctx context.Context, id GitHubIdentity) (User, error)
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	Pull(ctx context.Context, userID string, since int64) ([]Doc, int64, error)
	Push(ctx context.Context, userID string, docs []Doc) ([]PushResult, int64, error)
}
```

- [ ] **Step 4: Write the migration runner**

Create `internal/server/pgstore/migrate.go`:

```go
package pgstore

import (
	"embed"
	"errors"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the "pgx5" scheme
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all embedded migrations. It is idempotent: a fully-migrated
// database is a no-op. databaseURL is a standard postgres:// URL; the golang-
// migrate pgx/v5 driver registers the "pgx5" scheme, so we swap the prefix.
func Migrate(databaseURL string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	dbURL := databaseURL
	if strings.HasPrefix(dbURL, "postgresql://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgresql://")
	} else if strings.HasPrefix(dbURL, "postgres://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgres://")
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Write the pool + user/refresh repositories**

Create `internal/server/pgstore/pg.go`:

```go
package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pg is the Postgres-backed Store implementation.
type Pg struct {
	pool *pgxpool.Pool
}

// New opens a connection pool to databaseURL.
func New(ctx context.Context, databaseURL string) (*Pg, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &Pg{pool: pool}, nil
}

// Close releases the pool.
func (p *Pg) Close() { p.pool.Close() }

// errRefreshInvalid marks a refresh token that exists but is revoked/expired.
var errRefreshInvalid = errors.New("refresh token invalid")

// UpsertUserByGitHub inserts or updates a user keyed by github_id and ensures a
// user_versions counter row exists.
func (p *Pg) UpsertUserByGitHub(ctx context.Context, id GitHubIdentity) (User, error) {
	row := p.pool.QueryRow(ctx, `
INSERT INTO users (id, github_id, login, email, avatar_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (github_id) DO UPDATE SET
  login = EXCLUDED.login,
  email = EXCLUDED.email,
  avatar_url = EXCLUDED.avatar_url,
  updated_at = now()
RETURNING id, github_id, login, email, avatar_url`,
		uuid.NewString(), id.GitHubID, id.Login, id.Email, id.AvatarURL)
	var u User
	if err := row.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Email, &u.AvatarURL); err != nil {
		return User{}, err
	}
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO user_versions (user_id, current) VALUES ($1, 0) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

// CreateRefreshToken stores a hashed refresh token.
func (p *Pg) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, tokenHash, expiresAt)
	return err
}

// RotateRefreshToken validates oldHash (present, not revoked, not expired),
// revokes it, and inserts newHash for the same user, all in one transaction.
func (p *Pg) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID string
	var revoked bool
	var exp time.Time
	err = tx.QueryRow(ctx,
		`SELECT user_id, revoked, expires_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).
		Scan(&userID, &revoked, &exp)
	if err != nil {
		return "", err
	}
	if revoked || time.Now().After(exp) {
		return "", errRefreshInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`, oldHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, newHash, expiresAt); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

// RevokeRefreshToken marks a refresh token revoked (idempotent).
func (p *Pg) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`, tokenHash)
	return err
}
```

- [ ] **Step 6: Write the Postgres test helper**

Create `internal/server/pgstore/helper_test.go`:

```go
package pgstore

import (
	"context"
	"os"
	"testing"
)

// testPg connects to DATABASE_URL_TEST, migrates, and truncates all tables.
// It skips the test when the env var is unset so `go test` is green offline.
func testPg(t *testing.T) *Pg {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST not set; skipping Postgres-backed test")
	}
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pg, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pg.pool.Exec(context.Background(),
		`TRUNCATE documents, user_versions, refresh_tokens, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg
}
```

- [ ] **Step 7: Write the failing user + refresh tests**

Create `internal/server/pgstore/pg_test.go`:

```go
package pgstore

import (
	"context"
	"testing"
	"time"
)

func TestUpsertUserByGitHubIsIdempotent(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()

	u1, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 42, Login: "octocat", AvatarURL: "a1"})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	u2, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 42, Login: "octocat-renamed", AvatarURL: "a2"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u1.ID != u2.ID {
		t.Fatalf("id changed on re-upsert: %s -> %s", u1.ID, u2.ID)
	}
	if u2.Login != "octocat-renamed" || u2.AvatarURL != "a2" {
		t.Fatalf("profile not updated: %+v", u2)
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 7, Login: "u7"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	if err := pg.CreateRefreshToken(ctx, u.ID, "hash-old", exp); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := pg.RotateRefreshToken(ctx, "hash-old", "hash-new", exp)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got != u.ID {
		t.Fatalf("rotate returned user %q, want %q", got, u.ID)
	}
	// Old hash is now revoked: a second rotation on it must fail.
	if _, err := pg.RotateRefreshToken(ctx, "hash-old", "hash-newer", exp); err == nil {
		t.Fatal("expected error rotating a revoked token")
	}
	// The new hash still rotates.
	if _, err := pg.RotateRefreshToken(ctx, "hash-new", "hash-new2", exp); err != nil {
		t.Fatalf("rotate new: %v", err)
	}
}
```

- [ ] **Step 8: Run to verify it builds and skips (or passes) — offline**

Run: `go test ./internal/server/pgstore/ -run 'TestUpsertUserByGitHubIsIdempotent|TestRefreshTokenRotation'`
Expected: PASS with the two tests reported as skipped (`DATABASE_URL_TEST not set`). If `DATABASE_URL_TEST` is set, both PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/server/pgstore go.mod go.sum
git commit -m "feat(server): pgstore types, migrations, users + rotating refresh tokens"
```

---

### Task 3: Documents store — Push/Pull with LWW + monotonic cursor (one tx)

**Files:**
- Create: `internal/server/pgstore/documents.go`
- Test: `internal/server/pgstore/documents_test.go`

**Interfaces:**
- Consumes: `pgstore.Pg` (Task 2), `pgstore.Doc`, `pgstore.PushResult`, `pgstore.User`, `testPg` (Task 2 test helper).
- Produces (satisfies `Store`): `func (p *Pg) Push(ctx, userID string, docs []Doc) ([]PushResult, int64, error)`; `func (p *Pg) Pull(ctx, userID string, since int64) ([]Doc, int64, error)`.

- [ ] **Step 1: Write the failing table-driven LWW + isolation tests**

Create `internal/server/pgstore/documents_test.go`:

```go
package pgstore

import (
	"context"
	"encoding/json"
	"testing"
)

func mkDoc(docID, updatedAt string, deleted bool) Doc {
	return Doc{
		Kind:      "project",
		DocID:     docID,
		Payload:   json.RawMessage(`{"name":"x"}`),
		UpdatedAt: updatedAt,
		Deleted:   deleted,
	}
}

func TestPushLWWAndCursor(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 1, Login: "a"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	t1 := "2026-01-01T00:00:00Z"
	t2 := "2026-01-02T00:00:00Z"

	// First push: absent doc is accepted, version 1, cursor 1.
	res, cursor, err := pg.Push(ctx, u.ID, []Doc{mkDoc("d1", t1, false)})
	if err != nil {
		t.Fatalf("push1: %v", err)
	}
	if len(res) != 1 || !res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("push1 got %+v cursor=%d", res, cursor)
	}

	// Older updated_at is rejected; version stays 1; cursor unchanged.
	res, cursor, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", "2025-12-31T00:00:00Z", false)})
	if err != nil {
		t.Fatalf("push-stale: %v", err)
	}
	if res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("stale push not rejected: %+v cursor=%d", res, cursor)
	}

	// Newer updated_at wins; version 2; cursor 2.
	res, cursor, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", t2, false)})
	if err != nil {
		t.Fatalf("push-newer: %v", err)
	}
	if !res[0].Accepted || res[0].Version != 2 || cursor != 2 {
		t.Fatalf("newer push wrong: %+v cursor=%d", res, cursor)
	}

	// Tombstone accepted as a newer write; version 3.
	res, _, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", "2026-01-03T00:00:00Z", true)})
	if err != nil {
		t.Fatalf("push-tombstone: %v", err)
	}
	if !res[0].Accepted || res[0].Version != 3 {
		t.Fatalf("tombstone wrong: %+v", res)
	}

	// Pull since 0 returns the doc ordered by version; cursor = max version.
	docs, pcur, err := pg.Pull(ctx, u.ID, 0)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(docs) != 1 || docs[0].Version != 3 || !docs[0].Deleted || pcur != 3 {
		t.Fatalf("pull wrong: %+v cursor=%d", docs, pcur)
	}

	// Pull since current cursor returns nothing; cursor echoes since.
	docs, pcur, err = pg.Pull(ctx, u.ID, 3)
	if err != nil {
		t.Fatalf("pull-since: %v", err)
	}
	if len(docs) != 0 || pcur != 3 {
		t.Fatalf("pull-since wrong: %+v cursor=%d", docs, pcur)
	}
}

func TestPushPerUserIsolation(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	ua, _ := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 10, Login: "a"})
	ub, _ := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 11, Login: "b"})

	if _, _, err := pg.Push(ctx, ua.ID, []Doc{mkDoc("shared", "2026-01-01T00:00:00Z", false)}); err != nil {
		t.Fatalf("push a: %v", err)
	}
	// b pulling sees nothing; b's cursor stays 0.
	docs, cur, err := pg.Pull(ctx, ub.ID, 0)
	if err != nil {
		t.Fatalf("pull b: %v", err)
	}
	if len(docs) != 0 || cur != 0 {
		t.Fatalf("isolation broken: b saw %+v cursor=%d", docs, cur)
	}
	// a's cursor is independent (1).
	_, cura, _ := pg.Pull(ctx, ua.ID, 0)
	if cura != 1 {
		t.Fatalf("a cursor = %d, want 1", cura)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/pgstore/ -run TestPush`
Expected: FAIL — build error `pg.Push undefined` / `pg.Pull undefined`.

- [ ] **Step 3: Write the documents store**

Create `internal/server/pgstore/documents.go`:

```go
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Push applies the LWW rule per document in one transaction. A doc is accepted
// iff its updated_at is newer than the stored one (or the doc is absent). Each
// accepted write bumps the per-user counter (same tx) and stamps the version.
// The returned cursor is the user's current counter after the batch.
func (p *Pg) Push(ctx context.Context, userID string, docs []Doc) ([]PushResult, int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO user_versions (user_id, current) VALUES ($1, 0) ON CONFLICT DO NOTHING`, userID); err != nil {
		return nil, 0, err
	}

	results := make([]PushResult, 0, len(docs))
	for _, d := range docs {
		pushedAt, err := time.Parse(time.RFC3339, d.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("bad updated_at for doc %q: %w", d.DocID, err)
		}

		var storedAt time.Time
		var storedVersion int64
		found := true
		err = tx.QueryRow(ctx,
			`SELECT updated_at, version FROM documents WHERE user_id=$1 AND kind=$2 AND doc_id=$3 FOR UPDATE`,
			userID, d.Kind, d.DocID).Scan(&storedAt, &storedVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
		} else if err != nil {
			return nil, 0, err
		}

		if found && !pushedAt.After(storedAt) {
			results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: false, Version: storedVersion})
			continue
		}

		var newVersion int64
		if err := tx.QueryRow(ctx,
			`UPDATE user_versions SET current = current + 1 WHERE user_id = $1 RETURNING current`, userID).
			Scan(&newVersion); err != nil {
			return nil, 0, err
		}

		payload := d.Payload
		if len(payload) == 0 {
			payload = json.RawMessage("null")
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO documents (user_id, kind, doc_id, payload, updated_at, deleted, version)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, kind, doc_id) DO UPDATE SET
  payload    = EXCLUDED.payload,
  updated_at = EXCLUDED.updated_at,
  deleted    = EXCLUDED.deleted,
  version    = EXCLUDED.version`,
			userID, d.Kind, d.DocID, string(payload), pushedAt, d.Deleted, newVersion); err != nil {
			return nil, 0, err
		}
		results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: newVersion})
	}

	var cursor int64
	if err := tx.QueryRow(ctx, `SELECT current FROM user_versions WHERE user_id = $1`, userID).Scan(&cursor); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return results, cursor, nil
}

// Pull returns the user's documents with version > since, ordered by version
// ascending. The cursor is the max version returned, or since if none.
func (p *Pg) Pull(ctx context.Context, userID string, since int64) ([]Doc, int64, error) {
	rows, err := p.pool.Query(ctx, `
SELECT kind, doc_id, payload, updated_at, deleted, version
FROM documents WHERE user_id = $1 AND version > $2 ORDER BY version ASC`, userID, since)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	docs := []Doc{}
	cursor := since
	for rows.Next() {
		var d Doc
		var payload []byte
		var updatedAt time.Time
		if err := rows.Scan(&d.Kind, &d.DocID, &payload, &updatedAt, &d.Deleted, &d.Version); err != nil {
			return nil, 0, err
		}
		d.Payload = json.RawMessage(payload)
		d.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		if d.Version > cursor {
			cursor = d.Version
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return docs, cursor, nil
}
```

- [ ] **Step 4: Run test to verify it passes (or skips offline)**

Run: `go test ./internal/server/pgstore/ -run TestPush`
Expected: PASS. Offline (no `DATABASE_URL_TEST`): both tests report skipped and the package is `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/server/pgstore/documents.go internal/server/pgstore/documents_test.go
git commit -m "feat(server): document store with LWW, tombstones, per-user cursor"
```

---

### Task 4: Auth primitives — JWT, refresh hashing, PKCE

**Files:**
- Create: `internal/server/auth/token.go`
- Create: `internal/server/auth/refresh.go`
- Create: `internal/server/auth/pkce.go`
- Test: `internal/server/auth/token_test.go`
- Test: `internal/server/auth/pkce_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func IssueAccess(signingKey []byte, userID string, ttl time.Duration, now time.Time) (string, error)`
  - `func VerifyAccess(signingKey []byte, token string) (string, error)`
  - `func NewRefreshToken() (raw, hash string, err error)`
  - `func HashRefresh(raw string) string`
  - `func VerifyPKCE(codeChallenge, codeVerifier string) bool`

- [ ] **Step 1: Add the JWT dependency**

Run: `go get github.com/golang-jwt/jwt/v5@v5.2.1`
Expected: `go.mod` gains `github.com/golang-jwt/jwt/v5 v5.2.1`.

- [ ] **Step 2: Write the failing tests**

Create `internal/server/auth/token_test.go`:

```go
package auth

import (
	"testing"
	"time"
)

func TestIssueAndVerifyAccess(t *testing.T) {
	key := []byte("test-signing-key")
	now := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)

	tok, err := IssueAccess(key, "user-123", 15*time.Minute, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	sub, err := VerifyAccess(key, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != "user-123" {
		t.Fatalf("sub = %q, want user-123", sub)
	}
}

func TestVerifyAccessRejectsWrongKey(t *testing.T) {
	tok, _ := IssueAccess([]byte("key-a"), "u", time.Minute, time.Now())
	if _, err := VerifyAccess([]byte("key-b"), tok); err == nil {
		t.Fatal("expected error verifying with wrong key")
	}
}

func TestVerifyAccessRejectsExpired(t *testing.T) {
	key := []byte("k")
	tok, _ := IssueAccess(key, "u", -time.Minute, time.Now())
	if _, err := VerifyAccess(key, tok); err == nil {
		t.Fatal("expected error verifying an expired token")
	}
}

func TestRefreshTokenHashStable(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("empty raw/hash")
	}
	if HashRefresh(raw) != hash {
		t.Fatal("HashRefresh not stable with NewRefreshToken")
	}
}
```

Create `internal/server/auth/pkce_test.go`:

```go
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !VerifyPKCE(challenge, verifier) {
		t.Fatal("valid verifier rejected")
	}
	if VerifyPKCE(challenge, "wrong-verifier") {
		t.Fatal("invalid verifier accepted")
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/server/auth/ -run 'TestIssue|TestVerify|TestRefresh|TestVerifyPKCE'`
Expected: FAIL — build errors `undefined: IssueAccess` etc.

- [ ] **Step 4: Write token.go**

Create `internal/server/auth/token.go`:

```go
// Package auth implements fleet's server-side identity: GitHub OAuth (native
// loopback + PKCE), JWT access tokens, and rotating hashed refresh tokens.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IssueAccess mints an HS256 access token with sub=userID, expiring now+ttl.
func IssueAccess(signingKey []byte, userID string, ttl time.Duration, now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey)
}

// VerifyAccess validates an HS256 token and returns its subject (user id).
func VerifyAccess(signingKey []byte, token string) (string, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return signingKey, nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid || claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
```

- [ ] **Step 5: Write refresh.go**

Create `internal/server/auth/refresh.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewRefreshToken returns a fresh 32-byte base64url refresh token and its
// sha256 hex hash (what the server stores).
func NewRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefresh(raw), nil
}

// HashRefresh returns the sha256 hex hash of a raw refresh token.
func HashRefresh(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randToken returns a 32-byte base64url random string (state / link codes).
func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [ ] **Step 6: Write pkce.go**

Create `internal/server/auth/pkce.go`:

```go
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// VerifyPKCE reports whether S256(codeVerifier) equals codeChallenge.
func VerifyPKCE(codeChallenge, codeVerifier string) bool {
	sum := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(codeChallenge)) == 1
}
```

- [ ] **Step 7: Run to verify pass**

Run: `go test ./internal/server/auth/ -run 'TestIssue|TestVerify|TestRefresh|TestVerifyPKCE'`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/server/auth/token.go internal/server/auth/refresh.go internal/server/auth/pkce.go internal/server/auth/token_test.go internal/server/auth/pkce_test.go go.mod go.sum
git commit -m "feat(server): JWT access tokens, hashed refresh tokens, PKCE S256"
```

---

### Task 5: One-time TTL store (pending-auth + link codes)

**Files:**
- Create: `internal/server/auth/ttlstore.go`
- Test: `internal/server/auth/ttlstore_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package-internal, used by Task 7):
  - `type ttlStore[V any] struct{ ... }`
  - `func newTTLStore[V any](now func() time.Time) *ttlStore[V]`
  - `func (s *ttlStore[V]) put(key string, v V, ttl time.Duration)`
  - `func (s *ttlStore[V]) take(key string) (V, bool)` (removes on read; false if missing or expired)

- [ ] **Step 1: Write the failing test**

Create `internal/server/auth/ttlstore_test.go`:

```go
package auth

import (
	"testing"
	"time"
)

func TestTTLStoreTakeIsOneTime(t *testing.T) {
	s := newTTLStore[string](time.Now)
	s.put("k", "v", time.Minute)

	got, ok := s.take("k")
	if !ok || got != "v" {
		t.Fatalf("first take = %q,%v, want v,true", got, ok)
	}
	if _, ok := s.take("k"); ok {
		t.Fatal("second take should miss (one-time)")
	}
}

func TestTTLStoreExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := now
	s := newTTLStore[int](func() time.Time { return clock })
	s.put("k", 5, time.Minute)

	clock = now.Add(2 * time.Minute) // advance past TTL
	if _, ok := s.take("k"); ok {
		t.Fatal("expired entry should not be returned")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/auth/ -run TestTTLStore`
Expected: FAIL — `undefined: newTTLStore`.

- [ ] **Step 3: Write ttlstore.go**

Create `internal/server/auth/ttlstore.go`:

```go
package auth

import (
	"sync"
	"time"
)

type ttlEntry[V any] struct {
	val V
	exp time.Time
}

// ttlStore is a concurrency-safe map of string -> V with per-entry expiry and
// one-time take semantics. Used for pending OAuth state and link codes.
type ttlStore[V any] struct {
	mu  sync.Mutex
	m   map[string]ttlEntry[V]
	now func() time.Time
}

func newTTLStore[V any](now func() time.Time) *ttlStore[V] {
	if now == nil {
		now = time.Now
	}
	return &ttlStore[V]{m: map[string]ttlEntry[V]{}, now: now}
}

func (s *ttlStore[V]) put(key string, v V, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = ttlEntry[V]{val: v, exp: s.now().Add(ttl)}
}

// take removes and returns the entry; ok is false when missing or expired.
func (s *ttlStore[V]) take(key string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		var zero V
		return zero, false
	}
	delete(s.m, key)
	if s.now().After(e.exp) {
		var zero V
		return zero, false
	}
	return e.val, true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/auth/ -run TestTTLStore`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/auth/ttlstore.go internal/server/auth/ttlstore_test.go
git commit -m "feat(server): generic one-time TTL store for oauth state and link codes"
```

---

### Task 6: GitHub client behind an interface

**Files:**
- Create: `internal/server/auth/github.go`
- Test: `internal/server/auth/github_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type GitHubUser struct { ID int64; Login, Email, AvatarURL string }`
  - `type GitHubClient interface { Exchange(ctx context.Context, code string) (string, error); User(ctx context.Context, accessToken string) (GitHubUser, error) }`
  - `type HTTPGitHub struct { ClientID, ClientSecret, TokenURL, APIBaseURL string; HTTP *http.Client }`
  - `func NewHTTPGitHub(clientID, clientSecret string) *HTTPGitHub`

- [ ] **Step 1: Write the failing test**

Create `internal/server/auth/github_test.go`:

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPGitHubExchangeAndUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gho_abc"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":99,"login":"octocat","email":"o@x.io","avatar_url":"http://a"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gh := &HTTPGitHub{
		ClientID:     "cid",
		ClientSecret: "sec",
		TokenURL:     srv.URL + "/login/oauth/access_token",
		APIBaseURL:   srv.URL,
		HTTP:         srv.Client(),
	}
	tok, err := gh.Exchange(context.Background(), "code123")
	if err != nil || tok != "gho_abc" {
		t.Fatalf("exchange = %q, %v", tok, err)
	}
	u, err := gh.User(context.Background(), tok)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if u.ID != 99 || u.Login != "octocat" || u.Email != "o@x.io" {
		t.Fatalf("user = %+v", u)
	}

	// Compile-time check that HTTPGitHub satisfies the interface.
	var _ GitHubClient = gh
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/auth/ -run TestHTTPGitHub`
Expected: FAIL — `undefined: HTTPGitHub` / `GitHubClient`.

- [ ] **Step 3: Write github.go**

Create `internal/server/auth/github.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHubUser is the profile fleet needs from GitHub.
type GitHubUser struct {
	ID        int64
	Login     string
	Email     string
	AvatarURL string
}

// GitHubClient is the seam over GitHub's OAuth + user API; tests use a fake.
type GitHubClient interface {
	Exchange(ctx context.Context, code string) (string, error)
	User(ctx context.Context, accessToken string) (GitHubUser, error)
}

// HTTPGitHub is the real GitHub client. URLs are fields so tests can inject an
// httptest server.
type HTTPGitHub struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	APIBaseURL   string
	HTTP         *http.Client
}

// NewHTTPGitHub builds a client for the real GitHub endpoints.
func NewHTTPGitHub(clientID, clientSecret string) *HTTPGitHub {
	return &HTTPGitHub{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     "https://github.com/login/oauth/access_token",
		APIBaseURL:   "https://api.github.com",
		HTTP:         &http.Client{Timeout: 20 * time.Second},
	}
}

// Exchange trades an authorization code for a GitHub access token.
func (g *HTTPGitHub) Exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", g.ClientID)
	form.Set("client_secret", g.ClientSecret)
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github token: http %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("github token: %s", out.Error)
	}
	return out.AccessToken, nil
}

// User fetches the authenticated GitHub user's profile.
func (g *HTTPGitHub) User(ctx context.Context, accessToken string) (GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.APIBaseURL+"/user", nil)
	if err != nil {
		return GitHubUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return GitHubUser{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GitHubUser{}, fmt.Errorf("github user: http %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return GitHubUser{}, err
	}
	if out.ID == 0 {
		return GitHubUser{}, fmt.Errorf("github user: empty id")
	}
	return GitHubUser{ID: out.ID, Login: out.Login, Email: out.Email, AvatarURL: out.AvatarURL}, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/auth/ -run TestHTTPGitHub`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/auth/github.go internal/server/auth/github_test.go
git commit -m "feat(server): GitHub OAuth client behind an interface"
```

---

### Task 7: OAuth handlers (login, callback, exchange, refresh, logout)

**Files:**
- Create: `internal/server/auth/handlers.go`
- Test: `internal/server/auth/handlers_test.go`

**Interfaces:**
- Consumes: `pgstore.Store`, `pgstore.GitHubIdentity`, `pgstore.User` (Task 2); `GitHubClient`, `GitHubUser` (Task 6); `IssueAccess`, `VerifyAccess`, `NewRefreshToken`, `HashRefresh`, `VerifyPKCE`, `randToken` (Tasks 4-5); `ttlStore`, `newTTLStore` (Task 5).
- Produces:
  - `type Config struct { Store pgstore.Store; GitHub GitHubClient; SigningKey []byte; ClientID, AuthorizeURL, CallbackURL, AllowedRedirect string; AccessTTL, RefreshTTL time.Duration; Now func() time.Time }`
  - `type Handlers struct{ ... }`; `func New(cfg Config) *Handlers`
  - Methods: `GithubLogin`, `GithubCallback`, `Exchange`, `Refresh`, `Logout` (each `func(http.ResponseWriter, *http.Request)`)

- [ ] **Step 1: Write the failing full-flow test**

Create `internal/server/auth/handlers_test.go`:

```go
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/server/pgstore"
)

// fakeGitHub returns a canned user without network.
type fakeGitHub struct{ user GitHubUser }

func (f fakeGitHub) Exchange(ctx context.Context, code string) (string, error) {
	return "gh-token", nil
}
func (f fakeGitHub) User(ctx context.Context, tok string) (GitHubUser, error) {
	return f.user, nil
}

// fakeStore implements pgstore.Store in memory.
type fakeStore struct {
	users   map[int64]pgstore.User
	refresh map[string]refreshRow
	seq     int
}
type refreshRow struct {
	userID  string
	revoked bool
	expires time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]pgstore.User{}, refresh: map[string]refreshRow{}}
}
func (f *fakeStore) UpsertUserByGitHub(ctx context.Context, id pgstore.GitHubIdentity) (pgstore.User, error) {
	u, ok := f.users[id.GitHubID]
	if !ok {
		f.seq++
		u = pgstore.User{ID: fmt.Sprintf("user-%d", f.seq), GitHubID: id.GitHubID}
	}
	u.Login, u.Email, u.AvatarURL = id.Login, id.Email, id.AvatarURL
	f.users[id.GitHubID] = u
	return u, nil
}
func (f *fakeStore) CreateRefreshToken(ctx context.Context, userID, hash string, exp time.Time) error {
	f.refresh[hash] = refreshRow{userID: userID, expires: exp}
	return nil
}
func (f *fakeStore) RotateRefreshToken(ctx context.Context, oldHash, newHash string, exp time.Time) (string, error) {
	row, ok := f.refresh[oldHash]
	if !ok || row.revoked || time.Now().After(row.expires) {
		return "", fmt.Errorf("invalid")
	}
	row.revoked = true
	f.refresh[oldHash] = row
	f.refresh[newHash] = refreshRow{userID: row.userID, expires: exp}
	return row.userID, nil
}
func (f *fakeStore) RevokeRefreshToken(ctx context.Context, hash string) error {
	if row, ok := f.refresh[hash]; ok {
		row.revoked = true
		f.refresh[hash] = row
	}
	return nil
}
func (f *fakeStore) Pull(ctx context.Context, userID string, since int64) ([]pgstore.Doc, int64, error) {
	return nil, since, nil
}
func (f *fakeStore) Push(ctx context.Context, userID string, docs []pgstore.Doc) ([]pgstore.PushResult, int64, error) {
	return nil, 0, nil
}

func newTestServer(h *Handlers) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/github/login", h.GithubLogin)
	mux.HandleFunc("GET /auth/github/callback", h.GithubCallback)
	mux.HandleFunc("POST /auth/exchange", h.Exchange)
	mux.HandleFunc("POST /auth/refresh", h.Refresh)
	mux.HandleFunc("POST /auth/logout", h.Logout)
	return httptest.NewServer(mux)
}

func TestOAuthFullFlow(t *testing.T) {
	key := []byte("k")
	store := newFakeStore()
	h := New(Config{
		Store:           store,
		GitHub:          fakeGitHub{user: GitHubUser{ID: 5, Login: "octo", AvatarURL: "http://a"}},
		SigningKey:      key,
		ClientID:        "cid",
		AuthorizeURL:    "https://github.test/authorize",
		CallbackURL:     "https://api.test/auth/github/callback",
		AllowedRedirect: "http://127.0.0.1",
	})
	srv := newTestServer(h)
	defer srv.Close()

	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	verifier := "verifier-abc-123"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://127.0.0.1:12345/cb"

	// 1. login -> 302 to GitHub authorize; capture server state.
	loginURL := srv.URL + "/auth/github/login?state=cs&code_challenge=" + challenge + "&redirect=" + url.QueryEscape(redirect)
	resp, err := client.Get(loginURL)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if !strings.HasPrefix(loc.String(), "https://github.test/authorize") {
		t.Fatalf("login redirect = %s", loc)
	}
	serverState := loc.Query().Get("state")
	if serverState == "" {
		t.Fatal("no server state")
	}

	// 2. callback -> 302 to loopback with link_code + original client state.
	resp, err = client.Get(srv.URL + "/auth/github/callback?code=xyz&state=" + serverState)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d", resp.StatusCode)
	}
	cb, _ := url.Parse(resp.Header.Get("Location"))
	if !strings.HasPrefix(cb.String(), redirect) {
		t.Fatalf("callback redirect = %s", cb)
	}
	if cb.Query().Get("state") != "cs" {
		t.Fatalf("client state = %q", cb.Query().Get("state"))
	}
	linkCode := cb.Query().Get("link_code")
	if linkCode == "" {
		t.Fatal("no link_code")
	}

	// 3. exchange -> 200 with tokens + user.
	body, _ := json.Marshal(map[string]string{"link_code": linkCode, "code_verifier": verifier})
	resp, err = client.Post(srv.URL+"/auth/exchange", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange status = %d", resp.StatusCode)
	}
	var ex struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		User    struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ex)
	if ex.Access == "" || ex.Refresh == "" {
		t.Fatal("empty tokens")
	}
	if ex.User.Login != "octo" {
		t.Fatalf("user login = %q", ex.User.Login)
	}
	if sub, err := VerifyAccess(key, ex.Access); err != nil || sub != ex.User.ID {
		t.Fatalf("access sub = %q err=%v want %q", sub, err, ex.User.ID)
	}

	// 4. refresh -> 200 rotates; old refresh no longer valid.
	body, _ = json.Marshal(map[string]string{"refresh_token": ex.Refresh})
	resp, err = client.Post(srv.URL+"/auth/refresh", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d", resp.StatusCode)
	}
	var rf struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rf)
	if rf.Refresh == "" || rf.Refresh == ex.Refresh {
		t.Fatal("refresh not rotated")
	}
	// Reusing the old refresh must fail.
	body, _ = json.Marshal(map[string]string{"refresh_token": ex.Refresh})
	resp, _ = client.Post(srv.URL+"/auth/refresh", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want 401", resp.StatusCode)
	}

	// 5. logout -> 204.
	body, _ = json.Marshal(map[string]string{"refresh_token": rf.Refresh})
	resp, _ = client.Post(srv.URL+"/auth/logout", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}
}

func TestExchangeRejectsBadPKCE(t *testing.T) {
	h := New(Config{
		Store:           newFakeStore(),
		GitHub:          fakeGitHub{user: GitHubUser{ID: 5, Login: "o"}},
		SigningKey:      []byte("k"),
		AuthorizeURL:    "https://github.test/authorize",
		AllowedRedirect: "http://127.0.0.1",
	})
	srv := newTestServer(h)
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	sum := sha256.Sum256([]byte("real-verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://127.0.0.1:1/cb"

	resp, _ := client.Get(srv.URL + "/auth/github/login?state=cs&code_challenge=" + challenge + "&redirect=" + url.QueryEscape(redirect))
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = client.Get(srv.URL + "/auth/github/callback?code=xyz&state=" + loc.Query().Get("state"))
	cb, _ := url.Parse(resp.Header.Get("Location"))

	body, _ := json.Marshal(map[string]string{"link_code": cb.Query().Get("link_code"), "code_verifier": "WRONG"})
	resp, _ = client.Post(srv.URL+"/auth/exchange", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-pkce exchange status = %d, want 401", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/auth/ -run 'TestOAuthFullFlow|TestExchangeRejectsBadPKCE'`
Expected: FAIL — `undefined: New` / `undefined: Config`.

- [ ] **Step 3: Write handlers.go**

Create `internal/server/auth/handlers.go`:

```go
package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/server/pgstore"
)

// Config carries the OAuth handlers' dependencies.
type Config struct {
	Store           pgstore.Store
	GitHub          GitHubClient
	SigningKey      []byte
	ClientID        string
	AuthorizeURL    string // default https://github.com/login/oauth/authorize
	CallbackURL     string // this server's public callback URL
	AllowedRedirect string // allowed loopback redirect prefix, e.g. http://127.0.0.1
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	Now             func() time.Time
}

// Handlers holds the OAuth endpoints plus short-lived server state.
type Handlers struct {
	cfg     Config
	pending *ttlStore[pendingAuth]
	links   *ttlStore[linkData]
}

type pendingAuth struct {
	clientState   string
	codeChallenge string
	redirect      string
}

type linkData struct {
	userID        string
	login         string
	avatarURL     string
	codeChallenge string
}

// New builds Handlers, filling defaults.
func New(cfg Config) *Handlers {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	return &Handlers{
		cfg:     cfg,
		pending: newTTLStore[pendingAuth](cfg.Now),
		links:   newTTLStore[linkData](cfg.Now),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// GithubLogin starts the flow: stashes {clientState, challenge, redirect} under
// a server state and redirects to GitHub's authorize URL.
func (h *Handlers) GithubLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state, challenge, redirect := q.Get("state"), q.Get("code_challenge"), q.Get("redirect")
	if state == "" || challenge == "" || redirect == "" {
		http.Error(w, "missing params", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(redirect, h.cfg.AllowedRedirect) {
		http.Error(w, "redirect not allowed", http.StatusBadRequest)
		return
	}
	serverState := randToken()
	h.pending.put(serverState, pendingAuth{clientState: state, codeChallenge: challenge, redirect: redirect}, 10*time.Minute)

	au, err := url.Parse(h.cfg.AuthorizeURL)
	if err != nil {
		http.Error(w, "bad authorize url", http.StatusInternalServerError)
		return
	}
	v := url.Values{}
	v.Set("client_id", h.cfg.ClientID)
	v.Set("redirect_uri", h.cfg.CallbackURL)
	v.Set("scope", "read:user user:email")
	v.Set("state", serverState)
	au.RawQuery = v.Encode()
	http.Redirect(w, r, au.String(), http.StatusFound)
}

// GithubCallback exchanges the code, upserts the user, mints a one-time link
// code, and redirects the browser back to the desktop loopback URL.
func (h *Handlers) GithubCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code, serverState := q.Get("code"), q.Get("state")
	if code == "" || serverState == "" {
		http.Error(w, "missing params", http.StatusBadRequest)
		return
	}
	pend, ok := h.pending.take(serverState)
	if !ok {
		http.Error(w, "unknown state", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	ghToken, err := h.cfg.GitHub.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "github exchange failed", http.StatusBadGateway)
		return
	}
	ghUser, err := h.cfg.GitHub.User(ctx, ghToken)
	if err != nil {
		http.Error(w, "github user failed", http.StatusBadGateway)
		return
	}
	user, err := h.cfg.Store.UpsertUserByGitHub(ctx, pgstore.GitHubIdentity{
		GitHubID: ghUser.ID, Login: ghUser.Login, Email: ghUser.Email, AvatarURL: ghUser.AvatarURL,
	})
	if err != nil {
		http.Error(w, "user upsert failed", http.StatusInternalServerError)
		return
	}
	linkCode := randToken()
	h.links.put(linkCode, linkData{
		userID: user.ID, login: user.Login, avatarURL: user.AvatarURL, codeChallenge: pend.codeChallenge,
	}, 5*time.Minute)

	dest, err := url.Parse(pend.redirect)
	if err != nil {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	dv := dest.Query()
	dv.Set("link_code", linkCode)
	dv.Set("state", pend.clientState)
	dest.RawQuery = dv.Encode()
	http.Redirect(w, r, dest.String(), http.StatusFound)
}

// Exchange validates PKCE for a link code and returns fleet session tokens.
func (h *Handlers) Exchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LinkCode     string `json:"link_code"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	ld, ok := h.links.take(req.LinkCode)
	if !ok {
		http.Error(w, "invalid link_code", http.StatusUnauthorized)
		return
	}
	if !VerifyPKCE(ld.codeChallenge, req.CodeVerifier) {
		http.Error(w, "pkce mismatch", http.StatusUnauthorized)
		return
	}
	access, err := IssueAccess(h.cfg.SigningKey, ld.userID, h.cfg.AccessTTL, h.cfg.Now())
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	if err := h.cfg.Store.CreateRefreshToken(r.Context(), ld.userID, hash, h.cfg.Now().Add(h.cfg.RefreshTTL)); err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": raw,
		"user":          map[string]any{"id": ld.userID, "login": ld.login, "avatar_url": ld.avatarURL},
	})
}

// Refresh rotates the refresh token and issues a new access token.
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	oldHash := HashRefresh(req.RefreshToken)
	raw, newHash, err := NewRefreshToken()
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	userID, err := h.cfg.Store.RotateRefreshToken(r.Context(), oldHash, newHash, h.cfg.Now().Add(h.cfg.RefreshTTL))
	if err != nil {
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}
	access, err := IssueAccess(h.cfg.SigningKey, userID, h.cfg.AccessTTL, h.cfg.Now())
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "refresh_token": raw})
}

// Logout revokes a refresh token.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	_ = h.cfg.Store.RevokeRefreshToken(r.Context(), HashRefresh(req.RefreshToken))
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/auth/ -run 'TestOAuthFullFlow|TestExchangeRejectsBadPKCE'`
Expected: PASS.

- [ ] **Step 5: Run the whole auth package**

Run: `go test ./internal/server/auth/`
Expected: PASS (`ok  github.com/hoijun/fleet/internal/server/auth`).

- [ ] **Step 6: Commit**

```bash
git add internal/server/auth/handlers.go internal/server/auth/handlers_test.go
git commit -m "feat(server): github oauth login/callback + token exchange/refresh/logout"
```

---

### Task 8: HTTP middleware — Bearer-JWT auth, rate limit, request context

**Files:**
- Modify: `internal/server/http/middleware.go` (full-file replacement — keeps `statusWriter`/`LogRequests`, adds auth + rate limit + context)
- Test: `internal/server/http/middleware_test.go`

**Interfaces:**
- Consumes: `auth.VerifyAccess`, `auth.IssueAccess` (Task 4).
- Produces:
  - `func WithUserID(ctx context.Context, id string) context.Context`
  - `func UserID(ctx context.Context) (string, bool)`
  - `func AuthMiddleware(signingKey []byte) func(http.Handler) http.Handler`
  - `type RateLimiter struct{ ... }`; `func NewRateLimiter(rate, burst float64) *RateLimiter`; `func (rl *RateLimiter) Allow(key string) bool`; `func (rl *RateLimiter) ByIP(next http.Handler) http.Handler`; `func (rl *RateLimiter) ByUser(next http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test**

Create `internal/server/http/middleware_test.go`:

```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
)

func TestAuthMiddlewareSetsUserID(t *testing.T) {
	key := []byte("k")
	tok, _ := auth.IssueAccess(key, "user-9", 15*time.Minute, time.Now())

	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := UserID(r.Context())
		seen = id
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(key)(next)

	// Valid token.
	req := httptest.NewRequest(http.MethodGet, "/sync", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seen != "user-9" {
		t.Fatalf("valid: code=%d userID=%q", rec.Code, seen)
	}

	// Missing header.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token code=%d, want 401", rec.Code)
	}

	// Bad token.
	req = httptest.NewRequest(http.MethodGet, "/sync", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token code=%d, want 401", rec.Code)
	}
}

func TestRateLimiterAllowAndRefill(t *testing.T) {
	now := time.Unix(0, 0)
	rl := NewRateLimiter(1, 2) // 1 token/sec, burst 2
	rl.now = func() time.Time { return now }

	if !rl.Allow("ip") || !rl.Allow("ip") {
		t.Fatal("first two requests should pass (burst 2)")
	}
	if rl.Allow("ip") {
		t.Fatal("third request should be limited")
	}
	now = now.Add(1100 * time.Millisecond) // ~1 token refilled
	if !rl.Allow("ip") {
		t.Fatal("request after refill should pass")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/http/ -run 'TestAuthMiddleware|TestRateLimiter'`
Expected: FAIL — `undefined: AuthMiddleware` / `undefined: NewRateLimiter` / `undefined: UserID`.

- [ ] **Step 3: Replace middleware.go with the full auth + rate-limit + context version**

Replace the *entire contents* of `internal/server/http/middleware.go` with the following (this keeps the `statusWriter` and `LogRequests` from Task 1 and adds the new middleware; there is a single import block, so no manual merging is required):

```go
// Package httpapi is the fleet backend HTTP layer: router, middleware, and
// handlers. It imports the server-only auth and pgstore packages (never Wails).
package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
)

// statusWriter captures the response status for structured request logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// LogRequests logs one structured line per request via slog.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}

// ctxKey namespaces context values for this package.
type ctxKey int

const userIDKey ctxKey = 0

// WithUserID stores the authenticated user id on the context.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserID reads the authenticated user id from the context.
func UserID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

// AuthMiddleware requires a valid Bearer JWT and puts its subject on the context.
func AuthMiddleware(signingKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, prefix) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			userID, err := auth.VerifyAccess(signingKey, strings.TrimPrefix(h, prefix))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}

// RateLimiter is a per-key token-bucket limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter builds a limiter with the given steady rate and burst.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{buckets: map[string]*bucket{}, rate: rate, burst: burst, now: time.Now}
}

// Allow reports whether a request for key may proceed, consuming a token.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	t := rl.now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: t}
		return true
	}
	b.tokens = min(rl.burst, b.tokens+t.Sub(b.last).Seconds()*rl.rate)
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ByIP rate-limits by client IP (for unauthenticated auth routes).
func (rl *RateLimiter) ByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ByUser rate-limits by authenticated user id, falling back to client IP.
func (rl *RateLimiter) ByUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := UserID(r.Context())
		if id == "" {
			id = clientIP(r)
		}
		if !rl.Allow(id) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/http/ -run 'TestAuthMiddleware|TestRateLimiter'`
Expected: PASS. (`TestHealthzOK` from Task 1 still passes — `LogRequests`/`statusWriter` are preserved.)

- [ ] **Step 5: Commit**

```bash
git add internal/server/http/middleware.go internal/server/http/middleware_test.go
git commit -m "feat(server): bearer-jwt auth middleware, token-bucket rate limiting"
```

---

### Task 9: /sync handlers + full router wiring

**Files:**
- Create: `internal/server/http/sync.go`
- Modify: `internal/server/http/router.go`
- Test: `internal/server/http/sync_test.go`

**Interfaces:**
- Consumes: `pgstore.Store`, `pgstore.Doc`, `pgstore.PushResult` (Task 2); `UserID`, `AuthMiddleware`, `NewRateLimiter` (Task 8); `auth.Handlers`, `auth.IssueAccess` (Tasks 4, 7).
- Produces:
  - `type Sync struct { Store pgstore.Store }`; `func (s Sync) Get(w, r)`; `func (s Sync) Post(w, r)`
  - Extended `type Options struct { Store pgstore.Store; Auth *auth.Handlers; SigningKey []byte }`
  - `func NewRouter(opts Options) http.Handler` (full: healthz + auth routes + sync routes)

- [ ] **Step 1: Write the failing sync test**

Create `internal/server/http/sync_test.go`:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

// syncFakeStore implements pgstore.Store with canned sync data.
type syncFakeStore struct {
	pulled   []pgstore.Doc
	cursor   int64
	gotPush  []pgstore.Doc
	pushRes  []pgstore.PushResult
	pushCurs int64
}

func (f *syncFakeStore) UpsertUserByGitHub(ctx context.Context, id pgstore.GitHubIdentity) (pgstore.User, error) {
	return pgstore.User{}, nil
}
func (f *syncFakeStore) CreateRefreshToken(ctx context.Context, u, h string, e time.Time) error {
	return nil
}
func (f *syncFakeStore) RotateRefreshToken(ctx context.Context, o, n string, e time.Time) (string, error) {
	return "", nil
}
func (f *syncFakeStore) RevokeRefreshToken(ctx context.Context, h string) error { return nil }
func (f *syncFakeStore) Pull(ctx context.Context, userID string, since int64) ([]pgstore.Doc, int64, error) {
	return f.pulled, f.cursor, nil
}
func (f *syncFakeStore) Push(ctx context.Context, userID string, docs []pgstore.Doc) ([]pgstore.PushResult, int64, error) {
	f.gotPush = docs
	return f.pushRes, f.pushCurs, nil
}

func authedClient(t *testing.T, srv *httptest.Server, key []byte) (*http.Client, string) {
	t.Helper()
	tok, err := auth.IssueAccess(key, "user-1", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return srv.Client(), tok
}

func TestSyncGetReturnsDocsAndCursor(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{
		pulled: []pgstore.Doc{{Kind: "project", DocID: "d1", Payload: json.RawMessage(`{}`), UpdatedAt: "2026-01-01T00:00:00Z", Version: 3}},
		cursor: 3,
	}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()
	client, tok := authedClient(t, srv, key)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/sync?since=0", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Docs   []pgstore.Doc `json:"docs"`
		Cursor int64         `json:"cursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Docs) != 1 || out.Docs[0].DocID != "d1" || out.Cursor != 3 {
		t.Fatalf("out = %+v", out)
	}
}

func TestSyncPostReturnsResultsAndCursor(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{
		pushRes:  []pgstore.PushResult{{DocID: "d1", Kind: "project", Accepted: true, Version: 1}},
		pushCurs: 1,
	}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()
	client, tok := authedClient(t, srv, key)

	body := `{"docs":[{"kind":"project","doc_id":"d1","payload":{},"updated_at":"2026-01-01T00:00:00Z","deleted":false,"version":0}]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sync", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Results []pgstore.PushResult `json:"results"`
		Cursor  int64                `json:"cursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 1 || !out.Results[0].Accepted || out.Cursor != 1 {
		t.Fatalf("out = %+v", out)
	}
	if len(store.gotPush) != 1 || store.gotPush[0].DocID != "d1" {
		t.Fatalf("store did not receive doc: %+v", store.gotPush)
	}
}

func TestSyncRequiresAuth(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sync?since=0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/http/ -run TestSync`
Expected: FAIL — build errors: `Options` has no field `Store`/`Auth`/`SigningKey`; `undefined: Sync`.

- [ ] **Step 3: Write sync.go**

Create `internal/server/http/sync.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hoijun/fleet/internal/server/pgstore"
)

// Sync serves the authenticated document sync API.
type Sync struct {
	Store pgstore.Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Get handles GET /sync?since=<int64>.
func (s Sync) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserID(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "bad since", http.StatusBadRequest)
			return
		}
		since = n
	}
	docs, cursor, err := s.Store.Pull(r.Context(), userID, since)
	if err != nil {
		http.Error(w, "pull failed", http.StatusInternalServerError)
		return
	}
	if docs == nil {
		docs = []pgstore.Doc{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs, "cursor": cursor})
}

// Post handles POST /sync {docs:[Doc]}.
func (s Sync) Post(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserID(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Docs []pgstore.Doc `json:"docs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	results, cursor, err := s.Store.Push(r.Context(), userID, req.Docs)
	if err != nil {
		http.Error(w, "push failed", http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []pgstore.PushResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "cursor": cursor})
}
```

- [ ] **Step 4: Rewrite router.go with full wiring**

Replace the entire contents of `internal/server/http/router.go`:

```go
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hoijun/fleet/internal/server/auth"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

// Options carries the router's dependencies.
type Options struct {
	Store      pgstore.Store
	Auth       *auth.Handlers
	SigningKey []byte
}

// NewRouter builds the full HTTP handler: public /healthz, IP-rate-limited auth
// routes, and JWT-authenticated per-user-rate-limited /sync routes.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(LogRequests)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	if opts.Auth != nil {
		authLimit := NewRateLimiter(5, 10) // per-IP on auth routes
		r.Group(func(r chi.Router) {
			r.Use(authLimit.ByIP)
			r.Get("/auth/github/login", opts.Auth.GithubLogin)
			r.Get("/auth/github/callback", opts.Auth.GithubCallback)
			r.Post("/auth/exchange", opts.Auth.Exchange)
			r.Post("/auth/refresh", opts.Auth.Refresh)
			r.Post("/auth/logout", opts.Auth.Logout)
		})
	}

	sync := Sync{Store: opts.Store}
	userLimit := NewRateLimiter(20, 40) // per-user on sync
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(opts.SigningKey))
		r.Use(userLimit.ByUser)
		r.Get("/sync", sync.Get)
		r.Post("/sync", sync.Post)
	})

	return r
}
```

- [ ] **Step 5: Run the whole http package**

Run: `go test ./internal/server/http/`
Expected: PASS (`TestHealthzOK`, `TestAuthMiddleware*`, `TestRateLimiter*`, `TestSync*` all green).

- [ ] **Step 6: Commit**

```bash
git add internal/server/http/sync.go internal/server/http/router.go internal/server/http/sync_test.go
git commit -m "feat(server): authenticated GET/POST /sync and full router wiring"
```

---

### Task 10: Server entrypoint (cmd/fleetd)

**Files:**
- Create: `cmd/fleetd/main.go`
- Test: `cmd/fleetd/main_test.go`

**Interfaces:**
- Consumes: `pgstore.Migrate`, `pgstore.New` (Tasks 2); `auth.New`, `auth.Config`, `auth.NewHTTPGitHub` (Tasks 6-7); `httpapi.NewRouter`, `httpapi.Options` (Task 9).
- Produces: `func envOr(k, def string) string` (tested); `func main()`.

- [ ] **Step 1: Write the failing test**

Create `cmd/fleetd/main_test.go`:

```go
package main

import (
	"os"
	"testing"
)

func TestEnvOr(t *testing.T) {
	if got := envOr("FLEETD_TEST_MISSING", "def"); got != "def" {
		t.Fatalf("missing -> %q, want def", got)
	}
	os.Setenv("FLEETD_TEST_SET", "val")
	defer os.Unsetenv("FLEETD_TEST_SET")
	if got := envOr("FLEETD_TEST_SET", "def"); got != "val" {
		t.Fatalf("set -> %q, want val", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/fleetd/ -run TestEnvOr`
Expected: FAIL — build error `undefined: envOr` (no Go files in `cmd/fleetd`).

- [ ] **Step 3: Write main.go**

Create `cmd/fleetd/main.go`:

```go
// Command fleetd is the fleet backend server: GitHub OAuth identity plus a
// per-user versioned document store with LWW sync, backed by Postgres.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/hoijun/fleet/internal/server/auth"
	httpapi "github.com/hoijun/fleet/internal/server/http"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	databaseURL := mustEnv("DATABASE_URL")
	signingKey := []byte(mustEnv("JWT_SIGNING_KEY"))
	clientID := mustEnv("GITHUB_OAUTH_CLIENT_ID")
	clientSecret := mustEnv("GITHUB_OAUTH_CLIENT_SECRET")
	callbackURL := mustEnv("GITHUB_OAUTH_CALLBACK_URL")
	allowedRedirect := envOr("FLEET_ALLOWED_REDIRECT", "http://127.0.0.1")
	addr := ":" + envOr("PORT", "8080")

	if err := pgstore.Migrate(databaseURL); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	store, err := pgstore.New(context.Background(), databaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	authH := auth.New(auth.Config{
		Store:           store,
		GitHub:          auth.NewHTTPGitHub(clientID, clientSecret),
		SigningKey:      signingKey,
		ClientID:        clientID,
		CallbackURL:     callbackURL,
		AllowedRedirect: allowedRedirect,
	})

	router := httpapi.NewRouter(httpapi.Options{Store: store, Auth: authH, SigningKey: signingKey})

	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 4: Run to verify pass + build the binary**

Run: `go test ./cmd/fleetd/ -run TestEnvOr`
Expected: PASS.
Run: `go build ./cmd/fleetd`
Expected: succeeds, no output (binary compiles; server packages only, no Wails/CGO).

- [ ] **Step 5: Vet the whole server surface**

Run: `go vet ./cmd/fleetd/... ./internal/server/...`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add cmd/fleetd/main.go cmd/fleetd/main_test.go
git commit -m "feat(server): fleetd entrypoint - env, migrations, http server"
```

---

### Task 11: Deploy assets — Dockerfile, fly.toml, README notes

**Files:**
- Create: `Dockerfile`
- Create: `fly.toml`
- Modify: `README.md` (append a "Backend server (fleetd)" section)

**Interfaces:**
- Consumes: `cmd/fleetd` builds (Task 10).
- Produces: deployable image + Fly config; no Go symbols.

- [ ] **Step 1: Write the Dockerfile**

Create `Dockerfile`:

```dockerfile
# Multi-stage build of the fleet backend (cmd/fleetd) as a static binary.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/fleetd ./cmd/fleetd

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fleetd /fleetd
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/fleetd"]
```

- [ ] **Step 2: Verify the image builds (if Docker is available)**

Run: `docker build -t fleetd:local .`
Expected: build succeeds through both stages (`naming to docker.io/library/fleetd:local`). If Docker is unavailable in this environment, skip and rely on `go build ./cmd/fleetd` from Task 10 as the compile check.

- [ ] **Step 3: Write fly.toml**

Create `fly.toml`:

```toml
app = "fleet-api"
primary_region = "nrt"

[build]
  dockerfile = "Dockerfile"

[env]
  PORT = "8080"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = "stop"
  auto_start_machines = true
  min_machines_running = 0

  [[http_service.checks]]
    method = "GET"
    path = "/healthz"
    interval = "15s"
    timeout = "2s"
    grace_period = "5s"
```

- [ ] **Step 4: Append README notes**

Add to the end of `README.md`:

```markdown
## Backend server (fleetd)

`cmd/fleetd` is the fleet cloud backend: GitHub OAuth identity plus a per-user
versioned document store with Last-Write-Wins sync. It is deployed to Fly.io
(region `nrt`) on Neon serverless Postgres. The desktop app never imports it.

### Environment

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Neon Postgres connection string (`postgres://...?sslmode=require`) |
| `JWT_SIGNING_KEY` | HS256 secret for access tokens |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub OAuth app client id |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth app client secret |
| `GITHUB_OAUTH_CALLBACK_URL` | This server's public callback, e.g. `https://fleet-api.fly.dev/auth/github/callback` |
| `FLEET_ALLOWED_REDIRECT` | Allowed loopback redirect prefix (default `http://127.0.0.1`) |
| `PORT` | Listen port (default `8080`) |

### Neon

1. Create a Neon project and copy the pooled connection string into `DATABASE_URL`.
2. Migrations (golang-migrate, embedded) run automatically at startup.

### Fly.io secrets

```bash
fly apps create fleet-api
fly secrets set \
  DATABASE_URL="postgres://user:pass@ep-xxx.neon.tech/fleet?sslmode=require" \
  JWT_SIGNING_KEY="$(openssl rand -base64 48)" \
  GITHUB_OAUTH_CLIENT_ID="..." \
  GITHUB_OAUTH_CLIENT_SECRET="..." \
  GITHUB_OAUTH_CALLBACK_URL="https://fleet-api.fly.dev/auth/github/callback"
fly deploy
```

Set the GitHub OAuth app's Authorization callback URL to
`https://fleet-api.fly.dev/auth/github/callback`. Health check: `GET /healthz` -> `ok`.

### Run tests locally

```bash
# Unit/handler tests (no database needed):
go test ./cmd/fleetd/... ./internal/server/...
# Postgres repo tests (start a throwaway Postgres, then):
DATABASE_URL_TEST="postgres://postgres:postgres@localhost:5432/fleet_test?sslmode=disable" \
  go test ./internal/server/pgstore/...
```
```

- [ ] **Step 5: Commit**

```bash
git add Dockerfile fly.toml README.md
git commit -m "chore(server): dockerfile, fly.toml (nrt), and backend README notes"
```

---

### Task 12: CI — build and test the server

**Files:**
- Create: `.github/workflows/server.yml`

**Interfaces:**
- Consumes: all server packages (Tasks 1-10) and Postgres repo tests (Tasks 2-3).
- Produces: a GitHub Actions workflow; no Go symbols.

- [ ] **Step 1: Write the workflow**

Create `.github/workflows/server.yml`:

```yaml
name: server
on:
  push:
    paths:
      - "cmd/fleetd/**"
      - "internal/server/**"
      - "go.mod"
      - "go.sum"
      - "Dockerfile"
      - "fly.toml"
      - ".github/workflows/server.yml"
    tags: ["server-v*"]
  pull_request:
    paths:
      - "cmd/fleetd/**"
      - "internal/server/**"
      - "go.mod"
      - "go.sum"
      - ".github/workflows/server.yml"

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: fleet_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    env:
      DATABASE_URL_TEST: postgres://postgres:postgres@localhost:5432/fleet_test?sslmode=disable
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - name: Build server binary
        run: go build ./cmd/fleetd
      - name: Vet server packages
        run: go vet ./cmd/fleetd/... ./internal/server/...
      - name: Test server packages
        run: go test ./cmd/fleetd/... ./internal/server/...

  deploy:
    needs: test
    if: startsWith(github.ref, 'refs/tags/server-v')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - run: flyctl deploy --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
```

- [ ] **Step 2: Validate YAML + confirm the commands the workflow runs**

Run: `go build ./cmd/fleetd && go vet ./cmd/fleetd/... ./internal/server/... && go test ./cmd/fleetd/... ./internal/server/...`
Expected: all succeed; pgstore Postgres tests skip locally (no `DATABASE_URL_TEST`), everything else PASS.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/server.yml
git commit -m "ci(server): build, vet, and test fleetd with a postgres service; tag deploy to fly"
```

---

## Self-Review Notes

- **Spec coverage:** identity/OAuth (Tasks 4-7), tokens + rotation (Tasks 4, 7), users/refresh_tokens/documents/user_versions schema (Task 2), LWW + cursor + tombstones + per-user isolation (Task 3), REST `/sync` contract (Task 9), `/healthz` + slog + rate limiting (Tasks 1, 8), embedded golang-migrate (Task 2), Dockerfile + fly.toml (nrt) + README secrets/Neon (Task 11), CI build+test (Task 12). Testing strategy: pgstore vs real Postgres with skip (Tasks 2-3), httptest handler tests with fake store + fake GitHub (Tasks 7, 9), table-driven LWW (Task 3), auth exchange/refresh/rotation (Task 7).
- **Type consistency:** `Store` interface method set (Task 2) is implemented by `*Pg` (Tasks 2-3) and by every fake (`fakeStore` in Task 7, `syncFakeStore` in Task 9); `pgstore.Doc`/`PushResult` JSON tags match the shared contract; `auth.Config`/`auth.Handlers`/`auth.New` used identically in Task 7 tests, Task 9 router, and Task 10 main; `httpapi.Options` gains fields in Task 9 without breaking Task 1's `Options{}` healthz test.
- **Out of scope (correctly excluded):** `internal/cloud`, `internal/syncengine`, `store.Record.UpdatedAt`, and all Wails/Svelte changes are Plan B (desktop) and are not touched here.