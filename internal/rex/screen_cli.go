//go:build retty_cli && (darwin || linux)

package rex

import (
	"io"
	"retty/internal/terminal/screen"
)

func newSessionScreen(cols, rows int, bell func()) (sessionScreen, io.Closer, error) {
	t, err := screen.New(cols, rows, scrollback, bell)
	if err != nil {
		return nil, nil, err
	}
	return t, t, nil
}
