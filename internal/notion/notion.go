// Package notion reads and writes tasks in a Notion database via an
// internal-integration token. fleet pulls titles, due dates and status into its
// views, and can create a task or mark one done. The HTTP calls are thin; the
// fiddly parts - property mapping on read and schema discovery on write - are
// pure functions so they are unit-tested. Writes only ever touch the property
// the schema identifies (title, a date, a done-named checkbox, or a status's
// done option), so fleet never clobbers an unrelated column.
package notion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// version is the Notion API version fleet targets.
const version = "2022-06-28"

// Task is one row pulled from a Notion database.
type Task struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Due          string `json:"due"`    // YYYY-MM-DD or ""
	Status       string `json:"status"` // status/select name, or "done" for a checked checkbox
	Done         bool   `json:"done"`
	URL          string `json:"url"`
	CheckboxProp string `json:"checkboxProp"` // name of the checkbox property, if any (enables completing)
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
	// Follow Notion's cursor pagination so a board with more than one page of
	// rows is not silently truncated. Bounded by maxPages as a runaway guard.
	const maxPages = 10 // up to ~1000 rows at page_size 100
	var all []Task
	cursor := ""
	for page := 0; page < maxPages; page++ {
		body := `{"page_size":100}`
		if cursor != "" {
			b, _ := json.Marshal(map[string]any{"page_size": 100, "start_cursor": cursor})
			body = string(b)
		}
		req, err := http.NewRequest(http.MethodPost, base+"/databases/"+databaseID+"/query",
			bytes.NewReader([]byte(body)))
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
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
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
		tasks, err := parseResults(data)
		if err != nil {
			return nil, err
		}
		all = append(all, tasks...)

		var meta struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
		_ = json.Unmarshal(data, &meta)
		if !meta.HasMore || meta.NextCursor == "" {
			break
		}
		cursor = meta.NextCursor
	}
	return all, nil
}

// Complete checks a page's checkbox property to true (marks a task done). Only
// checkbox-based tasks are writable; status-based boards stay read-only.
func (c Client) Complete(pageID, checkboxProp string) error {
	if strings.TrimSpace(c.Token) == "" || strings.TrimSpace(pageID) == "" || strings.TrimSpace(checkboxProp) == "" {
		return fmt.Errorf("notion: missing page or property")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.notion.com/v1"
	}
	body, _ := json.Marshal(map[string]any{
		"properties": map[string]any{
			checkboxProp: map[string]any{"checkbox": true},
		},
	})
	req, err := http.NewRequest(http.MethodPatch, base+"/pages/"+pageID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Notion-Version", version)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("notion http %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// DB is a Notion database the integration can see (for the settings picker).
type DB struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Databases lists the databases shared with this integration, so the user can
// pick one instead of pasting a raw id. A missing token returns (nil, nil).
func (c Client) Databases() ([]DB, error) {
	if strings.TrimSpace(c.Token) == "" {
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
	req, err := http.NewRequest(http.MethodPost, base+"/search",
		bytes.NewReader([]byte(`{"filter":{"value":"database","property":"object"},"page_size":50}`)))
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
	return parseDatabases(data)
}

// parseDatabases maps a Notion search response into a database list.
func parseDatabases(data []byte) ([]DB, error) {
	var resp struct {
		Results []struct {
			ID    string `json:"id"`
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("notion: bad response: %w", err)
	}
	out := make([]DB, 0, len(resp.Results))
	for _, r := range resp.Results {
		var b strings.Builder
		for _, tt := range r.Title {
			b.WriteString(tt.PlainText)
		}
		title := strings.TrimSpace(b.String())
		if title == "" {
			title = "(untitled database)"
		}
		out = append(out, DB{ID: r.ID, Title: title})
	}
	return out, nil
}

// parseResults maps a Notion database-query response into Tasks. Property names
// vary per database, so it keys off each property's "type": the title property
// becomes the task title, the first date property its due, a status/select its
// status, and a checkbox its done flag.
func parseResults(data []byte) ([]Task, error) {
	var resp struct {
		Results []struct {
			ID         string                     `json:"id"`
			URL        string                     `json:"url"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("notion: bad response: %w", err)
	}
	out := make([]Task, 0, len(resp.Results))
	for _, r := range resp.Results {
		t := Task{ID: r.ID, URL: r.URL}
		// Iterate property names in sorted order so a board with two same-typed
		// columns (e.g. two dates) always resolves the same way, not per random
		// map order. A property whose name looks like a due date is preferred.
		names := make([]string, 0, len(r.Properties))
		for name := range r.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		dueNamed := false
		for _, name := range names {
			raw := r.Properties[name]
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
				var p struct {
					Date *struct {
						Start string `json:"start"`
					} `json:"date"`
				}
				if json.Unmarshal(raw, &p) == nil && p.Date != nil && p.Date.Start != "" {
					if t.Due == "" || (isDueName(name) && !dueNamed) {
						t.Due = dateOnly(p.Date.Start)
						if isDueName(name) {
							dueNamed = true
						}
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
				// Only a checkbox whose NAME reads as completion counts as the
				// done toggle - otherwise an unrelated checkbox (e.g. "Urgent")
				// would be treated as done and, worse, written to on complete.
				if isDoneCheckbox(name) {
					var p struct {
						Checkbox bool `json:"checkbox"`
					}
					if json.Unmarshal(raw, &p) == nil {
						t.CheckboxProp = name
						if p.Checkbox {
							t.Done = true
						}
					}
				}
			}
		}
		// A status/select whose name reads as complete also counts as done, so
		// finished tasks don't leak into the open list or the AI briefing.
		if isDoneStatus(t.Status) {
			t.Done = true
		}
		if t.Title == "" {
			t.Title = "(untitled)"
		}
		out = append(out, t)
	}
	return out, nil
}

// isDueName reports whether a property name reads as a due/deadline date.
func isDueName(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "due") || strings.Contains(n, "deadline") || strings.Contains(n, "end")
}

// isDoneCheckbox reports whether a checkbox property's NAME reads as a
// completion toggle, so fleet only ever writes to a "done"-style checkbox.
func isDoneCheckbox(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "done") || strings.Contains(n, "complete") ||
		strings.Contains(n, "finish") || strings.Contains(n, "check") ||
		strings.Contains(n, "closed")
}

// isDoneStatus reports whether a status/select value means the task is finished.
func isDoneStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "complete", "completed", "archived", "closed":
		return true
	}
	return false
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
