package agent

import (
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/store"
)

func TestBuildSystemPrompt(t *testing.T) {
	r := store.Record{
		Status:   "active",
		Deadline: "2026-08-01",
		Notes:    "ship the labeling tool",
		Tasks: []store.Task{
			{Title: "wire EMG parser", Status: "todo", Due: "2026-07-20"},
			{Title: "old task", Status: "done"},
		},
	}
	out := BuildSystemPrompt("fleet", r)
	for _, want := range []string{
		"fleet's code-aware assistant",
		"\"fleet\"",
		"approved by the user",
		"Status: active",
		"Deadline: 2026-08-01",
		"wire EMG parser",
		"due 2026-07-20",
		"ship the labeling tool",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "old task") {
		t.Error("done tasks must be omitted")
	}
}

func TestBuildSystemPromptEmpty(t *testing.T) {
	out := BuildSystemPrompt("proj", store.Record{})
	if !strings.Contains(out, "\"proj\"") {
		t.Errorf("empty record still needs role framing: %s", out)
	}
}

func TestBuildFleetSystemPrompt(t *testing.T) {
	got := BuildFleetSystemPrompt([]FleetProject{
		{Name: "fleet", Status: "active", Deadline: "2026-08-01", OpenTasks: 3},
		{Name: "arsi", Status: "paused"},
	})
	if !strings.Contains(got, "fleet") || !strings.Contains(got, "arsi") {
		t.Fatalf("projects not listed: %s", got)
	}
	// fleet-wide framing + the approval note must be present
	if !strings.Contains(got, "approve") && !strings.Contains(got, "approved") {
		t.Fatalf("missing approval framing: %s", got)
	}
	// empty list must not panic and still frame the role
	if BuildFleetSystemPrompt(nil) == "" {
		t.Fatal("empty fleet prompt should still frame the role")
	}
}
