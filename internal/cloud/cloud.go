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

// ErrRefreshFailed is returned by Refresh (and propagated verbatim through
// Session.WithAccess) when the server rejects the refresh token itself (401):
// the token was revoked or expired, not merely offline or a transient server
// error. Callers should treat this as a signed-out condition - clear local
// session state and prompt re-authentication - rather than retrying forever
// or showing a generic error. A non-401 failure (network error, 5xx) is
// returned unwrapped so it is not mistaken for a dead token.
var ErrRefreshFailed = errors.New("cloud: refresh token rejected")

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
	if resp.StatusCode == http.StatusUnauthorized {
		return Tokens{}, ErrRefreshFailed
	}
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

// DeleteAccount irreversibly deletes the caller's account and all of its synced
// data. It authenticates with an access token (not the refresh token) since the
// server derives the user from the bearer.
func (c *Client) DeleteAccount(access string) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+"/account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete account: status %d", resp.StatusCode)
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
