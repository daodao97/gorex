package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"retty/internal/push"
	"retty/internal/rex"
	"testing"
)

func noticeClient(t *testing.T, b *Bridge) *rex.Client {
	t.Helper()
	control, peer := net.Pipe()
	t.Cleanup(func() { control.Close(); peer.Close() })
	c := rex.NewClient(control, func(ctx context.Context) (net.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client, server := net.Pipe()
		go func() { defer server.Close(); b.serveConnection(server, "/nonexistent/test-only-session.sock") }()
		return client, nil
	})
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRemoteNotificationClaimDoesNotAttachOrRegisterDevice(t *testing.T) {
	a := rex.DesktopActivity{ID: "viewer", Sequence: 1, RoutingVersion: 1, Present: true, ViewedDesktop: "source", ViewedSession: "pane"}
	n := push.Notice{ID: "retty-agent-test", Desktop: "source", Session: "pane", Kind: "completed"}
	called := false
	b := &Bridge{devices: map[net.Conn]ConnectedDevice{}, onNotice: func(got rex.DesktopActivity, event push.Notice) (push.Route, error) {
		called = true
		if got != a || event != n {
			t.Errorf("claim metadata changed: %+v %+v", got, event)
		}
		return push.RouteQuiet, nil
	}}
	route, err := ClaimNotification(context.Background(), noticeClient(t, b), a, n)
	if err != nil || route != push.RouteQuiet || !called {
		t.Fatal("source arbiter not used", route, err)
	}
	if len(b.Devices()) != 0 {
		t.Fatal("notification extension registered another control/device")
	}
}

func TestRemoteNotificationClaimRecognizesUnsupportedBridge(t *testing.T) {
	_, err := ClaimNotification(context.Background(), noticeClient(t, &Bridge{}), rex.DesktopActivity{}, push.Notice{})
	if !errors.Is(err, push.ErrLegacyRouting) {
		t.Fatal("missing compatibility signal", err)
	}
}

func TestDesktopPresenceForwardedOnlyAfterCompatibleHello(t *testing.T) {
	for _, version := range []int{4, rex.ProtocolVersion, rex.ProtocolVersion + 1} {
		server, peer := net.Pipe()
		received := 0
		b := &Bridge{devices: map[net.Conn]ConnectedDevice{}, onDevice: func(info rex.DeviceInfo) {
			if info.Activity != nil {
				received++
			}
		}}
		conn := b.observe(server)
		payload, _ := json.Marshal(rex.Request{ID: 1, Op: "hello", Device: &rex.DeviceInfo{Name: "desktop", Activity: &rex.DesktopActivity{ID: "viewer", Sequence: 1, RoutingVersion: 1, Present: true}}})
		conn.reads.feed(append(payload, '\n'))
		if received != 0 {
			t.Fatal("presence applied before handshake")
		}
		response, _ := json.Marshal(rex.Response{ID: 1, Data: json.RawMessage([]byte(`{"version":` + stringJSON(version) + `}`))})
		conn.writes.feed(append(response, '\n'))
		want := 0
		if rex.CompatibleProtocol(version) {
			want = 1
		}
		if received != want {
			t.Fatalf("version %d presence forwards=%d, want %d", version, received, want)
		}
		server.Close()
		peer.Close()
	}
}

func stringJSON(v int) string { b, _ := json.Marshal(v); return string(b) }
