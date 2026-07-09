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
