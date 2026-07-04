package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSelectsProvider(t *testing.T) {
	if _, ok := New("openai", "", "k", "").(OpenAIRunner); !ok {
		t.Error("openai -> OpenAIRunner")
	}
	if _, ok := New("gemini", "", "", "k").(GeminiRunner); !ok {
		t.Error("gemini -> GeminiRunner")
	}
	if _, ok := New("claude", "", "", "").(ClaudeRunner); !ok {
		t.Error("claude -> ClaudeRunner")
	}
	if _, ok := New("bogus", "", "", "").(ClaudeRunner); !ok {
		t.Error("unknown provider must fall back to ClaudeRunner")
	}
}

func TestNewAppliesModelDefaults(t *testing.T) {
	if r := New("openai", "", "k", "").(OpenAIRunner); r.Model != "gpt-4o" {
		t.Errorf("openai default model = %q", r.Model)
	}
	if r := New("openai", "gpt-4.1", "k", "").(OpenAIRunner); r.Model != "gpt-4.1" {
		t.Errorf("openai model override = %q", r.Model)
	}
	if r := New("gemini", "", "", "k").(GeminiRunner); r.Model == "" {
		t.Error("gemini default model empty")
	}
}

func TestAvailable(t *testing.T) {
	if Available("openai", "", "") {
		t.Error("openai without key must be unavailable")
	}
	if !Available("openai", "sk-x", "") {
		t.Error("openai with key must be available")
	}
	if Available("gemini", "", "") {
		t.Error("gemini without key must be unavailable")
	}
	if !Available("gemini", "", "g-x") {
		t.Error("gemini with key must be available")
	}
}

func TestOpenAIRunnerAsk(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"  work on EMG  "}}]}`)
	}))
	defer srv.Close()

	r := OpenAIRunner{Key: "sk-test", Model: "gpt-4o", BaseURL: srv.URL, Client: srv.Client()}
	out, err := r.Ask("what next?")
	if err != nil {
		t.Fatal(err)
	}
	if out != "work on EMG" {
		t.Errorf("content = %q (want trimmed)", out)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if !strings.Contains(gotBody, "what next?") || !strings.Contains(gotBody, "gpt-4o") {
		t.Errorf("request body missing prompt/model: %s", gotBody)
	}
}

func TestOpenAIRunnerHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()
	r := OpenAIRunner{Key: "sk-bad", Model: "gpt-4o", BaseURL: srv.URL, Client: srv.Client()}
	if _, err := r.Ask("x"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected an http 401 error, got %v", err)
	}
}

func TestOpenAIRunnerNoKey(t *testing.T) {
	if _, err := (OpenAIRunner{}).Ask("x"); err == nil {
		t.Error("missing key must error before any request")
	}
}

func TestGeminiRunnerAsk(t *testing.T) {
	var gotURL, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotURL = req.URL.String()
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"do the labeling"}]}}]}`)
	}))
	defer srv.Close()

	r := GeminiRunner{Key: "g-test", Model: "gemini-2.0-flash", BaseURL: srv.URL, Client: srv.Client()}
	out, err := r.Ask("plan my day")
	if err != nil {
		t.Fatal(err)
	}
	if out != "do the labeling" {
		t.Errorf("text = %q", out)
	}
	if !strings.Contains(gotURL, "gemini-2.0-flash:generateContent") || !strings.Contains(gotURL, "key=g-test") {
		t.Errorf("url = %q", gotURL)
	}
	if !strings.Contains(gotBody, "plan my day") {
		t.Errorf("body missing prompt: %s", gotBody)
	}
}

func TestGeminiRunnerEmptyCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"candidates":[]}`)
	}))
	defer srv.Close()
	r := GeminiRunner{Key: "g", Model: "m", BaseURL: srv.URL, Client: srv.Client()}
	if _, err := r.Ask("x"); err == nil {
		t.Error("empty candidates must error")
	}
}
