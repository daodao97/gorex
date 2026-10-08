package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestConnectionFailuresPreserveCauseWithoutExposingCapability(t *testing.T) {
	cases := []struct {
		err  error
		kind FailureKind
	}{
		{context.DeadlineExceeded, ConnectionTimeout},
		{&net.OpError{Err: syscall.ENETUNREACH}, NetworkUnavailable},
		{&net.DNSError{Err: "fixture DNS failure"}, RelayUnavailable},
		{errors.New("private connection capability"), DesktopUnavailable},
		{&ConnectionError{Kind: ProtocolMismatch, Cause: context.DeadlineExceeded}, ProtocolMismatch},
	}
	for _, c := range cases {
		if got := Failure(fmt.Errorf("wrapper: %w", c.err)); got != c.kind {
			t.Fatalf("got %d, want %d", got, c.kind)
		}
		failure := &ConnectionError{Kind: c.kind, Cause: c.err}
		if !errors.Is(failure, c.err) || strings.Contains(failure.Error(), "private") {
			t.Fatal("unsafe or lost cause", failure)
		}
	}
	_, _, err := Connect(context.Background(), "private connection capability")
	if Failure(err) != InvalidLink || strings.Contains(err.Error(), "private") {
		t.Fatal("invalid capability exposed", err)
	}
}
