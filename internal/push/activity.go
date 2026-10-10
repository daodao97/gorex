package push

import (
	"errors"
	"retty/internal/rex"
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
		active = active || lease.Active && lease.RoutingVersion == 0 || lease.RoutingVersion > 0 && lease.Present
	}
	return active
}

func (s *service) desktopViewedLocked(desktop, session, kind string, now time.Time) bool {
	s.desktopActiveLocked(now) // Expire crashed windows before consulting them.
	for _, lease := range s.desktops {
		if lease.RoutingVersion == 0 {
			if lease.Active {
				return true
			}
			continue
		}
		if lease.Present && (kind == "waiting" && lease.HideWaiting || (kind == "completed" || kind == "failed") && lease.HideCompletion) {
			return true
		}
		if lease.Present && lease.ViewedDesktop == desktop && lease.ViewedSession == session {
			return true
		}
	}
	return false
}

func (s *service) desktopActivity(activity DesktopActivity, now time.Time) error {
	if activity.ID == "" || len(activity.ID) > 128 || activity.Sequence == 0 || len(activity.ViewedDesktop) > 128 || len(activity.ViewedSession) > 128 {
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
	s.markDesktopSeenLocked(now)
	return nil
}

// Only the pane being viewed is silent. Other events wait for the GUI to claim
// desktop delivery, rather than being consumed merely because Retty is open.
func (s *service) markDesktopSeenLocked(now time.Time) {
	changed := false
	for _, ss := range s.current {
		if s.currentDesktop == "" || !rex.AgentNoticeState(ss) || !s.desktopViewedLocked(s.currentDesktop, ss.ID, ss.Agent.State, now) {
			continue
		}
		id := s.noticeID(s.currentDesktop, ss)
		if _, seen := s.state.DesktopSeen[id]; !seen {
			s.state.DesktopSeen[id] = now
			changed = true
		}
	}
	for key, pending := range s.state.Pending {
		if !s.desktopViewedLocked(pending.Desktop, pending.Session, pending.Kind, now) {
			continue
		}
		s.state.DesktopSeen[pending.ID] = now
		s.removeLocked(key)
		changed = true
	}
	pruneReceipts(s.state.DesktopSeen, now, 1024)
	if changed && s.saveLocked() != nil {
		s.lastError = "无法保存通知状态"
	}
}
