//go:build !retty_cli && (darwin || linux)

package rex

import (
	"io"
	"retty/internal/terminal"
)

func newSessionScreen(cols, rows int, bell func()) (sessionScreen, io.Closer, error) {
	pr, pw := io.Pipe()
	t, err := terminal.New(terminal.Options{Conn: discard{pr}, Scrollback: scrollback, OnBell: bell})
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, nil, err
	}
	return t, pw, nil
}
