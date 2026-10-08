# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

GoRex is a replica of Superlogical's Rex terminal (tabs, split panes, persistent sessions) in Go, using MyGo's native UI (no webview, no cgo; `purego` loads libghostty-vt). macOS is the primary target. See README.md for user-facing behavior and shortcuts.

## Commands

Read [docs/development.md](docs/development.md) before deployment or device tests;
it is the command reference for production builds, preserving live sessions and
using each phone's existing signing setup.

MyGo's CLI is pinned as a Go tool in `go.mod` (Go 1.27+):

```sh
GOREX_DIR="$PWD/.mygo/dev-data" GOWORK=off go tool mygo dev # isolated development
GOWORK=off go tool mygo build  # production .app and .dmg
GOWORK=off go test ./...       # server + window-less view tests
GOWORK=off go test -run '^TestName$' .
GOWORK=off go test -run '^TestName$' ./internal/rex
GOREX_DIR="$PWD/.mygo/server-debug" GOWORK=off go run . -server # separate debug server
GOWORK=off go run ./tools/mkicon # redraw resources/icon.png
```

`MYGO_TEST_IMAGES=<dir>` makes view tests (e.g. `settings_test.go`) write PNG renders of the UI there.

## Architecture

**One binary, three roles** (dispatched in `main.go`):
- the app (window, UI);
- `-server`: the session server (`rex.Serve`), a background process that owns the PTYs so shells outlive the app. `rex.Connect` spawns it if it isn't running;
- `-agent-hook <agent>`: a silent, bounded CLI invoked by Claude Code / Codex hooks (via `$GOREX_HOOK`) to report agent lifecycle events to the server. Must never initialize AppKit or spawn a server.

**Session server / client (`internal/rex`)**: a Unix socket in the data dir. One control connection of JSON lines (`Request`/`Response` in `proto.go`, ops dispatched in `server.go` `do`), plus one connection per attached session carrying raw terminal bytes. Each session keeps a headless libghostty-vt screen and sends a snapshot on attach, so full-screen programs restore exactly. The server also tracks foreground process/cwd (`proc_darwin.go`: `tcgetpgrp`, `sysctl`, `proc_pidinfo`), titles, bells, output activity, and `AgentState` (agent status lives in the server so it survives window reconnects). The saved window layout is stored by the server too (`Layout`/`SetLayout`).
- Bump `ProtocolVersion` in `proto.go` whenever the wire protocol changes. Additive fields can remain compatible with older servers; raise `MinProtocolVersion` for incompatible changes. The app accepts versions in that supported range.
- `staleServer` compares executable path + mtime. Automatic replacement is guarded by development mode and the explicit hot-reload environment. Ordinary production GUI updates must attach to the existing compatible daemon. Never replace a live daemon to activate new code while tasks are running; server replacement ends its sessions.

**App state (`state.go`)**: `App` → `Tab` → binary tree of `Node` splits → `Pane` (a `terminal.Terminal` attached to a server session via `rex.Stream`). `poll` asks the server for `SessionInfo` every 500 ms and `apply`s it (program names, dots, attention, notifications). Tree mutations made while a frame is being built go through `a.later`/`a.post` and run in `runPosted` before the next frame, never inline.

**UI**: MyGo's immediate-mode `ui.View(a.view)`; `view.go` is the root, with `tabs.go`, `host.go`, `commands.go` (menus, palette), `preferences.go` (settings page), `compact.go`, `find.go`. `programs.go` maps foreground programs to names/icons; `agent_status.go` + `internal/agents` handle agent recognition, hook installation into `~/.claude/settings.json` / `~/.codex/hooks.json`, and event → state mapping. `settings.go` persists `settings.json`.

**`internal/terminal`** is a vendored, locally modified copy of MyGo's terminal plugin (Ghostty search API, match highlighting, copy-on-select, renderer padding changes). When updating MyGo, diff it against the new upstream plugin and carry the local changes forward — see `internal/terminal/UPSTREAM.md`.

## Data directory and debugging

- State (socket, server log, `layout.json`, `settings.json`) lives in `$GOREX_DIR`, defaulting to `~/Library/Application Support/GoRex` for the built app and `.../GoRex Dev` under `mygo dev`. An inherited `GOREX_DIR` overrides that separation; use an explicit isolated development directory.
- Tests use `newTestApp` (`app_test.go`): a temp `GOREX_DIR`, an in-process `rex.Serve()`, `SHELL=/bin/sh`, and a `ui.Tester` rendering the view at 1000×620 without a window.
- `GOREX_DEBUG=<dir>` lets a script drive the running app: write commands to `<dir>/do` (`shot`, `sleep`, `type`, `size`, `tab`, `focus`, `name`, `close`, `zoom` — see `debug.go`); `shot` writes `<dir>/shot.png`.

## Conventions

Comments are full-sentence prose in the style of the existing code (often explaining *why*, Rex-like behavior referenced as "as Rex does"); match that tone and density.
