package main

import (
	"github.com/egoist/mygo"
	"retty/internal/rex"
	"time"
)

// locksSize tells whether an opened session takes the PTY's size: the
// setting is on, the desktop's server supports the lock, and no window
// released it for the open session.
func (m *mobileApp) locksSize() bool {
	return m.sizeLock && !m.lockLost && m.hello.Version >= 6
}

// sessionStream attaches to a session as a viewer that reflows the
// desktop's screen locally, or as the device holding the PTY's size.
func (m *mobileApp) sessionStream(sid string, lock bool) *rex.Stream {
	if lock {
		cols, rows := 0, 0
		if m.term != nil && m.selected.ID == sid {
			// A reattach keeps this view's size, which the terminal does
			// not report again while it is unchanged.
			cols, rows = m.term.Size()
		}
		m.lockOwner = newSizeLockOwner()
		return m.client.LockStreamAfter(sid, m.lockOwner, mobileDevice().Name, cols, rows, m.lockRelease)
	}
	return m.client.ViewStream(sid)
}

func (m *mobileApp) releaseSizeLock() bool {
	if m.stream == nil {
		return false
	}
	stream, ok := m.stream.transport().(*rex.Stream)
	if !ok || m.lockOwner == "" {
		return false
	}
	m.lockRelease = stream.CloseAndReleaseSize()
	return true
}

func (m *mobileApp) closeConnectionAfterSizeRelease() {
	client, closeTunnel, released := m.client, m.closeTunnel, m.lockRelease
	m.client, m.closeTunnel = nil, nil
	go func() {
		if released != nil {
			<-released
		}
		if client != nil {
			client.Close()
		}
		if closeTunnel != nil {
			closeTunnel()
		}
	}()
}

func (m *mobileApp) setSizeLock(enabled bool) {
	m.sizeLock = enabled
	if m.store != nil && m.storage != nil {
		value := []byte("0")
		if enabled {
			value = []byte("1")
		}
		m.storage <- func() {
			if m.store.Set("size-lock", value) != nil {
				mygo.RunOnMain(func() { m.error = "无法保存尺寸设置"; m.invalidate() })
			}
		}
	}
	m.invalidate()
}

// reattach replaces the open session's transport after a reconnection.
// A lock a window released meanwhile is not taken back.
func (m *mobileApp) reattach(s rex.SessionInfo) {
	if !m.lockSuspended && m.locksSize() && m.stream != nil && m.term != nil && s.SizeLock != m.lockOwner {
		m.lockLost = true
		if s.SizeLock != "" {
			m.resumeSID = ""
			m.detach()
			m.invalidate()
			return
		}
		m.openSession(s)
		return
	}
	m.lockSuspended, m.lockSeen = false, false
	m.stream.replace(m.sessionStream(s.ID, m.locksSize()))
}

// followSizeLock switches the open session to the local reflow once a
// desktop window released its size lock. A list made before this phone's
// lock was taken must not count: the lock is lost once the phone saw it
// held, or a while after its attach took it.
func (m *mobileApp) followSizeLock(s rex.SessionInfo) {
	if m.background || m.lockSuspended || m.term == nil || s.ID != m.selected.ID || !m.locksSize() {
		return
	}
	if s.SizeLock == m.lockOwner {
		m.lockSeen = true
		return
	}
	holds, since := false, time.Time{}
	if m.stream != nil {
		if stream, ok := m.stream.transport().(*rex.Stream); ok {
			holds, since = stream.HoldsLock()
		}
	}
	if !m.lockSeen && (!holds || time.Since(since) < 3*time.Second) {
		return
	}
	m.lockLost = true
	if s.SizeLock != "" {
		m.resumeSID = ""
		m.detach()
		m.invalidate()
		return
	}
	m.openSession(s)
}
