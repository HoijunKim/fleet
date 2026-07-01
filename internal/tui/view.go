package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/hoijun/fleet/internal/repo"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dirtyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cleanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	behindStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("236"))
	barStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// View renders the whole screen.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.table())
	if m.showDetail {
		b.WriteString("\n")
		b.WriteString(m.detail())
	}
	if m.mode == modeOutput {
		b.WriteString("\n")
		b.WriteString(m.outputPane())
	}
	b.WriteString("\n")
	b.WriteString(m.bar())
	return b.String()
}

func (m Model) header() string {
	dirty, behind := 0, 0
	for _, r := range m.repos {
		if r.Dirty {
			dirty++
		}
		if r.Behind > 0 {
			behind++
		}
	}
	loading := ""
	if m.loading > 0 {
		loading = " " + m.spinner.View() + fmt.Sprintf(" loading %d", m.loading)
	}
	return headerStyle.Render(fmt.Sprintf("fleet — roots %d · repos %d · dirty %d · behind %d",
		len(m.cfg.Roots), len(m.repos), dirty, behind)) + loading
}

func (m Model) table() string {
	v := m.visible()
	var b strings.Builder
	b.WriteString(dimStyle.Render(fmt.Sprintf("%-20s %-10s %-6s %-6s %-10s %-8s %s",
		"NAME", "BRANCH", "ST", "UP/DN", "LAST", "LANG", "TODO")))
	b.WriteString("\n")
	for i, r := range v {
		row := fmt.Sprintf("%-20s %-10s %-6s %-6s %-10s %-8s %d",
			trunc(r.Name, 20), trunc(r.Branch, 10), r.Marker(), aheadBehind(r),
			relTime(r.Last.When), trunc(r.Language, 8), r.TodoCount)
		if i == m.cursor {
			b.WriteString(selStyle.Render(row))
		} else if !r.IsGit {
			b.WriteString(dimStyle.Render(row))
		} else if r.Dirty {
			b.WriteString(dirtyStyle.Render(row))
		} else {
			b.WriteString(cleanStyle.Render(row))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) detail() string {
	r, ok := m.selected()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("detail: ") + r.Name + "\n")
	b.WriteString(fmt.Sprintf("path   %s\n", r.Path))
	if r.Last.Hash != "" {
		b.WriteString(fmt.Sprintf("head   %s %q — %s, %s\n",
			short(r.Last.Hash), r.Last.Message, r.Last.Author, relTime(r.Last.When)))
	}
	if r.RemoteURL != "" {
		b.WriteString(fmt.Sprintf("remote %s\n", r.RemoteURL))
	}
	if len(r.DirtyFiles) > 0 {
		b.WriteString("dirty  " + strings.Join(r.DirtyFiles, "  ") + "\n")
	}
	return b.String()
}

func (m Model) outputPane() string {
	return dimStyle.Render("── output (esc to close) ──") + "\n" + m.output
}

func (m Model) bar() string {
	switch m.mode {
	case modeFilter:
		return barStyle.Render("filter: ") + m.filter + dimStyle.Render("  (enter=apply, esc=clear)")
	case modeRunPrompt:
		name := ""
		if r, ok := m.selected(); ok {
			name = r.Name
		}
		return barStyle.Render(fmt.Sprintf("run in %s: ", name)) + m.runInput + dimStyle.Render("  (enter=run, esc=cancel)")
	default:
		help := "[f]etch [F]all [p]ull [e]dit [t]erm [x]cmd [/]filter [enter]detail [q]uit"
		if m.status != "" {
			return barStyle.Render(help) + "\n" + dimStyle.Render(m.status)
		}
		return barStyle.Render(help)
	}
}

func aheadBehind(r repo.Repo) string {
	if !r.HasUpstream {
		return ""
	}
	s := ""
	if r.Ahead > 0 {
		s += fmt.Sprintf("up%d", r.Ahead)
	}
	if r.Behind > 0 {
		if s != "" {
			s += " "
		}
		s += behindStyle.Render(fmt.Sprintf("dn%d", r.Behind))
	}
	return s
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	// Rendered relative to the commit's own clock is impossible without "now";
	// show a compact date instead, which is deterministic and test-friendly.
	return t.Format("06-01-02")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
