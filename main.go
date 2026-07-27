package main

import (
	"embed"
	"net/http"
	"os"
	"time"

	"github.com/hoijun/fleet/internal/agent"
	"github.com/hoijun/fleet/internal/git"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Hook dispatch MUST come before any Wails init: when claude invokes the
	// PreToolUse hook it runs this same executable with the sentinel (or with
	// FLEET_AGENT_HOOK=1). Handle it and exit without starting the GUI.
	if isAgentHook() {
		client := &http.Client{Timeout: 15 * time.Minute}
		agent.RunHook(os.Stdin, os.Stdout, os.Getenv("FLEET_HOOK_URL"), client)
		return
	}

	// Interactive-rebase sequence editor: git runs this same executable as its
	// GIT_SEQUENCE_EDITOR with the todo path it generated; overwrite that file
	// with the todo fleet prepared (FLEET_REBASE_TODO) and exit before any GUI.
	if len(os.Args) > 2 && os.Args[1] == "--rebase-seq" {
		if err := git.ApplyRebaseSeq(os.Getenv("FLEET_REBASE_TODO"), os.Args[2]); err != nil {
			println("rebase-seq error:", err.Error())
			os.Exit(1)
		}
		return
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "Fleet",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "fleet-desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.focus() },
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
	if err != nil {
		println("error:", err.Error())
	}
}

// isAgentHook reports whether this process was launched as the PreToolUse hook
// rather than as the GUI: either the first argument is the sentinel flag or
// FLEET_AGENT_HOOK is set. Normal launch (no flag, no env) is unaffected.
func isAgentHook() bool {
	if len(os.Args) > 1 && os.Args[1] == agent.HookFlag {
		return true
	}
	return os.Getenv("FLEET_AGENT_HOOK") == "1"
}
