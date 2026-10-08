package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"gorex/internal/rex"
)

func TestPushMetadataRequiresSuccessfulCompatibleControlResponse(t *testing.T) {
	for _, version := range []int{4, rex.ProtocolVersion, rex.ProtocolVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			server, peer := net.Pipe()
			defer server.Close()
			defer peer.Close()
			var received []rex.DeviceInfo
			b := &Bridge{devices: make(map[net.Conn]ConnectedDevice), onDevice: func(d rex.DeviceInfo) { received = append(received, d) }}
			conn := b.observe(server)
			registration := &rex.PushRegistration{ID: strings.Repeat("a", 32), Token: strings.Repeat("ab", 32)}
			send := func(id int64, op string) {
				payload, _ := json.Marshal(rex.Request{ID: id, Op: op, Device: &rex.DeviceInfo{Name: "iPhone", Push: registration}})
				conn.reads.feed(append(payload, '\n'))
			}
			send(1, "hello")
			if len(received) != 0 {
				t.Fatal("registered before successful handshake")
			}
			conn.writes.feed([]byte(fmt.Sprintf("{\"id\":1,\"data\":{\"version\":%d}}\n", version)))
			want := 0
			if rex.CompatibleProtocol(version) {
				want = 1
			}
			if len(received) != want {
				t.Fatal("incompatible hello registered a push device")
			}
			send(2, "list")
			conn.writes.feed([]byte("{\"id\":2,\"error\":\"offline\"}\n"))
			if len(received) != want {
				t.Fatal("failed list registered device")
			}
			send(3, "list")
			conn.writes.feed([]byte("{\"id\":3,\"data\":[]}\n"))
			if want > 0 {
				want++
			}
			if len(received) != want {
				t.Fatal("heartbeat metadata not delivered")
			}
			if want > 0 && received[want-1].Push.Token != registration.Token {
				t.Fatal("registration metadata altered")
			}
		})
	}
}

func TestDeviceObservationRequiresCompletedHandshake(t *testing.T) {
	for _, tc := range []struct {
		name      string
		version   int
		listError string
		want      int
	}{
		{"ready", rex.ProtocolVersion, "", 1},
		{"incompatible", rex.ProtocolVersion + 1, "", 0},
		{"list failed", rex.ProtocolVersion, "server unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			b := &Bridge{devices: make(map[net.Conn]ConnectedDevice)}
			observed := b.observe(server)
			forward := func(line string, request bool) {
				t.Helper()
				var src io.Reader = client
				var dst io.Writer = observed
				if request {
					src, dst = observed, client
				}
				done := make(chan error, 1)
				go func() {
					for part := line; len(part) > 0; {
						n := min(3, len(part))
						if _, err := io.WriteString(dst, part[:n]); err != nil {
							done <- err
							return
						}
						part = part[n:]
					}
					done <- nil
				}()
				got := make([]byte, len(line))
				if _, err := io.ReadFull(src, got); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if string(got) != line {
					t.Fatal("observation altered protocol bytes")
				}
			}
			forward(`{"id":1,"op":"hello","device":{"name":"我的 iPhone","os":"iOS 26.6"}}`+"\n", true)
			if len(b.Devices()) != 0 {
				t.Fatal("request alone marked phone connected")
			}
			forward(fmt.Sprintf("{\"id\":1,\"data\":{\"version\":%d}}\n", tc.version), false)
			if len(b.Devices()) != 0 {
				t.Fatal("hello alone marked phone ready")
			}
			forward("{\"id\":2,\"op\":\"list\"}\n", true)
			forward(fmt.Sprintf("{\"id\":2,\"error\":%q,\"data\":[]}\n", tc.listError), false)
			devices := b.Devices()
			if len(devices) != tc.want {
				t.Fatalf("connected devices: %+v", devices)
			}
			if tc.want == 0 {
				return
			}
			if devices[0].Name != "我的 iPhone" || devices[0].OS != "iOS 26.6" {
				t.Fatal("phone information lost")
			}
			d := b.devices[server]
			d.lastSeen = time.Now().Add(-deviceLease - time.Second)
			b.devices[server] = d
			if len(b.Devices()) != 0 {
				t.Fatal("lost phone remained connected without a heartbeat")
			}
			forward("{\"id\":3,\"op\":\"list\"}\n", true)
			forward("{\"id\":3,\"data\":[]}\n", false)
			if len(b.Devices()) != 1 {
				t.Fatal("successful poll did not renew connection")
			}
			b.closed = true
			if len(b.Devices()) != 0 {
				t.Fatal("revoked bridge remained connected")
			}
		})
	}
}

func TestTerminalStreamDoesNotCountAsConnectedPhone(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	b := &Bridge{devices: make(map[net.Conn]ConnectedDevice)}
	observed := b.observe(server)
	line := "{\"op\":\"attach\",\"sid\":\"fixture\"}\n\x1b[2J中文🙂"
	go func() { defer client.Close(); io.WriteString(client, line) }()
	var got strings.Builder
	buf := make([]byte, 3)
	for {
		n, err := observed.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if got.String() != line || len(b.Devices()) != 0 {
		t.Fatal("terminal bytes changed or stream counted as phone")
	}
}
