package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/remote"
	"gorex/internal/rex"
)

// A host owns its transport; tabs never borrow another computer's client.
type desktopHost struct {
	retryTimer     *time.Timer
	retryAttempt   int
	layout         savedLayout
	key, link      string
	hello          rex.Hello
	client         *rex.Client
	tunnel         func()
	cancel         context.CancelFunc
	generation     int
	busy, creating bool
	err            string
	sessions       []rex.SessionInfo
	recent         desktopRecent
}
type desktopConnections struct {
	historyLoaded, dirty  bool
	hosts                 []*desktopHost
	store                 connectionRecordStore
	open, initialFocus    bool
	input, err, directory string
	selected              *desktopHost
}

func (h *desktopHost) name() string {
	if h.hello.Host.Name != "" {
		return h.hello.Host.Name
	}
	if h.recent.Name != "" {
		return h.recent.Name
	}
	return "远端电脑"
}
func (h *desktopHost) connected() bool {
	return remote.ConnectionUsable(h.client) && !h.busy && h.err == ""
}
func (a *App) currentHost() *desktopHost {
	if t := a.tab(); t != nil {
		return t.Host
	}
	return nil
}
func (a *App) paneClient(p *Pane) *rex.Client {
	if p.host != nil {
		return p.host.client
	}
	return a.client
}
func (a *App) hostHello(h *desktopHost) rex.Hello {
	if h != nil {
		return h.hello
	}
	return a.hello
}
func (p *Pane) noticeKey() string {
	if p.host != nil {
		return p.host.key + "/" + p.SID
	}
	return p.SID
}
func noticeSessionID(key string) string {
	_, sid, ok := strings.Cut(key, "/")
	if ok {
		return sid
	}
	return key
}
func noticeOnHost(key string, h *desktopHost) bool {
	if h == nil {
		return !strings.Contains(key, "/")
	}
	return strings.HasPrefix(key, h.key+"/")
}
func (a *App) markPaneClosed(p *Pane) {
	if p.host == nil {
		a.markSessionClosed(p.SID)
	} else {
		a.closeAgentNotice(p.noticeKey())
	}
}

