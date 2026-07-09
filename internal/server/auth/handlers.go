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
