// Package notion reads tasks from a Notion database via an internal-integration
// token. Read-only: fleet pulls titles, due dates and status into the Today
// view. The HTTP call is thin; the property mapping (the fiddly part) is a pure
// function so it is unit-tested.
package notion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// version is the Notion API version fleet targets.
const version = "2022-06-28"

// Task is one row pulled from a Notion database.
type Task struct {
	Title  string `json:"title"`
	Due    string `json:"due"`    // YYYY-MM-DD or ""
	Status string `json:"status"` // status/select name, or "done" for a checked checkbox
	Done   bool   `json:"done"`
	URL    string `json:"url"`
}

// Client queries a Notion database. BaseURL/HTTP are fields so tests can inject.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

// New builds a Client for the real Notion API.
func New(token string) Client {
	return Client{Token: token, BaseURL: "https://api.notion.com/v1", HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Tasks queries the database and returns its rows, mapped to Tasks. A missing
// token or database id returns (nil, nil) so the caller can degrade cleanly.
func (c Client) Tasks(databaseID string) ([]Task, error) {
	if strings.TrimSpace(c.Token) == "" || strings.TrimSpace(databaseID) == "" {
		return nil, nil
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.notion.com/v1"
	}
	req, err := http.NewRequest(http.MethodPost, base+"/databases/"+databaseID+"/query",
		bytes.NewReader([]byte(`{"page_size":50}`)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Notion-Version", version)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("notion http %d: %s", resp.StatusCode, msg)
	}
	return parseResults(data)
}

// parseResults maps a Notion database-query response into Tasks. Property names
// vary per database, so it keys off each property's "type": the title property
// becomes the task title, the first date property its due, a status/select its
// status, and a checkbox its done flag.
func parseResults(data []byte) ([]Task, error) {
	var resp struct {
		Results []struct {
			URL        string                     `json:"url"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("notion: bad response: %w", err)
	}
	out := make([]Task, 0, len(resp.Results))
	for _, r := range resp.Results {
		t := Task{URL: r.URL}
		for _, raw := range r.Properties {
			var head struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &head); err != nil {
				continue
			}
			switch head.Type {
			case "title":
				t.Title = plainText(raw, "title")
			case "date":
				if t.Due == "" {
					var p struct {
						Date *struct {
							Start string `json:"start"`
						} `json:"date"`
					}
					if json.Unmarshal(raw, &p) == nil && p.Date != nil {
						t.Due = dateOnly(p.Date.Start)
					}
				}
			case "status":
				var p struct {
					Status *struct {
						Name string `json:"name"`
					} `json:"status"`
				}
				if json.Unmarshal(raw, &p) == nil && p.Status != nil {
					t.Status = p.Status.Name
				}
			case "select":
				if t.Status == "" {
					var p struct {
						Select *struct {
							Name string `json:"name"`
						} `json:"select"`
					}
					if json.Unmarshal(raw, &p) == nil && p.Select != nil {
						t.Status = p.Select.Name
					}
				}
			case "checkbox":
				var p struct {
					Checkbox bool `json:"checkbox"`
				}
				if json.Unmarshal(raw, &p) == nil {
					t.Done = p.Checkbox
					if p.Checkbox && t.Status == "" {
						t.Status = "done"
					}
				}
			}
		}
		if t.Title == "" {
			t.Title = "(untitled)"
		}
		out = append(out, t)
	}
	return out, nil
}

// plainText concatenates the plain_text of a rich-text array property (title).
func plainText(raw json.RawMessage, key string) string {
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	arr, ok := p[key]
	if !ok {
		return ""
	}
	var items []struct {
		PlainText string `json:"plain_text"`
	}
	if json.Unmarshal(arr, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.PlainText)
	}
	return strings.TrimSpace(b.String())
}

// dateOnly trims a Notion date-time ("2026-07-10T09:00:00...") to YYYY-MM-DD.
func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// Available reports whether a token and database id are both set.
func Available(token, databaseID string) bool {
	return strings.TrimSpace(token) != "" && strings.TrimSpace(databaseID) != ""
}