func (a *App) desktopDispatcher() func(func()) {
	win := a.win
	return func(fn func()) {
		a.post(fn)
		if win != nil {
			win.Update(a.runPosted)
		}
	}
}
func disposeDesktopTransport(client *rex.Client, tunnel func()) {
	remote.CloseConnection(client, tunnel)
}
func desktopLayoutKey(h *desktopHost) string {
	if h.recent.ID != "" {
		return h.recent.ID
	}
	return h.key
}
func (a *App) desktopInputEnabled(h *desktopHost, enabled bool) {
	for _, tab := range a.tabs {
		if tab.Host == h {
			for _, p := range tab.panes() {
				if p.term != nil {
					p.term.SetInputEnabled(enabled && (p.remoteView == nil || a.activeRemote == p && !p.remoteYielded && p.remoteView.hasTransport()))
				}
			}
		}
	}
}
func (a *App) flushDesktopHistory() {
	if a.desktopStorage == nil {
		return
	}
	done := make(chan struct{})
	a.desktopStorage <- func() { close(done) }
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
func (a *App) loadDesktopHistory() {
	a.desktops = desktopConnections{store: newDesktopConnectionStore()}
	if a.desktopStorage == nil {
		a.desktopStorage = connectionStorage()
	}
	store, win, update := a.desktops.store, a.win, a.desktopDispatcher()
	a.desktopStorage <- func() {
		history := readDesktopHistory(store)
		var layouts map[string]savedLayout
		if data, err := store.Get("desktop-layouts"); err == nil {
			json.Unmarshal(data, &layouts)
		}
		update(func() {
			if a.win != win || a.quitting {
				return
			}
			a.desktops.historyLoaded = true
			for _, entry := range history {
				h, err := a.rememberDesktop(entry.Link)
				if err != nil {
					continue
				}
				h.recent = entry
				h.layout = layouts[desktopLayoutKey(h)]
				if len(h.layout.Tabs) > 0 {
					a.connectDesktop(h)
				}
			}
		})
	}
}
func (a *App) saveDesktopHistory() {
	if a.desktops.store == nil || a.desktopStorage == nil || !a.desktops.historyLoaded {
		return
	}
	var history []desktopRecent
	layouts := map[string]savedLayout{}
	for _, h := range a.desktops.hosts {
		history = append(history, h.recent)
		layout := a.snapshotHost(h)
		if len(layout.Tabs) == 0 && len(h.layout.Tabs) > 0 {
			layout = h.layout
		}
		layouts[desktopLayoutKey(h)] = layout
	}
	history = mergeDesktopHistory(history, nil)
	data, _ := json.Marshal(layouts)
	store, update := a.desktops.store, a.desktopDispatcher()
	a.desktopStorage <- func() {
		err := writeDesktopHistory(store, history)
		if err == nil {
			err = store.Set("desktop-layouts", data)
		}
		if err != nil {
			update(func() { a.desktops.err = "无法保存最近连接，请重试。" })
		}
	}
}

func (a *App) rememberDesktop(raw string) (*desktopHost, error) {
	addr, err := remote.ParseLink(raw)
	if err != nil {
		return nil, err
	}
	link := remote.Link(addr)
	for _, h := range a.desktops.hosts {
		if h.link == link {
			return h, nil
		}
	}
	sum := sha256.Sum256([]byte(addr))
	h := &desktopHost{key: hex.EncodeToString(sum[:12]), link: link, recent: desktopRecent{Link: link, Name: "远端电脑"}}
	a.desktops.hosts = append(a.desktops.hosts, h)
	return h, nil
}
func (a *App) submitDesktopConnection() {
	h, err := a.rememberDesktop(a.desktops.input)
	if err != nil {
		a.desktops.err = err.Error()
		return
	}
	a.desktops.input, a.desktops.err = "", ""
	a.desktops.selected = h
	a.connectDesktop(h)
}
func (a *App) connectDesktop(h *desktopHost) {
	if h.busy || h.connected() {
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	disposeDesktopTransport(h.client, h.tunnel)
	h.client, h.tunnel = nil, nil
	h.generation++
	generation := h.generation
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.busy, h.err = cancel, true, ""
	a.desktopInputEnabled(h, false)
	update := a.desktopDispatcher()
	link := h.link
	device := mygo.App.Device()
	deviceName := device.Name
	if deviceName == "" {
		deviceName = a.hello.Host.Name
	}
	go func() {
		deadline, stop := context.WithTimeout(ctx, 45*time.Second)
		defer stop()
		client, tunnel, hello, infos, err := remote.OpenConnection(deadline, link, rex.DeviceInfo{Name: deviceName, OS: device.System})
		update(func() {
			if h.generation != generation || a.quitting {
				disposeDesktopTransport(client, tunnel)
				return
			}
			h.busy = false
			if err != nil {
				disposeDesktopTransport(client, tunnel)
				h.err = connectionIssueFor(err).body
				if h.retryAttempt > 0 && connectionIssueFor(err).automatic {
					a.scheduleDesktopRetry(h, update)
				}
				return
			}
			if hello.Host.ID != "" && hello.Host.ID == a.hello.Host.ID {
				disposeDesktopTransport(client, tunnel)
				h.err = "这是此电脑，请使用本地标签页。"
				return
			}
			// A newly copied capability can belong to a computer already remembered.
			for _, other := range a.desktops.hosts {
				if other != h && hello.Host.ID != "" && sameDesktop(other.recent, desktopRecent{ID: hello.Host.ID, Name: hello.Host.Name}) {
					a.desktops.selected = other
					other.link, other.recent.Link = h.link, h.link
					disposeDesktopTransport(client, tunnel)
					a.desktops.hosts = slices.DeleteFunc(a.desktops.hosts, func(v *desktopHost) bool { return v == h })
					if !other.connected() {
						a.connectDesktop(other)
					}
					a.saveDesktopHistory()
					return
				}
			}
			h.client, h.tunnel, h.hello, h.sessions = client, tunnel, hello, liveDesktopSessions(infos)
			h.retryAttempt = 0
			a.desktops.hosts = slices.Insert(slices.DeleteFunc(a.desktops.hosts, func(other *desktopHost) bool { return other == h }), 0, h)
			h.recent = desktopRecent{Link: h.link, Name: h.name(), ID: hello.Host.ID, OS: desktopPlatform(hello.Host)}
			a.reattachDesktopTabs(h)

			a.restoreDesktopLayout(h)
			a.saveDesktopHistory()
			go a.pollDesktop(ctx, h, client, generation, update)
		})
	}()
}
func liveDesktopSessions(infos []rex.SessionInfo) []rex.SessionInfo {
	out := slices.DeleteFunc(slices.Clone(infos), func(in rex.SessionInfo) bool { return in.Exited })
	slices.SortFunc(out, func(x, y rex.SessionInfo) int { return x.Created.Compare(y.Created) })
	return out
}
func desktopSession(h *desktopHost, sid string) (rex.SessionInfo, bool) {
	for _, in := range h.sessions {
		if in.ID == sid {
			return in, true
		}
	}
	return rex.SessionInfo{}, false
}
func (a *App) pollDesktop(ctx context.Context, h *desktopHost, client *rex.Client, generation int, update func(func())) {
	remote.WatchConnection(ctx, client, nil, func(infos []rex.SessionInfo, err error) {
		update(func() {
			if ctx.Err() != nil || h.generation != generation || a.quitting {
				return
			}
			if err != nil {
				a.recoverDesktop(ctx, h, client, generation, update)
				return
			}
			h.sessions = liveDesktopSessions(infos)
			byID := map[string]rex.SessionInfo{}
			for _, in := range infos {
				byID[in.ID] = in
			}
			a.applyHost(h, byID)
		})
	})
}
func (a *App) recoverDesktop(ctx context.Context, h *desktopHost, client *rex.Client, generation int, update func(func())) {
	h.busy, h.creating = true, false
	h.err = "正在恢复连接，远端会话继续运行。"
	a.desktopInputEnabled(h, false)
	go func() {
		check, cancel := context.WithTimeout(ctx, remote.ResumeRecoveryTimeout)
		next, hello, infos, err := remote.ResumeConnection(check, client, nil)
		cancel()
		update(func() {
			if h.generation != generation || a.quitting {
				if next != nil && next != client {
					next.Close()
				}
				return
			}
			h.busy = false
			if err != nil {
				h.err = connectionIssueFor(err).body
				if connectionIssueFor(err).automatic {
					a.scheduleDesktopRetry(h, update)
				}
				return
			}
			h.err = ""
			if next != client {
				client.Close()
				h.client = next
				h.hello = hello
			}
			h.sessions = liveDesktopSessions(infos)
			a.reattachDesktopTabs(h)
			go a.pollDesktop(ctx, h, h.client, generation, update)
		})
	}()
}
func (a *App) scheduleDesktopRetry(h *desktopHost, update func(func())) {
	generation := h.generation
	delay := remote.RetryDelay(h.retryAttempt)
	h.retryAttempt++
	if h.retryTimer != nil {
		h.retryTimer.Stop()
	}
	h.retryTimer = time.AfterFunc(delay, func() {
		update(func() {
			if h.generation == generation && !a.quitting {
				h.retryTimer = nil
				a.connectDesktop(h)
			}
		})
	})
}
func (a *App) reattachDesktopTabs(h *desktopHost) {
	for _, t := range a.tabs {
		if t.Host == h {
			for _, p := range t.panes() {
				if in, ok := desktopSession(h, p.SID); ok {
					a.reattachDesktopPane(p, in)
				} else {
					p.info.Exited = true
				}
			}
		}
	}
}

func (a *App) reattachDesktopPane(p *Pane, in rex.SessionInfo) {
	if p.remoteView != nil {
		p.info = in
		if a.activeRemote == p && !p.remoteYielded {
			a.resumeRemotePane(p)
		}
		return
	}
	p.closed = true
	if p.term != nil {
		a.closePaneTerminal(p)
	}
	p.closed, p.info, p.term = false, in, nil
	a.attach(p, in.Cols, in.Rows)
}

func (a *App) disconnectDesktop(h *desktopHost) {
	h.generation++
	if h.retryTimer != nil {
		h.retryTimer.Stop()
		h.retryTimer = nil
	}
	h.retryAttempt = 0
	if h.cancel != nil {
		h.cancel()
	}
	h.busy, h.creating = false, false
	h.err = ""
	for _, t := range slices.Clone(a.tabs) {
		if t.Host == h {
			a.closeTab(t)
		}
	}
	h.layout = savedLayout{}
	a.closeReleasedDesktopTransport(h)
	h.client, h.tunnel = nil, nil
	a.saveDesktopHistory()
}
func (a *App) closeDesktopConnections() {
	for _, h := range a.desktops.hosts {
		h.generation++
		if h.retryTimer != nil {
			h.retryTimer.Stop()
			h.retryTimer = nil
		}
		if h.cancel != nil {
			h.cancel()
		}
		disposeDesktopTransport(h.client, h.tunnel)
		h.client, h.tunnel = nil, nil
	}
}
func (a *App) openDesktopSession(h *desktopHost, in rex.SessionInfo, selectIt bool) {
	if !h.connected() || in.Exited {
		return
	}
	for i, t := range a.tabs {
		if t.Host == h {
			for _, p := range t.panes() {
				if p.SID == in.ID {
					if selectIt {
						t.setFocus(p)
						a.selectTab(i)
						a.finishDesktopDialog()
					}
					return
				}
			}
		}
	}
	t := &Tab{ID: a.id(), Host: h}
	p := &Pane{ID: a.id(), SID: in.ID, host: h, info: in, startDir: in.Dir}
	t.Root = &Node{ID: a.id(), Pane: p}
	p.Tab, p.Node = t, t.Root
	t.setFocus(p)
	a.attach(p, in.Cols, in.Rows)
	at := len(a.tabs)
	if selectIt {
		at = min(a.active+1, len(a.tabs))
	}
	a.tabs = slices.Insert(a.tabs, at, t)
	if selectIt {
		a.selectTab(at)
		a.finishDesktopDialog()
	}
	a.changed()
}

// Network creates run outside the UI thread. A late response cannot replace
// another tab or resurrect a tab closed while the request was in flight.
func (a *App) createDesktopSession(h *desktopHost, dir string, split *Pane, vertical bool) {
	if !h.connected() {
		a.showDesktopConnections(h)
		return
	}
	if h.creating {
		return
	}
	if dir == "" || dir == "~" {
		dir = h.hello.Host.Home
	} else if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		dir = path.Join(h.hello.Host.Home, rest)
	}
	h.creating = true
	client, generation := h.client, h.generation
	update := a.desktopDispatcher()
	cols, rows := 80, 24
	if split != nil && split.term != nil {
		cols, rows = split.term.Size()
		if vertical {
			rows = max(rows/2, 2)
		} else {
			cols = max(cols/2, 10)
		}
	}
	go func() {
		in, err := client.Create(rex.CreateOptions{Dir: dir, Cols: cols, Rows: rows})
		update(func() {
			if generation != h.generation || a.quitting {
				return
			}
			h.creating = false
			if err != nil {
				a.desktops.err = "无法新建会话，请检查工作目录和远端连接。"
				a.showDesktopConnections(h)
				return
			}
			h.sessions = append(h.sessions, in)
			if split == nil {
				a.openDesktopSession(h, in, true)
				return
			}
			if split.closed || split.Tab.Host != h || !slices.Contains(a.tabs, split.Tab) {
				return
			}
			t, n := split.Tab, split.Node
			p := &Pane{ID: a.id(), SID: in.ID, host: h, info: in, startDir: in.Dir, Tab: t}
			left, right := &Node{ID: a.id(), Pane: split, Parent: n}, &Node{ID: a.id(), Pane: p, Parent: n}
			split.Node, p.Node = left, right
			n.Pane, n.A, n.B, n.Vertical, n.Ratio = nil, left, right, vertical, .5
			t.Zoom = nil
			a.attach(p, in.Cols, in.Rows)
			t.setFocus(p)
			a.selectTab(slices.Index(a.tabs, t))
			a.changed()
		})
	}()
}
func (a *App) restartDesktopPane(p *Pane) {
	// Ending a remote process is explicit; closing a remote tab only detaches.
	h, client, generation := p.host, p.host.client, p.host.generation
	if !h.connected() || h.creating {
		return
	}
	h.creating = true
	update := a.desktopDispatcher()
	cols, rows := p.info.Cols, p.info.Rows
	dir := p.info.Dir
	go func() {
		in, err := client.Create(rex.CreateOptions{Dir: dir, Cols: cols, Rows: rows})
		update(func() {
			if generation != h.generation || a.quitting {
				return
			}
			h.creating = false
			if err != nil {
				a.desktops.err = "无法重新启动会话。"
				a.showDesktopConnections(h)
				return
			}
			if p.closed {
				return
			}
			replacement := &Pane{ID: a.id(), SID: in.ID, host: h, info: in, startDir: dir, Tab: p.Tab, Node: p.Node}
			p.closed = true
			a.closePaneTerminal(p)
			a.closeAgentNotice(p.noticeKey())
			go client.Kill(p.SID)
			p.Node.Pane = replacement
			t := p.Tab
			if t.Zoom == p {
				t.Zoom = replacement
			}
			t.recent = slices.DeleteFunc(t.recent, func(q *Pane) bool { return q == p })
			a.attach(replacement, in.Cols, in.Rows)
			t.setFocus(replacement)
			a.focusReq = replacement
			a.changed()
		})
	}()
}
func (a *App) showDesktopConnections(h *desktopHost) {
	a.settingsOpen, a.paletteOpen, a.renaming = false, false, nil
	a.desktops.open, a.desktops.initialFocus = true, true
	a.desktops.selected, a.desktops.directory = h, ""
	a.focusReq = nil
}
func (a *App) finishDesktopDialog() {
	a.desktops.open = false
	if t := a.tab(); t != nil {
		a.focusReq = t.Focus
	}
}
func (a *App) desktopTabMenu(m *ui.Menu) {
	if m.Item("此电脑 · 新建会话").Chosen() {
		a.newLocalTab(a.hello.Host.Home)
	}
	for _, h := range a.desktops.hosts {
		h := h
		if m.Item(h.name() + " · 新建会话").Disabled(!h.connected() || h.creating).Chosen() {
			a.createDesktopSession(h, h.hello.Host.Home, nil, false)
		}
	}
	m.Separator()
	if m.Item("打开已有会话…").Chosen() {
		a.showDesktopConnections(a.currentHost())
	}
	if m.Item("连接其他桌面…").Chosen() {
		a.showDesktopConnections(nil)
	}
}

// Restore remote splits locally, using only sessions the remote server still
// has. A missing session is never replaced with a new shell during restore.
func (a *App) restoreDesktopLayout(h *desktopHost) {
	saved := h.layout
	h.layout = savedLayout{}
	shown := map[string]bool{}
	for _, tab := range a.tabs {
		if tab.Host == h {
			for _, p := range tab.panes() {
				shown[p.SID] = true
			}
		}
	}
	var load func(*savedNode, *Tab, *Node) *Node
	load = func(s *savedNode, t *Tab, parent *Node) *Node {
		if s == nil {
			return nil
		}
		n := &Node{ID: a.id(), Parent: parent}
		if s.A == nil || s.B == nil {
			in, ok := desktopSession(h, s.SID)
			if !ok || shown[s.SID] {
				return nil
			}
			shown[s.SID] = true
			p := &Pane{ID: a.id(), SID: in.ID, host: h, info: in, startDir: in.Dir, Tab: t, Node: n}
			n.Pane = p
			a.attach(p, in.Cols, in.Rows)
			return n
		}
		n.A, n.B = load(s.A, t, n), load(s.B, t, n)
		if n.A == nil {
			if n.B != nil {
				n.B.Parent = parent
			}
			return n.B
		}
		if n.B == nil {
			n.A.Parent = parent
			return n.A
		}
		n.Vertical, n.Ratio = s.Vertical, min(max(s.Ratio, .05), .95)
		return n
	}
	for _, savedTab := range saved.Tabs {
		t := &Tab{ID: a.id(), Host: h, Name: savedTab.Name}
		t.Root = load(savedTab.Root, t, nil)
		if t.Root == nil {
			continue
		}
		panes := t.panes()
		t.setFocus(panes[min(max(savedTab.Focus, 0), len(panes)-1)])
		if savedTab.Zoom && len(panes) > 1 {
			t.Zoom = t.Focus
		}
		a.tabs = append(a.tabs, t)
	}
	a.changed()
}
