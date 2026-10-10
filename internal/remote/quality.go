package remote

import (
	"context"
	"slices"
	"sync"
	"time"
)

const QualityWindow = 5 * time.Minute
const qualitySampleLimit = 256

// Quality retains bounded, in-memory measurements. It stores no host addresses,
// credentials, session identifiers, error strings or terminal content.
type Quality struct {
	mu         sync.Mutex
	requests   []qualityRequest
	recoveries []qualityRecovery
	screens    []time.Time
	probe      func(context.Context) (PathSample, error)
	probeEpoch uint64
	path       PathSample
}

type qualityRequest struct {
	at              time.Time
	duration        time.Duration
	timeout, failed bool
}
type qualityRecovery struct {
	at       time.Time
	duration time.Duration
	complete bool
}

type PathSample struct {
	At      time.Time
	Direct  bool
	Latency time.Duration
}

type QualitySnapshot struct {
	Requests, Successes, Timeouts, Reconnects, ScreenRecoveries int
	Median, P95, LastRecovery                                   time.Duration
	HasRecovery                                                 bool
	Path                                                        PathSample
}

func (q *Quality) RecordRequest(at time.Time, duration time.Duration, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requests = append(q.requests, qualityRequest{at: at, duration: duration, timeout: err != nil && Failure(err) == ConnectionTimeout, failed: err != nil})
	if len(q.requests) > qualitySampleLimit {
		q.requests = slices.Clone(q.requests[len(q.requests)-qualitySampleLimit:])
	}
}

func (q *Quality) RecoveryStarted(at time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.recoveries = append(q.recoveries, qualityRecovery{at: at})
	if len(q.recoveries) > qualitySampleLimit {
		q.recoveries = slices.Clone(q.recoveries[len(q.recoveries)-qualitySampleLimit:])
	}
}

func (q *Quality) RecoveryFinished(at time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n := len(q.recoveries); n > 0 && !q.recoveries[n-1].complete {
		q.recoveries[n-1].duration = at.Sub(q.recoveries[n-1].at)
		q.recoveries[n-1].complete = true
	}
}

func (q *Quality) ScreenRecovered(at time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.screens = append(q.screens, at)
	if len(q.screens) > qualitySampleLimit {
		q.screens = slices.Clone(q.screens[len(q.screens)-qualitySampleLimit:])
	}
}

func (q *Quality) Snapshot(now time.Time) QualitySnapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	cutoff := now.Add(-QualityWindow)
	s := QualitySnapshot{Path: q.path}
	var durations []time.Duration
	for _, r := range q.requests {
		if r.at.Before(cutoff) {
			continue
		}
		s.Requests++
		if r.timeout {
			s.Timeouts++
		}
		if !r.failed {
			s.Successes++
			durations = append(durations, r.duration)
		}
	}
	if len(durations) > 0 {
		slices.Sort(durations)
		s.Median = durations[(len(durations)-1)/2]
		if len(durations)%2 == 0 {
			upper := durations[len(durations)/2]
			s.Median += (upper - s.Median) / 2
		}
		s.P95 = durations[(95*len(durations)+99)/100-1]
	}
	for _, r := range q.recoveries {
		if r.at.Before(cutoff) {
			continue
		}
		s.Reconnects++
		if r.complete {
			s.LastRecovery, s.HasRecovery = r.duration, true
		}
	}
	for _, at := range q.screens {
		if !at.Before(cutoff) {
			s.ScreenRecoveries++
		}
	}
	return s
}

func (q *Quality) bindProbe(probe func(context.Context) (PathSample, error)) func() {
	q.mu.Lock()
	q.probeEpoch++
	epoch := q.probeEpoch
	q.probe, q.path = probe, PathSample{}
	q.mu.Unlock()
	return func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.probeEpoch == epoch {
			q.probeEpoch++
			q.probe = nil
		}
	}
}

// Probe uses the existing tunnel, never a terminal attachment. A late result
// from a disposed tunnel cannot overwrite a new connection's measurement.
func (q *Quality) Probe(ctx context.Context) error {
	q.mu.Lock()
	probe, epoch := q.probe, q.probeEpoch
	q.mu.Unlock()
	if probe == nil {
		return nil
	}
	p, err := probe(ctx)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.probeEpoch == epoch {
		q.path = PathSample{}
		if err == nil {
			q.path = p
		}
	}
	return err
}
