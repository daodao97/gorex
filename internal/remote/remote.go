// Package remote carries the existing Rex protocol over Tailcat's encrypted tunnel.
package remote

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"gorex/internal/rex"
)

const Port = 4242

func quiet(string, ...any) {}

// Link keeps the case-sensitive Tailcat capability in a URL query, never a hostname.
func Link(addr tailcat.Addr) string {
	return "gorex://connect?" + url.Values{"v": {"1"}, "address": {string(addr)}}.Encode()
}

func ParseLink(raw string) (tailcat.Addr, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 4096 {
		return "", errors.New("连接码过长")
	}
	addr := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "gorex" || u.Host != "connect" || u.Path != "" || u.Fragment != "" || u.User != nil {
			return "", errors.New("请扫描 GoRex 桌面端的连接二维码")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || q.Get("v") != "1" || len(q["address"]) != 1 {
			return "", errors.New("不支持的连接码版本")
		}
		addr = q.Get("address")
	}
	ci, err := tailcat.ParseAddr(tailcat.Addr(addr))
	if err != nil || ci.PresharedKey.IsZero() {
		return "", errors.New("无效的 GoRex 连接码")
	}
	return tailcat.Addr(addr), nil
}

// Bridge exposes session transport and image paste to a paired phone. Closing it
// disconnects phones without forgetting a persisted identity or ending sessions.
type Bridge struct {
	server       *tailcat.Server
	listener     net.Listener
	mu           sync.Mutex
	closed       bool
	conns        map[net.Conn]bool
	devices      map[net.Conn]ConnectedDevice
	onDevice     func(rex.DeviceInfo)
	onClose      func()
	onImage      func(context.Context, string, []byte) error
	imageGate    chan struct{}
	imageContext context.Context
	imageCancel  context.CancelFunc
	identityLock *os.File
}

// OnClose registers cleanup before publishing this bridge to consumers.
func (b *Bridge) OnClose(fn func()) { b.mu.Lock(); b.onClose = fn; b.mu.Unlock() }

type Options struct {
	// StateDir persists the identity and relay across restarts. Empty uses an
	// ephemeral capability, useful for isolated fixtures.
	StateDir string
	// OnDevice receives metadata only after a successful compatible control
	// response. Callbacks must not block; terminal bytes are forwarded unchanged.
	OnDevice func(rex.DeviceInfo)
	// OnPasteImage runs off the UI thread after a bounded authenticated upload.
	// It must copy to the desktop clipboard before sending Ctrl+V to the session.
	OnPasteImage func(context.Context, string, []byte) error
}

func Start(ctx context.Context, socket string, options ...Options) (*Bridge, error) {
	b := &Bridge{server: &tailcat.Server{Logf: quiet}, conns: make(map[net.Conn]bool), devices: make(map[net.Conn]ConnectedDevice)}
	var stateDir string
	if len(options) > 0 {
		stateDir = options[0].StateDir
	}
	if stateDir != "" {
		identity, lock, err := loadIdentity(ctx, stateDir)
		if err != nil {
			return nil, err
		}
		b.identityLock = lock
		b.server.Key, b.server.PresharedKey, b.server.Region = identity.Key, identity.PresharedKey, identity.Region
	}
	b.imageContext, b.imageCancel = context.WithCancel(context.Background())
	b.imageGate = make(chan struct{}, 1)
	if len(options) > 0 {
		b.onDevice = options[0].OnDevice
		b.onImage = options[0].OnPasteImage
	}
	ln, err := b.server.Listen(ctx, "tcp", fmt.Sprintf(":%d", Port))
	if err != nil {
		b.imageCancel()
		b.server.Close()
		releaseIdentity(b.identityLock)
		return nil, err
	}
	b.listener = ln
	if stateDir != "" {
		if err := saveIdentity(stateDir, b.server); err != nil {
			b.Close()
			return nil, err
		}
	}
	go b.accept(socket)
	return b, nil
}

func (b *Bridge) Link() string { return Link(b.server.TailcatAddr()) }

