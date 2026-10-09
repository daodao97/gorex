package remote

import (
	"bytes"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"retty/internal/rex"
)

type ConnectedDevice struct {
	Name      string
	OS        string
	Connected time.Time
	lastSeen  time.Time
}

const deviceLease = 20 * time.Second

// UI presence expires quickly, but a suspended iPhone can reuse its control
// socket on return. Keep authenticated idle connections bounded separately.
const controlIdleTimeout = 10 * time.Minute

// Devices counts control connections, excluding terminal streams opened by
// the same phone. Removing a control connection also removes its status.
func (b *Bridge) Devices() []ConnectedDevice {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	devices := make([]ConnectedDevice, 0, len(b.devices))
	for _, d := range b.devices {
		if time.Since(d.lastSeen) < deviceLease {
			devices = append(devices, d)
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Connected.Before(devices[j].Connected) })
	return devices
}

// A phone is ready only after compatible hello and session-list responses
// have reached it. Successful polling renews its lease, even if an abrupt
// app exit or network loss prevents TCP from reporting the disconnect.
func (b *Bridge) observe(conn net.Conn) *observedConn {
	var mu sync.Mutex
	pending := make(map[int64]string)
	metadata := make(map[int64]rex.DeviceInfo)
	d := ConnectedDevice{Name: "iPhone"}
	hello, control, terminalStream := false, false, false
	reads := jsonLines{line: func(line []byte) {
		var req rex.Request
		if json.Unmarshal(line, &req) != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if terminalStream || req.Op == "attach" {
			terminalStream = true
			return
		}
		if req.Op == "hello" {
			control = true
			timeout := controlIdleTimeout
			if d.Connected.IsZero() {
				timeout = 30 * time.Second
			}
			// Push receipt updates also use hello. Once authenticated,
			// they must not shorten an established idle connection again.
			conn.SetReadDeadline(time.Now().Add(timeout))
			if req.Device != nil {
				if name := deviceLabel(req.Device.Name); name != "" {
					d.Name = name
				}
				d.OS = deviceLabel(req.Device.OS)
			}
		}
		if control && (req.Op == "hello" || req.Op == "list") && len(pending) < 64 {
			pending[req.ID] = req.Op
			if req.Device != nil {
				metadata[req.ID] = *req.Device
			}
		}
	}}
	writes := jsonLines{line: func(line []byte) {
		var res rex.Response
		if json.Unmarshal(line, &res) != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if terminalStream {
			return
		}
		op := pending[res.ID]
		info := metadata[res.ID]
		delete(pending, res.ID)
		delete(metadata, res.ID)
		if res.Error != "" {
			return
		}
		if op == "hello" {
			var h rex.Hello
			hello = json.Unmarshal(res.Data, &h) == nil && rex.CompatibleProtocol(h.Version)
			if hello && info.Push != nil && b.onDevice != nil {
				b.onDevice(info)
			}
		}
		if op != "list" || !hello {
			return
		}
		var sessions []rex.SessionInfo
		if len(res.Data) > 0 && json.Unmarshal(res.Data, &sessions) != nil {
			return
		}
		if info.Push != nil && b.onDevice != nil {
			b.onDevice(info)
		}
		now := time.Now()
		if d.Connected.IsZero() {
			d.Connected = now
		}
		d.lastSeen = now
		conn.SetReadDeadline(now.Add(controlIdleTimeout))
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.closed {
			b.devices[conn] = d
		}
	}}
	return &observedConn{Conn: conn, reads: reads, writes: writes}
}

func deviceLabel(text string) string {
	runes := []rune(strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)))
	return string(runes[:min(len(runes), 80)])
}

// Observe both directions without altering the forwarded protocol bytes.
type observedConn struct {
	net.Conn
	reads, writes jsonLines
}

func (c *observedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.reads.feed(p[:n])
	return n, err
}

func (c *observedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.writes.feed(p[:n])
	return n, err
}

type jsonLines struct {
	prefix  []byte
	dropped bool
	line    func([]byte)
}

func (j *jsonLines) feed(p []byte) {
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		part := p
		if end >= 0 {
			part = p[:end]
		}
		if len(j.prefix)+len(part) > 1<<20 {
			j.dropped, j.prefix = true, nil
		} else if !j.dropped {
			j.prefix = append(j.prefix, part...)
		}
		if end < 0 {
			return
		}
		if !j.dropped {
			j.line(j.prefix)
		}
		j.prefix, j.dropped = j.prefix[:0], false
		p = p[end+1:]
	}
}
