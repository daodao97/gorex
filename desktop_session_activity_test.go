package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"os/exec"
	"retty/internal/rex"
	"strings"
	"testing"
	"time"
)

func TestRemotePaneAndPhoneTransferActiveSession(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires Python PTY fixture")
	}
	a, desktop := newTestApp(t)
	client, _ := desktopFixtureClient(t)
	h := fixtureDesktopHost(t, client)
	a.desktops.hosts = []*desktopHost{h}
	script := `import os,fcntl,termios,struct,signal,tty,time
tty.setraw(0)
def draw(*_):
    h,w,_,_=struct.unpack('HHHH',fcntl.ioctl(0,termios.TIOCGWINSZ,b'\0'*8))
    os.write(1,('\x1b[?1049h\x1b[2J\x1b[1;1HSIZE %d %d'%(w,h)).encode())
signal.signal(signal.SIGWINCH, draw)
draw()
while True:
    time.sleep(1)`
	in, err := client.Create(rex.CreateOptions{Command: []string{python, "-u", "-c", script}, Dir: t.TempDir(), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	a.openDesktopSession(h, in, true)
	p := a.tab().Focus
	defer a.closePaneTerminal(p)
	refreshRemote := func() rex.SessionInfo {
		infos, err := client.List()
		if err != nil || len(infos) != 1 {
			t.Fatalf("session disappeared: %v", err)
		}
		a.applyHost(h, map[string]rex.SessionInfo{in.ID: infos[0]})
		return infos[0]
	}
	waitFor(t, desktop, "desktop-sized TUI", func() bool {
		s := refreshRemote()
		cols, rows := p.term.Size()
		return cols > 60 && s.SizeLock == p.remoteOwner && s.Cols == cols && s.Rows == rows && strings.Contains(p.term.Text(), fmt.Sprintf("SIZE %d %d", cols, rows))
	})
	retainedTerm, retainedView := p.term, p.remoteView
	phone, err := client.Redial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	m := &mobileApp{client: phone, hello: h.hello, sizeLock: true}
	m.openSession(p.info)
	defer m.detach()
	tt := ui.NewTester(m.view, 390, 680)
	waitFor(t, tt, "phone takes session and desktop detaches", func() bool {
		s := refreshRemote()
		m.followSizeLock(s)
		desktop.Frame()
		return s.SizeLock == m.lockOwner && s.Cols < 60 && p.remoteYielded && !p.remoteView.hasTransport()
	})
	phoneOwner := m.lockOwner
	for range 10 {
		desktop.Frame()
		tt.Frame()
		s := refreshRemote()
		if s.SizeLock != phoneOwner || s.Cols >= 60 || s.PID != in.PID {
			t.Fatal("inactive desktop reclaimed phone size or restarted task")
		}
	}
	if p.term != retainedTerm || p.remoteView != retainedView || a.tab().Focus != p {
		t.Fatal("handoff destroyed desktop tab or retained frame")
	}
	if err := desktop.Click("接入当前窗格"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, desktop, "desktop takes session and phone exits view", func() bool {
		s := refreshRemote()
		m.followSizeLock(s)
		tt.Frame()
		cols, rows := p.term.Size()
		return s.SizeLock == p.remoteOwner && s.Cols == cols && s.Rows == rows && cols > 60 && m.term == nil && strings.Contains(p.term.Text(), fmt.Sprintf("SIZE %d %d", cols, rows))
	})
	for range 10 {
		tt.Frame()
		desktop.Frame()
		s := refreshRemote()
		if s.SizeLock != p.remoteOwner || s.PID != in.PID {
			t.Fatal("inactive phone reclaimed desktop ownership")
		}
	}
	owner := p.remoteOwner
	a.selectTab(0)
	desktop.Frame()
	select {
	case <-p.remoteRelease:
	case <-time.After(2 * time.Second):
		t.Fatal("inactive tab did not release size")
	}
	if s := refreshRemote(); s.SizeLock != "" || s.PID != in.PID || p.remoteView.hasTransport() {
		t.Fatal("tab switch kept attachment or ended task")
	}
	a.selectTab(1)
	desktop.Frame()
	waitFor(t, desktop, "same session resumes in selected tab", func() bool {
		s := refreshRemote()
		return s.SizeLock == p.remoteOwner && p.remoteOwner != owner && s.PID == in.PID
	})
	desktop.SetSize(720, 480)
	waitFor(t, desktop, "active pane follows window size", func() bool {
		s := refreshRemote()
		cols, rows := p.term.Size()
		return s.Cols == cols && s.Rows == rows && strings.Contains(p.term.Text(), fmt.Sprintf("SIZE %d %d", cols, rows))
	})
	a.focusedWin = false
	a.syncRemotePaneActivity()
	select {
	case <-p.remoteRelease:
	case <-time.After(2 * time.Second):
		t.Fatal("inactive window did not release size")
	}
	if s := refreshRemote(); s.SizeLock != "" || p.remoteView.hasTransport() {
		t.Fatal("window blur retained active attachment")
	}
	a.focusedWin = true
	a.syncRemotePaneActivity()
	waitFor(t, desktop, "window focus resumes same shell", func() bool { s := refreshRemote(); return s.SizeLock == p.remoteOwner && s.PID == in.PID })
}
