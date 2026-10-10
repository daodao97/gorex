package remote

import (
	"context"
	"time"
)

// Discovery belongs to the tunnel, not the short-lived dialing context or a
// terminal attachment. Network changes can make a previously relayed path direct.
func startPathDiscovery(q *Quality) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go discoverPath(ctx, q, 2*time.Second, 30*time.Second)
	return cancel
}

func discoverPath(ctx context.Context, q *Quality, warm, steady time.Duration) {
	for attempt := 0; ctx.Err() == nil; attempt++ {
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := q.Probe(probe)
		cancel()
		delay := steady
		if attempt < 4 && (err != nil || !q.Snapshot(time.Now()).Path.Direct) {
			delay = warm << attempt
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// The popover and background discovery share one ping at a time. Waiting and
// in-flight probes both stop when the owning tunnel is disposed.
func serializedPathProbe(lifetime context.Context, probe func(context.Context) (PathSample, error)) func(context.Context) (PathSample, error) {
	gate := make(chan struct{}, 1)
	return func(ctx context.Context) (PathSample, error) {
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime, cancel)
		defer stop()
		defer cancel()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return PathSample{}, ctx.Err()
		}
		if lifetime.Err() != nil {
			return PathSample{}, lifetime.Err()
		}
		if ctx.Err() != nil {
			return PathSample{}, ctx.Err()
		}
		return probe(ctx)
	}
}
