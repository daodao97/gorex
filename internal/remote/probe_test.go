package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"retty/internal/rex"
)

func TestProbeHandshakeDoesNotRegisterPhone(t *testing.T) {
	server, peer := net.Pipe()
	defer server.Close()
	client := rex.NewClient(peer, nil)
	defer client.Close()
	bridge := &Bridge{devices: make(map[net.Conn]ConnectedDevice)}
	observed := bridge.observe(server)
	requests := make(chan rex.Request, 1)
	go func() {
		var req rex.Request
		if json.NewDecoder(observed).Decode(&req) != nil {
			return
		}
		requests <- req
		data, _ := json.Marshal(rex.Hello{Version: rex.ProtocolVersion, Host: rex.HostInfo{Name: "Mac"}})
		json.NewEncoder(observed).Encode(rex.Response{ID: req.ID, Data: data})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probeClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	req := <-requests
	if req.Op != "hello" || req.Device != nil || req.SID != "" || req.Cols != 0 || req.Rows != 0 {
		t.Fatalf("probe sent an active session/device request: %+v", req)
	}
	if len(bridge.Devices()) != 0 {
		t.Fatal("checking availability displayed a connected phone")
	}
}

func TestPreviewReadsIconsWithoutRegisteringOrAttaching(t *testing.T) {
	server, peer := net.Pipe()
	defer server.Close()
	client := rex.NewClient(peer, nil)
	defer client.Close()
	bridge := &Bridge{devices: make(map[net.Conn]ConnectedDevice)}
	observed := bridge.observe(server)
	requests := make(chan rex.Request, 2)
	go func() {
		decoder, encoder := json.NewDecoder(observed), json.NewEncoder(observed)
		for range 2 {
			var req rex.Request
			if decoder.Decode(&req) != nil {
				return
			}
			requests <- req
			var value any = rex.Hello{Version: rex.ProtocolVersion, Host: rex.HostInfo{Name: "Mac", OS: "macOS 26.0"}}
			if req.Op == "list" {
				value = []rex.SessionInfo{{ID: "task", Program: "codex"}}
			}
			data, _ := json.Marshal(value)
			encoder.Encode(rex.Response{ID: req.ID, Data: data})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hello, sessions, err := readDesktopInfo(ctx, client, true)
	if err != nil || hello.Host.OS != "macOS 26.0" || len(sessions) != 1 || sessions[0].Program != "codex" {
		t.Fatalf("preview lost icon metadata: %+v %+v %v", hello, sessions, err)
	}
	for _, op := range []string{"list", "hello"} {
		req := <-requests
		if req.Op != op || req.Device != nil || req.SID != "" || req.Cols != 0 || req.Rows != 0 {
			t.Fatalf("preview sent an active session/device request: %+v", req)
		}
	}
	if len(bridge.Devices()) != 0 {
		t.Fatal("preview displayed a connected phone")
	}
}

func TestPreviewCancellationInterruptsUnresponsiveList(t *testing.T) {
	server, peer := net.Pipe()
	defer server.Close()
	client := rex.NewClient(peer, nil)
	defer client.Close()
	go func() {
		decoder := json.NewDecoder(server)
		var req rex.Request
		decoder.Decode(&req) // Read the list request, but never answer.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := readDesktopInfo(ctx, client, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unresponsive session list: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("preview ignored its deadline")
	}
}

func TestProbeCancellationInterruptsUnresponsiveHello(t *testing.T) {
	server, peer := net.Pipe()
	defer server.Close()
	client := rex.NewClient(peer, nil)
	defer client.Close()
	go func() {
		var req rex.Request
		json.NewDecoder(server).Decode(&req) // Read the request, but never answer.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := probeClient(ctx, client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unresponsive server: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("probe ignored its deadline")
	}
}
