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
