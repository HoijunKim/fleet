# fleet

A desktop dashboard for every git repo under your project roots. See at a glance
which are dirty, behind, or stale, then fetch, pull, open an editor/terminal, or run
a command - from a single window.

## Run (Windows)

Download `fleet.exe` from Releases and run it, or build from source:

    wails build
    ./build/bin/fleet.exe

First run writes a config at `%APPDATA%\fleet\config.toml` (roots default to
`~/Projects`). Edit `roots` and restart.

## Build from source

Requires Go 1.22+, Node 18+, and Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

    wails build            # desktop app -> build/bin/fleet.exe
    go build ./cmd/fleet   # optional terminal UI (TUI) bonus binary

## Config

    roots = ["C:/Users/you/Projects"]
    scan_depth = 2
    editor = "code"
    terminal = "wt"
    auto_fetch_minutes = 0
    show_non_git = true
