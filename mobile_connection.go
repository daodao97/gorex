package main

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"gorex/internal/remote"
	"gorex/internal/rex"
)

const mobileResumeCheckTimeout = 2 * time.Second
const mobileResumeRecoveryTimeout = 4 * time.Second

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

func (m *mobileApp) enterBackground() {
	m.clearKeyboardModifiers()
	if m.background {
		return
	}
	m.background = true
	if m.term != nil {
		m.term.SetInputEnabled(false)
	}
	m.invalidate()
	m.resumeCheckID++
	m.stopRecentPresence()
	m.stopPolling()
	m.cancelImagePaste()
	if m.client == nil && !m.busy && !m.reconnecting {
		return
	}
	m.resumeLink = m.link
	if m.resumeSID == "" && m.selected.ID != "" {
		m.resumeSID = m.selected.ID
	}
	if m.busy {
		m.pauseConnection()
		return
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	// Let iOS suspend this idle transport. No background heartbeat or UI work
	// is needed, and an elapsed timer must not destroy a reusable tunnel.
}

func (m *mobileApp) enterForeground() {
	if !m.background {
		return
	}
	m.background = false
	if m.resumeLink == "" {
		m.invalidate()
		return
	}
	m.link, m.resumeLink = m.resumeLink, ""
	if m.connectionIssue != nil && !m.connectionIssue.automatic {
		m.invalidate()
		return
	}
	if m.client != nil {
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
	m.resumeCheckID++
	checkID := m.resumeCheckID
	ctx, cancel := context.WithTimeout(context.Background(), mobileResumeRecoveryTimeout)
	m.cancel = cancel
	m.reconnecting = true // Disable terminal input until the check completes.
	if m.term != nil {
		m.term.SetInputEnabled(false)
	}
	m.invalidate()
	go func() {
		next, hello, sessions, err := resumeMobileConnection(ctx, client, m.pushSnapshot.Load())
		cancel()
		mygo.RunOnMain(func() {
			if m.generation != generation || m.client != client || m.background || m.resumeCheckID != checkID {
				if next != nil && next != client {
					next.Close()
				}
				return
			}
			if err == nil && next != client {
				client.Close()
				m.client, m.hello = next, hello
				if m.stream != nil {
					m.stream.replace(nil)
				}
			}
			m.finishRetainedConnection(m.client, generation, sessions, err)
		})
	}()
}

// Check a retained socket first, then reopen only its control channel on the
// same authenticated Tailcat tunnel. A network change may require a fresh
// tunnel; the caller falls back to the ordinary reconnect in that case.
func resumeMobileConnection(ctx context.Context, client *rex.Client, info *rex.DeviceInfo) (*rex.Client, rex.Hello, []rex.SessionInfo, error) {
	checkCtx, cancel := context.WithTimeout(ctx, mobileResumeCheckTimeout)
	sessions, err := checkMobileConnection(checkCtx, client, info)
	cancel()
	if err == nil {
		return client, rex.Hello{}, sessions, nil
	}
	if ctx.Err() != nil {
		return nil, rex.Hello{}, nil, ctx.Err()
	}
	next, err := client.Redial(ctx)
	if err != nil {
		return nil, rex.Hello{}, nil, err
	}
	stop := context.AfterFunc(ctx, func() { next.Close() })
	var hello rex.Hello
	if info != nil {
		hello, err = next.HelloFrom(*info)
	} else {
		hello, err = next.Hello()
	}
	if err == nil && !rex.CompatibleProtocol(hello.Version) {
		err = &remote.ConnectionError{Kind: remote.ProtocolMismatch}
	}
	if err == nil {
		sessions, err = checkMobileConnection(ctx, next, info)
	}
	stop()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		next.Close()
		return nil, rex.Hello{}, nil, err
	}
	return next, hello, sessions, nil
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
		if remote.Failure(err) == remote.ProtocolMismatch {
			m.connectionFailed(err, true)
		} else {
			m.startConnection(true)
		}
		return
	}
	m.reconnecting, m.retryAttempt, m.error = false, 0, ""
	m.connectionIssue, m.connectionDetailsOpen = nil, false
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
