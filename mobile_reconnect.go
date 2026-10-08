package main

import (
	"github.com/egoist/mygo"
	"time"
)

func (m *mobileApp) pauseConnection() {
	m.clearKeyboardModifiers()
	m.cancelImagePaste()
	m.stopPolling()
	m.generation++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.retryTimer != nil {
		m.retryTimer.Stop()
		m.retryTimer = nil
	}
	m.busy = false
	m.reconnecting = true
	if m.stream != nil {
		m.stream.replace(nil)
	}
	if m.term != nil {
		m.term.SetInputEnabled(false)
	}
	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	if m.closeTunnel != nil {
		close := m.closeTunnel
		m.closeTunnel = nil
		go close()
	}
	m.invalidate()
}

func (m *mobileApp) connectionLost() {
	if m.reconnecting || m.background || m.link == "" {
		return
	}
	if m.client != nil {
		m.clearKeyboardModifiers()
		m.cancelImagePaste()
		m.stopPolling()
		m.checkRetainedConnection()
		return
	}
	m.pauseConnection()
	m.retryAttempt = 0
	m.scheduleRetry()
}

func mobileRetryDelay(attempt int) time.Duration {
	return min(time.Duration(1<<min(max(attempt, 0), 3))*time.Second, 5*time.Second)
}

func (m *mobileApp) scheduleRetry() {
	if m.background || !m.reconnecting || m.connectionIssue != nil && !m.connectionIssue.automatic {
		return
	}
	if m.retryTimer != nil {
		m.retryTimer.Stop()
	}
	generation := m.generation
	delay := mobileRetryDelay(m.retryAttempt)
	if m.retryAttempt == 0 {
		delay = 0
	}
	m.retryAttempt++
	m.retryTimer = time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			if m.generation == generation && m.reconnecting && !m.background {
				m.retryTimer = nil
				m.startConnection(true)
			}
		})
	})
	m.invalidate()
}
