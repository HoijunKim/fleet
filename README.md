# fleet

A single-binary terminal dashboard for every git repo under your project roots.
See at a glance which are dirty, behind, or stale — then fetch, pull, open an
editor/terminal, or run a command, without leaving the keyboard.

## Install

    go install github.com/hoijun/fleet/cmd/fleet@latest

Or download a prebuilt binary from the Releases page.

## Configure

On first run fleet writes a default config and prints its path
(`%APPDATA%\fleet\config.toml` on Windows, `~/.config/fleet/config.toml` elsewhere):

    roots = ["C:/Users/you/Projects"]
    scan_depth = 2
    editor = "code"
    terminal = "wt"
    auto_fetch_minutes = 0
    show_non_git = true

## Keys

| Key | Action |
| --- | --- |
| j / k | move |
| enter | toggle detail |
| f / F | fetch selected / all |
| p | pull (ff-only) |
| e / t | open editor / terminal |
| x | run a command in the repo |
| r | refresh selected |
| / | filter by name |
| q | quit |
