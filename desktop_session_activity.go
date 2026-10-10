package main

import (
	"retty/internal/rex"
	"time"
)

// Focus changes acquire ownership once. Polls and redraws never reclaim a
// session another device took, preventing two foreground apps from fighting.
func (a *App) syncRemotePaneActivity() {
	var desired *Pane
	if t := a.tab(); a.focusedWin && t != nil && t.Host != nil {
		desired = t.Focus
	}
	if desired != a.activeRemote {
		if a.activeRemote != nil {
			a.pauseRemotePane(a.activeRemote)
		}
		a.activeRemote = desired
		if desired != nil {
			a.activateRemotePane(desired)
		}
	}
	if desired != nil && desired.term != nil && desired.remoteView != nil {
		ready := desired.remoteView.inputReady() && !desired.remoteYielded && desired.host.connected()
		if !ready {
			desired.cancelFileUpload()
		}
		desired.term.SetInputEnabled(ready)
	}
}

func (a *App) activateRemotePane(p *Pane) {
	if p == nil || p.closed || p.host == nil || !a.focusedWin {
		return
	}
	p.remoteYielded = false
	if p.remoteView != nil && p.remoteView.hasTransport() {
		return
	}
	a.resumeRemotePane(p)
}

func (a *App) resumeRemotePane(p *Pane) {
	if p.remoteView == nil || p.closed || p.info.Exited || !p.host.connected() {
		return
	}
	a.pauseRemotePane(p)
	p.remoteOwner, p.remoteSeen, p.remoteYielded = newSizeLockOwner(), false, false
	cols, rows := p.term.Size()
	if a.hostHello(p.host).Version >= 6 {
		p.stream = a.paneClient(p).LockStreamAfter(p.SID, p.remoteOwner, a.hello.Host.Name, cols, rows, p.remoteRelease)
	} else {
		// Legacy servers cannot arbitrate devices, but still resize to the
		// focused view. Hidden tabs do not keep an attachment open.
		p.stream = a.paneClient(p).Stream(p.SID, cols, rows)
	}
	p.stream.OnData = func(int) { p.lastData.Store(time.Now().UnixNano()) }
	p.remoteView.geometry(cols, rows)
	p.remoteView.replace(p.stream)
	p.streamEnded = false
	p.term.SetInputEnabled(false) // The replacement snapshot enables input.
}

func (a *App) pauseRemotePane(p *Pane) {
	p.cancelFileUpload()
	if p.remoteView == nil {
		return
	}
	if s, ok := p.remoteView.transport().(*rex.Stream); ok {
		p.remoteRelease = s.CloseAndReleaseSize()
		a.retainRemoteSizeRelease(p.host, p.remoteRelease)
	}
	p.remoteView.replace(nil)
	if p.term != nil {
		p.term.SetInputEnabled(false)
	}
	p.streamEnded = false
}

func (a *App) closePaneTerminal(p *Pane) {
	a.pauseRemotePane(p)
	if p.term != nil {
		p.term.Close()
	}
}

func (a *App) waitRemoteSizeReleases() {
	deadline := time.NewTimer(remoteSizeReleaseTimeout)
	defer deadline.Stop()
	for _, release := range a.remoteReleases {
		select {
		case <-release.done:
		case <-deadline.C:
			return
		}
	}
}

const remoteSizeReleaseTimeout = 30 * time.Second

type remoteSizeRelease struct {
	host *desktopHost
	done <-chan struct{}
}

func (a *App) retainRemoteSizeRelease(h *desktopHost, done <-chan struct{}) {
	pending := a.remoteReleases[:0]
	for _, release := range a.remoteReleases {
		select {
		case <-release.done:
		default:
			pending = append(pending, release)
		}
	}
	a.remoteReleases = append(pending, remoteSizeRelease{h, done})
}

// Do not close the shared control channel before the detached panes have
// released their ownership, including panes already removed from the layout.
func (a *App) closeReleasedDesktopTransport(h *desktopHost) {
	var releases []<-chan struct{}
	for _, release := range a.remoteReleases {
		if release.host == h {
			releases = append(releases, release.done)
		}
	}
	client, tunnel := h.client, h.tunnel
	go func() {
		deadline := time.NewTimer(remoteSizeReleaseTimeout)
		defer deadline.Stop()
		for _, done := range releases {
			select {
			case <-done:
			case <-deadline.C:
				disposeDesktopTransport(client, tunnel)
				return
			}
		}
		disposeDesktopTransport(client, tunnel)
	}()
}

func (a *App) followRemoteSizeOwner(p *Pane) {
	if p.remoteView == nil || !p.remoteView.hasTransport() || p.remoteOwner == "" || a.hostHello(p.host).Version < 6 {
		return
	}
	if p.info.SizeLock == p.remoteOwner {
		p.remoteSeen = true
		return
	}
	holds, since := p.stream.HoldsLock()
	// LIST may have been issued before this attachment acquired its lock.
	if !p.remoteSeen && (!holds || time.Since(since) < 3*time.Second) {
		return
	}
	p.remoteYielded = true
	a.pauseRemotePane(p)
}
