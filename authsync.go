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

// randB64 returns n random bytes as base64url (no padding), or an error if
// the system CSPRNG fails.
func randB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkce generates a PKCE verifier and its S256 challenge.
func pkce() (verifier, challenge string, err error) {
	verifier, err = randB64(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// oauthCapture is what the loopback callback captures from the browser
// redirect: the one-time link_code and the state it echoed back.
type oauthCapture struct {
	code, state string
}

// validateOAuthCallback checks a captured callback against the state AuthStart
// generated, returning the link_code on success or an error message on
// failure. Split out of AuthStart so the CSRF-state check is unit-testable
// without a live loopback listener or a real system browser.
func validateOAuthCallback(got oauthCapture, wantState string) (code, errMsg string) {
	if got.state != wantState {
		return "", "state mismatch (possible CSRF)"
	}
	if got.code == "" {
		return "", "no link code returned"
	}
	return got.code, ""
}

// AuthStart runs the RFC 8252 native GitHub OAuth flow: a loopback listener on
// 127.0.0.1:<ephemeral>, PKCE, the system browser, capture of the link_code,
// token exchange, and refresh-token storage in the OS keychain. Returns "" on
// success or an error message.
//
// This method is only manually verifiable end-to-end (it opens a real browser
// and waits on a real loopback redirect); the CSRF-state check it performs is
// covered in isolation by TestValidateOAuthCallback, and the PKCE verifier/
// challenge derivation by TestPKCE.
func (a *App) AuthStart() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "cannot open loopback listener: " + err.Error()
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	verifier, challenge, err := pkce()
	if err != nil {
		return "cannot generate pkce verifier: " + err.Error()
	}
	state, err := randB64(16)
	if err != nil {
		return "cannot generate state: " + err.Error()
	}
	login := apiURL() + "/auth/github/login?" + url.Values{
		"state":          {state},
		"code_challenge": {challenge},
		"redirect":       {redirect},
	}.Encode()

	ch := make(chan oauthCapture, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body style=\"font-family:sans-serif;background:#0e1116;color:#e6edf3;text-align:center;padding-top:80px\"><h2>fleet</h2><p>Signed in. You can close this window.</p></body></html>"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		ch <- oauthCapture{code: q.Get("link_code"), state: q.Get("state")}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	wruntime.BrowserOpenURL(a.ctx, login)

	var got oauthCapture
	select {
	case got = <-ch:
	case <-time.After(3 * time.Minute):
		return "sign-in timed out"
	}
	code, errMsg := validateOAuthCallback(got, state)
	if errMsg != "" {
		return errMsg
	}

	tokens, user, err := a.cloudClient.Exchange(code, verifier)
	if err != nil {
		return "exchange failed: " + err.Error()
	}
	if err := a.creds.SaveRefresh(tokens.Refresh); err != nil {
		return "keychain save failed: " + err.Error()
	}

	a.authMu.Lock()
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
	a.signOutLocally()
	return ""
}

// SyncNow runs one sync immediately and returns "" or an error message.
func (a *App) SyncNow() string {
	switch err := a.runSync(); {
	case err == nil:
		return ""
	case errors.Is(err, errNotSignedIn):
		return "not signed in"
	case errors.Is(err, cloud.ErrRefreshFailed):
		return "session expired, please sign in again"
	default:
		return err.Error()
	}
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

// signOutLocally clears in-memory session state and the persisted refresh
// token, then broadcasts signed-out. This is the same local cleanup SignOut
// performs; it is also invoked from runSync when the refresh token itself is
// rejected by the server (cloud.ErrRefreshFailed) - the token is dead, so
// keeping it around would just make every future sync fail again, and the UI
// needs auth:changed(signedIn=false) to prompt a fresh login.
func (a *App) signOutLocally() {
	_ = a.creds.DeleteRefresh()

	a.authMu.Lock()
	a.user = cloud.User{}
	a.signedIn = false
	a.session = nil
	a.authMu.Unlock()

	a.emitAuth()
	a.setSyncState("signedout", "")
}

// runSync performs one guarded sync and updates the pill state.
//
// Two invariants matter here, both required by the Task B4 (cloud.Session)
// review:
//
//  1. Signed-out signal: a refresh-token rejection (cloud.ErrRefreshFailed) is
//     mapped to the distinguishable "signedout" state (not "error" or
//     "offline"), and the dead session/credential is cleared, so the UI can
//     prompt re-login instead of retrying a session that can never recover.
//  2. Single-flight: syncRunMu ensures only one SyncOnce runs at a time. The
//     background loop (syncLoop) and a UI-triggered SyncNow can both reach
//     this method concurrently (each Wails-bound call runs on its own
//     goroutine); running two syncs at once on the same Session/Engine would
//     risk a double Refresh (one spuriously failing on an already-rotated
//     refresh token) and concurrent store/state mutation. A caller that finds
//     a sync already in progress is a no-op success - the in-flight sync will
//     still pick up the latest local state when it snapshots the store.
func (a *App) runSync() error {
	a.authMu.Lock()
	signedIn := a.signedIn
	sess := a.session
	a.authMu.Unlock()
	if !signedIn || sess == nil {
		return errNotSignedIn
	}

	if !a.syncRunMu.TryLock() {
		return nil // a sync is already in progress; treat as a no-op success
	}
	defer a.syncRunMu.Unlock()

	a.setSyncState("syncing", "")
	if err := sess.WithAccess(a.engine.SyncOnce); err != nil {
		if errors.Is(err, cloud.ErrRefreshFailed) {
			a.signOutLocally()
			return err
		}
		if isOffline(err) {
			a.setSyncState("offline", err.Error())
		} else {
			a.setSyncState("error", err.Error())
		}
		return err
	}
	a.setSyncState("synced", "")
	// Capture both flags (each clears itself), then surface the stronger one: a
	// clobbered UNSYNCED local edit (recoverable) outranks a plain remote update.
	lost := a.engine.LostLocalEdit()
	remote := a.engine.TookRemoteEdit()
	if a.ctx != nil {
		if lost {
			wruntime.EventsEmit(a.ctx, "sync:conflict", nil)
		} else if remote {
			wruntime.EventsEmit(a.ctx, "sync:remoteEdit", nil)
		}
	}
	return nil
}

// startSync restores a stored session (silent sign-in) and starts the loop.
func (a *App) startSync(ctx context.Context) {
	go func() {
		if refresh, _ := a.creds.LoadRefresh(); refresh != "" {
			tok, err := a.cloudClient.Refresh(refresh)
			switch {
			case err == nil:
				_ = a.creds.SaveRefresh(tok.Refresh)
				a.authMu.Lock()
				a.signedIn = true
				a.session = cloud.NewSession(a.cloudClient, tok.Access, tok.Refresh, func(t cloud.Tokens) {
					_ = a.creds.SaveRefresh(t.Refresh)
				})
				a.authMu.Unlock()
				a.emitAuth()
				a.triggerSync()
			case errors.Is(err, cloud.ErrRefreshFailed):
				// Token permanently rejected: drop it so we stop silently retrying
				// a dead token on every launch. signOutLocally clears the token and
				// broadcasts signed-out so the user can re-authenticate.
				a.signOutLocally()
			}
			// A transient error (network/5xx) leaves the token in place to retry.
		}
	}()
	go a.syncLoop(ctx)
}

// syncLoop syncs on an interval, on demand (triggerSync), and retries failures
// with capped exponential backoff. A successful sync, a not-signed-in no-op,
// or a fresh signed-out transition (cloud.ErrRefreshFailed) all reset the
// backoff and fall back to the normal interval: a dead refresh token cannot
// succeed on retry, and signOutLocally has already flipped signedIn to false,
// so the next tick's signedIn check idles the loop instead of backing off.
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
		err := a.runSync()
		if err == nil || errors.Is(err, errNotSignedIn) || errors.Is(err, cloud.ErrRefreshFailed) {
			backoff = base
			timer.Reset(interval)
			continue
		}
		timer.Reset(backoff)
		backoff = NextBackoffDelay(backoff, base, max)
	}
}

// NextBackoffDelay is the loop's capped exponential backoff, delegating to the
// tested syncengine helper so the backoff has a single source of truth.
func NextBackoffDelay(cur, base, max time.Duration) time.Duration {
	return syncengine.NextBackoff(cur, base, max)
}
