package remote

import (
	"net"
	"testing"
	"time"
)

type peerAddressConn struct {
	net.Conn
	address *net.TCPAddr
}

func (c *peerAddressConn) RemoteAddr() net.Addr { return c.address }

func TestDisconnectDeviceClosesItsControlAndTerminalStreams(t *testing.T) {
	makeConn := func(ip string, port int) (*peerAddressConn, net.Conn) {
		server, client := net.Pipe()
		t.Cleanup(func() { server.Close(); client.Close() })
		return &peerAddressConn{Conn: server, address: &net.TCPAddr{IP: net.ParseIP(ip), Port: port}}, client
	}
	control, controlPeer := makeConn("fd00::1", 1000)
	terminal, terminalPeer := makeConn("fd00::1", 1001)
	other, otherPeer := makeConn("fd00::2", 1000)
	id := deviceConnectionID(control)
	b := &Bridge{conns: map[net.Conn]bool{control: true, terminal: true, other: true}, devices: map[net.Conn]ConnectedDevice{
		control: {ID: id, Name: "iPhone", lastSeen: time.Now()},
		other:   {ID: deviceConnectionID(other), Name: "Mac", lastSeen: time.Now()},
	}}
	if b.DisconnectDevice("unknown") || !b.DisconnectDevice(id) || !b.DeviceDisconnected(id) {
		t.Fatal("disconnect did not target a known device")
	}
	for _, peer := range []net.Conn{controlPeer, terminalPeer} {
		peer.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := peer.Read(make([]byte, 1)); err == nil {
			t.Fatal("target device stream remained connected")
		} else if e, ok := err.(net.Error); ok && e.Timeout() {
			t.Fatal("target device stream was not closed")
		}
	}
	otherPeer.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if _, err := otherPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("unexpected bytes on other device")
	} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatal("disconnect affected another device", err)
	}
	devices := b.Devices()
	if len(devices) != 1 || devices[0].Name != "Mac" {
		t.Fatal("device presence was not removed independently")
	}
	b.AllowDevice(id)
	if b.DeviceDisconnected(id) {
		t.Fatal("device could not be allowed again")
	}
}

func TestPeerHistoryCallbackRequiresReadyControlAndExcludesStreams(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	var records []ConnectedDevice
	b := &Bridge{devices: make(map[net.Conn]ConnectedDevice), onPeer: func(d ConnectedDevice) { records = append(records, d) }}
	conn := b.observe(server)
	conn.reads.feed([]byte("{\"id\":1,\"op\":\"hello\",\"device\":{\"name\":\"My phone\",\"os\":\"iOS\"}}\n"))
	conn.writes.feed([]byte("{\"id\":1,\"data\":{\"version\":4}}\n"))
	if len(records) != 0 {
		t.Fatal("history stored before a complete handshake")
	}
	conn.reads.feed([]byte("{\"id\":2,\"op\":\"list\"}\n"))
	conn.writes.feed([]byte("{\"id\":2,\"data\":[]}\n"))
	conn.reads.feed([]byte("{\"id\":3,\"op\":\"list\"}\n"))
	conn.writes.feed([]byte("{\"id\":3,\"data\":[]}\n"))
	if len(records) != 1 || records[0].ID == "" || records[0].Name != "My phone" || records[0].Connected.IsZero() {
		t.Fatal("ready device missing or heartbeat duplicated history", records)
	}
	stream := b.observe(server)
	stream.reads.feed([]byte("{\"id\":4,\"op\":\"attach\"}\n"))
	stream.writes.feed([]byte("{\"id\":4,\"data\":{}}\n"))
	if len(records) != 1 {
		t.Fatal("terminal stream duplicated device history")
	}
}
