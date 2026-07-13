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
	pingErr  error
}

func (f *syncFakeStore) Ping(ctx context.Context) error { return f.pingErr }

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
