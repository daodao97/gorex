package main

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/remote"
)

type desktopPresenceState uint8

const (
	desktopChecking desktopPresenceState = iota
	desktopOnline
	desktopUnavailable
)

func (s desktopPresenceState) label() string {
	switch s {
	case desktopOnline:
		return "在线"
	case desktopUnavailable:
		return "不可达"
	default:
		return "检查中"
	}
}

type desktopPresence struct {
	state   desktopPresenceState
	checked time.Time
}

const recentPresenceInterval = 30 * time.Second

func (m *mobileApp) stopRecentPresence() {
	if m.presenceCancel != nil {
		m.presenceCancel()
		m.presenceCancel = nil
	}
	m.presenceEpoch++ // Late results cannot overwrite a new connection or history.
}

// Probes run only while the foreground connection page is visible. Keep cached
// results during refresh, and use at most two short-lived tunnels at a time.
func (m *mobileApp) refreshRecentPresence(c *ui.Context) {
	if m.win == nil || m.background || m.client != nil || m.busy || m.scanning || len(m.history) == 0 || m.presenceCancel != nil {
		return
	}
	if m.presence == nil {
		m.presence = make(map[string]desktopPresence)
	}
	wait := recentPresenceInterval
	var links []string
	for _, entry := range m.history {
		result := m.presence[entry.Link]
		remaining := recentPresenceInterval - c.Now().Sub(result.checked)
		if result.checked.IsZero() || remaining <= 0 {
			links = append(links, entry.Link)
		} else {
			wait = min(wait, remaining)
		}
	}
	if len(links) == 0 {
		c.After(wait)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.presenceCancel = cancel
	m.presenceEpoch++
	epoch := m.presenceEpoch
	go func() {
		defer cancel()
		var workers sync.WaitGroup
		limit := make(chan struct{}, 2)
		for _, link := range links {
			workers.Go(func() {
				select {
				case limit <- struct{}{}:
					defer func() { <-limit }()
				case <-ctx.Done():
					return
				}
				probeCtx, stop := context.WithTimeout(ctx, 12*time.Second)
				err := remote.Probe(probeCtx, link)
				stop()
				if ctx.Err() != nil {
					return
				}
				result := desktopPresence{state: desktopOnline, checked: time.Now()}
				if err != nil {
					result.state = desktopUnavailable
				}
				mygo.RunOnMain(func() {
					if m.presenceEpoch == epoch {
						m.presence[link] = result
						m.invalidate()
					}
				})
			})
		}
		workers.Wait()
		mygo.RunOnMain(func() {
			if m.presenceEpoch == epoch {
				m.presenceCancel = nil
				m.invalidate()
			}
		})
	}()
}
