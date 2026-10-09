//go:build gorex_cli && (darwin || linux)

package rex

import (
	"gorex/internal/terminal/screen"
	"io"
)

func newSessionScreen(cols, rows int, bell func()) (sessionScreen, io.Closer, error) {
	t, err := screen.New(cols, rows, scrollback, bell)
	if err != nil {
		return nil, nil, err
	}
	return t, t, nil
}
