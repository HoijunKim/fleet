package intel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBriefRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrief(Brief{Text: "hello", At: "2026-07-24T00:00:00Z", Lang: "ko"}); err != nil {
		t.Fatal(err)
	}
	// Reopen to prove it persisted, not just cached.
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if b := s2.Brief(); b.Text != "hello" || b.Lang != "ko" {
		t.Errorf("Brief = %+v, want hello/ko", b)
	}
}

func TestChatRoundTripAndCap(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	turns := make([]Turn, 0, 25)
	for i := 0; i < 25; i++ {
		turns = append(turns, Turn{Role: "user", Text: string(rune('a' + i%26))})
	}
	if err := s.SetChat("git:x", turns); err != nil {
		t.Fatal(err)
	}
	got := s.Chat("git:x")
	if len(got) != chatCap {
		t.Fatalf("Chat len = %d, want %d (capped)", len(got), chatCap)
	}
	// The cap keeps the LAST 20, so the first kept turn is the 6th written.
	if got[0].Text != turns[len(turns)-chatCap].Text {
		t.Errorf("cap kept the wrong end: got[0]=%q", got[0].Text)
	}
}

func TestSetChatEmptyDeletesKey(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetChat("git:x", []Turn{{Role: "user", Text: "hi"}})
	if err := s.SetChat("git:x", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Snapshot().Chats["git:x"]; ok {
		t.Error("an emptied chat should delete its key, not linger as []")
	}
}

func TestClearChat(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetChat("__fleet__", []Turn{{Role: "user", Text: "hi"}})
	if err := s.ClearChat("__fleet__"); err != nil {
		t.Fatal(err)
	}
	if len(s.Chat("__fleet__")) != 0 {
		t.Error("ClearChat left turns behind")
	}
}

func TestCorruptFileOpensReadOnlyAndQuarantines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "intel.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err == nil {
		t.Fatal("expected an error opening a corrupt file")
	}
	if s.Degraded() == nil {
		t.Error("Degraded should report the load failure")
	}
	if s.Quarantined() == "" {
		t.Error("the bad bytes should have been quarantined")
	}
	// Writes are refused while degraded: the empty fallback must never overwrite.
	if err := s.SetBrief(Brief{Text: "x"}); err == nil {
		t.Error("SetBrief must be refused while the store is degraded")
	}
	// The original bytes were moved aside, not destroyed.
	if _, err := os.Stat(s.Quarantined()); err != nil {
		t.Errorf("quarantined file missing: %v", err)
	}
}

func TestMissingFileIsEmptyNoError(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file must not error: %v", err)
	}
	if s.Brief().Text != "" || len(s.Snapshot().Chats) != 0 {
		t.Error("a missing file should yield an empty store")
	}
}

func TestSetChatStampsUpdatedAt(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetClock(func() time.Time { return time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC) })
	if err := s.SetChat("git:x", []Turn{{Role: "user", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.ChatUpdatedAt("git:x"); got != "2026-07-24T01:02:03Z" {
		t.Errorf("ChatUpdatedAt = %q, want the stamped time", got)
	}
}

func TestSetBriefStampsUpdatedAt(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetClock(func() time.Time { return time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC) })
	if err := s.SetBrief(Brief{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := s.BriefUpdatedAt(); got != "2026-07-24T01:02:03Z" {
		t.Errorf("BriefUpdatedAt = %q, want the stamped time", got)
	}
}

func TestOpenAcceptsOldBareArrayChatShape(t *testing.T) {
	p := filepath.Join(t.TempDir(), "intel.json")
	// The tier-4d on-disk shape: chats are bare [Turn] arrays.
	old := `{"brief":{"text":"b"},"chats":{"git:x":[{"role":"user","text":"q"}]}}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open must accept the old shape: %v", err)
	}
	got := s.Chat("git:x")
	if len(got) != 1 || got[0].Text != "q" {
		t.Errorf("old-shape chat not loaded: %+v", got)
	}
	if s.ChatUpdatedAt("git:x") != "" {
		t.Error("an old chat should load with an empty updatedAt (older-than-anything)")
	}
}
