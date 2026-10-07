# GoRex

A replica of [Superlogical's Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its
terminal plugin (Ghostty's libghostty-vt). No webview, no cgo: one ~15 MB app.

![GoRex](docs/screenshot.png)

## What it does

- **Tabs and split panes.** Every pane is a card over the window's gradient,
  with its program's icon, name and directory in its header, and buttons to
  split right, split down, zoom and close. Drag the gaps between panes to
  resize them (double-click resets a split to half).
- **Persistent sessions.** Shells run in a session server, a background
  process of the same binary (`GoRex -server`). Quit GoRex and everything
  keeps running; open it again and every tab, split and pane comes back as
  it was, programs still running. The server keeps each session's screen in
  a headless libghostty-vt emulator and sends a snapshot on attach, so
  full-screen programs (lazygit, vim, htop…) come back exactly.
  Normal quit leaves that server running. Protocol versions 4 and 5 are
  compatible, so upgrading or reopening the UI reuses those live sessions.
  Older servers use observed state/input changes for Agent completion alerts;
  version 5's completion counter also covers turns completed between polls.
- **Program activity.** The server watches each terminal's foreground
  process (`tcgetpgrp`, `sysctl`, `proc_pidinfo`): headers and tabs name the
  program (`Node`, `Git Changes` for lazygit, `Codex`, `SSH host`…) and its
  working directory, a green dot shows a program printing, and an orange dot
  marks a pane whose program finished or rang the bell out of sight. A long
  command finishing in a background tab/unfocused pane or window posts a
  notification; clicking it returns to that pane.
- **Tab icons** stack the tiles of a tab's programs, the focused pane's in
  front, as Rex does. Tabs reorder by dragging, rename by double-click, and
  have a context menu.
- **Right-click splits.** Choose `Split Pane Vertically` for side-by-side
  panes or `Split Pane Horizontally` for stacked panes. The new session
  inherits the right-clicked pane's working directory and receives focus.
- **Agent recognition:** tab titles and icons follow Codex, Claude Code,
  Gemini CLI, Cursor Agent, OpenCode, Copilot, Aider, Amp, Pi, Oh My Pi,
  Goose, Droid, Grok, Qwen Code, Kimi Code, Crush, CodeBuddy, Qoder CLI,
  Qoder CN CLI and TraeCode. Native executables and common Node/Python/npm
  launchers are recognized. Compact tabs show the focused agent's icon too;
  custom tab names remain intact, and icons return to the shell on exit.
- **Agent status and input reminders:** Claude Code and local Codex hooks
  report ready, running, waiting for authorization/answers, completed and
  failed states. Waiting panes take priority in a split tab; click its status
  marker to focus that pane. A new waiting period posts one desktop notification
  unless you are already viewing its pane. Clicking the notification selects
  the correct tab and split, reveals it if another pane was zoomed, and restores
  keyboard focus. Status stays in the session server across window reconnects.
  Completed/failed Agent turns also notify in background tabs or unfocused panes,
  even while the app is in front. Each completed turn notifies at most once;
  already viewed or historical completed turns are not replayed on tab changes
  or window reconnects. Settings → Agent separately controls completion/failure
  and waiting-input notifications; both are enabled by default.
- **Host chip** with the machine's name and model; its popover shows the
  chip, memory, OS and the session server.
- **Command palette** (⇧⌘P, or the ⌘ button): every command, and every pane
  to jump to, fuzzy-matched.
- **Find in terminal** (⌘F): search the current pane's screen and scrollback,
  with highlighted matches and a result count. Enter / ⌘G moves toward older
  output, Shift-Enter / ⇧⌘G toward newer output, wrapping at either end.
  Escape closes the bar and returns focus to the terminal. Searches are
  literal and ignore ASCII letter case; Unicode text is matched exactly.
- **Copy on selection:** releasing a drag selection, double-clicking a word,
  or triple-clicking a line copies it automatically. Empty selections leave
  the clipboard intact; ⌘C and the Copy menu continue to work.
  Settings → Terminal → Copy content chooses visible text (the default,
  omitting concealed characters) or raw terminal text. Both keep selected
  list numbers and literal code; switching applies to existing panes without
  restarting sessions. Raw text cannot restore Markdown absent from the terminal.
  In recognized Agent panes, dragging uses the terminal's selection so list
  markers stay selectable even when the Agent enables mouse reporting. Plain
  clicks and scrolling still reach the Agent; hold Option to let it handle a
  drag, or Shift to force terminal selection in other mouse-aware programs.
- **Command-click links and file paths:** hold ⌘ for a hand cursor and underline,
  then click a URL, OSC 8 link, or printed file reference. Relative paths use
  the pane's current working directory; absolute paths, `~/`, quoted paths with
  spaces, `file:line:column`, `file(line,column)` and `file#LlineCcolumn` work too.
  Soft-wrapped visible links and scrollback are supported. Settings → Terminal
  selects Auto, VS Code, Cursor or the system default app. Auto prefers installed
  VS Code, then Cursor; those editors support line/column navigation through their
  file URL handlers ([VS Code reference](https://code.visualstudio.com/docs/configure/command-line)).
  Directories open in Finder. Missing files show an error; remote SSH/Mosh file
  paths are not opened locally. Command-click also works while a program reports
  mouse events and leaves the clipboard intact. Relative paths in older output
  use the current directory, rather than a historical command directory.
- **Settings** (⌘, or GoRex ▸ Settings): show or hide the session titles and
  control buttons above terminal panes, and the host name and model in the
  title bar. Both show by default; changes apply immediately and are remembered
  after restarting. The full-window settings page has a left-aligned sidebar,
  General, Appearance, Terminal, Agent and About sections, search across sections,
  a modified-only filter, and per-setting reset controls. Theme and terminal
  text size can also be changed here. Escape returns focus to your terminal
  or its open search field.
- **Compact mode** in Settings: a thin, flat tab bar with shortcut numbers,
  no host or pane headers, and terminals extending to the window edges.
  Splits, tab dragging and renaming, search and keyboard shortcuts still work.
  Turning it off restores your usual host and header preferences.
- Light and dark appearances, with iTerm2-inspired charcoal chrome and a
  near-black terminal in dark mode. Click the moon/sun in the title bar to
  switch; View ▸ Appearance (or right-click the theme button) also offers
  following the system. Your choice is remembered. Text size (⌘+ ⌘− ⌘0),
  JetBrains Mono embedded.
  Light mode uses a darker ANSI palette and adjusts low-contrast terminal
  text against its cell background, including 256-color, RGB and faint Agent
  output. Text adjustment only affects rendering; copied text, concealed
  text and block artwork retain their existing behavior.
  All panes additionally adapt neutral RGB/256-color panels
  cached by programs such as Codex across appearance changes. Their text
  stays readable in either theme without restarting the program; colored and
  standard ANSI backgrounds retain their program-supplied colors.

## Shortcuts

| | |
|---|---|
| ⌘T | New tab |
| ⌘D / ⇧⌘D | Split right / down |
| ⌘W / ⇧⌘W | Close pane / tab |
| ⇧⌘↩ | Zoom the pane |
| ⌥⌘ arrows | Focus the pane in that direction |
| ⌃⌘ arrows | Move the nearest divider |
| ⌃⌘= | Equalize panes |
| ⌘1…⌘9, ⇧⌘[ ⇧⌘] | Switch tabs |
| ⌃Tab / ⌃⇧Tab | Next / previous tab (wraps around) |
| ⇧⌘R | Rename tab |
| ⇧⌘P, ⌘P | Command palette |
| ⌘, | Settings |
| ⌘F | Find in the current pane |
| ⌘G / ⇧⌘G | Next / previous search match |
| ⌘K | Clear |
| ⌥⌘Q | Quit and end all sessions |

## Enable Agent status

Open Settings (⌘,) → Agent and enable the Claude Code and/or Codex integration.
This merges GoRex's handlers into `~/.claude/settings.json` and
`~/.codex/hooks.json`, respecting `CLAUDE_CONFIG_DIR` and `CODEX_HOME` overrides.
Existing settings and hooks are preserved; the first original file is backed
up as `<file>.gorex-backup`. Remove integration in the same page to remove only
GoRex's handlers. The hooks are silent no-ops in other terminal apps.

Start a new Agent session after installation. Claude Code runs normally.
For Codex, run `codex` normally and review/trust the added handlers in `/hooks`.
Shared-daemon events are routed by conversation ID, working directory and new-session
state, including when the daemon's original pane has closed. If several new Codex
panes cannot be distinguished, GoRex skips the event instead of notifying the wrong
pane. A daemon already started outside GoRex lacks the hook environment; restart it
from a GoRex pane to enable integration. Remote daemons, disabled/untrusted hooks
and other Agent CLIs retain ordinary activity indicators;
they do not provide input-state reminders. No trust settings or approval decisions
are changed by GoRex. Desktop reminders are enabled by default and can be disabled
in Settings → Agent; macOS must allow notifications for GoRex.

Hook status uses actual lifecycle events, not terminal-output heuristics.
Claude permissions, `AskUserQuestion`/MCP elicitation and Codex permission requests
are supported. Codex `request_user_input` is recognized when that tool passes
through Codex's local hook path; specialized tool paths that skip hooks cannot
provide a waiting signal. Unrelated tool completions cannot clear another tool's
pending question, and delayed events from previous conversations/turns are ignored.

## Develop and build

GoRex needs [Go](https://go.dev/dl/) 1.27+ and MyGo 0.2.11, whose CLI
`go.mod` pins as a tool:

```sh
go tool mygo dev     # GoRex Dev, rebuilt and restarted as the code changes
go test ./...        # the server, and the view without a window
go tool mygo build   # build/darwin-arm64/GoRex.app and a .dmg
go run ./tools/mkicon  # redraw resources/icon.png
```

`go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates MyGo and its
CLI together.

The app keeps its state in its data directory: the server's socket and
log, `layout.json` and `settings.json`, in `~/Library/Application
Support/GoRex` for the built app and `GoRex Dev` for `mygo dev`'s, so
that developing never touches the sessions of the app you use
(`GOREX_DIR` names another directory). The sessions outlive the app, but
not a rebuild: a development build replaces a server that an older build
started, ending its sessions, which then start again in the same
directories.

Slow synchronized redraws and incomplete-update watchdog releases are
recorded in `render.log` in the same data directory. This contains only
timestamps, session IDs and durations, never terminal output or input, and
rotates at 1 MiB with one previous file. The watchdog waits for one second
without incoming output, so a large active redraw can finish without exposing
its intermediate clear-screen frame.

## Layout

| | |
|---|---|
| `main.go` | the app, its window, and `-server` |
| `state.go` | tabs, the tree of splits, panes, saving and restoring the layout |
| `view.go`, `tabs.go`, `host.go`, `commands.go` | the interface: title bar, tabs, panes, host popover, menus and palette |
| `programs.go` | how programs show: names, glyphs, tile colors |
| `style.go`, `settings.go` | colors, fonts, terminal themes; appearance and text size |
| `find.go` | each pane's search bar, shortcuts and focus handling |
| `preferences.go` | settings page and its focus handling |
| `compact.go` | compact title bar and tab labels |
| `agent_status.go`, `internal/agents` | agent recognition, lifecycle states, notifications and hook installation |
| `internal/rex` | the session server and its client: PTYs, foreground processes, headless screens, the socket protocol |
| `internal/terminal` | MyGo's terminal plugin, locally extended with Ghostty search bindings and match highlighting; provenance in `UPSTREAM.md` |

Not replicated: Rex's connections to servers on other machines; GoRex's
server listens on a local socket only.

Icons: [Lucide](https://lucide.dev) (ISC) and [Simple Icons](https://simpleicons.org)
(CC0). Font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (OFL).
Agent avatars: [tty7](https://github.com/l0ng-ai/tty7) (Apache-2.0); source
revision and license are included in `assets/agents/` and embedded in the app.
