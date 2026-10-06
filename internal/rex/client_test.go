package rex

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

func resizeStream(t *testing.T) (*Stream, <-chan Request) {
	t.Helper()
	client, server := net.Pipe()
	c := &Client{conn: client, pending: map[int64]chan Response{}, closed: make(chan struct{})}
	go c.read()
	requests := make(chan Request, 100)
	go func() {
		dec, enc := json.NewDecoder(server), json.NewEncoder(server)
		for {
			var req Request
			if dec.Decode(&req) != nil {
				return
			}
			requests <- req
			if enc.Encode(Response{ID: req.ID}) != nil {
				return
			}
		}
	}()
	data, peer := net.Pipe()
	s := &Stream{c: c, sid: "test", conn: data, cols: 80, rows: 24, sentCols: 80, sentRows: 24}
	s.cond = sync.NewCond(&s.mu)
	t.Cleanup(func() { s.Close(); peer.Close(); c.Close(); server.Close() })
	return s, requests
}

func TestStreamCoalescesResize(t *testing.T) {
	s, requests := resizeStream(t)
	for cols := 81; cols <= 110; cols++ {
		if err := s.Resize(cols, 30); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case req := <-requests:
		if req.Op != "resize" || req.Cols != 110 || req.Rows != 30 {
			t.Fatalf("intermediate resize sent: %+v", req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final resize not sent")
	}
	select {
	case req := <-requests:
		t.Fatalf("extra resize sent: %+v", req)
	case <-time.After(2 * resizeDelay):
	}
}

func TestStreamCloseCancelsResize(t *testing.T) {
	s, requests := resizeStream(t)
	s.Resize(120, 40)
	s.Close()
	select {
	case req := <-requests:
		t.Fatalf("resize sent after close: %+v", req)
	case <-time.After(2 * resizeDelay):
	}
}
