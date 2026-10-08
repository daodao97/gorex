package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"gorex/internal/rex"
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
