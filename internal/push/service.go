package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/push/apns"
	"os"
	"path/filepath"
	"retty/internal/agents"
	"retty/internal/rex"
	"strings"
	"sync"
	"time"
)

type sender interface {
	Send(context.Context, apns.Notification) (apns.Response, error)
}
type device struct {
	Token         string    `json:"token"`
	Updated       time.Time `json:"updated"`
	LastSeen      time.Time `json:"lastSeen,omitzero"`
	lastSeenSaved time.Time
	Receipts      map[string]time.Time `json:"receipts,omitempty"`
}
type pendingNotice struct {
	ID, Device, Desktop, Session, Kind string
	Title, Body                        string
	Created, Next                      time.Time
	Attempts                           int
	External                           bool
}
type savedSession struct {
	ID        string
	Agent     rex.AgentState
	LastInput time.Time
}

func (s savedSession) info() rex.SessionInfo {
	return rex.SessionInfo{ID: s.ID, Agent: s.Agent, LastInput: s.LastInput}
}

type database struct {
	Devices  map[string]device        `json:"devices"`
	Previous map[string]savedSession  `json:"previous"`
	Pending  map[string]pendingNotice `json:"pending"`
	Sent     uint64                   `json:"sent"`
	LastSent time.Time                `json:"lastSent,omitzero"`
	// Legacy JSON name retained: these are shared viewed/delivered receipts,
	// including successful phone delivery, rather than desktop focus alone.
	DesktopSeen map[string]time.Time `json:"desktopSeen,omitempty"`
}

type service struct {
	mu                   sync.Mutex
	dir                  string
	state                database
	provider             sender
	lastError            string
	managed              *apns.Provider
	current              map[string]rex.SessionInfo
	initialized          bool
	legacy               bool
	inflightKey          string
	inflightCancel       context.CancelFunc
	desktops             map[string]desktopLease
	currentDesktop       string
	programNoticeLimiter rex.ProgramNoticeLimiter
}

