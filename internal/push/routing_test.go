package push

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"retty/internal/rex"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureNotice(s *service, h rex.Hello, p rex.SessionInfo) Notice {
	return Notice{ID: s.noticeID(h.Host.ID, p), Desktop: h.Host.ID, Session: p.ID, Kind: p.Agent.State, Title: "Task completed", Body: "Result"}
}

func TestNoticeRoutingChoosesOneChannel(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		present, viewed, phone bool
		want                   Route
	}{
		{"viewed pane", true, true, true, RouteQuiet},
		{"another foreground pane", true, false, true, RouteDesktop},
		{"background app, user at computer", true, false, true, RouteDesktop},
		{"locked or idle computer", false, false, true, RoutePhone},
		{"away without phone", false, false, false, RouteDesktop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, provider, h, p, _ := newFixture(t)
			now := time.Now()
			if !tc.phone {
				s.provider = nil
			}
			a := DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: tc.present}
			if tc.viewed {
				a.ViewedDesktop = h.Host.ID
				a.ViewedSession = p.ID
			}
			if err := s.desktopActivity(a, now); err != nil {
				t.Fatal(err)
			}
			p = complete(s, h, p)
			n := fixtureNotice(s, h, p)
			route, err := s.claim(n, now)
			if err != nil || route != tc.want {
				t.Fatalf("route = %q, %v; want %q", route, err, tc.want)
			}
			// Repeated GUI claims cannot create a second desktop notification.
			if route != RoutePhone {
				if next, err := s.claim(n, now); err != nil || next != RouteQuiet {
					t.Fatal("event claimed twice", next, err)
				}
			}
			// Leave the computer. Previously seen/notified events must not replay.
			a.Sequence++
			a.Present = false
			a.ViewedDesktop = ""
			a.ViewedSession = ""
			s.desktopActivity(a, now.Add(time.Second))
			s.deliver(context.Background(), now.Add(4*time.Second))
			wantSent := 0
			if tc.want == RoutePhone {
				wantSent = 1
			}
			if len(provider.sent) != wantSent {
				t.Fatalf("phone deliveries=%d, want %d", len(provider.sent), wantSent)
			}
			reloaded, err := newService(s.dir)
			if err != nil {
				t.Fatal(err)
			}
			reloaded.provider = provider
			reloaded.observe(h, []rex.SessionInfo{p}, now.Add(10*time.Second))
			if next, err := reloaded.claim(n, now.Add(10*time.Second)); err != nil || next != RouteQuiet {
				t.Fatal("restart replayed event", next, err)
			}
		})
	}
}

func TestPresenceDoesNotConsumeOtherPaneOrHost(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	now := time.Now()
	a := DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true, ViewedDesktop: "other-host", ViewedSession: p.ID}
	s.desktopActivity(a, now)
	p = complete(s, h, p)
	s.deliver(context.Background(), now.Add(4*time.Second))
	if len(provider.sent) != 0 || len(s.state.Pending) != 1 {
		t.Fatal("presence consumed an unseen event or also notified phone")
	}
	if route, err := s.claim(fixtureNotice(s, h, p), now); err != nil || route != RouteDesktop {
		t.Fatal("host/session identity collision", route, err)
	}
}

func TestPhoneDeliveryPreventsLaterDesktopNotification(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	p = complete(s, h, p)
	now := time.Now()
	s.deliver(context.Background(), now.Add(4*time.Second))
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true}, now.Add(5*time.Second))
	if route, err := s.claim(fixtureNotice(s, h, p), now.Add(5*time.Second)); err != nil || route != RouteQuiet {
		t.Fatal("phone event also routed to desktop", route, err)
	}
	if len(provider.sent) != 1 {
		t.Fatal("phone event missing")
	}
}

func TestInFlightPhoneCannotAlsoBeClaimedByDesktop(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	p = complete(s, h, p)
	now := time.Now()
	provider.started = make(chan struct{})
	provider.resume = make(chan struct{})
	done := make(chan struct{})
	go func() { s.deliver(context.Background(), now.Add(4*time.Second)); close(done) }()
	<-provider.started
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true}, now.Add(4*time.Second))
	route, err := s.claim(fixtureNotice(s, h, p), now.Add(4*time.Second))
	if err != nil || route != RoutePhone {
		t.Fatal("in-flight phone delivery was also claimed by desktop", route, err)
	}
	close(provider.resume)
	<-done
	if len(provider.sent) != 1 || len(s.state.Pending) != 0 {
		t.Fatal("in-flight delivery duplicated or lost")
	}
}

