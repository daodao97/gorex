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
	if m.presentNotification(event) != mygo.PresentNotificationDefault || m.notice == nil {
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
