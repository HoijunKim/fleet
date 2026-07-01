// Command fleet is a multi-repo command-center TUI.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/scan"
	"github.com/hoijun/fleet/internal/tui"
)

func main() {
	cfg, path, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error (%s): %v\n", path, err)
		os.Exit(1)
	}
	if len(cfg.Roots) == 0 {
		fmt.Fprintf(os.Stderr, "no roots configured. Edit %s and add at least one root.\n", path)
		os.Exit(1)
	}

	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, cfg.ShowNonGit)
	model := tui.New(cfg, git.ExecRunner{}, repos)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
