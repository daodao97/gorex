package remote

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
)

func TestDiagnosticErrorCodesNeverIncludeErrorText(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("secret-token: %w", context.DeadlineExceeded), "deadline_exceeded"},
		{&net.DNSError{Name: "secret-token", Err: "secret-token", IsNotFound: true}, "dns_not_found"},
		{&net.DNSError{Name: "secret-token", Err: "secret-token", IsTimeout: true}, "dns_timeout"},
		{fmt.Errorf("secret-token: %w", syscall.ECONNREFUSED), "connection_refused"},
		{x509.HostnameError{Host: "secret-token"}, "tls_certificate_invalid"},
		{&mygo.NetworkError{Kind: "timeout"}, "system_network_timeout"},
		{&mygo.NetworkError{Kind: "secret-token"}, "system_network_failed"},
		{errors.New("gorex://connect?address=secret-token / session content"), "unclassified_error"},
	} {
		if got := diagnosticCause(tt.err); got != tt.code {
			t.Fatalf("got %q, want %q", got, tt.code)
		}
	}
}

func TestDiagnosticsCaptureStagesAndSafeRelayEvents(t *testing.T) {
	key := tailcat.NewPrivateKey()
	key.Public.Region = []*tailcfg.DERPRegion{{RegionID: 1, Nodes: []*tailcfg.DERPNode{{HostName: "relay.example"}}}}
	trace := NewDiagnostics(false, 1, 30*time.Second)
	trace.target(key.Public.Addr())
	if err := trace.Measure(StageNetwork, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	trace.TransportLog("NetworkMap: %v", key.Public.Addr())
	trace.TransportLog("magicsock: derp-%d connected; connGen=%v", 1, 2)
	trace.TransportLog("magicsock: [%p] derp.Recv(derp-%d): %v", nil, 1, fmt.Errorf("secret-token: %w", syscall.ECONNREFUSED))
	err := trace.Measure(StageTunnel, func() error { return context.DeadlineExceeded })
	if !errors.Is(err, context.DeadlineExceeded) || Failure(err) != ConnectionTimeout {
		t.Fatal("lost timeout identity")
	}
	report := trace.Finish(err)
	if report.Stage != StageTunnel {
		t.Fatal("failed stage missing")
	}
	for _, required := range []string{"准备系统网络：完成", "建立加密隧道：失败", "中继已连接", "connection_refused", "relay.example", "桌面指纹："} {
		if !strings.Contains(report.Text, required) {
			t.Fatalf("missing %s", required)
		}
	}
	for _, secret := range []string{string(key.Public.Addr()), key.Public.ServerPublic.String(), fmt.Sprintf("%x", key.Public.PresharedKey[:]), "secret-token", "NetworkMap"} {
		if strings.Contains(report.Text, secret) {
			t.Fatal("diagnostics leaked transport data")
		}
	}
}

func TestDiagnosticsBoundEventsAndFreezeAfterFinish(t *testing.T) {
	trace := NewDiagnostics(true, 3, 12*time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				trace.TransportLog("magicsock: derp-%d connected; connGen=%v", 1, j)
			}
		}()
	}
	wg.Wait()
	trace.Finish(nil)
	if len(trace.events) != 12 {
		t.Fatal("events not bounded")
	}
	before := strings.Join(trace.events, "\n")
	trace.TransportLog("magicsock: [%p] derp.Recv(derp-%d): %v", nil, 1, context.DeadlineExceeded)
	if strings.Join(trace.events, "\n") != before {
		t.Fatal("cleanup mutated finished report")
	}
	var disabled *Diagnostics
	err := disabled.Measure(StageHello, func() error { return context.DeadlineExceeded })
	var failure *ConnectionError
	if !errors.As(err, &failure) || failure.Stage != StageHello {
		t.Fatal("disabled diagnostic lost stage")
	}
}

func TestConnectDiagnosticsInvalidLinkWithoutNetwork(t *testing.T) {
	trace := NewDiagnostics(false, 1, time.Second)
	client, closeTunnel, err := ConnectWithDiagnostics(context.Background(), "secret-invalid-link", trace)
	if client != nil || closeTunnel != nil || Failure(err) != InvalidLink {
		t.Fatal("invalid link attempted a connection")
	}
	report := trace.Finish(err)
	if report.Stage != StageLink || strings.Contains(report.Text, "secret-invalid-link") {
		t.Fatal("invalid link report is unsafe or missing stage")
	}
}
