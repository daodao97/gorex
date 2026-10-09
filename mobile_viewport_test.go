package main

import (
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/rex"
)

// Even replies to terminal queries are writes, not requests to take ownership
// of the shared PTY. Keep both viewers attached and verify real kernel geometry.
func TestMobileFullScreenViewportDoesNotResizeDesktop(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires Python PTY fixture")
	}
	a, _ := newTestApp(t)
	script := `import os,fcntl,termios,struct,select,tty
tty.setraw(0)
h,w,_,_=struct.unpack('HHHH',fcntl.ioctl(0,termios.TIOCGWINSZ,b'\0'*8))
os.write(1,('\x1b[?1049h\x1b[2J\x1b[1;1HSIZE %d %d\x1b[20;70HOpenCode fixture\x1b[6n'%(w,h)).encode())
while True:
    if select.select([0],[],[],.1)[0]:
        os.read(0,4096)
        os.write(1,b'\x1b[21;70Hquery answered')`
	info, err := a.client.Create(rex.CreateOptions{Command: []string{python, "-u", "-c", script}, Dir: t.TempDir(), Cols: 180, Rows: 58})
	if err != nil {
		t.Fatal(err)
	}
	defer a.client.Kill(info.ID)
	desktop := a.client.Stream(info.ID, 180, 58)
	defer desktop.Close()
	go io.Copy(io.Discard, desktop)
	phone, err := rex.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	m := &mobileApp{client: phone, sessions: []rex.SessionInfo{info}}
	m.openSession(info)
	defer m.detach()
	tt := ui.NewTester(m.view, 390, 680)
	waitFor(t, tt, "full-screen source snapshot", func() bool { return strings.Contains(m.term.Text(), "SIZE 180 58") })
	for _, size := range [][2]int{{390, 680}, {390, 360}, {750, 310}, {390, 680}} {
		tt.SetSize(size[0], size[1])
		m.term.Send([]byte("\x1b[1;1R"))
		for range 10 {
			tt.Frame()
			time.Sleep(10 * time.Millisecond)
		}
		list, err := phone.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range list {
			if in.ID == info.ID && (in.PID != info.PID || in.Cols != 180 || in.Rows != 58) {
				t.Fatal("phone or protocol reply changed shared geometry", in.Cols, in.Rows)
			}
		}
		if !strings.Contains(m.term.Text(), "SIZE 180 58") {
			t.Fatal("phone corrupted shared screen")
		}
	}
}