func newService(dir string) (*service, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &service{dir: dir, state: database{Devices: map[string]device{}, Previous: map[string]savedSession{}, Pending: map[string]pendingNotice{}}, current: map[string]rex.SessionInfo{}}
	data, err := os.ReadFile(filepath.Join(dir, "push-devices.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if len(data) > 2<<20 || json.Unmarshal(data, &s.state) != nil {
			return nil, errors.New("invalid push database")
		}
		if s.state.Devices == nil {
			s.state.Devices = map[string]device{}
		}
		if s.state.Previous == nil {
			s.state.Previous = map[string]savedSession{}
		}
		if s.state.Pending == nil {
			s.state.Pending = map[string]pendingNotice{}
		}
	}
	s.desktops = map[string]desktopLease{}
	for id, d := range s.state.Devices {
		d.lastSeenSaved = d.LastSeen
		s.state.Devices[id] = d
	}
	if s.state.DesktopSeen == nil {
		s.state.DesktopSeen = map[string]time.Time{}
	}
	return s, nil
}
func (s *service) saveLocked() error {
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".push-state-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.dir, "push-devices.json"))
}
func (s *service) register(r Registration) error {
	if err := r.Validate(); err != nil {
		return err
	}
	r.Token = strings.ToLower(r.Token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Disabled {
		_, changed := s.state.Devices[r.ID]
		delete(s.state.Devices, r.ID)
		for key, p := range s.state.Pending {
			if p.Device == r.ID {
				s.removeLocked(key)
				changed = true
			}
		}
		if changed {
			return s.saveLocked()
		}
		return nil
	}
	if len(s.state.Devices) >= 32 {
		if _, ok := s.state.Devices[r.ID]; !ok {
			matchingToken := false
			for _, existing := range s.state.Devices {
				matchingToken = matchingToken || existing.Token == r.Token
			}
			if !matchingToken {
				return errors.New("notification device limit reached")
			}
		}
	}
	now := time.Now()
	d := s.state.Devices[r.ID]
	changed := false
	if now.Sub(d.lastSeenSaved) >= time.Minute {
		changed = true
		d.lastSeenSaved = now
	}
	d.LastSeen = now
	// Reinstallation can change the installation ID while APNs keeps the token.
	// A physical device must have only one delivery subscription.
	for otherID, other := range s.state.Devices {
		if otherID != r.ID && other.Token == r.Token {
			if d.Receipts == nil {
				d.Receipts = map[string]time.Time{}
			}
			for id, at := range other.Receipts {
				d.Receipts[id] = at
			}
			delete(s.state.Devices, otherID)
			for key, p := range s.state.Pending {
				if p.Device == otherID {
					s.removeLocked(key)
					p.Device = r.ID
					if _, acknowledged := d.Receipts[p.ID]; !acknowledged {
						s.state.Pending[r.ID+":"+p.ID] = p
					}
				}
			}
			changed = true
		}
	}
	if d.Token != r.Token {
		d.Token = r.Token
		d.Updated = now
		changed = true
	}
	if d.Receipts == nil {
		d.Receipts = map[string]time.Time{}
	}
	for id, at := range d.Receipts {
		if now.Sub(at) > receiptLifetime {
			delete(d.Receipts, id)
			changed = true
		}
	}
	for _, id := range r.Receipts {
		if _, seen := d.Receipts[id]; !seen {
			d.Receipts[id] = now
			changed = true
		}
		if _, pending := s.state.Pending[r.ID+":"+id]; pending {
			changed = true
		}
		s.removeLocked(r.ID + ":" + id)
		// Seeing an event on any phone also prevents a desktop or another
		// phone from announcing it after reconnecting.
		if _, exists := s.state.DesktopSeen[id]; !exists {
			s.noticeHandledLocked(id, now)
			changed = true
		}
	}
	if len(d.Receipts) > 256 {
		// Preserve newest receipts. They are acknowledgements, never credentials.
		for len(d.Receipts) > 256 {
			oldest := ""
			var at time.Time
			for id, t := range d.Receipts {
				if oldest == "" || t.Before(at) {
					oldest, at = id, t
				}
			}
			delete(d.Receipts, oldest)
		}
	}
	s.state.Devices[r.ID] = d
	if changed {
		return s.saveLocked()
	}
	return nil
}
func (s *service) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Configured: s.provider != nil, Devices: len(s.state.Devices), Sent: s.state.Sent, LastSent: s.state.LastSent, LastError: s.lastError, DesktopActive: s.desktopActiveLocked(time.Now()), RoutingVersion: 1}
}
func (s *service) removeLocked(key string) {
	delete(s.state.Pending, key)
	if key == s.inflightKey && s.inflightCancel != nil {
		s.inflightCancel()
	}
}
func (s *service) observe(hello rex.Hello, sessions []rex.SessionInfo, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	desktop := hello.Host.ID
	if desktop == "" {
		return
	} // never persist a Tailcat access URL as identity
	next := make(map[string]rex.SessionInfo, len(sessions))
	saved := make(map[string]savedSession, len(sessions))
	changed := false
	s.currentDesktop = desktop
	s.legacy = hello.Version < 5
	pruneReceipts(s.state.DesktopSeen, now, 1024)
	for _, ss := range sessions {
		previous, seen := s.state.Previous[ss.ID]
		if s.legacy {
			// Existing databases stored raw version 4 snapshots. Seed their
			// completion counter without announcing a historical result.
			if seen && previous.Agent.CompletionRevision == 0 {
				previous.Agent = rex.LegacyCompletionState(rex.SessionInfo{}, previous.info())
			}
			ss.Agent = rex.LegacyCompletionState(previous.info(), ss)
		}
		next[ss.ID] = ss
		saved[ss.ID] = savedSession{ID: ss.ID, Agent: ss.Agent, LastInput: ss.LastInput}
		id := s.noticeID(desktop, ss)
		if s.desktopViewedLocked(desktop, ss.ID, ss.Agent.State, now) && rex.AgentNoticeState(ss) {
			if _, exists := s.state.DesktopSeen[id]; !exists {
				s.state.DesktopSeen[id] = now
				changed = true
			}
		}
		if _, viewed := s.state.DesktopSeen[id]; viewed {
			continue
		}
		if !seen || !rex.AgentNoticeTransition(previous.info(), ss) || ss.Agent.Updated.IsZero() || now.Sub(ss.Agent.Updated) > 15*time.Minute {
			continue
		}
		if !s.programNoticeLimiter.Allow(desktop+":"+ss.ID, ss.Agent, now) {
			continue
		}
		title := ss.Agent.ID + " · " + stateLabel(ss.Agent.State)
		if agent, ok := agents.Lookup(ss.Agent.ID); ok {
			title = agent.Name + " · " + stateLabel(ss.Agent.State)
		}
		s.queuePhoneLocked(Notice{ID: id, Desktop: desktop, Session: ss.ID, Kind: ss.Agent.State, Title: title, Body: rex.AgentNoticeBody(s.dir, ss)}, now, false)
		changed = true
	}
	s.current = next
	s.initialized = true
	for key, p := range s.state.Pending {
		if p.External {
			if _, handled := s.state.DesktopSeen[p.ID]; handled || now.Sub(p.Created) > 15*time.Minute {
				s.removeLocked(key)
				changed = true
			}
			continue
		}
		ss, exists := next[p.Session]
		_, viewed := s.state.DesktopSeen[p.ID]
		if viewed || !exists || now.Sub(p.Created) > 15*time.Minute || s.noticeID(p.Desktop, ss) != p.ID || !rex.AgentNoticeState(ss) {
			s.removeLocked(key)
			changed = true
		} else if body := rex.AgentNoticeBody(s.dir, ss); p.Body != body {
			// A prompt excerpt can arrive just after its lifecycle hook, or an
			// outbox entry may have been created by an older app version.
			p.Body = body
			s.state.Pending[key] = p
			changed = true
		}
	}
	for id, ss := range saved {
		prev, ok := s.state.Previous[id]
		if !ok || !prev.Agent.Equal(ss.Agent) || !prev.LastInput.Equal(ss.LastInput) {
			changed = true
			break
		}
	}
	if len(saved) != len(s.state.Previous) {
		changed = true
	}
	s.state.Previous = saved
	if changed {
		if s.saveLocked() != nil {
			s.lastError = "无法保存通知状态"
		}
	}
}

