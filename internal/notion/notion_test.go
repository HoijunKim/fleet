package notion

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a realistic Notion database-query response: title, a Due date property, a
// Status property, a checkbox, plus a row with a checked checkbox and no status.
const sampleQuery = `{
  "results": [
    {
      "url": "https://notion.so/task-a",
      "properties": {
        "Name":   {"type":"title","title":[{"plain_text":"Ship "},{"plain_text":"the labeler"}]},
        "Due":    {"type":"date","date":{"start":"2026-07-10T09:00:00.000+09:00"}},
        "Status": {"type":"status","status":{"name":"In progress"}}
      }
    },
    {
      "url": "https://notion.so/task-b",
      "properties": {
        "Name":  {"type":"title","title":[{"plain_text":"Pay invoice"}]},
        "Done":  {"type":"checkbox","checkbox":true},
        "When":  {"type":"date","date":{"start":"2026-07-05"}}
      }
    },
    {
      "url": "https://notion.so/task-c",
      "properties": {
        "Name": {"type":"title","title":[]}
      }
    }
  ]
}`

func TestParseResults(t *testing.T) {
	tasks, err := parseResults([]byte(sampleQuery))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "Ship the labeler" {
		t.Errorf("title concat wrong: %q", tasks[0].Title)
	}
	if tasks[0].Due != "2026-07-10" {
		t.Errorf("date not trimmed to day: %q", tasks[0].Due)
	}
	if tasks[0].Status != "In progress" {
		t.Errorf("status: %q", tasks[0].Status)
	}
	if !tasks[1].Done {
		t.Errorf("checked checkbox -> done: %+v", tasks[1])
	}
	if tasks[1].Due != "2026-07-05" {
		t.Errorf("date-only start: %q", tasks[1].Due)
	}
	if tasks[2].Title != "(untitled)" {
		t.Errorf("empty title -> placeholder: %q", tasks[2].Title)
	}
}

func TestStatusDoneMarksDone(t *testing.T) {
	j := `{"results":[
	  {"url":"u1","properties":{"Name":{"type":"title","title":[{"plain_text":"A"}]},"Stage":{"type":"status","status":{"name":"Done"}}}},
	  {"url":"u2","properties":{"Name":{"type":"title","title":[{"plain_text":"B"}]},"Stage":{"type":"select","select":{"name":"In progress"}}}}
	]}`
	tasks, err := parseResults([]byte(j))
	if err != nil {
		t.Fatal(err)
	}
	if !tasks[0].Done {
		t.Errorf("status Done must mark done: %+v", tasks[0])
	}
	if tasks[1].Done {
		t.Errorf("in-progress must stay open: %+v", tasks[1])
	}
}

func TestDuePrefersDueNamedProperty(t *testing.T) {
	// two date properties; the one named "Due" must win regardless of map order.
	j := `{"results":[{"url":"u","properties":{
	  "Name":{"type":"title","title":[{"plain_text":"X"}]},
	  "Start":{"type":"date","date":{"start":"2026-01-01"}},
	  "Due":{"type":"date","date":{"start":"2026-09-09"}}
	}}]}`
	tasks, _ := parseResults([]byte(j))
	if tasks[0].Due != "2026-09-09" {
		t.Errorf("expected the Due-named date, got %q", tasks[0].Due)
	}
}

func TestTasksSendsAuthAndVersion(t *testing.T) {
	var auth, ver, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth = req.Header.Get("Authorization")
		ver = req.Header.Get("Notion-Version")
		path = req.URL.Path
		io.WriteString(w, sampleQuery)
	}))
	defer srv.Close()

	c := Client{Token: "secret_x", BaseURL: srv.URL, HTTP: srv.Client()}
	tasks, err := c.Tasks("db123")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	if auth != "Bearer secret_x" {
		t.Errorf("auth = %q", auth)
	}
	if ver == "" {
		t.Error("Notion-Version header missing")
	}
	if !strings.Contains(path, "/databases/db123/query") {
		t.Errorf("path = %q", path)
	}
}

