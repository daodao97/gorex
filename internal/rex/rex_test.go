//go:build darwin || linux

package rex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServer(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("RETTY_DIR", dir)
	go Serve()
	var c *Client
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, err = Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	h, err := c.Hello()
	if err != nil || h.Version != ProtocolVersion {
		t.Fatalf("hello %+v %v", h, err)
	}
	t.Logf("host %+v", h.Host)
	info, err := c.Create(CreateOptions{Command: []string{"/bin/sh"}, Dir: "/tmp", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Stream(info.ID, 80, 24)
	s.Resize(100, 30)
	s.Write([]byte("echo he''llo; sleep 1\n"))
	var out bytes.Buffer
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "hello") && time.Now().Before(deadline) {
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("output %q", out.String())
	}
	time.Sleep(200 * time.Millisecond)
	list, err := c.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	in := list[0]
	t.Logf("info %+v", in)
	if in.Program != "sleep" || in.Idle || in.Cols != 100 {
		t.Errorf("info %+v", in)
	}
	if !strings.HasSuffix(in.Dir, "/tmp") {
		t.Errorf("dir %q", in.Dir)
	}
	s.Close()
	// Attaching again replays what it printed.
	s2 := c.Stream(info.ID, 100, 30)
	s2.Resize(100, 30)
	n, _ := s2.Read(buf)
	if !strings.Contains(string(buf[:n]), "hello") {
		t.Errorf("replay %q", buf[:n])
	}
	if err := c.Kill(info.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.List(); len(list) != 0 {
		t.Errorf("sessions left %+v", list)
	}
}
