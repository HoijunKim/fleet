package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/store"
	"github.com/hoijun/fleet/internal/syncengine"
)

func TestAwaitOAuth(t *testing.T) {
	// A delivered callback wins.
	ch := make(chan oauthCapture, 1)
	ch <- oauthCapture{code: "c", state: "s"}
	if got, e := awaitOAuth(ch, make(chan struct{}), time.Second); e != "" || got.code != "c" {
		t.Errorf("callback path: got %+v, %q", got, e)
	}
	// Cancel is reported as the soft "cancelled" sentinel.
	cancel := make(chan struct{}, 1)
	cancel <- struct{}{}
	if _, e := awaitOAuth(make(chan oauthCapture), cancel, time.Second); e != "cancelled" {
		t.Errorf("cancel path: got %q, want cancelled", e)
	}
	// Timeout is reported distinctly.
	if _, e := awaitOAuth(make(chan oauthCapture), make(chan struct{}), time.Millisecond); e != "sign-in timed out" {
		t.Errorf("timeout path: got %q", e)
	}
}

func TestCancelAuthUnblocksAwait(t *testing.T) {
	a := &App{authCancel: make(chan struct{}, 1)}
	done := make(chan string, 1)
	go func() {
		_, e := awaitOAuth(make(chan oauthCapture), a.authCancel, time.Minute)
		done <- e
	}()
	a.CancelAuth()
	select {
	case e := <-done:
		if e != "cancelled" {
			t.Errorf("CancelAuth should yield cancelled, got %q", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CancelAuth did not unblock the wait")
	}
	a.CancelAuth() // no-op second call must not panic on a drained buffered channel
}

func TestDirExists(t *testing.T) {
	a := &App{}
	dir := t.TempDir()
	if !a.DirExists(dir) {
		t.Error("an existing dir should be true")
	}
	if a.DirExists(filepath.Join(dir, "nope")) {
		t.Error("a missing path should be false")
	}
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.DirExists(f) {
		t.Error("a file should not count as a directory")
	}
	if a.DirExists("") {
		t.Error("an empty path should be false")
	}
}

// --- PKCE + OAuth-callback validation -----------------------------------
//
// These are the only pieces of AuthStart that are unit-testable in isolation:
// AuthStart itself opens a real loopback listener, launches the system
// browser, and blocks on a real HTTP redirect, which is only manually
// verifiable (see task-B8-report.md for the manual verification steps).

func TestPKCE(t *testing.T) {
	v1, c1, err := pkce()
	if err != nil {
		t.Fatalf("pkce(): %v", err)
	}
	v2, c2, err := pkce()
	if err != nil {
		t.Fatalf("pkce(): %v", err)
	}
	if v1 == "" || c1 == "" {
		t.Fatal("pkce() returned an empty verifier/challenge")
	}
	if v1 == v2 || c1 == c2 {
		t.Error("two pkce() calls produced identical output; rand.Read is not varying")
	}
	sum := sha256.Sum256([]byte(v1))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if c1 != want {
		t.Errorf("challenge = %q, want S256(verifier) = %q", c1, want)
	}
}

func TestValidateOAuthCallback(t *testing.T) {
	if code, errMsg := validateOAuthCallback(oauthCapture{code: "lc", state: "s1"}, "s1"); code != "lc" || errMsg != "" {
		t.Errorf("matching state: code=%q errMsg=%q, want code=lc errMsg=empty", code, errMsg)
	}
	if code, errMsg := validateOAuthCallback(oauthCapture{code: "lc", state: "attacker"}, "s1"); code != "" || errMsg == "" {
		t.Errorf("state mismatch (CSRF) must be rejected: code=%q errMsg=%q", code, errMsg)
	}
	if code, errMsg := validateOAuthCallback(oauthCapture{code: "", state: "s1"}, "s1"); code != "" || errMsg == "" {
		t.Errorf("missing link_code must be rejected: code=%q errMsg=%q", code, errMsg)
	}
}

// --- sync state machine, single-flight, signed-out mapping --------------

// testApp wires an App the same way NewApp does (real store, real
// syncengine.Engine) but pointed at an httptest backend and a MemCredStore
// instead of the OS keychain, so the sync path is exercised end-to-end
// without touching the network or a real credential store.
func testApp(t *testing.T, backendURL string, signedIn bool) (*App, *cloud.MemCredStore) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := cloud.New(backendURL)
	eng := syncengine.New(st, cl, filepath.Join(dir, "sync.json"), func(string) string { return "" })
	creds := &cloud.MemCredStore{}
	a := &App{
		store: st, cloudClient: cl, creds: creds, engine: eng,
		syncTrigger: make(chan struct{}, 1),
		syncView:    SyncStateView{State: "signedout"},
	}
	if signedIn {
		_ = creds.SaveRefresh("r0")
		a.signedIn = true
		a.session = cloud.NewSession(cl, "acc", "r0", func(tok cloud.Tokens) { _ = creds.SaveRefresh(tok.Refresh) })
	}
	return a, creds
}

func TestRunSyncNotSignedIn(t *testing.T) {
	a, _ := testApp(t, "http://127.0.0.1:0", false)
	if err := a.runSync(); !errors.Is(err, errNotSignedIn) {
		t.Fatalf("runSync() = %v, want errNotSignedIn", err)
	}
	if got := a.SyncState().State; got != "signedout" {
		t.Errorf("state = %q, want untouched signedout", got)
	}
}

func TestRunSyncSuccessTransitionsToSynced(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []cloud.Doc{}, "cursor": 0})
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []cloud.PushResult{}, "cursor": 0})
		}
	}))
	defer ts.Close()

	a, _ := testApp(t, ts.URL, true)
	if err := a.runSync(); err != nil {
		t.Fatalf("runSync(): %v", err)
	}
	got := a.SyncState()
	if got.State != "synced" {
		t.Errorf("state = %q, want synced", got.State)
	}
	if got.LastSyncedUnix == 0 {
		t.Error("LastSyncedUnix not stamped on a successful sync")
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty on success", got.Error)
	}
}

