package push

import (
	"errors"
	"gorex/internal/rex"
	"time"
)

const desktopLeaseLifetime = 5 * time.Second
const receiptLifetime = 7 * 24 * time.Hour

type desktopLease struct {
	DesktopActivity
	Expires time.Time
}

func pruneReceipts(receipts map[string]time.Time, now time.Time, limit int) {
	for id, at := range receipts {
		if now.Sub(at) > receiptLifetime {
			delete(receipts, id)
		}
	}
	for len(receipts) > limit {
		oldest := ""
		var at time.Time
		for id, t := range receipts {
			if oldest == "" || t.Before(at) {
				oldest, at = id, t
			}
		}
		delete(receipts, oldest)
	}
}

func (s *service) desktopActiveLocked(now time.Time) bool {
	active := false
	for id, lease := range s.desktops {
		if !now.Before(lease.Expires) {
			delete(s.desktops, id)
			continue
		}
		active = active || lease.Active
	}
	return active
}

func (s *service) desktopActivity(activity DesktopActivity, now time.Time) error {
	if activity.ID == "" || len(activity.ID) > 128 || activity.Sequence == 0 {
		return errors.New("invalid desktop activity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, exists := s.desktops[activity.ID]; exists && old.Sequence >= activity.Sequence {
		return nil
	}
	if len(s.desktops) >= 32 {
		s.desktopActiveLocked(now)
		if _, exists := s.desktops[activity.ID]; !exists && len(s.desktops) >= 32 {
			return errors.New("desktop activity limit reached")
		}
	}
	s.desktops[activity.ID] = desktopLease{DesktopActivity: activity, Expires: now.Add(desktopLeaseLifetime)}
	if activity.Active {
		s.markDesktopSeenLocked(now)
	}
	return nil
}

// Foreground GoRex already handles task reminders on the desktop. Persist these
// IDs so switching away or restarting the worker cannot replay them on a phone.
func (s *service) markDesktopSeenLocked(now time.Time) {
	changed := false
	for _, ss := range s.current {
		if s.currentDesktop == "" || !rex.AgentNoticeState(ss) {
			continue
		}
		id := rex.AgentNoticeID(s.currentDesktop, ss)
		if _, seen := s.state.DesktopSeen[id]; !seen {
			s.state.DesktopSeen[id] = now
			changed = true
		}
	}
	for key, pending := range s.state.Pending {
		s.state.DesktopSeen[pending.ID] = now
		s.removeLocked(key)
		changed = true
	}
	pruneReceipts(s.state.DesktopSeen, now, 1024)
	if changed && s.saveLocked() != nil {
		s.lastError = "无法保存通知状态"
	}
}