func (b *Bridge) accept(socket string) {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			conn.Close()
			return
		}
		b.conns[conn] = true
		b.mu.Unlock()
		go func() {
			defer func() { conn.Close(); b.mu.Lock(); delete(b.conns, conn); delete(b.devices, conn); b.mu.Unlock() }()
			b.serveConnection(conn, socket)
		}()
	}
}

func (b *Bridge) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	if b.imageCancel != nil {
		b.imageCancel()
	}
	if b.onClose != nil {
		b.onClose()
	}
	for c := range b.conns {
		c.Close()
	}
	b.mu.Unlock()
	b.listener.Close()
	// Keep the overlay alive long enough to deliver TCP FINs. Closing the
	// WireGuard/DERP engine immediately can leave the peer awaiting EOF.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b.server.DrainTCP(ctx)
	b.server.Close()
	releaseIdentity(b.identityLock)
}

// Connect owns a Tailcat client until closeTunnel is called. Every Rex stream
// shares its tunnel; a separate TCP connection carries each terminal's bytes.
func Connect(ctx context.Context, raw string) (*rex.Client, func(), error) {
	return connect(ctx, raw, true)
}

func connect(ctx context.Context, raw string, reportFailure bool) (*rex.Client, func(), error) {
	addr, err := ParseLink(raw)
	if err != nil {
		return nil, nil, &ConnectionError{Kind: InvalidLink, Cause: err}
	}
	if err := prepareNetwork(ctx, addr); err != nil {
		return nil, nil, err
	}
	if runtime.GOOS == "ios" {
		addr = hostnameRelayAddr(addr)
	}
	logf := quiet
	if os.Getenv("GOREX_DEBUG_TUNNEL") == "1" {
		logf = func(format string, args ...any) {
			message := strings.ReplaceAll(fmt.Sprintf(format, args...), string(addr), "<desktop>")
			if !strings.Contains(message, "NetworkMap:") {
				log.Printf("GoRex tunnel: %s", message)
			}
		}
	}
	tunnel := &tailcat.Client{Server: addr, Logf: logf}
	dial := func(parent context.Context) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		return tunnel.DialTCPPort(ctx, Port)
	}
	client, err := rex.ConnectDial(ctx, dial)
	if err != nil {
		go tunnel.Close()
		failure := &ConnectionError{Kind: Failure(err), Cause: err}
		if reportFailure {
			log.Printf("GoRex tunnel dial failed: %s", failure)
		}
		return nil, nil, failure
	}
	return client, func() {
		// Send the final FIN/ACK before shutting down the userspace TCP stack.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tunnel.DrainTCP(ctx)
		tunnel.Close()
	}, nil
}

// Probe checks whether the saved capability reaches a responding GoRex server.
// It sends only hello: no device registration, session list or terminal attach.
func Probe(ctx context.Context, raw string) error {
	client, closeTunnel, err := connect(ctx, raw, false)
	if err != nil {
		return err
	}
	defer closeTunnel()
	defer client.Close()
	return probeClient(ctx, client)
}

func probeClient(ctx context.Context, client *rex.Client) error {
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	hello, err := client.Hello()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if hello.Version <= 0 || hello.Host.Name == "" {
		return errors.New("invalid GoRex hello")
	}
	return nil
}

// iOS networks may use DNS64 or a domain-based packet tunnel. Dialing the
// relay's baked-in IPs bypasses those routes; keep its TLS hostname and let
// the device resolver choose the reachable address family.
func hostnameRelayAddr(addr tailcat.Addr) tailcat.Addr {
	ci, err := tailcat.ParseAddr(addr)
	if err != nil {
		return addr
	}
	for _, region := range ci.Region {
		for _, node := range region.Nodes {
			if node.HostName != "" {
				if ip, err := netip.ParseAddr(node.IPv4); err == nil && ip.Is4() {
					node.IPv4 = ""
				}
				if ip, err := netip.ParseAddr(node.IPv6); err == nil && ip.Is6() {
					node.IPv6 = ""
				}
			}
		}
	}
	return ci.Addr()
}
