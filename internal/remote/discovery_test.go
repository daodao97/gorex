package remote

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoveryRetriesRelayAndStopsWithTunnel(t *testing.T) {
	q := &Quality{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	direct := make(chan struct{})
	q.bindProbe(func(context.Context) (PathSample, error) {
		n := calls.Add(1)
		if n == 1 {
			return PathSample{}, errors.New("temporary discovery failure")
		}
		if n == 3 {
			close(direct)
		}
		return PathSample{At: time.Now(), Direct: n >= 3}, nil
	})
	done := make(chan struct{})
	go func() { defer close(done); discoverPath(ctx, q, time.Millisecond, time.Hour) }()
	select {
	case <-direct:
	case <-time.After(time.Second):
		t.Fatal("did not retry failed and relayed probes")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("discovery survived tunnel close")
	}
	if calls.Load() != 3 || !q.Snapshot(time.Now()).Path.Direct {
		t.Fatal("unexpected discovery result", calls.Load())
	}
}

func TestPathProbeCancellationReleasesWaitingProbes(t *testing.T) {
	lifetime, closeTunnel := context.WithCancel(context.Background())
	defer closeTunnel()
	var calls atomic.Int32
	started := make(chan struct{})
	probe := serializedPathProbe(lifetime, func(ctx context.Context) (PathSample, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return PathSample{}, ctx.Err()
	})
	done := make(chan error, 2)
	go func() { _, err := probe(context.Background()); done <- err }()
	<-started
	go func() { _, err := probe(context.Background()); done <- err }()
	closeTunnel()
	for range 2 {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("probe did not inherit tunnel cancellation", err)
			}
		case <-time.After(time.Second):
			t.Fatal("probe blocked tunnel cleanup")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent probe entered disposed tunnel", calls.Load())
	}
}

func TestPathProbeCallerCancellationDoesNotStopDiscovery(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	probe := serializedPathProbe(lifetime, func(ctx context.Context) (PathSample, error) {
		calls++
		return PathSample{At: time.Now(), Direct: true}, nil
	})
	popup, closePopup := context.WithCancel(context.Background())
	closePopup()
	if _, err := probe(popup); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled popup entered probe", err)
	}
	if p, err := probe(context.Background()); err != nil || !p.Direct || calls != 1 {
		t.Fatal("popup cancellation stopped tunnel discovery", p, err, calls)
	}
}
