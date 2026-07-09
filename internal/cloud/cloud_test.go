package cloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushThenPull(t *testing.T) {
	var stored []Doc
	var cur int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Docs []Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			var results []PushResult
			for _, d := range body.Docs {
				cur++
				d.Version = cur
				stored = append(stored, d)
				results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: cur})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "cursor": cur})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": stored, "cursor": cur})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	res, cursor, err := c.Push([]Doc{{Kind: "project", DocID: "m-1", Payload: json.RawMessage(`{"name":"a"}`), UpdatedAt: "2026-07-09T00:00:00Z"}}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("push result: %+v cursor=%d", res, cursor)
	}
	docs, cur2, err := c.Pull(0, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].DocID != "m-1" || cur2 != 1 {
		t.Fatalf("pull docs=%+v cursor=%d", docs, cur2)
	}
}

func TestPullUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()
	c := New(ts.URL)
	if _, _, err := c.Pull(0, "bad"); err != ErrUnauthorized {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestExchangeParsesUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "\"link_code\":\"lc\"") || !strings.Contains(string(b), "\"code_verifier\":\"ver\"") {
			t.Errorf("bad exchange body: %s", b)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "acc",
			"refresh_token": "ref",
			"user":          map[string]any{"id": "u1", "login": "octo", "avatar_url": "http://a/x.png"},
		})
	}))
	defer ts.Close()
	c := New(ts.URL)
	tok, user, err := c.Exchange("lc", "ver")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "acc" || tok.Refresh != "ref" {
		t.Errorf("tokens: %+v", tok)
	}
	if user.ID != "u1" || user.Login != "octo" || user.AvatarURL != "http://a/x.png" {
		t.Errorf("user: %+v", user)
	}
}
