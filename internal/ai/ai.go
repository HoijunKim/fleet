// Package ai turns a prompt into a completion from a configurable provider:
// the local Claude CLI (default, no key), OpenAI, or Gemini. The single Runner
// seam keeps callers and tests provider-agnostic.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/winhide"
)

// Runner turns a prompt into a completion. Tests substitute a fake.
type Runner interface {
	Ask(prompt string) (string, error)
}

// timeout bounds a single completion so a hung provider never blocks the UI.
const timeout = 120 * time.Second

// New builds the Runner for the configured provider. Unknown providers (and
// "claude") fall back to the local Claude CLI.
func New(provider, model, openAIKey, geminiKey string) Runner {
	switch provider {
	case "openai":
		return OpenAIRunner{Key: openAIKey, Model: orDefault(model, "gpt-4o"), BaseURL: "https://api.openai.com/v1", Client: httpClient()}
	case "gemini":
		return GeminiRunner{Key: geminiKey, Model: orDefault(model, "gemini-2.0-flash"), BaseURL: "https://generativelanguage.googleapis.com/v1beta", Client: httpClient()}
	default:
		return ClaudeRunner{}
	}
}

// Available reports whether the configured provider can actually run: the
// Claude CLI must be on PATH; an API provider needs its key.
func Available(provider, openAIKey, geminiKey string) bool {
	switch provider {
	case "openai":
		return strings.TrimSpace(openAIKey) != ""
	case "gemini":
		return strings.TrimSpace(geminiKey) != ""
	default:
		_, err := exec.LookPath("claude")
		return err == nil
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func httpClient() *http.Client { return &http.Client{Timeout: timeout} }

// ---- Claude CLI ----------------------------------------------------------

// ClaudeRunner runs the real `claude --print`, hiding the console window on
// Windows. The prompt goes on stdin (not argv) so long prompts never hit the
// command-line length limit.
type ClaudeRunner struct{}

func (ClaudeRunner) Ask(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--print")
	winhide.Apply(cmd)
	cmd.Stdin = strings.NewReader(prompt)
	// Bound Wait after a kill: claude (a Node CLI) can spawn a grandchild that
	// keeps the stdout pipe open, which would otherwise hang the copy goroutine
	// past the deadline. WaitDelay force-closes the pipes so Run() returns.
	cmd.WaitDelay = 10 * time.Second
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude timed out after %s", timeout)
		}
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// ---- OpenAI --------------------------------------------------------------

// OpenAIRunner calls the OpenAI chat-completions API. BaseURL/Client are fields
// so tests can point at an httptest server.
type OpenAIRunner struct {
	Key     string
	Model   string
	BaseURL string
	Client  *http.Client
}

func (r OpenAIRunner) Ask(prompt string) (string, error) {
	if strings.TrimSpace(r.Key) == "" {
		return "", fmt.Errorf("OpenAI API key not set")
	}
	body, _ := json.Marshal(map[string]any{
		"model":    r.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequest(http.MethodPost, r.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.Key)
	data, err := doJSON(r.Client, req)
	if err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("openai: bad response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// ---- Gemini --------------------------------------------------------------

// GeminiRunner calls the Google Gemini generateContent API.
type GeminiRunner struct {
	Key     string
	Model   string
	BaseURL string
	Client  *http.Client
}

func (r GeminiRunner) Ask(prompt string) (string, error) {
	if strings.TrimSpace(r.Key) == "" {
		return "", fmt.Errorf("Gemini API key not set")
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
	})
	url := r.BaseURL + "/models/" + r.Model + ":generateContent"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", r.Key)
	data, err := doJSON(r.Client, req)
	if err != nil {
		return "", fmt.Errorf("gemini: %w", err)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("gemini: bad response: %w", err)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}

// doJSON sends req and returns the body, turning a non-2xx into an error that
// includes the (truncated) response so the UI can show what went wrong.
func doJSON(client *http.Client, req *http.Request) ([]byte, error) {
	if client == nil {
		client = httpClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, msg)
	}
	return data, nil
}
