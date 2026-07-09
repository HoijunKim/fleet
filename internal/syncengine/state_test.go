package syncengine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/store"
)

func TestDocID(t *testing.T) {
	if got := DocID("m-9", store.Record{Manual: true}, ""); got != "m-9" {
		t.Errorf("manual doc_id = %q, want m-9", got)
	}
	got := DocID("C:/repos/app", store.Record{Manual: false}, "git@github.com:O/App.git")
	if got != "git:github.com/o/app" {
		t.Errorf("code doc_id = %q, want git:github.com/o/app", got)
	}
	noRemote := DocID("C:/repos/app", store.Record{Manual: false}, "")
	if noRemote[:6] != "local:" || len(noRemote) != 6+12 {
		t.Errorf("no-remote doc_id = %q, want local:<12hex>", noRemote)
	}
}

func TestNewer(t *testing.T) {
	a := "2026-07-09T00:00:01Z"
	b := "2026-07-09T00:00:00Z"
	if !newer(a, b) {
		t.Error("a should be newer than b")
	}
	if newer(b, a) {
		t.Error("b should not be newer than a")
	}
	if !newer(a, "") {
		t.Error("any time is newer than empty")
	}
	if newer("", "") {
		t.Error("empty is not newer than empty")
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sync.json")
	got, err := loadState(p) // missing file
	if err != nil || got.Cursor != 0 || got.Docs == nil {
		t.Fatalf("missing load: %+v %v", got, err)
	}
	got.Cursor = 7
	got.Docs["m-1"] = DocState{LocalID: "m-1", Hash: "h", UpdatedAt: "u", Deleted: false}
	if err := saveState(p, got); err != nil {
		t.Fatal(err)
	}
	back, err := loadState(p)
	if err != nil || back.Cursor != 7 || back.Docs["m-1"].Hash != "h" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}

func TestNextBackoff(t *testing.T) {
	base, max := 5*time.Second, time.Minute
	if got := NextBackoff(0, base, max); got != base {
		t.Errorf("from 0 -> %v, want %v", got, base)
	}
	if got := NextBackoff(10*time.Second, base, max); got != 20*time.Second {
		t.Errorf("doubling -> %v, want 20s", got)
	}
	if got := NextBackoff(40*time.Second, base, max); got != max {
		t.Errorf("cap -> %v, want %v", got, max)
	}
}
