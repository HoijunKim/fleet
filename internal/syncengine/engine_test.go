package syncengine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/store"
)

// fakeSrv is an in-process stand-in for the backend /sync endpoints with the
// same per-user LWW + monotonic version semantics.
type fakeSrv struct {
	mu     sync.Mutex
	docs   map[string]cloud.Doc
	cur    int64
	pushes int
}

func newFake() *fakeSrv { return &fakeSrv{docs: map[string]cloud.Doc{}} }

// keys is a deterministic-free helper to report the server's doc_id set on a
// test failure.
func keys(m map[string]cloud.Doc) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeSrv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		var out []cloud.Doc
		for _, d := range f.docs {
			if d.Version > since {
				out = append(out, d)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
		cursor := since
		for _, d := range out {
			if d.Version > cursor {
				cursor = d.Version
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": out, "cursor": cursor})
	case http.MethodPost:
		f.pushes++
		var body struct {
			Docs []cloud.Doc `json:"docs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var results []cloud.PushResult
		for _, d := range body.Docs {
			stored, ok := f.docs[d.DocID]
			accept := !ok || newer(d.UpdatedAt, stored.UpdatedAt)
			if accept {
				f.cur++
				d.Version = f.cur
				f.docs[d.DocID] = d
				results = append(results, cloud.PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: d.Version})
			} else {
				results = append(results, cloud.PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: false, Version: stored.Version})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "cursor": f.cur})
	}
}

func newEngine(t *testing.T, url string) (*Engine, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	statePath := filepath.Join(dir, "sync.json")
	e := New(st, cloud.New(url), statePath, func(string) string { return "" })
	return e, st, statePath
}

func TestSyncPushesDirtyOnce(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()
	e, st, statePath := newEngine(t, ts.URL)

	_ = st.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "a" })
	if err := e.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if f.pushes != 1 || len(f.docs) != 1 {
		t.Fatalf("first sync: pushes=%d docs=%d", f.pushes, len(f.docs))
	}
	// cursor persisted
	back, _ := loadState(statePath)
	if back.Cursor != 1 {
		t.Errorf("cursor not persisted: %d", back.Cursor)
	}
	// second sync with no local change must not push again
	if err := e.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if f.pushes != 1 {
		t.Errorf("clean re-sync pushed again: pushes=%d", f.pushes)
	}
}

func TestSyncPullAppliesLWW(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	// device A creates and syncs a manual project
	eA, stA, _ := newEngine(t, ts.URL)
	_ = stA.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "fromA" })
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}

	// device B pulls it into an empty store
	eB, stB, _ := newEngine(t, ts.URL)
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	rec, ok := stB.Get("m-1")
	if !ok || rec.Name != "fromA" {
		t.Fatalf("device B did not receive doc: %+v ok=%v", rec, ok)
	}
}

func TestSyncTombstoneDeletes(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	eA, stA, _ := newEngine(t, ts.URL)
	_ = stA.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "x" })
	_ = eA.SyncOnce("tok")

	eB, stB, _ := newEngine(t, ts.URL)
	_ = eB.SyncOnce("tok") // B now has m-1
	if _, ok := stB.Get("m-1"); !ok {
		t.Fatal("precondition: B should have m-1")
	}

	// A deletes locally and syncs -> pushes a tombstone
	_ = stA.Delete("m-1")
	// ensure the tombstone timestamp is strictly newer than the create
	time.Sleep(2 * time.Millisecond)
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}

	// B syncs and must delete locally
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stB.Get("m-1"); ok {
		t.Error("device B still has the deleted doc")
	}
	if !eB.TookRemoteEdit() {
		t.Error("expected remote-edit flag after a tombstone overwrote local")
	}
}

func TestSyncOfflineNoCorruption(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	url := ts.URL
	ts.Close() // server is now unreachable

	e, st, statePath := newEngine(t, url)
	_ = st.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "a" })
	if err := e.SyncOnce("tok"); err == nil {
		t.Fatal("expected an error while offline")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("sync.json must not be written on an offline failure (stat err=%v)", err)
	}
}

// TestSyncDetachedCodeDocNotRepushed guards the "detached record" rule: a pulled
// code-project doc with no local repo on this machine is retained under its own
// doc_id and must NOT, on a subsequent sync, be re-pushed under a fresh
// "local:" id nor tombstoned under its original "git:" id (spec: detached
// records are retained, never dropped or duplicated).
func TestSyncDetachedCodeDocNotRepushed(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	// device A has a code project WITH a git remote and syncs it.
	dirA := t.TempDir()
	stA, _ := store.Open(filepath.Join(dirA, "projects.json"))
	eA := New(stA, cloud.New(ts.URL), filepath.Join(dirA, "sync.json"),
		func(string) string { return "git@github.com:o/app.git" })
	_ = stA.Update("C:/repos/app", func(r *store.Record) { r.Manual = false; r.Name = "app" })
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.docs["git:github.com/o/app"]; !ok {
		t.Fatalf("server missing the git doc: %v", keys(f.docs))
	}

	// device B has NO such repo (remoteOf returns "") and pulls a detached doc.
	dirB := t.TempDir()
	stB, _ := store.Open(filepath.Join(dirB, "projects.json"))
	eB := New(stB, cloud.New(ts.URL), filepath.Join(dirB, "sync.json"),
		func(string) string { return "" })
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stB.Get("git:github.com/o/app"); !ok {
		t.Fatal("device B should hold the detached record under its doc_id")
	}
	pushesAfterFirst := f.pushes
	docsAfterFirst := len(f.docs)

	// a SECOND B sync must not grow the server's doc set nor re-push.
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != docsAfterFirst {
		t.Errorf("detached doc grew server doc set: %d -> %d (%v)", docsAfterFirst, len(f.docs), keys(f.docs))
	}
	if f.pushes != pushesAfterFirst {
		t.Errorf("second B sync re-pushed the detached doc: pushes %d -> %d", pushesAfterFirst, f.pushes)
	}
}
