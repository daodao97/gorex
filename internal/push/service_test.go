package push

import (
	"context"
	"errors"
	"github.com/egoist/mygo/push/apns"
	"gorex/internal/agents"
	"gorex/internal/rex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testSender struct {
	sent    []apns.Notification
	err     error
	started chan struct{}
	resume  chan struct{}
}

func TestPushUsesSpecificTaskInsteadOfHostOrProjectPath(t *testing.T) {
	s, provider, hello, pane, _ := newFixture(t)
	now := time.Now()
	if err := agents.SaveHookTask(s.dir, "codex", agents.HookInput{Event: "UserPromptSubmit", SessionID: pane.Agent.SessionID}, []byte(`{"prompt":"为移动端首页增加最近会话入口"}`), now); err != nil {
		t.Fatal(err)
	}
	pane = complete(s, hello, pane)
	s.deliver(context.Background(), now.Add(5*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("task notification was not delivered")
	}
	payload := provider.sent[0].Payload
	if payload.Title != "Codex · 已完成" || payload.Body != "任务：为移动端首页增加最近会话入口" || strings.Contains(payload.Body, pane.Dir) || strings.Contains(payload.Body, hello.Host.Name) {
		t.Fatal("push lacked specific task content or still contained location metadata", payload)
	}
}

func (p *testSender) Send(ctx context.Context, n apns.Notification) (apns.Response, error) {
	p.sent = append(p.sent, n)
	if p.started != nil {
		close(p.started)
		select {
		case <-p.resume:
		case <-ctx.Done():
			return apns.Response{}, ctx.Err()
		}
	}
	return apns.Response{}, p.err
}
func newFixture(t *testing.T) (*service, *testSender, rex.Hello, rex.SessionInfo, Registration) {
	t.Helper()
	s, err := newService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &testSender{}
	s.provider = provider
	now := time.Now()
	hello := rex.Hello{Version: 5, Host: rex.HostInfo{ID: "desktop", Name: "My Mac"}}
	pane := rex.SessionInfo{ID: "pane", Dir: "/private/project", LastInput: now, Agent: rex.AgentState{ID: "codex", SessionID: "thread", State: "running", Updated: now}}
	r := Registration{ID: strings.Repeat("a", 32), Token: strings.Repeat("ab", 32)}
	if err := s.register(r); err != nil {
		t.Fatal(err)
	}
	s.observe(hello, []rex.SessionInfo{pane}, now)
	return s, provider, hello, pane, r
}
func complete(s *service, h rex.Hello, p rex.SessionInfo) rex.SessionInfo {
	p.Agent.State = "completed"
	p.Agent.CompletionRevision++
	p.Agent.Updated = time.Now()
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	return p
}
func TestBackgroundDeliveryPersistsSubscriptionAndDoesNotRepeat(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	p = complete(s, h, p)
	reloaded, err := newService(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Devices[r.ID].Token != r.Token || len(reloaded.state.Pending) != 1 {
		t.Fatal("subscription/outbox lost on restart")
	}
	stateFile := filepath.Join(s.dir, "push-devices.json")
	stat, _ := os.Stat(stateFile)
	raw, _ := os.ReadFile(stateFile)
	if stat.Mode().Perm() != 0600 || strings.Contains(string(raw), "/private/project") {
		t.Fatal("unnecessary terminal data or public-readable tokens persisted")
	}
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 || provider.sent[0].Payload.Data["session"] != "pane" || provider.sent[0].Payload.Data["desktop"] != "desktop" || s.status().Sent != 1 {
		t.Fatal("background event not routed correctly")
	}
	p = complete(s, h, p) // a new authoritative revision is a distinct task
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	s.deliver(context.Background(), time.Now().Add(5*time.Second))
	if len(provider.sent) != 2 {
		t.Fatal("duplicate stop or task revision mishandled")
	}
}
func TestHistoricalResultsForegroundReceiptsAndNewInput(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	p = complete(s, h, p)
	id := rex.AgentNoticeID(h.Host.ID, p)
	r.Receipts = []string{id}
	if err := s.register(r); err != nil {
		t.Fatal(err)
	}
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("foreground local reminder repeated remotely")
	}
	// A receipt for a previous completion never suppresses a new turn.
	p.LastInput = p.LastInput.Add(time.Second)
	p.Agent.State = "running"
	p.Agent.Updated = time.Now()
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	p = complete(s, h, p)
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("new turn suppressed by old receipt")
	}
	historical, err := newService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	historical.provider = provider
	historical.register(r)
	historical.observe(h, []rex.SessionInfo{p}, time.Now())
	if len(historical.state.Pending) != 0 {
		t.Fatal("historical completion produced a reminder")
	}
}