func (s *service) noticeID(desktop string, ss rex.SessionInfo) string {
	if s.legacy {
		// Local counters depend on when the observer subscribed. Use the
		// completion timestamp shared with mobile foreground receipts.
		ss.Agent.CompletionRevision = 0
	}
	return rex.AgentNoticeID(desktop, ss)
}
func stateLabel(state string) string {
	switch state {
	case "completed":
		return "已完成"
	case "failed":
		return "执行失败"
	case "waiting":
		return "等待确认"
	}
	return "任务提醒"
}
func (s *service) deliver(ctx context.Context, now time.Time) {
	s.mu.Lock()
	if !s.initialized || s.provider == nil {
		s.mu.Unlock()
		return
	}
	if s.desktopActiveLocked(now) {
		s.markDesktopSeenLocked(now)
		s.mu.Unlock()
		return
	}
	if s.inflightKey != "" {
		s.mu.Unlock()
		return
	}
	var chosen pendingNotice
	key := ""
	for k, p := range s.state.Pending {
		if !now.Before(p.Next) && (key == "" || p.Created.Before(chosen.Created)) {
			key, chosen = k, p
		}
	}
	if key == "" {
		s.mu.Unlock()
		return
	}
	d, exists := s.state.Devices[chosen.Device]
	provider := s.provider
	if !exists {
		s.removeLocked(key)
		s.mu.Unlock()
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	s.inflightKey, s.inflightCancel = key, cancel
	s.mu.Unlock()
	_, err := provider.Send(sendCtx, apns.Notification{DeviceToken: d.Token, CollapseID: chosen.ID, Expiration: chosen.Created.Add(15 * time.Minute), Payload: apns.Payload{ID: chosen.ID, Title: chosen.Title, Body: chosen.Body, Group: "retty-agents", Data: map[string]string{"desktop": chosen.Desktop, "session": chosen.Session, "event": chosen.ID, "title": chosen.Title, "body": chosen.Body, "state": chosen.Kind}}})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflightKey = ""
	s.inflightCancel = nil
	current, exists := s.state.Pending[key]
	if !exists {
		return
	} // a receipt or new task cancelled it while in flight
	if err == nil {
		s.noticeHandledLocked(chosen.ID, now)
		fresh := s.state.Devices[chosen.Device]
		if fresh.Receipts == nil {
			fresh.Receipts = map[string]time.Time{}
		}
		fresh.Receipts[chosen.ID] = now
		pruneReceipts(fresh.Receipts, now, 256)
		s.state.Devices[chosen.Device] = fresh
		s.state.Sent++
		s.state.LastSent = time.Now()
		s.lastError = ""
	} else {
		var response *apns.Error
		if errors.As(err, &response) && response.Unregistered() {
			// A late response must not remove a freshly rotated token.
			fresh := s.state.Devices[chosen.Device]
			if fresh.Token == d.Token && (response.Timestamp.IsZero() || !fresh.Updated.After(response.Timestamp)) {
				delete(s.state.Devices, chosen.Device)
				for k, p := range s.state.Pending {
					if p.Device == chosen.Device {
						delete(s.state.Pending, k)
					}
				}
			} else {
				// Retry this event with the refreshed registration, rather than
				// losing it because the old token became invalid in flight.
				current.Next = time.Now()
				s.state.Pending[key] = current
			}
		} else if errors.As(err, &response) && !response.Retryable() {
			s.lastError = err.Error()
			delete(s.state.Pending, key)
		} else {
			current.Attempts++
			current.Next = time.Now().Add(time.Duration(1<<min(current.Attempts, 5)) * 15 * time.Second)
			s.state.Pending[key] = current
			s.lastError = "推送暂未送达，正在重试"
		}
	}
	if s.saveLocked() != nil {
		s.lastError = "无法保存通知状态"
	}
}

func (s *service) configure() {
	var err error
	if s.managed == nil {
		s.managed, err = apns.OpenProvider(s.dir)
	} else {
		err = s.managed.Reload()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.provider = nil
		s.lastError = err.Error()
		return
	}
	s.provider = s.managed
	s.lastError = ""
}

func (s *service) String() string {
	status := s.status()
	return fmt.Sprintf("push configured=%t devices=%d sent=%d", status.Configured, status.Devices, status.Sent)
}
