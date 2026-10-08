package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"gorex/internal/rex"
	"tailscale.com/tailcfg"
)

func TestPairingLinkPreservesCapability(t *testing.T) {
	key := tailcat.NewPrivateKey()
	key.Public.RegionID = 301
	addr := key.Public.Addr()
	got, err := ParseLink(Link(addr))
	if err != nil || got != addr {
		t.Fatalf("pairing round trip: %v", err)
	}
	if got, err = ParseLink(string(addr)); err != nil || got != addr {
		t.Fatal("raw code rejected")
	}
	for _, bad := range []string{"", "https://example.com", "gorex://connect?v=2&address=" + string(addr), "gorex://connect?v=1&address=broken", "gorex://connect?v=1&address=" + string(addr) + "&address=" + string(addr), strings.Repeat("a", 4097)} {
		if _, err := ParseLink(bad); err == nil {
			t.Fatalf("accepted invalid pairing link")
		}
	}
}

func TestHostnameRelayKeepsAuthentication(t *testing.T) {
	key := tailcat.NewPrivateKey()
	ci, err := tailcat.ParseAddr(key.Public.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ci.Region = []*tailcfg.DERPRegion{{RegionID: 301, Nodes: []*tailcfg.DERPNode{{HostName: "relay.example", IPv4: "192.0.2.1", IPv6: "2001:db8::1"}, {HostName: "v4.example", IPv4: "192.0.2.2", IPv6: "-1"}}}}
	prepared, err := tailcat.ParseAddr(hostnameRelayAddr(ci.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PresharedKey != ci.PresharedKey || prepared.ServerPublic != ci.ServerPublic || prepared.ServerDiscoPublic != ci.ServerDiscoPublic {
		t.Fatal("relay routing changed authentication")
	}
	nodes := prepared.Region[0].Nodes
	if nodes[0].HostName != "relay.example" || nodes[0].IPv4 != "" || nodes[0].IPv6 != "" || nodes[1].IPv6 != "-1" {
		t.Fatal("relay routing did not preserve hostnames and explicit family policy")
	}
}

// This opt-in test also supplies a private desktop fixture for the XCTest run.
// Its isolated server cannot alter sessions in an installed GoRex app.
func TestTailcatSessionLifecycle(t *testing.T) {
	if os.Getenv("GOREX_REMOTE_E2E") != "1" {
		t.Skip("set GOREX_REMOTE_E2E=1 for live Tailcat transport")
	}
	dir := t.TempDir()
	t.Setenv("GOREX_DIR", dir)
	serverDone := make(chan error, 1)
	go func() { serverDone <- rex.Serve() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", rex.SocketPath())
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	desktop, err := rex.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	defer desktop.Shutdown()
	existing, err := desktop.Create(rex.CreateOptions{Dir: dir, Command: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	imagePastes := make(chan string, 1)
	image := clipboardPNG(t)
	bridge, err := Start(ctx, rex.SocketPath(), Options{StateDir: dir, OnPasteImage: func(_ context.Context, sid string, data []byte) error {
		if string(data) != string(image) {
			return fmt.Errorf("image bytes changed in tunnel")
		}
		imagePastes <- sid
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	probeCtx, stopProbe := context.WithTimeout(context.Background(), 12*time.Second)
	err = Probe(probeCtx, bridge.Link())
	stopProbe()
	if err != nil {
		t.Fatalf("live desktop availability probe failed: %v", err)
	}
	if len(bridge.Devices()) != 0 {
		t.Fatal("availability probe registered a connected phone")
	}
	probeSessions, err := desktop.List()
	if err != nil || len(probeSessions) != 1 || probeSessions[0].ID != existing.ID || probeSessions[0].Cols != 80 || probeSessions[0].Rows != 24 {
		t.Fatalf("availability probe altered desktop sessions: %v", err)
	}
	client, closeTunnel, err := Connect(ctx, bridge.Link())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTunnel()
	defer client.Close()
	hello, err := client.HelloFrom(rex.DeviceInfo{Name: "Test iPhone", OS: "iOS"})
	if err != nil || hello.Host.ID == "" {
		t.Fatalf("desktop identity missing: %v", err)
	}
	if devices := bridge.Devices(); len(devices) != 0 {
		t.Fatal("phone marked ready before session list completed")
	}
	list, err := client.List()
	if err != nil || len(list) != 1 || list[0].ID != existing.ID {
		t.Fatalf("existing desktop session not listed: %v", err)
	}
	if err := client.PasteImage(ctx, existing.ID, image); err != nil {
		t.Fatal("image upload over Tailcat failed", err)
	}
	select {
	case sid := <-imagePastes:
		if sid != existing.ID {
			t.Fatal("image targeted wrong session")
		}
	case <-time.After(time.Second):
		t.Fatal("image upload not delivered")
	}
	deadline = time.Now().Add(time.Second)
	for len(bridge.Devices()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if devices := bridge.Devices(); len(devices) != 1 || devices[0].Name != "Test iPhone" {
		t.Fatalf("phone connection information missing: %+v", devices)
	}
	stream := client.Stream(existing.ID, 48, 20)
	defer stream.Close()
	if _, err := stream.Write([]byte("printf '\\nGOREX_PHONE_INPUT_OK\\n'\r")); err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	go func() {
		var output strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			output.Write(buf[:n])
			if strings.Contains(output.String(), "\r\nGOREX_PHONE_INPUT_OK\r\n") || err != nil {
				received <- output.String()
				return
			}
		}
	}()
	select {
	case output := <-received:
		if !strings.Contains(output, "\r\nGOREX_PHONE_INPUT_OK\r\n") {
			t.Fatal("remote input never reached shell")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("remote stream did not replay output")
	}
	created, err := client.Create(rex.CreateOptions{Dir: dir, Cols: 48, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == existing.ID {
		t.Fatal("new session reused old ID")
	}
	list, err = desktop.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("phone-created session not on desktop: %v", err)
	}
	// A mobile viewport must not resize the program in the desktop's PTY.
	viewer := client.ViewStream(existing.ID)
	if err := viewer.Resize(37, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.Read(make([]byte, 256)); err != nil {
		t.Fatal(err)
	}
	if err := viewer.Resize(90, 9); err != nil {
		t.Fatal(err)
	}
	viewer.Close()
	if devices := bridge.Devices(); len(devices) != 1 {
		t.Fatalf("terminal streams duplicated phone information: %+v", devices)
	}
	list, err = desktop.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == existing.ID && (s.Cols != 48 || s.Rows != 20) {
			t.Fatalf("mobile viewer resized desktop: %dx%d", s.Cols, s.Rows)
		}
	}

	if fixture := os.Getenv("GOREX_IOS_FIXTURE"); fixture != "" {
		data, _ := json.Marshal(struct{ Link, Existing, Created, Directory string }{bridge.Link(), existing.ID, created.ID, dir})
		if err := os.WriteFile(fixture, data, 0600); err != nil {
			t.Fatal(err)
		}
		// The phone requests one controlled loss of its control transport.
		// Keep the listener/capability and fixture streams alive for automatic reconnect.
		dropped := false
		// XCTest creates this marker when it has finished driving the phone.
		end := time.Now().Add(20 * time.Minute)
		for time.Now().Before(end) {
			if !dropped {
				if _, err := os.Stat(filepath.Join(dir, "disconnect-phone")); err == nil {
					bridge.mu.Lock()
					var connections []net.Conn
					for conn, device := range bridge.devices {
						if device.Name != "Test iPhone" {
							connections = append(connections, conn)
						}
					}
					bridge.mu.Unlock()
					for _, conn := range connections {
						conn.Close()
					}
					dropped = len(connections) > 0
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(fixture), "ios-finished")); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if !dropped {
			t.Fatal("phone did not exercise transport loss/reconnect")
		}
		list, err = desktop.List()
		if err != nil || len(list) < 3 {
			t.Fatalf("iPhone did not create a desktop session: %v", err)
		}
		for _, s := range list {
			if s.ID == existing.ID && (s.Cols != 48 || s.Rows != 20) {
				t.Fatalf("iPhone keyboard/rotation resized desktop: %dx%d", s.Cols, s.Rows)
			}
		}
		keyboard := make(chan bool, 1)
		go func() {
			var output strings.Builder
			buf := make([]byte, 4096)
			for {
				n, err := stream.Read(buf)
				output.Write(buf[:n])
				if strings.Contains(output.String(), "\r\nGOREX_IPHONE_KEYBOARD_OK\r\n") && strings.Contains(output.String(), "\r\nGOREX_IPHONE_SELECTION_selection word\r\n") && strings.Contains(output.String(), "\r\nGOREX_IPHONE_IME_你好\r\n") && strings.Contains(output.String(), "\r\nGOREX_IPHONE_RECONNECTED_OK\r\n") {
					keyboard <- true
					return
				}
				if err != nil {
					keyboard <- false
					return
				}
			}
		}()
		select {
		case ok := <-keyboard:
			if !ok {
				t.Fatal("iPhone input, backspace, selection or IME did not reach the shell correctly")
			}
		case <-time.After(10 * time.Second):
			stream.Close()
			t.Fatal("iPhone input/backspace output was not observed")
		}
	}
	stream.Close()
	client.Close()
	closeTunnel()
	// Disconnecting the phone leaves both sessions alive on the desktop.
	list, err = desktop.List()
	if err != nil || len(list) < 2 || list[0].Exited {
		t.Fatalf("remote detach ended a session: %v", err)
	}
	reconnectCtx, reconnectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reconnectCancel()
	reconnected, revokeTunnel, err := Connect(reconnectCtx, bridge.Link())
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	defer revokeTunnel()
	if _, err := reconnected.List(); err != nil {
		t.Fatal(err)
	}
	bridge.Close()
	revokedCtx, stopRevoked := context.WithTimeout(context.Background(), 3*time.Second)
	err = Probe(revokedCtx, bridge.Link())
	stopRevoked()
	if err == nil {
		t.Fatal("revoked desktop capability still reported available")
	}
	select {
	case <-reconnected.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("revocation did not close an active remote control connection")
	}
	list, err = desktop.List()
	if err != nil || len(list) < 2 || list[0].Exited {
		t.Fatal("revoking pairing ended desktop sessions")
	}
	// Closing the GUI preserves both the identity and the session server.
	// A saved link must reach the reopened bridge without another QR scan.
	savedLink := bridge.Link()
	restartCtx, stopRestart := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopRestart()
	restarted, err := Start(restartCtx, rex.SocketPath(), Options{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.Link() != savedLink {
		t.Fatal("desktop restart changed the saved connection code")
	}
	phone, closePhone, err := Connect(restartCtx, savedLink)
	if err != nil {
		t.Fatal("saved phone link cannot reconnect after desktop restart")
	}
	list, err = phone.List()
	if err != nil || len(list) < 2 || list[0].Exited {
		t.Fatal("saved phone link did not reach existing sessions")
	}
	phone.Close()
	closePhone()
	restarted.Close()
	if err := ForgetIdentity(restartCtx, dir); err != nil {
		t.Fatal(err)
	}
	fresh, err := Start(restartCtx, rex.SocketPath(), Options{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if fresh.Link() == savedLink {
		t.Fatal("explicit stop reused the old connection code")
	}
	oldCtx, stopOld := context.WithTimeout(context.Background(), 3*time.Second)
	err = Probe(oldCtx, savedLink)
	stopOld()
	if err == nil {
		t.Fatal("old connection code accepted after explicit stop and reenable")
	}
}
