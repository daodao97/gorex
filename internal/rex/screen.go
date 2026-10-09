//go:build darwin || linux

package rex

// sessionScreen is the authoritative emulator, independent of its viewers.
// The CLI uses the same Ghostty VT bindings without loading the native UI.
type sessionScreen interface {
	Feed([]byte)
	Resize(int, int)
	Snapshot() []byte
	Title() string
	Text() string
	Close() error
}
