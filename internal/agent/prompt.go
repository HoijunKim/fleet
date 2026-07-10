package agent

import (
	"fmt"
	"strings"

	"github.com/hoijun/fleet/internal/store"
)

// BuildSystemPrompt builds the text passed to `claude --append-system-prompt`:
// fleet's role framing plus this project's PM context (status, deadline, open
// tasks, notes) so the agent's answers are grounded in what the user tracks.
// name is the display name (a code project's store Record.Name is empty, so the
// caller passes the repo folder name).
func BuildSystemPrompt(name string, r store.Record) string {
	var b strings.Builder
	b.WriteString("You are fleet's code-aware assistant for the project \"")
	b.WriteString(name)
	b.WriteString("\". You are working inside this project's repository with read tools; ")
	b.WriteString("propose concrete, file-grounded changes. Any edit, file write, or shell ")
	b.WriteString("command you run is reviewed and approved by the user before it takes effect.\n\n")
	b.WriteString("=== Project management context (from fleet) ===\n")
	if s := strings.TrimSpace(r.Status); s != "" {
		fmt.Fprintf(&b, "Status: %s\n", s)
	}
	if d := strings.TrimSpace(r.Deadline); d != "" {
		fmt.Fprintf(&b, "Deadline: %s\n", d)
	}
	open := 0
	for _, t := range r.Tasks {
		if t.Status != "done" {
			open++
		}
	}
	if len(r.Tasks) > 0 {
		fmt.Fprintf(&b, "Tasks: %d open of %d total\n", open, len(r.Tasks))
		for _, t := range r.Tasks {
			if t.Status == "done" {
				continue
			}
			line := "- " + t.Title
			if strings.TrimSpace(t.Due) != "" {
				line += " (due " + t.Due + ")"
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if n := strings.TrimSpace(r.Notes); n != "" {
		b.WriteString("Notes: ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
