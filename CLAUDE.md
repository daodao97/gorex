# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

GoRex is a replica of Superlogical's Rex terminal (tabs, split panes, persistent sessions) in Go, using MyGo's native UI (no webview, no cgo on desktop; `purego` loads libghostty-vt). macOS is the primary target; the same `main` package also builds an iPhone client that attaches to a desktop's sessions over Tailcat. See README.md for user-facing behavior and shortcuts.

## Commands

Read [docs/development.md](docs/development.md) before deployment or device tests;
it is the command reference for production builds, preserving live sessions and
using each phone's existing signing setup.

MyGo's CLI is pinned as a Go tool in `go.mod` (Go 1.27+):

```sh
GOREX_DIR="$PWD/.mygo/dev-data" GOWORK=off go tool mygo dev # isolated development
GOWORK=off go tool mygo build  # production .app and .dmg
GOWORK=off go test ./...       # server + window-less view tests
GOWORK=off go test -run '^TestName$' -count=1 .
GOWORK=off go test -run '^TestName$' -count=1 ./internal/rex
GOWORK=off go vet ./... && GOWORK=off go vet -tags mygo_noinspector ./...
GOWORK=off go test . ./internal/rex ./internal/remote -race
GOREX_DIR="$PWD/.mygo/server-debug" GOWORK=off go run . -server # separate debug server
GOWORK=off go run ./tools/mkicon # redraw resources/icon.png
GOWORK=off ./scripts/build-ios.sh -ios-simulator # iOS (device: -ios-team/-ios-device)
```

`GOWORK=off` pins the MyGo fork from `go.mod`; drop it only when deliberately developing against a local MyGo in `go.work`. Manual desktop builds need `-tags mygo_noinspector -ldflags '-X github.com/egoist/mygo.production=1'`; a plain `go build` is a MyGo development build even when copied into an installed `.app`. Remote/mobile end-to-end tests are opt-in (`GOREX_REMOTE_E2E=1`, `GOREX_MOBILE_RECOVERY_E2E=1`; see docs/development.md). `MYGO_TEST_IMAGES=<dir>` makes view tests (e.g. `settings_test.go`) write PNG renders of the UI there.

## Protecting live sessions

The installed app's daemon owns the user's real shells and agents. Never batch-kill GoRex processes (`pkill`/`killall`/by path: the server, push worker, Codex relays and hook CLIs share the binary), delete `server.sock` or the data dir, run `mygo dev` or `go run . -server` against the real data dir, or restart a live server to activate new code. Quit only the GUI normally; tests attach only to shells they created, never to real Codex/Claude sessions. For iOS, reuse the phone's existing Team/Bundle ID/profile and overwrite-install.

## Architecture

**One binary, many roles** (dispatched by argv in `main.go`, before any AppKit setup):
- the desktop app (window, UI); on `GOOS=ios` it runs `mobileMain` instead (`mobile*.go`);
- `-server`: the session server (`rex.Serve`), a background process that owns the PTYs so shells outlive the app. `rex.Connect` starts it if it isn't running; on macOS it goes through launchd with the user's login-shell environment (`background_darwin.go`), not the caller's environment, so tool-injected variables (e.g. `NO_COLOR`) don't leak into new sessions;
- `-agent-hook <agent>`: a silent, bounded CLI invoked by Claude Code / Codex hooks (via `$GOREX_HOOK`) to report agent lifecycle events to the server. Must never initialize AppKit or spawn a server;
- `-codex-bridge` / `-codex-proxy` / `-codex-watch`: metadata-only observers of Codex app-server threads (`internal/rex/codex_*.go`); they never restart or steer the Codex daemon;
- `-push-service` / `-push-import` / `-push-status`: the APNs notification worker (`internal/push`), separate from the session server.

**Session server / client (`internal/rex`)**: a Unix socket in the data dir. One control connection of JSON lines (`Request`/`Response` in `proto.go`, ops dispatched in `server.go` `do`), plus one connection per attached session carrying raw terminal bytes. Each session keeps a headless libghostty-vt screen and sends a snapshot on attach, so full-screen programs restore exactly. The server also tracks foreground process/cwd (`proc_darwin.go`: `tcgetpgrp`, `sysctl`, `proc_pidinfo`), titles, bells, output activity, and `AgentState` (agent status lives in the server so it survives window reconnects). The saved window layout is stored by the server too (`Layout`/`SetLayout`).
- Bump `ProtocolVersion` in `proto.go` whenever the wire protocol changes. Additive fields can remain compatible with older servers; raise `MinProtocolVersion` for incompatible changes. The app accepts versions in that supported range.
- `staleServer` compares executable path + mtime. Automatic replacement is guarded by development mode and the explicit hot-reload environment. Ordinary production GUI updates must attach to the existing compatible daemon. Never replace a live daemon to activate new code while tasks are running; server replacement ends its sessions.

**App state (`state.go`)**: `App` → `Tab` → binary tree of `Node` splits → `Pane` (a `terminal.Terminal` attached to a server session via `rex.Stream`). `poll` asks the server for `SessionInfo` every 500 ms and `apply`s it (program names, dots, attention, notifications). Tree mutations made while a frame is being built go through `a.later`/`a.post` and run in `runPosted` before the next frame, never inline.

**UI**: MyGo's immediate-mode `ui.View(a.view)`; `view.go` is the root, with `tabs.go`, `host.go`, `commands.go` (menus, palette), `preferences.go` (settings page), `compact.go`, `find.go`. `programs.go` maps foreground programs to names/icons; `agent_status.go` + `internal/agents` handle agent recognition, hook installation into `~/.claude/settings.json` / `~/.codex/hooks.json`, and event → state mapping. `settings.go` persists `settings.json`.

**Remote / iPhone**: `internal/remote` carries the same Rex protocol over Tailcat's encrypted tunnel (identity in `<data dir>/remote/identity.json`; the QR code is a full-access capability). The desktop side is `pairing.go`; the phone side is `mobile*.go` (`mobileApp`) plus `internal/mobile` (cgo/UIKit scanner, iOS only, stubbed elsewhere). The phone reflows the desktop's screen locally and never resizes the desktop PTY. XCUITests live in `tests/ios`, driven by `scripts/test-ios.sh`.

**`internal/terminal`** is a vendored, locally modified copy of MyGo's terminal plugin (Ghostty search API, match highlighting, copy-on-select, renderer padding changes). When updating MyGo, diff it against the new upstream plugin and carry the local changes forward — see `internal/terminal/UPSTREAM.md`.

## Data directory and debugging

- State (socket, server log, `layout.json`, `settings.json`, `render.log`) lives in `$GOREX_DIR`, defaulting to `~/Library/Application Support/GoRex` for the built app and `.../GoRex Dev` under `mygo dev`. An inherited `GOREX_DIR` overrides that separation; use an explicit isolated development directory.
- Tests use `newTestApp` (`app_test.go`): a temp `GOREX_DIR`, an in-process `rex.Serve()`, `SHELL=/bin/sh`, and a `ui.Tester` rendering the view at 1000×620 without a window.
- `GOREX_DEBUG=<dir>` lets a script drive the running app: write commands to `<dir>/do` (`shot`, `sleep`, `type`, `size`, `tab`, `focus`, `name`, `close`, `zoom` — see `debug.go`); `shot` writes `<dir>/shot.png`.

## Conventions

Comments are full-sentence prose in the style of the existing code (often explaining *why*, Rex-like behavior referenced as "as Rex does"); match that tone and density.
