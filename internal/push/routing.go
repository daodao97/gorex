package push

import (
	"errors"
	"strings"
	"time"
)

func (s *service) phoneAvailableLocked() bool {
	return s.provider != nil && len(s.state.Devices) > 0
}

func (s *service) noticeHandledLocked(id string, now time.Time) {
	s.state.DesktopSeen[id] = now
	for key, p := range s.state.Pending {
		if p.ID == id {
			s.removeLocked(key)
		}
	}
	pruneReceipts(s.state.DesktopSeen, now, 1024)
}

// One subscription receives each event. Prefer the phone that most recently
// contacted this sender; APNs does not expose a device's real-time online state.
func (s *service) queuePhoneLocked(n Notice, now time.Time, external bool) {
	chosen := ""
	var newest time.Time
	for id, d := range s.state.Devices {
		if _, seen := d.Receipts[n.ID]; seen {
			return
		}
		at := d.LastSeen
		if at.IsZero() {
			at = d.Updated
		}
		if chosen == "" || at.After(newest) || at.Equal(newest) && id < chosen {
			chosen, newest = id, at
		}
	}
	if chosen == "" {
		return
	}
	for _, p := range s.state.Pending {
		if p.ID == n.ID {
			return
		}
	}
	s.state.Pending[chosen+":"+n.ID] = pendingNotice{ID: n.ID, Device: chosen, Desktop: n.Desktop, Session: n.Session, Kind: n.Kind, Title: n.Title, Body: n.Body, Created: now, Next: now.Add(3 * time.Second), External: external}
}

// Claim makes the channel decision and records it under the same lock used by
// APNs delivery. A later focus change or worker restart cannot replay the event.
func (s *service) claim(n Notice, now time.Time) (Route, error) {
	if !strings.HasPrefix(n.ID, "retty-agent-") || len(n.ID) > 64 || n.Desktop == "" || len(n.Desktop) > 128 || n.Session == "" || len(n.Session) > 128 || len(n.Title) > 512 || len(n.Body) > 8192 || (n.Kind != "waiting" && n.Kind != "completed" && n.Kind != "failed" && n.Kind != "terminal") {
		return RouteQuiet, errors.New("invalid notification event")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.state.DesktopSeen[n.ID]; seen {
		return RouteQuiet, nil
	}
	for _, d := range s.state.Devices {
		if _, seen := d.Receipts[n.ID]; seen {
			return RouteQuiet, nil
		}
	}
	if p, ok := s.state.Pending[s.inflightKey]; ok && p.ID == n.ID {
		// Cancellation cannot recall a request already accepted by APNs.
		return RoutePhone, nil
	}
	route := RouteDesktop
	if n.Viewed || s.desktopViewedLocked(n.Desktop, n.Session, n.Kind, now) {
		route = RouteQuiet
	} else if !s.desktopActiveLocked(now) && s.phoneAvailableLocked() {
		route = RoutePhone
	}
	if route == RouteDesktop && n.Caller != "" && s.desktopActiveLocked(now) && !s.desktops[n.Caller].Present {
		// A present viewer will claim this event. An away viewer must not
		// consume the source desktop's opportunity to notify its user.
		return RouteQuiet, nil
	}
	if route == RoutePhone {
		// Local lifecycle events come from the worker's session observer.
		// A connected remote pane or OSC message is observed only by the GUI.
		if n.Desktop != s.currentDesktop || n.Kind == "terminal" {
			s.queuePhoneLocked(n, now, true)
		}
	} else {
		s.noticeHandledLocked(n.ID, now)
	}
	if err := s.saveLocked(); err != nil {
		return RouteQuiet, err
	}
	return route, nil
}
