This package is the MyGo terminal plugin from github.com/egoist/mygo v0.2.14, under the MIT license in LICENSE.

GoRex keeps a local copy because that version does not expose the Ghostty search API or search highlights to applications. The search additions are in search.go, internal/vt/search.go, and the terminal renderer. view.go also copies completed mouse selections automatically. The renderer removes horizontal grid padding and extends last-column backgrounds to the viewport edge. Native library metadata remains pinned to the upstream build.

When updating MyGo, compare this package with the new upstream terminal plugin and carry forward the local changes (or switch back when upstream exposes them).

Selection copying supports visible text (excluding SGR-concealed characters)
and raw terminal text through Options.CopyRawText and SetCopyRawText. All three
copy paths share the same formatter. The visible-text binding uses the pinned
Ghostty selection snapshot/formatter APIs and keeps wrap, scrollback and block
selection semantics; it never parses Markdown or strips visible punctuation.

Options.SelectOnDrag / SetSelectOnDrag allow native text drags while a program
tracks the mouse, deferring plain clicks until release and preserving mouse
scrolling. GoRex enables this for recognized Agent panes so their generated
Markdown markers remain selectable. Option bypasses it; Shift still forces
selection for other mouse-aware programs.
