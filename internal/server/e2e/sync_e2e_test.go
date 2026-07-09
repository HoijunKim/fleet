// Package e2e drives the real fleet backend end-to-end: real router, real
// auth wiring, real pgstore against Postgres, real internal/cloud client -
// all in-process (no separate fleetd process). It proves the /sync HTTP
// contract the desktop client depends on: push/pull, Last-Write-Wins,
// per-user isolation, tombstones, and unauthorized rejection.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/server/auth"
	httpapi "github.com/hoijun/fleet/internal/server/http"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

// signingKey is fixed for the whole test so mustAccessToken and the router's
// AuthMiddleware agree on how to verify tokens.
var signingKey = []byte("test-e2e-signing-key")

// stubGitHubClient satisfies auth.GitHubClient so the router can be built
// exactly like production (with /auth/github routes registered). The test
// only drives /sync, so these methods are never invoked; they fail loudly if
// that assumption is ever violated.
type stubGitHubClient struct{}

func (stubGitHubClient) Exchange(ctx context.Context, code string) (string, error) {
	return "", errors.New("stubGitHubClient.Exchange: not implemented, /sync e2e test never calls /auth/github")
}

func (stubGitHubClient) User(ctx context.Context, accessToken string) (auth.GitHubUser, error) {
	return auth.GitHubUser{}, errors.New("stubGitHubClient.User: not implemented, /sync e2e test never calls /auth/github")
}

// newTestServer migrates and truncates dbURL, builds the real router (real
// pgstore.Store, real auth.Handlers) and wraps it in an httptest server. It
// also returns a raw pgxpool.Pool for inserting users directly, bypassing the
// GitHub OAuth flow this test does not exercise.
func newTestServer(t *testing.T, dbURL string) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	if err := pgstore.Migrate(dbURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx,
		`TRUNCATE documents, user_versions, refresh_tokens, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	store, err := pgstore.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	t.Cleanup(store.Close)

	authHandlers := auth.New(auth.Config{
		Store:       store,
		GitHub:      stubGitHubClient{},
		SigningKey:  signingKey,
		ClientID:    "test-client-id",
		CallbackURL: "http://127.0.0.1:0/auth/github/callback",
	})

	router := httpapi.NewRouter(httpapi.Options{
		Store:      store,
		Auth:       authHandlers,
		SigningKey: signingKey,
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, pool
}

// insertUser writes a user row directly via pgx, bypassing the GitHub OAuth
// handshake. github_id is derived from the current nanosecond clock so
// repeated runs against a non-truncated database never collide on the
// UNIQUE(github_id) constraint.
func insertUser(t *testing.T, pool *pgxpool.Pool, login string) string {
	t.Helper()
	id := uuid.NewString()
	githubID := time.Now().UnixNano()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, github_id, login) VALUES ($1, $2, $3)`, id, githubID, login); err != nil {
		t.Fatalf("insert user %s: %v", login, err)
	}
	return id
}

