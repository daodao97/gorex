package main

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"gorex/internal/rex"
)

// The desktop control lease expires after 30 seconds without polling. Keep a
// short background visit reusable, then release resources when possible. iOS
// may suspend timers, so foreground entry also checks the elapsed wall time.
const mobileBackgroundRetention = 25 * time.Second
const mobileResumeCheckTimeout = 2 * time.Second

func (m *mobileApp) connectionUsable() bool {
	if m.client == nil || m.reconnecting {
		return false
	}
	select {
	case <-m.client.Closed():
		return false
	default:
		return true
	}
}

func (m *mobileApp) isConnectedDesktop(desktop desktopRecent) bool {
	return m.connectionUsable() && (desktop.Link == m.link || desktop.ID != "" && desktop.ID == m.hello.Host.ID)
}

func (m *mobileApp) openDesktop(desktop desktopRecent) {
	if m.isConnectedDesktop(desktop) {
		m.connect(m.link)
		return
	}
	m.connect(desktop.Link)
}

func (m *mobileApp) goHome() {
	m.detach()
	m.home, m.creating, m.error = true, false, ""
	m.resumeSID = ""
	m.invalidate()
}

func (m *mobileApp) stopPolling() {
	if m.pollCancel != nil {
		m.pollCancel()
		m.pollCancel = nil
	}
}

func (m *mobileApp) startPolling(client *rex.Client, generation int) {
	m.stopPolling()
	if m.background {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.pollCancel = cancel
	go m.poll(ctx, client, generation)
}

func (m *mobileApp) stopBackgroundTimer() {
	if m.backgroundTimer != nil {
		m.backgroundTimer.Stop()
		m.backgroundTimer = nil
	}
}

func (m *mobileApp) enterBackground() {
	if m.background {
		return
	}
	m.background = true
	m.backgroundAt = time.Now()
	m.stopRecentPresence()
	m.stopPolling()
	m.cancelImagePaste()
	m.stopBackgroundTimer()
	if m.client == nil && !m.busy && !m.reconnecting {
		return
	}
	m.resumeLink = m.link
	if m.resumeSID == "" && m.selected.ID != "" {
		m.resumeSID = m.selected.ID
	}
	if !m.connectionUsable() || m.busy {
		m.pauseConnection()
		return
	}
	generation, backgroundAt := m.generation, m.backgroundAt
	m.backgroundTimer = time.AfterFunc(mobileBackgroundRetention, func() {
		mygo.RunOnMain(func() {
			if m.background && m.backgroundAt == backgroundAt && m.generation == generation {
				m.backgroundTimer = nil
				m.pauseConnection()
			}
		})
	})
}

func (m *mobileApp) enterForeground() {
	if !m.background {
		return
	}
	m.background = false
	m.stopBackgroundTimer()
	if m.resumeLink == "" {
		m.invalidate()
		return
	}
	m.link, m.resumeLink = m.resumeLink, ""
	if m.connectionUsable() && time.Since(m.backgroundAt) < mobileBackgroundRetention {
		m.checkRetainedConnection()
	} else {
		m.pauseConnection()
		m.startConnection(true)
	}
	m.invalidate()
}

// Reuse the existing authenticated control connection. Bound the check so a
// stale TCP connection after a network change cannot delay recovery for 15s.
func (m *mobileApp) checkRetainedConnection() {
	client, generation := m.client, m.generation
	ctx, cancel := context.WithTimeout(context.Background(), mobileResumeCheckTimeout)
	m.cancel = cancel
	m.reconnecting = true // Disable terminal input until the check completes.
	go func() {
		sessions, err := checkMobileConnection(ctx, client, m.pushSnapshot.Load())
		cancel()
		mygo.RunOnMain(func() { m.finishRetainedConnection(client, generation, sessions, err) })
	}()
}

func checkMobileConnection(ctx context.Context, client *rex.Client, info *rex.DeviceInfo) ([]rex.SessionInfo, error) {
	stop := context.AfterFunc(ctx, func() { client.Close() })
	var sessions []rex.SessionInfo
	var err error
	if info != nil {
		sessions, err = client.ListFrom(*info)
	} else {
		sessions, err = client.List()
	}
	stop()
	if err == nil {
		err = ctx.Err()
	}
	return sessions, err
}

func (m *mobileApp) finishRetainedConnection(client *rex.Client, generation int, sessions []rex.SessionInfo, err error) {
	if m.generation != generation || m.client != client || m.background {
		return
	}
	m.cancel = nil
	if err != nil {
		m.pauseConnection()
		m.startConnection(true)
		return
	}
	m.reconnecting, m.retryAttempt, m.error = false, 0, ""
	m.updateSessions(sessions, false)
	if m.resumeSID != "" && m.resumeSID != m.selected.ID {
		sid := m.resumeSID
		m.detach()
		m.openSessionID(sid)
	} else if m.term != nil && m.stream != nil {
		found := false
		for _, session := range sessions {
			if session.ID == m.selected.ID {
				geometryChanged := !m.stream.HasScreenSize() && (session.Cols != m.selected.Cols || session.Rows != m.selected.Rows)
				m.selected = session
				m.stream.geometry(session.Cols, session.Rows)
				if !m.stream.hasTransport() || geometryChanged {
					m.stream.replace(client.ViewStream(session.ID))
				}
				found = true
				break
			}
		}
		if !found {
			m.detach()
			m.error = "原会话已结束，请选择其他会话"
		}
	}
	m.resumeSID = ""
	m.startPolling(client, generation)
	m.invalidate()
}
