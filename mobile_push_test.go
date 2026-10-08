package main

import (
	"github.com/egoist/mygo"
	"gorex/internal/rex"
	"testing"
	"time"
)

func TestMobilePushRegistrationWaitsForActivation(t *testing.T) {
	// The mobile store may finish loading while UIKit is still launching.
	// Starting a permission request here would fail before the app is active.
	if mygo.App.Lifecycle() == mygo.LifecycleActive {
		t.Fatal("fixture unexpectedly has an active application")
	}
	m := &mobileApp{pushDeviceID: "0123456789abcdef0123456789abcdef"}
	m.registerSystemPush()
	if m.pushRequesting || m.pushError != "" {
		t.Fatal("inactive startup started a notification permission request")
	}
}

func TestMobileRemoteAndLocalRemindersShareEventIdentity(t *testing.T) {
	m := &mobileApp{hello: rex.Hello{Version: 5, Host: rex.HostInfo{ID: "desktop"}}}
	now := time.Now()
	s := rex.SessionInfo{ID: "pane", Agent: rex.AgentState{ID: "codex", SessionID: "thread", State: "completed", CompletionRevision: 1, Updated: now}}
	id := m.taskNoticeID(s)
	event := mygo.NotificationEvent{ID: id, Source: mygo.NotificationRemote, Data: map[string]string{"desktop": "desktop", "session": "pane", "event": id, "title": "Codex · 已完成", "body": "Task"}}
	if m.presentNotification(event) != 0 || m.notice == nil {
		t.Fatal("remote event not surfaced")
	}
	if m.presentNotification(event) != 0 {
		t.Fatal("duplicate remote event shown")
	}
	if m.rememberNotice(id) {
		t.Fatal("local event would duplicate remote event")
	}
	s.Agent.CompletionRevision++
	if !m.rememberNotice(m.taskNoticeID(s)) {
		t.Fatal("new task suppressed")
	}
	m.pushDisabled = true
	event.Data["event"] = "another"
	if m.presentNotification(event) != 0 {
		t.Fatal("disabled notifications presented")
	}
}
func TestMobileLegacyReceiptsUseServerInputIdentity(t *testing.T) {
	m := &mobileApp{hello: rex.Hello{Version: 4, Host: rex.HostInfo{ID: "desktop"}}}
	s := rex.SessionInfo{ID: "pane", LastInput: time.Now(), Agent: rex.AgentState{ID: "codex", SessionID: "thread", State: "completed", CompletionRevision: 42}}
	raw := s
	raw.Agent.CompletionRevision = 0
	if m.taskNoticeID(s) != rex.AgentNoticeID("desktop", raw) {
		t.Fatal("local legacy counter changed remote receipt ID")
	}
}

func TestMobileSeenReceiptsSurviveColdLaunchAndMergeEarlyNotification(t *testing.T) {
	now := time.Now()
	m := &mobileApp{}
	m.rememberNotice("gorex-agent-early")
	m.mergeNoticeReceipts(map[string]time.Time{"gorex-agent-old": now, "gorex-agent-expired": now.Add(-8 * 24 * time.Hour)})
	if m.rememberNotice("gorex-agent-old") || m.rememberNotice("gorex-agent-early") {
		t.Fatal("cold launch lost notification receipts")
	}
	if _, exists := m.noticeReceipts["gorex-agent-expired"]; exists {
		t.Fatal("expired receipt retained")
	}
	event := mygo.NotificationEvent{Source: mygo.NotificationRemote, Data: map[string]string{"desktop": "desktop", "session": "pane", "event": "gorex-agent-old"}}
	if m.presentNotification(event) != 0 || m.notice != nil {
		t.Fatal("previously seen remote reminder repeated after cold launch")
	}
}

func TestMobilePollingAndAPNsRaceHasOneForegroundReminder(t *testing.T) {
	for _, remoteFirst := range []bool{false, true} {
		m := &mobileApp{hello: rex.Hello{Version: 5, Host: rex.HostInfo{ID: "desktop"}}}
		s := rex.SessionInfo{ID: "pane", Program: "claude", Agent: rex.AgentState{ID: "claude", SessionID: "thread", State: "running"}}
		m.updateSessions([]rex.SessionInfo{s}, true)
		s.Agent.State = "completed"
		s.Agent.CompletionRevision = 1
		s.Agent.Updated = time.Now()
		id := m.taskNoticeID(s)
		event := mygo.NotificationEvent{Source: mygo.NotificationRemote, Data: map[string]string{"desktop": "desktop", "session": "pane", "event": id, "title": "Claude Code · 已完成"}}
		if remoteFirst && m.presentNotification(event) != 0 {
			t.Fatal("foreground APNs showed a system banner")
		}
		m.updateSessions([]rex.SessionInfo{s}, false)
		if !remoteFirst && m.presentNotification(event) != 0 {
			t.Fatal("polling receipt failed to suppress APNs")
		}
		if m.notice == nil || m.notice.ID != id || len(m.noticeReceipts) != 1 {
			t.Fatal("race lost or duplicated the in-app reminder")
		}
	}
}
