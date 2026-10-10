package remote

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestQualityWindowAndErrorSamples(t *testing.T) {
	q := &Quality{}
	now := time.Unix(10000, 0)
	q.RecordRequest(now.Add(-QualityWindow-time.Second), time.Hour, nil)
	for n := 1; n <= 20; n++ {
		q.RecordRequest(now, time.Duration(n)*time.Millisecond, nil)
	}
	q.RecordRequest(now, 10*time.Second, context.DeadlineExceeded)
	q.RecordRequest(now, time.Microsecond, io.EOF)
	q.RecoveryStarted(now.Add(-time.Second))
	q.RecoveryFinished(now)
	q.ScreenRecovered(now)
	s := q.Snapshot(now)
	if s.Requests != 22 || s.Successes != 20 || s.Timeouts != 1 || s.Median != 10500*time.Microsecond || s.P95 != 19*time.Millisecond || s.Reconnects != 1 || !s.HasRecovery || s.LastRecovery != time.Second || s.ScreenRecoveries != 1 {
		t.Fatalf("incorrect quality snapshot: %+v", s)
	}
	if expired := q.Snapshot(now.Add(QualityWindow + time.Second)); expired.Requests != 0 || expired.Reconnects != 0 || expired.ScreenRecoveries != 0 {
		t.Fatalf("stale samples retained: %+v", expired)
	}
	for n := 0; n < 1000; n++ {
		q.RecordRequest(now, time.Millisecond, nil)
		q.RecoveryStarted(now)
		q.ScreenRecovered(now)
	}
	if len(q.requests) > qualitySampleLimit || len(q.recoveries) > qualitySampleLimit || len(q.screens) > qualitySampleLimit {
		t.Fatal("quality history is unbounded")
	}
}

func TestQualityProbeRejectsReplacedTunnel(t *testing.T) {
	q := &Quality{}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unbindOld := q.bindProbe(func(context.Context) (PathSample, error) {
		close(started)
		<-release
		return PathSample{At: time.Now(), Latency: time.Hour}, nil
	})
	go func() { defer close(done); q.Probe(context.Background()) }()
	<-started
	unbindNew := q.bindProbe(func(context.Context) (PathSample, error) {
		return PathSample{At: time.Now(), Direct: true, Latency: 5 * time.Millisecond}, nil
	})
	defer unbindNew()
	unbindOld()
	q.Probe(context.Background())
	close(release)
	<-done
	s := q.Snapshot(time.Now())
	if !s.Path.Direct || s.Path.Latency != 5*time.Millisecond {
		t.Fatal("old tunnel changed current measurement", s)
	}
	q.bindProbe(func(context.Context) (PathSample, error) { return PathSample{}, errors.New("fixture") })
	q.Probe(context.Background())
	if !q.Snapshot(time.Now()).Path.At.IsZero() {
		t.Fatal("failed probe showed stale latency")
	}
}
