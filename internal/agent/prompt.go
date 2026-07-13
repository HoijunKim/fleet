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
	b.WriteString("command you run is reviewed and approved by the user before it takes effect. ")
	b.WriteString("Write in plain text with ASCII punctuation only: use a hyphen (-), not em or en dashes; ")
	b.WriteString("straight quotes; and no other special Unicode symbols.\n\n")
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

// FleetProject is one project's PM summary for the fleet-wide system prompt.
type FleetProject struct {
	Name      string
	Status    string
	Deadline  string
	OpenTasks int
}

// BuildFleetSystemPrompt frames the agent as working across ALL of the user's
// projects under the run directory, and lists each project's PM state so the
// agent knows the fleet. Read tools span every repo under the directory; any
// edit, file write, or shell command is reviewed and approved by the user
// before it takes effect.
func BuildFleetSystemPrompt(projects []FleetProject) string {
	var b strings.Builder
	b.WriteString("You are fleet's code-aware assistant working across ALL of the user's projects ")
	b.WriteString("under this directory. You can read and grep any repo here to answer questions that ")
	b.WriteString("span projects. Propose concrete, file-grounded changes. Any edit, file write, or shell ")
	b.WriteString("command you run is reviewed and approved by the user before it takes effect. ")
	b.WriteString("Write in plain text with ASCII punctuation only: use a hyphen (-), not em or en dashes; ")
	b.WriteString("straight quotes; and no other special Unicode symbols.\n\n")
	b.WriteString("=== Projects (from fleet) ===\n")
	for _, p := range projects {
		fmt.Fprintf(&b, "- %s", p.Name)
		var bits []string
		if s := strings.TrimSpace(p.Status); s != "" {
			bits = append(bits, s)
		}
		if d := strings.TrimSpace(p.Deadline); d != "" {
			bits = append(bits, "deadline "+d)
		}
		if p.OpenTasks > 0 {
			bits = append(bits, fmt.Sprintf("%d open tasks", p.OpenTasks))
		}
		if len(bits) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(bits, ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