func TestTasksEmptyOnMissingConfig(t *testing.T) {
	if got, err := (Client{}).Tasks(""); err != nil || got != nil {
		t.Errorf("missing token/db must be a clean no-op, got %v %v", got, err)
	}
	if got, err := (Client{Token: "x"}).Tasks(""); err != nil || got != nil {
		t.Errorf("missing db must be a clean no-op, got %v %v", got, err)
	}
}

func TestTasksHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"unauthorized"}`)
	}))
	defer srv.Close()
	c := Client{Token: "bad", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Tasks("db"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestParseDatabases(t *testing.T) {
	j := `{"results":[
	  {"id":"db-1","title":[{"plain_text":"Tasks "},{"plain_text":"2026"}]},
	  {"id":"db-2","title":[]}
	]}`
	dbs, err := parseDatabases([]byte(j))
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 || dbs[0].ID != "db-1" || dbs[0].Title != "Tasks 2026" {
		t.Fatalf("dbs = %+v", dbs)
	}
	if dbs[1].Title != "(untitled database)" {
		t.Errorf("empty title placeholder: %q", dbs[1].Title)
	}
}

func TestDatabasesEmptyOnNoToken(t *testing.T) {
	if got, err := (Client{}).Databases(); err != nil || got != nil {
		t.Errorf("no token must be a clean no-op, got %v %v", got, err)
	}
}

func TestNonDoneCheckboxIgnored(t *testing.T) {
	// an "Urgent" checkbox must not become the writable done toggle, nor mark
	// the task done, even when checked.
	j := `{"results":[{"url":"u","properties":{
	  "Name":{"type":"title","title":[{"plain_text":"A"}]},
	  "Urgent":{"type":"checkbox","checkbox":true},
	  "Done":{"type":"checkbox","checkbox":false}
	}}]}`
	tasks, _ := parseResults([]byte(j))
	if tasks[0].CheckboxProp != "Done" {
		t.Errorf("writable checkbox must be the done-named one, got %q", tasks[0].CheckboxProp)
	}
	if tasks[0].Done {
		t.Errorf("a checked Urgent (not Done) must not mark the task done: %+v", tasks[0])
	}
}

func TestComplete(t *testing.T) {
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		method = req.Method
		path = req.URL.Path
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c := Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Complete("page-1", "Done"); err != nil {
		t.Fatal(err)
	}
	if method != "PATCH" {
		t.Errorf("method = %q, want PATCH", method)
	}
	if !strings.Contains(path, "/pages/page-1") {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"Done"`) || !strings.Contains(body, `"checkbox":true`) {
		t.Errorf("body = %q", body)
	}
}

func TestCompleteMissingArgs(t *testing.T) {
	if err := (Client{Token: "t"}).Complete("", "Done"); err == nil {
		t.Error("missing page must error")
	}
	if err := (Client{Token: "t"}).Complete("p", ""); err == nil {
		t.Error("missing prop must error")
	}
}

func TestAvailable(t *testing.T) {
	if Available("", "db") || Available("tok", "") {
		t.Error("both token and db required")
	}
	if !Available("tok", "db") {
		t.Error("token+db must be available")
	}
}

func TestTasksPaginates(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			_, _ = io.WriteString(w, `{"results":[{"url":"u1","properties":{"Name":{"type":"title","title":[{"plain_text":"a"}]}}}],"has_more":true,"next_cursor":"cur2"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "cur2") {
			t.Errorf("page 2 must forward the cursor, body=%s", body)
		}
		_, _ = io.WriteString(w, `{"results":[{"url":"u2","properties":{"Name":{"type":"title","title":[{"plain_text":"b"}]}}}],"has_more":false,"next_cursor":null}`)
	}))
	defer srv.Close()

	c := Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	tasks, err := c.Tasks("db")
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks across 2 pages, got %d", len(tasks))
	}
	if page != 2 {
		t.Fatalf("expected 2 page requests, got %d", page)
	}
}