func TestRunSyncOfflineSetsOfflineState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // now unreachable (connection refused): a network, not application, failure

	a, _ := testApp(t, url, true)
	if err := a.runSync(); err == nil {
		t.Fatal("expected an error while offline")
	}
	got := a.SyncState()
	if got.State != "offline" {
		t.Errorf("state = %q, want offline", got.State)
	}
	if got.Error == "" {
		t.Error("expected Error populated for the offline pill")
	}
}

// TestRunSyncRefreshFailedSignsOut is fix (1) from the Task B4 review: a dead
// refresh token (server rejects /auth/refresh with 401 -> cloud.ErrRefreshFailed)
// must surface the distinguishable "signedout" state, not "offline"/"error",
// and must clear the in-memory session and the persisted refresh token so the
// app does not keep retrying a token that can never succeed.
func TestRunSyncRefreshFailedSignsOut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // both /sync (401 -> refresh) and /auth/refresh (401 -> dead token)
	}))
	defer ts.Close()

	a, creds := testApp(t, ts.URL, true)
	err := a.runSync()
	if !errors.Is(err, cloud.ErrRefreshFailed) {
		t.Fatalf("runSync() err = %v, want cloud.ErrRefreshFailed", err)
	}

	if got := a.SyncState().State; got != "signedout" {
		t.Errorf("state = %q, want signedout", got)
	}

	a.authMu.Lock()
	signedIn, sess := a.signedIn, a.session
	a.authMu.Unlock()
	if signedIn {
		t.Error("signedIn still true after a refresh-token rejection")
	}
	if sess != nil {
		t.Error("session not cleared after a refresh-token rejection")
	}
	if tok, _ := creds.LoadRefresh(); tok != "" {
		t.Errorf("refresh token not cleared from creds: %q", tok)
	}

	// A subsequent SyncNow now takes the not-signed-in path, confirming the
	// app is genuinely signed out (not just showing the pill as such).
	if msg := a.SyncNow(); msg != "not signed in" {
		t.Errorf("SyncNow() after sign-out = %q, want %q", msg, "not signed in")
	}
}

// TestSyncNowMapsRefreshFailedToFriendlyMessage checks SyncNow's own error
// mapping for the ErrRefreshFailed case, before signOutLocally's side effects
// change what a later call would see.
func TestSyncNowMapsRefreshFailedToFriendlyMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	a, _ := testApp(t, ts.URL, true)
	if msg := a.SyncNow(); msg != "session expired, please sign in again" {
		t.Errorf("SyncNow() = %q, want the friendly session-expired message", msg)
	}
}

