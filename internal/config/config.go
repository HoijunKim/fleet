// Package config loads and persists fleet's user configuration.
package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Config is fleet's on-disk configuration.
type Config struct {
	Roots            []string `toml:"roots"`
	ScanDepth        int      `toml:"scan_depth"`
	Editor           string   `toml:"editor"`
	Terminal         string   `toml:"terminal"`
	AutoFetchMinutes int      `toml:"auto_fetch_minutes"`
	ShowNonGit       bool     `toml:"show_non_git"`

	// AI provider for the Project Intelligence briefing. "claude" (default)
	// shells the local Claude CLI and needs no key; "openai"/"gemini" call
	// their HTTP APIs with the key below.
	AIProvider string `toml:"ai_provider"`
	AIModel    string `toml:"ai_model"`
	OpenAIKey  string `toml:"openai_key"`
	GeminiKey  string `toml:"gemini_key"`

	// Notion integration (read-only): an internal-integration token and the
	// database id fleet pulls tasks/deadlines from.
	NotionToken   string `toml:"notion_token"`
	NotionTasksDB string `toml:"notion_tasks_db"`
}

// Default returns the configuration used when no file exists yet.
func Default() Config {
	editor := "code"
	terminal := "x-terminal-emulator"
	if runtime.GOOS == "windows" {
		terminal = "wt"
	} else if runtime.GOOS == "darwin" {
		terminal = "open"
	}
	home, _ := os.UserHomeDir()
	roots := []string{}
	if home != "" {
		roots = append(roots, filepath.ToSlash(filepath.Join(home, "Projects")))
	}
	return Config{
		Roots:            roots,
		ScanDepth:        2,
		Editor:           editor,
		Terminal:         terminal,
		AutoFetchMinutes: 0,
		ShowNonGit:       true,
		AIProvider:       "claude",
	}
}

// Path returns the resolved config file path for this OS.
func Path() (string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("APPDATA"); base != "" {
			return filepath.Join(base, "fleet", "config.toml"), nil
		}
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "fleet", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fleet", "config.toml"), nil
}

// Load resolves the config path, loading it (writing defaults if absent).
// It returns the config and the path it used.
func Load() (Config, string, error) {
	p, err := Path()
	if err != nil {
		return Default(), "", err
	}
	c, err := loadFrom(p)
	return c, p, err
}

// loadFrom loads the config at p. If p does not exist, it writes the default
// config to p and returns it. Exposed (unexported) for tests.
func loadFrom(p string) (Config, error) {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		d := Default()
		if err := d.Save(p); err != nil {
			return d, err
		}
		return d, nil
	}
	var c Config
	if _, err := toml.DecodeFile(p, &c); err != nil {
		return Default(), err
	}
	// Backfill any fields left empty by an old/partial file.
	d := Default()
	if c.ScanDepth == 0 {
		c.ScanDepth = d.ScanDepth
	}
	if c.Editor == "" {
		c.Editor = d.Editor
	}
	if c.Terminal == "" {
		c.Terminal = d.Terminal
	}
	if c.AIProvider == "" {
		c.AIProvider = d.AIProvider
	}
	return c, nil
}

// Save writes the config to p, creating parent directories as needed.
func (c Config) Save(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}
