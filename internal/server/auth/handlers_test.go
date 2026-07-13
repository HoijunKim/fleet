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
	fam     int
}
type refreshRow struct {
	userID   string
	revoked  bool
	expires  time.Time
	familyID string
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]pgstore.User{}, refresh: map[string]refreshRow{}}
}
func (f *fakeStore) Ping(ctx context.Context) error { return nil }
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
	f.fam++
	f.refresh[hash] = refreshRow{userID: userID, expires: exp, familyID: fmt.Sprintf("fam-%d", f.fam)}
	return nil
}

// revokeFamily marks every token in familyID revoked (mirrors the real store).
func (f *fakeStore) revokeFamily(familyID string) {
	for h, r := range f.refresh {
		if r.familyID == familyID {
			r.revoked = true
			f.refresh[h] = r
		}
	}
}

func (f *fakeStore) RotateRefreshToken(ctx context.Context, oldHash, newHash string, exp time.Time) (string, error) {
	row, ok := f.refresh[oldHash]
	if !ok {
		return "", fmt.Errorf("invalid") // unknown token
	}
	if row.revoked {
		f.revokeFamily(row.familyID) // reuse: revoke the whole family
		return row.userID, pgstore.ErrRefreshReuse
	}
	if time.Now().After(row.expires) {
		return "", fmt.Errorf("invalid")
	}
	row.revoked = true
	f.refresh[oldHash] = row
	f.refresh[newHash] = refreshRow{userID: row.userID, expires: exp, familyID: row.familyID}
	return row.userID, nil
}

func (f *fakeStore) RevokeRefreshToken(ctx context.Context, hash string) error {
	if row, ok := f.refresh[hash]; ok {
		f.revokeFamily(row.familyID) // logout ends the whole chain
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
		Store:        store,
		GitHub:       fakeGitHub{user: GitHubUser{ID: 5, Login: "octo", AvatarURL: "http://a"}},
		SigningKey:   key,
		ClientID:     "cid",
		AuthorizeURL: "https://github.test/authorize",
		CallbackURL:  "https://api.test/auth/github/callback",
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
	// Reusing the old refresh must fail (opaque 401) AND revoke the family.
	body, _ = json.Marshal(map[string]string{"refresh_token": ex.Refresh})
	resp, _ = client.Post(srv.URL+"/auth/refresh", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want 401", resp.StatusCode)
	}
	// Family revocation: the rotated (live) token now fails too - a stolen token
	// cannot outlive the reuse of any token in its chain.
	body, _ = json.Marshal(map[string]string{"refresh_token": rf.Refresh})
	resp, _ = client.Post(srv.URL+"/auth/refresh", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-reuse rotated token status = %d, want 401 (family revoked)", resp.StatusCode)
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
		Store:        newFakeStore(),
		GitHub:       fakeGitHub{user: GitHubUser{ID: 5, Login: "o"}},
		SigningKey:   []byte("k"),
		AuthorizeURL: "https://github.test/authorize",
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

// TestGithubLoginRedirectValidation exercises the strict loopback-only
// redirect check (FIX 1). A naive strings.HasPrefix(redirect, "http://127.0.0.1")
// check would accept lookalike hosts like "127.0.0.1.attacker.com" and
// userinfo tricks like "127.0.0.1@evil.com", both of which resolve to an
// attacker-controlled host and would leak the link_code on to it.
func TestGithubLoginRedirectValidation(t *testing.T) {
	cases := []struct {
		name     string
		redirect string
		wantOK   bool
	}{
		{"subdomain lookalike host", "http://127.0.0.1.attacker.com:9/cb", false},
		{"userinfo trick", "http://127.0.0.1@evil.com/cb", false},
		{"https scheme not allowed", "https://127.0.0.1:9/cb", false},
		{"non-loopback host", "http://evil.com/cb", false},
		{"loopback with no port", "http://127.0.0.1/cb", false},
		{"loopback with port", "http://127.0.0.1:53017/cb", true},
		{"localhost with port", "http://localhost:53017/cb", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New(Config{
				Store:        newFakeStore(),
				GitHub:       fakeGitHub{user: GitHubUser{ID: 1, Login: "o"}},
				SigningKey:   []byte("k"),
				AuthorizeURL: "https://github.test/authorize",
			})
			srv := newTestServer(h)
			defer srv.Close()
			client := srv.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

			sum := sha256.Sum256([]byte("verifier"))
			challenge := base64.RawURLEncoding.EncodeToString(sum[:])
			loginURL := srv.URL + "/auth/github/login?state=cs&code_challenge=" + challenge + "&redirect=" + url.QueryEscape(tc.redirect)

			resp, err := client.Get(loginURL)
			if err != nil {
				t.Fatalf("login: %v", err)
			}
			if tc.wantOK {
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("redirect %q: status = %d, want 302", tc.redirect, resp.StatusCode)
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("redirect %q: status = %d, want 400", tc.redirect, resp.StatusCode)
			}
			if loc := resp.Header.Get("Location"); loc != "" {
				t.Fatalf("redirect %q: unexpected Location header %q on rejection", tc.redirect, loc)
			}
		})
	}
}

// TestCallbackRejectsInvalidStashedRedirect exercises the defense-in-depth
// re-validation in GithubCallback. It bypasses GithubLogin's own check by
// stashing a malicious redirect directly into the pending store, simulating
// what a future regression in the login-time check would allow through, and
// confirms the callback still refuses to redirect to it.
func TestCallbackRejectsInvalidStashedRedirect(t *testing.T) {
	h := New(Config{
		Store:        newFakeStore(),
		GitHub:       fakeGitHub{user: GitHubUser{ID: 1, Login: "o"}},
		SigningKey:   []byte("k"),
		AuthorizeURL: "https://github.test/authorize",
	})
	if !h.pending.put("forged-state", pendingAuth{
		clientState: "cs", codeChallenge: "chal", redirect: "http://evil.com/cb",
	}, 10*time.Minute) {
		t.Fatal("pending.put unexpectedly failed")
	}

	srv := newTestServer(h)
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Get(srv.URL + "/auth/github/callback?code=xyz&state=forged-state")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want 400", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Fatalf("unexpected Location header %q on rejection", loc)
	}
}

// TestExchangeRejectsOversizedBody exercises FIX 2: the request body is
// wrapped with http.MaxBytesReader before decoding, so a body over the cap
// is rejected (400) instead of being buffered in full.
func TestExchangeRejectsOversizedBody(t *testing.T) {
	h := New(Config{
		Store:        newFakeStore(),
		GitHub:       fakeGitHub{user: GitHubUser{ID: 1, Login: "o"}},
		SigningKey:   []byte("k"),
		AuthorizeURL: "https://github.test/authorize",
	})
	srv := newTestServer(h)
	defer srv.Close()

	oversized := strings.Repeat("a", maxRequestBodyBytes+1)
	body := `{"link_code":"x","code_verifier":"` + oversized + `"}`
	resp, err := srv.Client().Post(srv.URL+"/auth/exchange", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want 400", resp.StatusCode)
	}
}
