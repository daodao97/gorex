This package is the MyGo terminal plugin from github.com/egoist/mygo v0.2.14, under the MIT license in LICENSE.

GoRex keeps a local copy because that version does not expose the Ghostty search API or search highlights to applications. The search additions are in search.go, internal/vt/search.go, and the terminal renderer. view.go also copies completed mouse selections automatically. The renderer removes horizontal grid padding and extends last-column backgrounds to the viewport edge. Native library metadata remains pinned to the upstream build.

When updating MyGo, compare this package with the new upstream terminal plugin and carry forward the local changes (or switch back when upstream exposes them).

Selection copying supports visible text (excluding SGR-concealed characters)
and raw terminal text through Options.CopyRawText and SetCopyRawText. All three
copy paths share the same formatter. The visible-text binding uses the pinned
Ghostty selection snapshot/formatter APIs and keeps wrap, scrollback and block
selection semantics; it never parses Markdown or strips visible punctuation.

Options.SelectOnDrag / SetSelectOnDrag allow native text drags while a
primary-screen program tracks the mouse, deferring plain clicks until release
and preserving mouse scrolling. GoRex enables this for recognized Agent panes
so their generated Markdown markers remain selectable. Option bypasses it;
Shift still forces selection for other mouse-aware programs.

Theme.MinimumContrast optionally adjusts text foregrounds against their final
cell background, including RGB, extended-palette and faint text. GoRex enables
this for its light theme. Backgrounds, concealed text and drawn block/line
characters are preserved. Adjustments use a bounded color-pair cache while
building changed rows; terminal state and selection copying are unaffected.

Options.AdaptiveColors / SetAdaptiveColors handle programs that cache
their startup palette. Neutral RGB/extended-palette backgrounds with the
opposite appearance are mapped relative to the light/dark theme backgrounds,
and text gets a 4.5 contrast floor in both themes. Standard ANSI backgrounds,
colored panels and block/line artwork are preserved. GoRex enables this for
all panes; disabling it invalidates cached renderer rows.

Synchronized-output protection uses an inactivity watchdog rather than the
update's total age, and schedules its own wakeup even with a hidden cursor.
Active long redraws keep the previous frame visible until commit. Optional
OnRenderEvent reports slow updates and watchdog releases outside emulator
locks, with timing metadata only; GoRex stores these in a bounded render log.

Software modifiers from MyGo's InputModifiers use the session's key encoder
for ASCII input and control keys, including Kitty event reporting and release
events. Terminal.SendKey gives native accessory buttons the same cursor/key
protocol behavior. Software Cmd+C/V/A invoke local selection/clipboard actions;
hardware app shortcuts retain their existing routing.

Selection drags auto-scroll within an 8-DIP band of the visible top/bottom
edge, so a maximized window does not require the pointer to leave its grid.
Ticks preserve the edge trigger and extend the selection across scrollback;
moving back inside or releasing the pointer stops scrolling.

Mouse-aware alternate-screen applications handle selection and their own
history scrolling automatically, based on terminal modes rather than program
identity. Their off-screen history is not in the terminal's scrollback.
Primary-screen drags keep SelectOnDrag behavior; Shift always forces local
terminal selection, and alternate screens without mouse tracking select locally.