// mustAccessToken mints an access token with the same signing key the router
// was built with, so AuthMiddleware accepts it.
func mustAccessToken(t *testing.T, userID string) string {
	t.Helper()
	tok, err := auth.IssueAccess(signingKey, userID, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	return tok
}

// findDoc returns the doc with docID in docs, or nil if absent.
func findDoc(docs []cloud.Doc, docID string) *cloud.Doc {
	for i := range docs {
		if docs[i].DocID == docID {
			return &docs[i]
		}
	}
	return nil
}

// TestSyncE2E drives the real HTTP router (real pgstore, real auth wiring)
// through the real internal/cloud client against a real Postgres. It skips
// unless DATABASE_URL_TEST is set, matching the pgstore package's tests, so
// it auto-runs in the CI postgres job and skips cleanly on a bare `go test`.
func TestSyncE2E(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL_TEST")
	if dbURL == "" {
		t.Skip("set DATABASE_URL_TEST to run")
	}

	srv, pool := newTestServer(t, dbURL)
	client := cloud.New(srv.URL)

	user1 := insertUser(t, pool, "user1")
	user2 := insertUser(t, pool, "user2")
	access1 := mustAccessToken(t, user1)
	access2 := mustAccessToken(t, user2)

	// t0 < t1 < t2 < t3, RFC3339Nano UTC, as the LWW compare expects.
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	t1 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	t2 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	t3 := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)

	const docID = "proj-1"

	t.Run("push new doc is accepted", func(t *testing.T) {
		results, cursor, err := client.Push([]cloud.Doc{{
			Kind: "project", DocID: docID, Payload: json.RawMessage(`{"name":"v1"}`), UpdatedAt: t1,
		}}, access1)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		if len(results) != 1 || !results[0].Accepted || results[0].Version <= 0 {
			t.Fatalf("push result = %+v, want one accepted result with version > 0", results)
		}
		if cursor <= 0 {
			t.Fatalf("cursor = %d, want > 0", cursor)
		}
	})

	t.Run("pull returns the pushed doc", func(t *testing.T) {
		docs, cursor, err := client.Pull(0, access1)
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
		got := findDoc(docs, docID)
		if got == nil || string(got.Payload) != `{"name":"v1"}` {
			t.Fatalf("pull docs = %+v, want %q with payload v1", docs, docID)
		}
		if cursor <= 0 {
			t.Fatalf("cursor = %d, want > 0", cursor)
		}
	})

	t.Run("LWW: older updated_at is rejected", func(t *testing.T) {
		results, _, err := client.Push([]cloud.Doc{{
			Kind: "project", DocID: docID, Payload: json.RawMessage(`{"name":"stale"}`), UpdatedAt: t0,
		}}, access1)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		if len(results) != 1 || results[0].Accepted {
			t.Fatalf("stale push result = %+v, want accepted=false", results)
		}
	})

	t.Run("LWW: newer updated_at wins and pull reflects it", func(t *testing.T) {
		results, _, err := client.Push([]cloud.Doc{{
			Kind: "project", DocID: docID, Payload: json.RawMessage(`{"name":"v2"}`), UpdatedAt: t2,
		}}, access1)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		if len(results) != 1 || !results[0].Accepted {
			t.Fatalf("newer push result = %+v, want accepted=true", results)
		}

		docs, _, err := client.Pull(0, access1)
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
		got := findDoc(docs, docID)
		if got == nil || string(got.Payload) != `{"name":"v2"}` {
			t.Fatalf("pull after LWW winner = %+v, want %q with payload v2", docs, docID)
		}
	})

	t.Run("per-user isolation", func(t *testing.T) {
		docs, cursor, err := client.Pull(0, access2)
		if err != nil {
			t.Fatalf("pull user2: %v", err)
		}
		if len(docs) != 0 || cursor != 0 {
			t.Fatalf("user2 pull = %+v cursor=%d, want no docs from user1", docs, cursor)
		}
	})

	t.Run("tombstone: deleted push then pull shows deleted", func(t *testing.T) {
		results, _, err := client.Push([]cloud.Doc{{
			Kind: "project", DocID: docID, Payload: json.RawMessage(`{"name":"v2"}`), UpdatedAt: t3, Deleted: true,
		}}, access1)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		if len(results) != 1 || !results[0].Accepted {
			t.Fatalf("tombstone push result = %+v, want accepted=true", results)
		}

		docs, _, err := client.Pull(0, access1)
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
		got := findDoc(docs, docID)
		if got == nil || !got.Deleted {
			t.Fatalf("pull after tombstone = %+v, want %q deleted=true", docs, docID)
		}
	})

	t.Run("unauthorized: bogus access token is rejected", func(t *testing.T) {
		_, _, err := client.Pull(0, "bogus-access-token")
		if err == nil {
			t.Fatal("pull with bogus token: want error, got nil")
		}
		if !errors.Is(err, cloud.ErrUnauthorized) {
			t.Fatalf("pull with bogus token: err = %v, want ErrUnauthorized", err)
		}
	})
}