func TestOnlyMostRecentlyConnectedPhoneReceivesEvent(t *testing.T) {
	s, provider, h, p, r := newFixture(t)
	other := Registration{ID: strings.Repeat("b", 32), Token: strings.Repeat("cd", 32)}
	if err := s.register(other); err != nil {
		t.Fatal(err)
	}
	p = complete(s, h, p)
	if len(s.state.Pending) != 1 {
		t.Fatal("one event queued for multiple phones")
	}
	s.deliver(context.Background(), time.Now().Add(4*time.Second))
	if len(provider.sent) != 1 || provider.sent[0].DeviceToken != other.Token {
		t.Fatal("latest phone not selected")
	}
	// Simulate an old worker's duplicate outbox entry before migration.
	n := fixtureNotice(s, h, p)
	s.state.Pending[r.ID+":"+n.ID] = pendingNotice{ID: n.ID, Device: r.ID, Desktop: n.Desktop, Session: n.Session, Kind: n.Kind}
	s.observe(h, []rex.SessionInfo{p}, time.Now().Add(5*time.Second))
	s.deliver(context.Background(), time.Now().Add(10*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("old outbox duplicate replayed")
	}
}

func TestConcurrentDesktopClaimsHaveSingleWinner(t *testing.T) {
	s, _, h, p, _ := newFixture(t)
	p = complete(s, h, p)
	now := time.Now()
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true}, now)
	n := fixtureNotice(s, h, p)
	results := make(chan Route, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			route, err := s.claim(n, now)
			if err != nil {
				t.Error(err)
			}
			results <- route
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for route := range results {
		if route == RouteDesktop {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("desktop winners=%d", winners)
	}
}

func TestRemoteAndTerminalEventsUseSameArbiter(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	now := time.Now()
	p = complete(s, h, p)
	for _, kind := range []string{"completed", "terminal"} {
		n := fixtureNotice(s, h, p)
		n.Desktop = "remote-host"
		n.ID = "retty-agent-" + kind
		n.Kind = kind
		if route, err := s.claim(n, now); err != nil || route != RoutePhone {
			t.Fatal(route, err)
		}
	}
	s.observe(h, []rex.SessionInfo{p}, now.Add(time.Second))
	for range 3 {
		s.deliver(context.Background(), now.Add(4*time.Second))
	}
	if len(provider.sent) != 3 {
		t.Fatalf("remote or terminal event lost: %d", len(provider.sent))
	}
}

func TestViewedEventAndDisabledPreferencesRemainQuiet(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	now := time.Now()
	p = complete(s, h, p)
	n := fixtureNotice(s, h, p)
	n.Viewed = true
	if route, err := s.claim(n, now); err != nil || route != RouteQuiet {
		t.Fatal(route, err)
	}
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true, HideCompletion: true}, now)
	p = complete(s, h, p)
	s.deliver(context.Background(), now.Add(4*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("viewed or disabled completion notified phone")
	}
	s.desktopActivity(DesktopActivity{ID: "gui", Sequence: 2, RoutingVersion: 1, Present: false, HideCompletion: true}, now)
	p = complete(s, h, p)
	s.deliver(context.Background(), now.Add(4*time.Second))
	if len(provider.sent) != 1 {
		t.Fatal("desktop-only preference disabled the away phone's notifications")
	}
}

func TestRoutingIPCUsesDisposableWorker(t *testing.T) {
	if dir := os.Getenv("RETTY_PUSH_ROUTING_FIXTURE"); dir != "" {
		if err := Run(dir, dir+"/absent-session.sock"); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestRoutingIPCUsesDisposableWorker$")
	child.Env = append(os.Environ(), "RETTY_PUSH_ROUTING_FIXTURE="+dir)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill(); child.Wait() }) // Only this test-owned worker.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := Query(dir); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture worker not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a := DesktopActivity{ID: "gui", Sequence: 1, RoutingVersion: 1, Present: true}
	n := Notice{ID: "retty-agent-fixture", Desktop: "fixture-host", Session: "fixture-pane", Kind: "completed"}
	if route, err := Claim(dir, a, n); err != nil || route != RouteDesktop {
		t.Fatal(route, err)
	}
	a.Sequence++
	if route, err := Claim(dir, a, n); err != nil || route != RouteQuiet {
		t.Fatal("IPC duplicate", route, err)
	}
	status, err := Query(dir)
	if err != nil || status.RoutingVersion != 1 {
		t.Fatal("missing capability", status, err)
	}
}

func TestLegacyWorkerReturnsRoutingUpgradeSignal(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", serviceSocket(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var req serviceRequest
		if err := json.NewDecoder(c).Decode(&req); err != nil {
			return
		}
		c.Write([]byte(`{"error":"unknown notification operation","status":{"configured":true,"devices":1}}` + "\n"))
	}()
	_, err = Claim(dir, DesktopActivity{}, Notice{})
	if !errors.Is(err, ErrLegacyRouting) {
		t.Fatal("live legacy worker not recognized", err)
	}
	<-done
}

func TestAwayViewerCannotConsumePresentDesktopsEvent(t *testing.T) {
	s, provider, h, p, _ := newFixture(t)
	now := time.Now()
	s.desktopActivity(DesktopActivity{ID: "source-gui", Sequence: 1, RoutingVersion: 1, Present: true}, now)
	s.desktopActivity(DesktopActivity{ID: "remote-gui", Sequence: 1, RoutingVersion: 1, Present: false}, now)
	p = complete(s, h, p)
	n := fixtureNotice(s, h, p)
	n.Caller = "remote-gui"
	if route, err := s.claim(n, now); err != nil || route != RouteQuiet {
		t.Fatal("away viewer took source's desktop delivery", route, err)
	}
	n.Caller = "source-gui"
	if route, err := s.claim(n, now); err != nil || route != RouteDesktop {
		t.Fatal("present source cannot claim event", route, err)
	}
	s.deliver(context.Background(), now.Add(10*time.Second))
	if len(provider.sent) != 0 {
		t.Fatal("source desktop event also went to phone")
	}
}