func TestLegacyDraftEditingAndRestartNeverReplayCompletion(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	h.Version = 4
	now := time.Now()
	p.Agent.State, p.Agent.Updated = "completed", now
	s.observe(h, []rex.SessionInfo{p}, now)
	id := rex.AgentNoticeID(h.Host.ID, p)
	// A key press while the three-second grace period is still running must
	// neither cancel the real completion nor create a different event.
	p.LastInput = now.Add(time.Second)
	s.observe(h, []rex.SessionInfo{p}, now.Add(time.Second))
	s.deliver(context.Background(), now.Add(4*time.Second))
	if len(provider.sent) != 1 || provider.sent[0].Payload.ID != id {
		t.Fatal("draft edits cancelled or replaced the actual completion")
	}
	reloaded, err := newService(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.provider = provider
	// Matches the real report: same completed task, input nine minutes later.
	p.LastInput = now.Add(9 * time.Minute)
	reloaded.observe(h, []rex.SessionInfo{p}, now.Add(9*time.Minute))
	reloaded.deliver(context.Background(), now.Add(9*time.Minute+4*time.Second))
	if len(provider.sent) != 1 || len(reloaded.state.Pending) != 0 {
		t.Fatal("typing after restart repeated the previous task")
	}
	// A genuine fast new turn may start and complete between worker polls.
	p.Agent.Updated = p.LastInput.Add(time.Second)
	reloaded.observe(h, []rex.SessionInfo{p}, p.Agent.Updated)
	reloaded.deliver(context.Background(), p.Agent.Updated.Add(4*time.Second))
	if len(provider.sent) != 2 || provider.sent[1].Payload.ID == id {
		t.Fatal("a new completion was suppressed by the previous task")
	}
	// A duplicate Stop without new input keeps the completion timestamp and
	// persistent receipt, even if its hook timestamp is later.
	p.Agent.Updated = p.Agent.Updated.Add(time.Second)
	reloaded.observe(h, []rex.SessionInfo{p}, p.Agent.Updated)
	reloaded.deliver(context.Background(), p.Agent.Updated.Add(4*time.Second))
	if len(provider.sent) != 2 {
		t.Fatal("duplicate completion hook generated another push")
	}
}

func TestLegacyDatabaseMigrationAndForegroundReceipt(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	h.Version = 4
	now := time.Now()
	p.Agent.State, p.Agent.Updated = "completed", now
	// A pre-fix database contains raw snapshots and input-based receipts.
	s.state.Previous[p.ID] = savedSession{ID: p.ID, Agent: p.Agent, LastInput: p.LastInput}
	p.LastInput = now.Add(time.Minute)
	s.observe(h, []rex.SessionInfo{p}, p.LastInput)
	s.deliver(context.Background(), p.LastInput.Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("migration replayed a historical completion")
	}
	p.Agent.State = "running"
	p.Agent.Updated = p.LastInput.Add(time.Second)
	s.observe(h, []rex.SessionInfo{p}, p.Agent.Updated)
	p.Agent.State = "completed"
	p.Agent.Updated = p.Agent.Updated.Add(time.Second)
	s.observe(h, []rex.SessionInfo{p}, p.Agent.Updated)
	// Mobile has its own local counter, but acknowledges the shared timestamp.
	r.Receipts = []string{rex.AgentNoticeID(h.Host.ID, p)}
	if err := s.register(r); err != nil {
		t.Fatal(err)
	}
	s.deliver(context.Background(), p.Agent.Updated.Add(4*time.Second))
	if len(provider.sent) != 0 || len(s.state.Pending) != 0 {
		t.Fatal("mobile foreground receipt did not suppress legacy APNs delivery")
	}
}
func TestPendingWaitCancellationAndUnsubscribe(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	p.Agent.State = "waiting"
	p.Agent.WaitRevision++
	p.Agent.Updated = time.Now()
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	p.Agent.State = "running"
	p.Agent.Updated = time.Now()
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("resolved wait produced a stale reminder")
	}
	p = complete(s, h, p)
	r.Disabled = true
	r.Token = ""
	if s.register(r) != nil {
		t.Fatal("unsubscribe failed")
	}
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 0 || s.status().Devices != 0 {
		t.Fatal("unsubscribed device notified")
	}
}
func TestInFlightReceiptCancelsSendAndRotatedTokenSurvivesLate410(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	p = complete(s, h, p)
	provider.started = make(chan struct{})
	provider.resume = make(chan struct{})
	provider.err = &apns.Error{Status: 410, Reason: "Unregistered", Timestamp: time.Now()}
	done := make(chan struct{})
	go func() { s.deliver(context.Background(), time.Now().Add(4*time.Second)); close(done) }()
	<-provider.started
	r.Token = strings.Repeat("cd", 32)
	s.register(r)
	close(provider.resume)
	<-done
	if s.state.Devices[r.ID].Token != r.Token {
		t.Fatal("late 410 removed freshly rotated token")
	}
	provider.started, provider.resume, provider.err = nil, nil, nil
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 2 || provider.sent[1].DeviceToken != r.Token || len(s.state.Pending) != 0 {
		t.Fatal("event lost instead of retried with rotated token")
	}
	// A new task is cancelled if its foreground receipt arrives during send.
	provider.started = make(chan struct{})
	provider.resume = make(chan struct{})
	provider.err = errors.New("transport")
	p = complete(s, h, p)
	done = make(chan struct{})
	go func() { s.deliver(context.Background(), time.Now().Add(4*time.Second)); close(done) }()
	<-provider.started
	r.Receipts = []string{rex.AgentNoticeID(h.Host.ID, p)}
	s.register(r)
	<-done
	if len(s.state.Pending) != 0 {
		t.Fatal("acknowledged event still pending")
	}
}
func TestRegistrationRejectsUntrustedMetadata(t *testing.T) {
	s, _, _, _, r := newFixture(t)
	r.ID = "arbitrary path"
	if s.register(r) == nil {
		t.Fatal("invalid identity accepted")
	}
	r.ID = strings.Repeat("a", 32)
	r.Token = "nonhex"
	if s.register(r) == nil {
		t.Fatal("invalid token accepted")
	}
	r.Token = "ab"
	r.Receipts = make([]string, 129)
	if s.register(r) == nil {
		t.Fatal("unbounded receipts accepted")
	}
}