// TestRunSyncSingleFlightBusyIsNoOp is fix (2) from the Task B4 review: a
// caller that finds a sync already in progress (syncRunMu held) must not
// start a second SyncOnce, must not touch the network, and must leave the
// pill state as whatever the in-flight sync already set.
func TestRunSyncSingleFlightBusyIsNoOp(t *testing.T) {
	var hit int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []cloud.Doc{}, "cursor": 0})
	}))
	defer ts.Close()

	a, _ := testApp(t, ts.URL, true)
	a.syncView = SyncStateView{State: "syncing"} // simulate: another sync already set this
	a.syncRunMu.Lock()                           // simulate: that other sync is still in flight

	if err := a.runSync(); err != nil {
		t.Fatalf("runSync() while busy = %v, want nil (no-op success)", err)
	}
	if got := atomic.LoadInt32(&hit); got != 0 {
		t.Errorf("runSync() while busy made %d network call(s), want 0", got)
	}
	if got := a.SyncState().State; got != "syncing" {
		t.Errorf("state changed to %q while busy, want untouched syncing", got)
	}
	a.syncRunMu.Unlock()
}

// TestRunSyncConcurrentSingleFlight proves the guard under real concurrency
// (not just a manually-held lock): a slow /sync handler holds the first
// runSync's Pull open on a channel; a second, concurrent runSync must return
// immediately without reaching the server, and once the first is released it
// must complete normally. This is the scenario the Task B4 review flagged -
// the background loop and a UI-triggered SyncNow calling runSync at the same
// time - reproduced with real goroutines instead of a single mutex assertion.
func TestRunSyncConcurrentSingleFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var pullCalls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt32(&pullCalls, 1)
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []cloud.Doc{}, "cursor": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []cloud.PushResult{}, "cursor": 0})
	}))
	defer ts.Close()

	a, _ := testApp(t, ts.URL, true)

	var wg sync.WaitGroup
	var firstErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstErr = a.runSync()
	}()

	<-started // the first sync is in flight, holding syncRunMu, blocked inside Pull

	secondErr := a.runSync() // must return immediately, must not call the server
	if secondErr != nil {
		t.Errorf("second concurrent runSync() = %v, want nil (single-flight no-op)", secondErr)
	}

	close(release)
	wg.Wait()

	if firstErr != nil {
		t.Errorf("first runSync() = %v, want nil", firstErr)
	}
	if got := atomic.LoadInt32(&pullCalls); got != 1 {
		t.Errorf("Pull was called %d time(s) while single-flighted, want exactly 1", got)
	}
	if got := a.SyncState().State; got != "synced" {
		t.Errorf("final state = %q, want synced", got)
	}
}

// --- AuthStatus / SignOut / SyncState getters ----------------------------

func TestAuthStatusReflectsFields(t *testing.T) {
	a := &App{}
	a.authMu.Lock()
	a.signedIn = true
	a.user = cloud.User{Login: "octo", AvatarURL: "http://a/x.png"}
	a.authMu.Unlock()
	got := a.AuthStatus()
	if !got.SignedIn || got.Login != "octo" || got.AvatarURL != "http://a/x.png" {
		t.Errorf("AuthStatus() = %+v", got)
	}
}

func TestSignOutClearsSessionAndCreds(t *testing.T) {
	var logoutHits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/logout" {
			atomic.AddInt32(&logoutHits, 1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	a, creds := testApp(t, ts.URL, true)
	if msg := a.SignOut(); msg != "" {
		t.Fatalf("SignOut() = %q, want empty", msg)
	}
	if got := atomic.LoadInt32(&logoutHits); got != 1 {
		t.Errorf("expected exactly one /auth/logout call, got %d", got)
	}
	if got := a.AuthStatus(); got.SignedIn {
		t.Errorf("still signed in after SignOut(): %+v", got)
	}
	if tok, _ := creds.LoadRefresh(); tok != "" {
		t.Errorf("refresh token not cleared: %q", tok)
	}
	if got := a.SyncState().State; got != "signedout" {
		t.Errorf("pill state = %q, want signedout", got)
	}
}

func TestSyncStateReturnsCurrentView(t *testing.T) {
	a := &App{syncView: SyncStateView{State: "offline", Error: "boom", LastSyncedUnix: 42}}
	got := a.SyncState()
	if got.State != "offline" || got.Error != "boom" || got.LastSyncedUnix != 42 {
		t.Errorf("SyncState() = %+v", got)
	}
}
