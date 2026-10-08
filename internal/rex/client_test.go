package rex

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadSnapshotWaitsForCompleteFrameAndLeavesLiveOutput(t *testing.T) {
	conn, peer := net.Pipe()
	s := &Stream{conn: conn, screenFrames: true}
	s.cond = sync.NewCond(&s.mu)
	defer s.Close()
	defer peer.Close()
	payload := bytes.Repeat([]byte("中文🙂 snapshot "), 6000)
	frame := func(data []byte) []byte {
		b := make([]byte, 8+len(data))
		binary.BigEndian.PutUint32(b, uint32(len(data)))
		binary.BigEndian.PutUint16(b[4:], 112)
		binary.BigEndian.PutUint16(b[6:], 42)
		copy(b[8:], data)
		return b
	}
	firstHalf := make(chan struct{})
	release := make(chan struct{})
	go func() {
		b := frame(payload)
		peer.Write(b[:4000])
		close(firstHalf)
		<-release
		peer.Write(b[4000:])
		peer.Write(frame([]byte("live output")))
	}()
	done := make(chan []byte, 1)
	go func() {
		data, c, r, e := s.ReadSnapshot()
		if e != nil || c != 112 || r != 42 {
			done <- nil
			return
		}
		done <- data
	}()
	<-firstHalf
	select {
	case <-done:
		t.Fatal("partial snapshot escaped")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if got := <-done; !bytes.Equal(got, payload) {
		t.Fatal("snapshot lost fragmented Unicode data")
	}
	buf := make([]byte, 32)
	n, c, r, e := s.ReadScreen(buf)
	if e != nil || string(buf[:n]) != "live output" || c != 112 || r != 42 {
		t.Fatal("snapshot consumed subsequent live output", e)
	}
}

func TestReadSnapshotRejectsTruncatedFrameAndPreservesLegacyBytes(t *testing.T) {
	for _, framed := range []bool{false, true} {
		conn, peer := net.Pipe()
		s := &Stream{conn: conn, screenFrames: framed}
		s.cond = sync.NewCond(&s.mu)
		go func() {
			defer peer.Close()
			if framed {
				var h [8]byte
				binary.BigEndian.PutUint32(h[:], 500)
				binary.BigEndian.PutUint16(h[4:], 80)
				binary.BigEndian.PutUint16(h[6:], 24)
				peer.Write(h[:])
			}
			peer.Write([]byte("partial"))
		}()
		data, _, _, err := s.ReadSnapshot()
		if framed {
			if err == nil || len(data) != 0 {
				t.Fatal("truncated snapshot accepted")
			}
		} else {
			if err != nil || len(data) != 0 {
				t.Fatal("legacy bytes unexpectedly consumed")
			}
			buf := make([]byte, 7)
			if _, err := io.ReadFull(s, buf); err != nil || string(buf) != "partial" {
				t.Fatal("legacy fallback lost bytes")
			}
		}
		s.Close()
	}
}

func TestViewStreamNeverResizesDesktop(t *testing.T) {
	s, requests := resizeStream(t)
	s.preserveSize = true
	for _, size := range [][2]int{{48, 24}, {90, 16}, {48, 9}} {
		if err := s.Resize(size[0], size[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.sync(); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-requests:
		t.Fatalf("phone resized desktop PTY: %+v", req)
	case <-time.After(2 * resizeDelay):
	}

	data, peer := net.Pipe()
	c := &Client{dial: func(context.Context) (net.Conn, error) { return data, nil }}
	attached := make(chan Attach, 1)
	go func() {
		var req Attach
		json.NewDecoder(peer).Decode(&req)
		attached <- req
		peer.Write([]byte("{\"ok\":true}\n"))
	}()
	viewer := c.ViewStream("existing")
	defer viewer.Close()
	defer peer.Close()
	if err := viewer.Resize(48, 12); err != nil {
		t.Fatal(err)
	}
	req := <-attached
	if req.Cols != 0 || req.Rows != 0 || req.SID != "existing" {
		t.Fatalf("viewer attach changed desktop dimensions: %+v", req)
	}
}

func TestLegacyHelloIdentityMatchesNotificationWorker(t *testing.T) {
	client, server := net.Pipe()
	c := NewClient(client, nil)
	t.Cleanup(func() { c.Close(); server.Close() })
	host := HostInfo{Name: "MacBook", User: "owner", Home: "/Users/owner", Model: "MacBook Pro", Chip: "Apple M4"}
	go func() {
		decoder, encoder := json.NewDecoder(server), json.NewEncoder(server)
		for i := 0; i < 3; i++ {
			var request Request
			if decoder.Decode(&request) != nil {
				return
			}
			wire := host
			if i == 2 {
				wire.ID = "machine:existing"
			}
			data, _ := json.Marshal(Hello{Version: 4, Host: wire})
			encoder.Encode(Response{ID: request.ID, Data: data})
		}
	}()
	worker, err := c.Hello()
	if err != nil || !strings.HasPrefix(worker.Host.ID, "legacy:") {
		t.Fatalf("legacy worker identity missing: %v", err)
	}
	phone, err := c.HelloFrom(DeviceInfo{Name: "iPhone"})
	if err != nil || phone.Host.ID != worker.Host.ID {
		t.Fatalf("phone and worker cannot match notification identities: %v", err)
	}
	existing, err := c.Hello()
	if err != nil || existing.Host.ID != "machine:existing" {
		t.Fatal("server identity overwritten")
	}
	host.OS = "macOS updated"
	if legacyHostID(host) != worker.Host.ID {
		t.Fatal("OS update rotated legacy identity")
	}
	host.Name = "Different desktop"
	if legacyHostID(host) == worker.Host.ID || legacyHostID(HostInfo{}) != "" {
		t.Fatal("unrelated or unknown desktop shares an identity")
	}
}

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

func TestViewStreamReadsGeometryBeforeFragmentedANSI(t *testing.T) {
	data, peer := net.Pipe()
	c := &Client{dial: func(context.Context) (net.Conn, error) { return data, nil }}
	go func() {
		defer peer.Close()
		var req Attach
		json.NewDecoder(peer).Decode(&req)
		peer.Write([]byte("{\"ok\":true,\"screen_frames\":true}\n"))
		for _, f := range []struct {
			cols, rows int
			text       string
		}{{100, 32, "\x1b[2;70H中文🙂⣿"}, {70, 20, "\x1b[18;60Hupdated"}} {
			frame := make([]byte, 8+len(f.text))
			binary.BigEndian.PutUint32(frame[:4], uint32(len(f.text)))
			binary.BigEndian.PutUint16(frame[4:6], uint16(f.cols))
			binary.BigEndian.PutUint16(frame[6:8], uint16(f.rows))
			copy(frame[8:], f.text)
			// Fragment inside the header, UTF-8 characters and ANSI sequence.
			for _, b := range frame {
				peer.Write([]byte{b})
			}
		}
	}()
	viewer := c.ViewStream("screen")
	defer viewer.Close()
	if err := viewer.Resize(39, 10); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		cols, rows int
		text       string
	}{{100, 32, "\x1b[2;70H中文🙂⣿"}, {70, 20, "\x1b[18;60Hupdated"}} {
		var text strings.Builder
		buf := make([]byte, 3)
		for text.Len() < len(want.text) {
			n, cols, rows, err := viewer.ReadScreen(buf)
			if err != nil {
				t.Fatal(err)
			}
			if cols != want.cols || rows != want.rows {
				t.Fatalf("geometry arrived after its output: %dx%d, want %dx%d", cols, rows, want.cols, want.rows)
			}
			text.Write(buf[:n])
		}
		if text.String() != want.text {
			t.Fatalf("fragmented payload changed: %q", text.String())
		}
	}
	if !viewer.HasScreenSize() {
		t.Fatal("framing negotiation missing")
	}
}
