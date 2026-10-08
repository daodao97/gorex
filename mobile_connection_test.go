package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gorex/internal/rex"
	"gorex/internal/terminal"
)

func TestMobileHomeReusesDesktopConnectionAndExplicitDisconnectClosesIt(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	closedTunnel := make(chan struct{}, 1)
	link := recentTestLink()
	m := &mobileApp{client: client, link: link, closeTunnel: func() { closedTunnel <- struct{}{} }, history: []desktopRecent{{ID: "mac", Name: "Mac", Link: link}}}
	m.hello.Host.ID = "mac"
	tt := ui.NewTester(m.view, 390, 750)
	tt.Click("返回")
	tt.Frame()
	if !m.home || m.client != client || m.navigation.Path() != "/connect" {
		t.Fatal("home closed the retained connection")
	}
	if _, ok := tt.Find("设备状态 Mac 已连接"); !ok {
		t.Fatal("home did not indicate the retained connection")
	}
	tt.Click("打开桌面 Mac")
	tt.Frame()
	if m.home || m.client != client || m.busy || m.generation != 0 || m.navigation.Path() != "/sessions" {
		t.Fatal("opening the same desktop performed a new handshake")
	}
	select {
	case <-closedTunnel:
		t.Fatal("page navigation closed the tunnel")
	default:
	}
	tt.Click("返回")
	tt.Click("断开桌面连接")
	tt.Frame()
	if m.client != nil || len(m.history) != 1 {
		t.Fatal("explicit disconnect retained the client or removed history")
	}
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("explicit disconnect left the control connection open")
	}
	select {
	case <-closedTunnel:
	case <-time.After(time.Second):
		t.Fatal("explicit disconnect left the tunnel open")
	}
}

func TestMobileBackgroundRetainsConnectionAndStopsForegroundWork(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	streamConn, streamPeer := net.Pipe()
	defer streamPeer.Close()
	transport := testMobileTransport{streamConn}
	stream := newMobileStream(transport, nil)
	defer stream.Close()
	polled, probed := false, false
	m := &mobileApp{
		client: client, stream: stream, link: "fixture", home: true,
		pollCancel: func() { polled = true }, presenceCancel: func() { probed = true },
	}
	defer m.stopBackgroundTimer()
	m.enterBackground()
	if m.client != client || m.generation != 0 || m.reconnecting || !m.background || !polled || !probed || m.backgroundTimer == nil || m.resumeLink != "fixture" {
		t.Fatal("short background visit closed the connection or kept polling")
	}
	if !stream.hasTransport() {
		t.Fatal("short background visit reset the terminal transport")
	}
	m.enterBackground() // Duplicate lifecycle events must not extend retention.
	if m.generation != 0 || m.client != client {
		t.Fatal("duplicate background event changed the connection")
	}
}

func TestMobileRetainedConnectionCheckUsesOnlyListAndHonorsDeadline(t *testing.T) {
	conn, peer := net.Pipe()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	defer peer.Close()
	request := make(chan rex.Request, 1)
	go func() {
		var req rex.Request
		if json.NewDecoder(peer).Decode(&req) != nil {
			return
		}
		request <- req
		data, _ := json.Marshal([]rex.SessionInfo{{ID: "alive", Cols: 112, Rows: 42}})
		json.NewEncoder(peer).Encode(rex.Response{ID: req.ID, Data: data})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := checkMobileConnection(ctx, client, &rex.DeviceInfo{Name: "iPhone"})
	if err != nil || len(got) != 1 || got[0].ID != "alive" {
		t.Fatal("retained control connection was not usable", got, err)
	}
	req := <-request
	if req.Op != "list" || req.Device == nil || req.Device.Name != "iPhone" {
		t.Fatal("reuse performed a handshake or lost device metadata", req)
	}
	select {
	case <-client.Closed():
		t.Fatal("successful check closed the connection")
	default:
	}
	// An unresponsive control channel must be released quickly, including a
	// blocked write, instead of waiting for the protocol's 15-second timeout.
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = checkMobileConnection(ctx, client, nil)
	if err == nil || time.Since(started) > time.Second || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("stale connection check ignored its deadline", err)
	}
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("timed-out retained connection was left open")
	}
}

func TestMobileResumeKeepsTerminalAndHomeAndIgnoresStaleResult(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	streamConn, streamPeer := net.Pipe()
	defer streamPeer.Close()
	stream := newMobileStream(testMobileTransport{streamConn}, nil)
	term, err := terminal.New(terminal.Options{Conn: stream, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm})
	if err != nil {
		t.Fatal(err)
	}
	m := &mobileApp{client: client, stream: stream, term: term, selected: rex.SessionInfo{ID: "alive", Cols: 112, Rows: 42}, resumeSID: "alive", reconnecting: true}
	defer m.stopPolling()
	defer m.detach()
	transport := stream.current
	m.finishRetainedConnection(client, 0, []rex.SessionInfo{m.selected}, nil)
	if m.client != client || m.term != term || m.stream != stream || stream.current != transport || m.reconnecting || m.resumeSID != "" || m.pollCancel == nil {
		t.Fatal("healthy resume reset the terminal, its transport or control connection")
	}
	m.goHome()
	m.reconnecting = true
	m.finishRetainedConnection(client, 0, []rex.SessionInfo{{ID: "alive"}}, nil)
	if !m.home || m.term != nil || m.client != client {
		t.Fatal("resume forced a retained home connection into a session")
	}
	m.generation++
	m.reconnecting = true
	m.finishRetainedConnection(client, 0, nil, nil)
	if !m.reconnecting || len(m.sessions) != 1 {
		t.Fatal("late validation result overwrote a newer connection")
	}
}

func TestMobileNotificationDuringResumeOpensRequestedSession(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, func(context.Context) (net.Conn, error) { return nil, io.EOF })
	defer client.Close()
	link := recentTestLink()
	m := &mobileApp{client: client, link: link, home: true, reconnecting: true, history: []desktopRecent{{ID: "mac", Name: "Mac", Link: link}}}
	m.hello.Host.ID = "mac"
	defer m.detach()
	defer m.stopPolling()
	m.openNotifiedSession("mac", "target")
	if m.client != client || m.resumeSID != "target" || m.generation != 0 {
		t.Fatal("notification restarted the connection while validating it")
	}
	m.finishRetainedConnection(client, 0, []rex.SessionInfo{{ID: "target", Cols: 112, Rows: 42}}, nil)
	if m.term == nil || m.selected.ID != "target" || m.home || m.resumeSID != "" || m.client != client {
		t.Fatal("resume lost the notification's target session")
	}
}

func TestMobileBackgroundNotificationDefersSessionUntilResume(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, func(context.Context) (net.Conn, error) { return nil, io.EOF })
	defer client.Close()
	m := &mobileApp{client: client, background: true, selected: rex.SessionInfo{ID: "old"}, resumeSID: "old"}
	m.hello.Host.ID = "mac"
	defer m.detach()
	defer m.stopPolling()
	m.openNotifiedSession("mac", "target")
	if m.resumeSID != "target" || m.selected.ID != "old" || m.term != nil || m.client != client {
		t.Fatal("background click opened a terminal early or lost its target")
	}
	m.background = false
	m.reconnecting = true
	m.finishRetainedConnection(client, 0, []rex.SessionInfo{{ID: "target", Cols: 112, Rows: 42}}, nil)
	if m.selected.ID != "target" || m.term == nil || m.resumeSID != "" || m.client != client {
		t.Fatal("foreground resume returned to the previous session")
	}
}
