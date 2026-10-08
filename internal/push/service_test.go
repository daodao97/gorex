package push

import (
	"context"
	"errors"
	"github.com/egoist/mygo/push/apns"
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
