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