func TestActiveDesktopSuppressesPendingAndFuturePhoneNotifications(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	p = complete(s, h, p)
	now := time.Now()
	if err := s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, Active: true}, now); err != nil {
		t.Fatal(err)
	}
	if len(s.state.Pending) != 0 {
		t.Fatal("pending reminder survived foreground desktop")
	}
	p = complete(s, h, p)
	s.deliver(context.Background(), now.Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("active desktop also notified phone")
	}
	// Blur never replays an event already handled by the desktop.
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 2}, now.Add(time.Second))
	s.observe(h, []rex.SessionInfo{p}, now.Add(2*time.Second))
	s.deliver(context.Background(), now.Add(6*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("desktop reminder replayed on blur")
	}
	reloaded, err := newService(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.provider = provider
	p.Agent.State = "running"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	p.Agent.State = "completed"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	reloaded.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("desktop receipt lost on worker restart")
	}
	p = complete(reloaded, h, p)
	reloaded.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("new background turn suppressed")
	}
}

func TestDesktopLeaseExpiryOrderingAndMultipleWindows(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	now := time.Now()
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 2}, now)
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, Active: true}, now)
	if s.desktopActiveLocked(now) {
		t.Fatal("out-of-order focus overrode blur")
	}
	s.desktopActivity(DesktopActivity{ID: "other", Sequence: 1, Active: true}, now)
	if !s.desktopActiveLocked(now) {
		t.Fatal("second active window ignored")
	}
	after := now.Add(desktopLeaseLifetime + time.Second)
	p.Agent.State = "completed"
	p.Agent.CompletionRevision++
	p.Agent.Updated = after
	s.observe(h, []rex.SessionInfo{p}, after)
	s.deliver(context.Background(), after.Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("crashed GUI lease suppressed background notification forever")
	}
}

func TestDeliveredEventCannotReplayAfterStateFluctuationOrRestart(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	p.Agent.State = "waiting"
	p.Agent.WaitRevision = 1
	p.Agent.Updated = time.Now()
	s.observe(h, []rex.SessionInfo{p}, time.Now())
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("first wait not delivered")
	}
	reloaded, err := newService(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.provider = provider
	p.Agent.State = "running"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	p.Agent.State = "waiting"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	reloaded.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("same wait revision delivered twice")
	}
	// An installation ID change on the same phone keeps its delivery receipts.
	r.ID = strings.Repeat("b", 32)
	if err := reloaded.register(r); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.state.Devices) != 1 {
		t.Fatal("same APNs token has duplicate subscriptions")
	}
	p.Agent.State = "running"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	p.Agent.State = "waiting"
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	reloaded.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("installation migration lost receipts")
	}
	p.Agent.WaitRevision++
	reloaded.observe(h, []rex.SessionInfo{p}, time.Now())
	reloaded.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 2 {
		t.Fatal("new wait revision suppressed")
	}
}

func TestDesktopFocusCancelsInflightPhonePush(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	complete(s, h, p)
	provider.started = make(chan struct{})
	provider.resume = make(chan struct{})
	done := make(chan struct{})
	go func() { s.deliver(context.Background(), time.Now().Add(4*time.Second)); close(done) }()
	<-provider.started
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, Active: true}, time.Now())
	<-done
	if len(s.state.Pending) != 0 || s.status().Sent != 0 {
		t.Fatal("desktop focus did not cancel pending send")
	}
}

func TestInstallationMigrationPreservesPendingDelivery(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	complete(s, h, p)
	oldID := r.ID
	r.ID = strings.Repeat("b", 32)
	r.Token = strings.ToUpper(r.Token)
	if err := s.register(r); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.state.Devices[oldID]; exists {
		t.Fatal("old installation subscription retained")
	}
	if len(s.state.Pending) != 1 {
		t.Fatal("migration lost pending event")
	}
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 || provider.sent[0].DeviceToken != strings.ToLower(r.Token) {
		t.Fatal("migrated event not delivered exactly once")
	}
}
