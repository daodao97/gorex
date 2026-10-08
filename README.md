# GoRex

A replica of [Superlogical's Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its
terminal plugin (Ghostty's libghostty-vt). Desktop builds use no webview or cgo;
iOS uses the UIKit host in the pinned MyGo fork.

![GoRex](docs/screenshot.png)

## What it does

- **Tabs and split panes.** A compact tab bar leaves the rest of the window
  to edge-to-edge terminals. Split, zoom and close panes with menus, the
  command palette or keyboard shortcuts. Drag the separators to resize
  panes (double-click resets a split to half).
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
  process (`tcgetpgrp`, `sysctl`, `proc_pidinfo`): tabs name the
  program (`Node`, `Git Changes` for lazygit, `Codex`, `SSH host`…) and its
  working directory, a green dot shows a program printing, and an orange dot
  marks a pane whose program finished or rang the bell out of sight. A long
  command finishing in a background tab/unfocused pane or window posts a
  notification; clicking it returns to that pane.
- **Tabs** reorder by dragging, rename by double-click, and have a context
  menu. Each tab shows its keyboard shortcut and the focused Agent's icon.
- **Right-click splits.** Choose `Split Pane Vertically` for side-by-side
  panes or `Split Pane Horizontally` for stacked panes. The new session
  inherits the right-clicked pane's working directory and receives focus.
- **Agent recognition:** tab titles and icons follow Codex, Claude Code,
  Gemini CLI, Cursor Agent, OpenCode, Copilot, Aider, Amp, Pi, Oh My Pi,
  Goose, Droid, Grok, Qwen Code, Kimi Code, Crush, CodeBuddy, Qoder CLI,
  Qoder CN CLI and TraeCode. Native executables and common Node/Python/npm
  launchers are recognized. Custom tab names remain intact; exiting an Agent
  restores the shell title and removes the Agent icon.
- **Agent status and input reminders:** Claude Code, Codex, Gemini CLI and Qwen Code hooks
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
- **Command palette** (⇧⌘P): every command, and every pane
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
- **Settings** (⌘, or GoRex ▸ Settings): the full-window settings page has
  Appearance, Terminal, Agent, Connections and About sections, search across sections,
  a modified-only filter, and per-setting reset controls. Escape returns focus
  to your terminal or its open search field.
- **Compact desktop layout**: a thin, flat tab bar with shortcut numbers,
  no host or pane headers, and terminals extending to the window edges.
  Splits, tab dragging and renaming, search and keyboard shortcuts still work.
- Light and dark appearances, with iTerm2-inspired charcoal chrome and a
  near-black terminal in dark mode. Change the theme in Settings or View ▸
  Appearance, including following the system. Your choice is remembered.
  Text size (⌘+ ⌘− ⌘0), JetBrains Mono embedded.
  Light mode uses a darker ANSI palette and adjusts low-contrast terminal
  text against its cell background, including 256-color, RGB and faint Agent
  output. Text adjustment only affects rendering; copied text, concealed
  text and block artwork retain their existing behavior.
  All panes additionally adapt neutral RGB/256-color panels
  cached by programs such as Codex across appearance changes. Their text
  stays readable in either theme without restarting the program; colored and
  standard ANSI backgrounds retain their program-supplied colors.

## iPhone

Open desktop Settings → **连接** → **显示二维码** to enable a Tailcat connection.
The title-bar phone icon appears only while a phone is connected; click it
to view connection details and the QR code. On iPhone, tap **扫码连接桌面** and scan it. The phone
lists the desktop's sessions; tap one to attach or the top-right **＋** to start the
default shell in a desktop directory. The terminal follows the system keyboard, including Chinese nine-key input.
A fixed accessory row provides Esc, Tab, Ctrl+C, up/down, **更多** and **收起**.
**更多** expands left/right, Ctrl+D, Ctrl+R, paste and common symbols while keeping
the keyboard open. Reading hides the accessory; tap the header keyboard icon to
resume input. Long-press to select text, then use the nearby copy/select-all menu.
Leaving a terminal detaches it; desktop sessions continue running. Returning
from the background reconnects to the selected session. Swipe right from the
left edge to return from a terminal or new-session form to the session list,
or from the list to the connection screen. Cancelling the swipe keeps the
current page, selection and keyboard. Recent desktops
appear below the scan button for one-tap reconnection; their capabilities
are stored in the device Keychain. Each desktop keeps only its newest
connection record, even after generating a new QR code.
Recent connections show **在线**, **不可达**, or **检查中**. While the connection
screen is in the foreground, saved links are checked every 30 seconds with a
short handshake. These checks do not attach to sessions or register a connected
phone. Availability describes the saved GoRex link, rather than the computer's
power state.
The desktop title bar uses a muted green phone icon while connected; the pairing panel includes
device names, OS and connection times. Finger swipes scroll terminal history.
Long-press a word to select it, then drag to expand the selection; **复制**
copies the selected text. Long presses do not open the keyboard,
and the cursor keeps blinking while the keyboard is hidden. The phone
decodes ANSI at the desktop's original grid size, then reflows the primary
screen into its own columns and rows at a readable font size. Keyboard and
orientation changes adjust only the phone's view. **完整** shows the original
screen scaled to fit, and **适应** returns to the local layout. The original grid of full-screen programs remains available in **完整** mode. Programs in a
shared session still generate output for a single PTY; the phone never
changes the desktop's PTY dimensions.

The QR code contains the Tailcat capability needed to access every session
on that desktop. Keep it private. **停止连接** revokes it and disconnects
phones; a newly enabled connection produces a new code. No Tailscale account
or separately installed VPN is needed. Both devices need internet access.

### Build iOS

The module pins the iOS-capable [MyGo fork](https://github.com/daodao97/mygo).
For local framework development, keep it beside this repository and run
`go work init . ../mygo`; the workspace files remain local. On an Apple Silicon
Mac with Xcode and an iOS signing team, build with:

```sh
./scripts/build-ios.sh -ios-team YOUR_TEAM_ID -ios-device YOUR_DEVICE_ID
```

The script downloads Zig 0.16.0 and the pinned Ghostty source into `.mygo/ios`,
builds libghostty-vt for iOS and embeds it into the Go archive, then invokes
the fork's MyGo CLI to package and sign the app. Camera access is requested
only when opening the scanner. Use `-ios-simulator` for an arm64 simulator
build; device and simulator builds regenerate the static library object.

```sh
go test ./...
GOREX_REMOTE_E2E=1 go test ./internal/remote -run TestTailcatSessionLifecycle -v
```

```sh
IOS_TEAM=YOUR_TEAM_ID IOS_DEVICE=YOUR_DEVICE_UDID ./scripts/test-ios.sh
```

`tests/ios` contains a real-device XCTest flow for scanner presentation,
existing/new sessions, keyboard actions, nine-key candidate preservation,
selection/paste, rotation, background restoration and Keychain reconnection.
Install English (US) and Simplified Chinese Pinyin nine-key keyboards on the test
device. The flow uses a separate Keychain namespace so its
temporary desktop never replaces everyday recent connections. Its private `Fixture.swift` is generated from the
isolated transport test's `GOREX_IOS_FIXTURE` JSON and is never committed.

Mobile connections automatically retry after transport loss (1–16 second backoff),
keeping the terminal view and reading position. Input is disabled during retries
and is never replayed. Cancel stops retries; returning from the background restores
the same session. Mobile viewing does not resize the desktop PTY.

Long-press a mobile session to pin it or set a mobile display name. Preferences
are stored in Keychain per desktop identity and session, without changing desktop
titles. Sessions waiting for Agent permission/answers appear first, followed by
pinned sessions. Supported lifecycle integrations show running/waiting/completed/failed
states; reminders can open the corresponding session. Notifications require iOS
permission. With APNs configured, a separate desktop notification worker observes
the existing session server and sends waiting/completed/failed reminders while iOS
is suspended. It keeps running when the desktop window closes. A focused desktop
window handles reminders locally and suppresses phone pushes; switching away does
not replay those reminders. On the phone, foreground reminders appear only inside
the app, while APNs handles background system notifications. Persisted event receipts
suppress duplicate pushes across app/worker restarts, new task input cancels stale reminders, and notification
taps reconnect to the original desktop and pane, including a cold launch. The
mobile session list includes a background-reminder switch.

The project enables APNs with `ios.pushNotifications: true` in `mygo.json`.
Enable Push Notifications for the `dev.gorex.app` App ID in Apple Developer, use
an appropriate provisioning profile, and create a topic-specific APNs key. Import
it once from the project directory on the desktop sender:

```sh
go tool mygo push setup /private/path/AuthKey_KEYID.p8
go tool mygo push status # verify local provider configuration and key loading
GoRex -push-status      # worker device/delivery count and last error
```

MyGo infers Bundle ID, Team ID and Key ID; configuration loading, credential
storage, JWT/HTTP2 reuse and key reload live in its `push/apns` package. GoRex
owns only subscriptions and task policies. New projects can use
`mygo.App.PushProvider()` directly; standalone senders use
`apns.OpenProvider(dataDir)`. See the fork's
[APNs onboarding guide](https://github.com/daodao97/mygo/blob/main/docs/push.md).
The older `GoRex -push-import KEYFILE KEYID TEAMID ENVIRONMENT` command remains
available for compatibility, including existing installed versions.

On macOS the private key is stored in the login Keychain. Provider metadata and
device subscriptions live in private files in the GoRex application-support
directory. The mobile app sends its refreshed token through the existing encrypted
connection; provider keys are never embedded in the mobile app. Only short task
status, desktop name and project basename appear in a push, without terminal output
or prompts. Use `mygo push setup -environment production KEYFILE` and the matching key/profile for a
distribution build; development device builds use `sandbox`. APNs accepts delivery
requests independently of the phone connection, but notification taps still need
the desktop's Tailcat link to be available.

The Gemini and Qwen hook mappings and timeout units follow their official references:
[Gemini CLI hooks](https://geminicli.com/docs/hooks/reference/) and
[Qwen Code hooks](https://qwenlm.github.io/qwen-code-docs/en/users/features/hooks/).

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

Open Settings (⌘,) → Agent and enable the integrations for Claude Code, Codex, Gemini CLI or Qwen Code.
This merges GoRex's handlers into `~/.claude/settings.json` and
`~/.codex/hooks.json`, `~/.gemini/settings.json` and `~/.qwen/settings.json`, respecting `CLAUDE_CONFIG_DIR` and `CODEX_HOME` overrides.
Existing settings and hooks are preserved; the first original file is backed
up as `<file>.gorex-backup`. Remove integration in the same page to remove only
GoRex's handlers. The hooks are silent no-ops in other terminal apps.

Gemini CLI and Qwen Code require the protocol-5 session server. Updating the app
does not replace a running older service. After existing tasks have finished,
use Shell → Quit and End All Sessions and reopen the updated app to start its
new service. This ends the existing terminal sessions.

Start a new Agent session after installation. Claude Code runs normally.
For Codex, run `codex` normally and review/trust the added handlers in `/hooks`.
New terminals prepend a pane-local Codex launcher. Interactive tasks (including
resume/fork) connect to the existing shared daemon through a private local relay.
GoRex observes its structured turn and waiting events, associating the thread IDs
returned to that CLI with the owning pane. The daemon can already be running outside
GoRex: no special startup flags, daemon restart or inherited hook environment are
required. Separate panes in the same directory retain independent status. The relay
preserves the pane's working directory and forwards messages and approval responses
unchanged. It stores no prompt text, credentials or transcripts.
Explicit remote connections and utility commands keep their original behavior;
if the local relay is unavailable, normal CLI startup remains available. Existing
running tasks are not restarted. Legacy shared-daemon hooks that already carry GoRex's
environment retain conversation-based routing; ambiguous starts are skipped.
Remote daemons, disabled/untrusted hooks without a local relay
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
| `view.go`, `tabs.go`, `commands.go` | the interface: tabs, panes, menus and palette |
| `programs.go` | how programs show: names, glyphs, icon colors |
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
