package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/mobile"
	"gorex/internal/remote"
	"gorex/internal/rex"
	"gorex/internal/terminal"
)

type mobileApp struct {
	win                                *mygo.Window
	client                             *rex.Client
	closeTunnel                        func()
	cancel                             context.CancelFunc
	generation                         int
	hello                              rex.Hello
	sessions                           []rex.SessionInfo
	term                               *terminal.Terminal
	stream                             *mobileStream
	selected                           rex.SessionInfo
	link, error                        string
	history                            []desktopRecent
	historySelection                   map[string]bool
	recentSessions                     []mobileRecentSession
	historyEpoch                       int
	home                               bool
	pollCancel                         context.CancelFunc
	resumeCheckID                      uint64
	busy, scanning, creating           bool
	directory                          string
	resumeLink, resumeSID              string
	storage                            chan func()
	store                              *mygo.SecureStore
	scroll                             ui.ScrollState
	navigation                         *ui.Router
	navigationPage                     string
	keyboardMore                       bool
	background                         bool
	presence                           map[string]desktopPresence
	presenceCancel                     context.CancelFunc
	presenceEpoch                      int
	reconnecting                       bool
	retryAttempt                       int
	retryTimer                         *time.Timer
	sessionPreferences                 map[string]mobileSessionPreference
	preferenceEpoch                    int
	editingSession                     string
	editingOpen                        bool
	editingName                        string
	editingPinned                      bool
	endingSession                      rex.SessionInfo
	endingOpen                         bool
	closingSession                     string
	closedSessions                     map[string]bool
	agentPrevious                      map[string]rex.SessionInfo
	notificationDenied                 bool
	agentNotify                        func(mobileAgentNotice)
	pendingDesktop, pendingSession     string
	pushDeviceID, pushToken, pushError string
	pushDisabled                       bool
	pushRequesting                     bool
	pushSnapshot                       atomic.Pointer[rex.DeviceInfo]
	noticeReceipts                     map[string]time.Time
	noticeReceiptsLoaded               bool
	preferenceTouched                  map[string]bool
	imagePasteBusy                     bool
	imagePasteCancel                   context.CancelFunc
	imagePasteEpoch                    int
	keyboardModifiers, keyboardLocked  ui.Modifiers
	connectionIssue                    *mobileConnectionIssue
	connectionDetailsOpen              bool
	sessionSettingsOpen                bool
	// sizeLock, a setting, makes an opened session take the PTY's size
	// (and the desktop's pane show it) while the phone session is active.
	// lockLost tells that a window did, for the open session.
	sizeLock, lockLost, lockSeen bool
	lockOwner                    string
	lockSuspended                bool
	lockRelease                  <-chan struct{}
}

type desktopRecent struct {
	Link string
	Name string
	ID   string `json:",omitempty"`
	OS   string `json:",omitempty"`
}

func sameDesktop(a, b desktopRecent) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	if a.Name == "桌面" || a.Name != b.Name {
		return false
	}
	if a.ID == "" || b.ID == "" {
		return true
	}
	// Upgrade records from early builds that used an installation UUID.
	if strings.HasPrefix(a.ID, "legacy:") || strings.HasPrefix(b.ID, "legacy:") {
		return strings.HasPrefix(a.ID, "machine:") || strings.HasPrefix(b.ID, "machine:")
	}
	return strings.HasPrefix(a.ID, "machine:") && !strings.Contains(b.ID, ":") ||
		strings.HasPrefix(b.ID, "machine:") && !strings.Contains(a.ID, ":")
}

func mergeDesktopHistory(first, second []desktopRecent) []desktopRecent {
	var history []desktopRecent
	for _, entries := range [][]desktopRecent{first, second} {
		for _, entry := range entries {
			if _, err := remote.ParseLink(entry.Link); err != nil {
				continue
			}
			if strings.TrimSpace(entry.Name) == "" {
				entry.Name = "桌面"
			}
			duplicate := false
			for i, kept := range history {
				if entry.Link == kept.Link || sameDesktop(entry, kept) {
					// Migrate older records without IDs while keeping the newest URL.
					if kept.ID == "" || strings.HasPrefix(entry.ID, "machine:") && !strings.HasPrefix(kept.ID, "legacy:") {
						history[i].ID = entry.ID
					}
					if kept.OS == "" {
						history[i].OS = entry.OS
					}
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			history = append(history, entry)
			if len(history) == 6 {
				return history
			}
		}
	}
	return history
}

func mobileMain() {
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		os.Setenv("GOREX_DIR", dir)
	}
	registerFonts()
	m := &mobileApp{storage: make(chan func(), 32), sizeLock: true}
	go func() {
		for task := range m.storage {
			task()
		}
	}()
	storeName := "desktop-connection"
	if os.Getenv("GOREX_UI_TEST") == "1" {
		storeName += "-ui-tests"
	}
	m.store, _ = mygo.NewSecureStore(storeName, mygo.SecureStoreOptions{})
	mygo.App.OnLifecycleChanged(func(state mygo.LifecycleState) {
		if os.Getenv("GOREX_DEBUG_TUNNEL") == "1" {
			log.Printf("GoRex lifecycle: %v", state)
		}
		m.invalidate()
	})
	mygo.App.OnOpenURL(func(link string) { m.connect(link) })
	mygo.App.OnDidBecomeActive(func() { m.registerSystemPush() })
	mygo.App.OnNotification(func(event mygo.NotificationEvent) {
		if event.Clicked {
			if id := event.Data["event"]; id != "" {
				m.rememberNotice(id)
				m.refreshPushSnapshot()
				m.syncPushRegistration()
			}
			m.openNotifiedSession(event.Data["desktop"], event.Data["session"])
		}
	})
	mygo.App.WhenReady(func() {
		m.win = mygo.NewWindow(mygo.WindowOptions{Title: "GoRex", Width: 390, Height: 844, BackgroundColor: "light-dark(#f5f6f8, #111315)", Content: ui.View(m.view)})
		m.invalidate()
		m.loadSessionPreferences()
		m.setupPush()
		historyEpoch := m.historyEpoch
		m.storage <- func() {
			var history []desktopRecent
			var recentSessions []mobileRecentSession
			historySaved := false
			if saved, err := m.store.Get("recent-sessions"); err == nil {
				json.Unmarshal(saved, &recentSessions)
			}
			sizeLock := true
			if saved, err := m.store.Get("size-lock"); err == nil {
				sizeLock = string(saved) == "1"
			}
			if saved, err := m.store.Get("history"); err == nil {
				historySaved = json.Unmarshal(saved, &history) == nil
			}
			if !historySaved {
				if saved, err := m.store.Get("recent"); err == nil {
					history = []desktopRecent{{Link: string(saved), Name: "桌面"}}
				}
			}
			mygo.RunOnMain(func() {
				m.sizeLock = sizeLock
				if m.historyEpoch == historyEpoch {
					m.applyLoadedConnectionHistory(history, recentSessions, historyEpoch)
					if m.pendingSession != "" {
						desktop, sid := m.pendingDesktop, m.pendingSession
						m.pendingDesktop, m.pendingSession = "", ""
						m.openNotifiedSession(desktop, sid)
					}
					m.invalidate()
				}
			})
		}
	})
	mygo.App.OnDidEnterBackground(m.enterBackground)
	mygo.App.OnWillEnterForeground(m.enterForeground)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (m *mobileApp) invalidate() {
	if m.win != nil {
		m.win.Invalidate()
	}
}

func (m *mobileApp) detach() {
	m.releaseSizeLock()
	m.clearKeyboardModifiers()
	m.cancelImagePaste()
	m.keyboardMore = false
	if m.term != nil {
		m.term.Close()
		m.term = nil
	}
	m.stream = nil
	m.selected = rex.SessionInfo{}
	m.lockOwner, m.lockSuspended, m.lockSeen = "", false, false
	mobile.HideKeyboard()
}

func (m *mobileApp) disconnect(forget bool) {
	m.historySelection = nil
	m.stopRecentPresence()
	m.stopPolling()
	m.home = true
	m.generation++
	m.reconnecting = false
	m.retryAttempt = 0
	if m.retryTimer != nil {
		m.retryTimer.Stop()
		m.retryTimer = nil
	}
	m.agentPrevious = nil
	m.editingOpen = false
	m.sessionSettingsOpen = false
	m.endingOpen, m.closingSession = false, ""
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.detach()
	m.closeConnectionAfterSizeRelease()
	m.busy, m.creating = false, false
	m.sessions = nil
	m.resumeLink, m.resumeSID = "", ""
	m.connectionIssue, m.connectionDetailsOpen = nil, false
	if forget {
		m.link, m.resumeLink, m.resumeSID = "", "", ""
		m.historyEpoch++
		m.history = nil
		m.recentSessions = nil
		m.presence = nil
		if m.store != nil && m.storage != nil {
			m.storage <- func() { m.store.Delete("recent"); m.store.Delete("history"); m.store.Delete("recent-sessions") }
		}
	}
	m.invalidate()
}

func (m *mobileApp) connect(raw string) {
	addr, err := remote.ParseLink(raw)
	if err != nil {
		m.connectionIssue = connectionIssueFor(&remote.ConnectionError{Kind: remote.InvalidLink, Cause: err})
		m.error = ""
		m.invalidate()
		return
	}
	link := remote.Link(addr)
	m.historySelection = nil
	if m.link == link && m.connectionUsable() {
		m.detach()
		m.home, m.creating, m.error = false, false, ""
		m.invalidate()
		return
	}
	if m.link == link && (m.busy || m.reconnecting) {
		m.home = false
		m.invalidate()
		return
	}
	m.disconnect(false)
	m.home = false
	m.link = link
	m.resumeLink, m.resumeSID = "", ""
	if m.background {
		m.resumeLink = link
		m.reconnecting = true
		m.invalidate()
		return
	}
	m.startConnection(false)
}

func (m *mobileApp) startConnection(recovering bool) {
	if m.background || m.busy {
		return
	}
	m.busy, m.error = true, ""
	generation := m.generation
	timeout := 30 * time.Second
	if recovering {
		timeout = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancel = cancel
	link := m.link
	device := mobile.DeviceInfo()
	if snapshot := m.pushSnapshot.Load(); snapshot != nil {
		device = *snapshot
	}
	go func() {
		client, closeTunnel, err := remote.Connect(ctx, link)
		var hello rex.Hello
		var sessions []rex.SessionInfo
		stop := func() bool { return false }
		if client != nil {
			stop = context.AfterFunc(ctx, func() { client.Close() })
		}
		if err == nil {
			hello, err = client.HelloFrom(device)
		}
		if err == nil && !rex.CompatibleProtocol(hello.Version) {
			err = &remote.ConnectionError{Kind: remote.ProtocolMismatch}
		}
		if err == nil {
			sessions, err = client.List()
		}
		stop()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		cancel()
		mygo.RunOnMain(func() {
			if generation != m.generation || err != nil {
				if client != nil {
					client.Close()
				}
				if closeTunnel != nil {
					go closeTunnel()
				}
				if generation == m.generation {
					m.connectionFailed(err, recovering)
				}
				return
			}
			m.client, m.closeTunnel, m.hello = client, closeTunnel, hello
			m.reconnecting, m.retryAttempt = false, 0
			m.connectionIssue, m.connectionDetailsOpen = nil, false
			currentDesktop := desktopRecent{Link: m.link, Name: m.hello.Host.Name, ID: m.hello.Host.ID, OS: desktopPlatform(m.hello.Host)}
			m.migrateRecentDesktop(currentDesktop)
			m.history = mergeDesktopHistory([]desktopRecent{currentDesktop}, m.history)
			m.updateSessions(sessions, !recovering)
			m.syncPushRegistration()
			m.busy, m.error, m.cancel = false, "", nil
			if m.presence == nil {
				m.presence = make(map[string]desktopPresence)
			}
			m.presence[m.link] = desktopPresence{state: desktopOnline, checked: time.Now()}
			if m.store != nil {
				history, _ := json.Marshal(m.history)
				m.storage <- func() {
					if err := m.store.Set("history", history); err != nil {
						mygo.RunOnMain(func() {
							if m.generation == generation {
								m.error = "已连接；无法保存连接记录，下次启动请重新扫码"
								m.invalidate()
							}
						})
					}
				}
			}
			if recovering && m.term != nil && m.stream != nil && (m.resumeSID == "" || m.resumeSID == m.selected.ID) {
				found := false
				for _, session := range m.sessions {
					if session.ID == m.selected.ID {
						m.selected = session
						m.stream.geometry(session.Cols, session.Rows)
						m.reattach(session)
						found = true
						break
					}
				}
				if !found {
					m.detach()
					m.error = "原会话已结束，请选择其他会话"
				}
				m.resumeSID = ""
			} else if recovering && m.term != nil && m.resumeSID != "" {
				m.detach()
			}
			if m.resumeSID != "" {
				sid := m.resumeSID
				m.resumeSID = ""
				m.openSessionID(sid)
			}
			m.invalidate()
			m.startPolling(client, generation)
		})
	}()
}

func (m *mobileApp) poll(ctx context.Context, client *rex.Client, generation int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Closed():
			mygo.RunOnMain(func() {
				if ctx.Err() == nil && !m.background && m.generation == generation && m.client == client {
					m.connectionLost()
				}
			})
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			// Stopping foreground polling must not close a healthy retained
			// connection when a list request happens to be in flight. Let
			// that bounded request finish; the parent still suppresses its
			// callback after background entry or a transport replacement.
			checkCtx, cancel := context.WithTimeout(context.Background(), mobileResumeCheckTimeout)
			sessions, err := checkMobileConnection(checkCtx, client, m.pushSnapshot.Load())
			cancel()
			mygo.RunOnMain(func() {
				if ctx.Err() == nil && !m.background && m.generation == generation && m.client == client {
					if err != nil {
						m.connectionLost()
						return
					}
					m.updateSessions(sessions, false)
					for _, s := range sessions {
						if s.ID == m.selected.ID {
							if m.term != nil && m.stream != nil && !m.stream.HasScreenSize() && (s.Cols != m.selected.Cols || s.Rows != m.selected.Rows) {
								// Older desktop daemons send raw ANSI. Reattach for a
								// fresh snapshot if their source grid changes.
								m.selected = s
								m.stream.geometry(s.Cols, s.Rows)
								m.stream.replace(m.sessionStream(s.ID, false))
							} else {
								m.selected = s
								m.followSizeLock(s)
							}
						}
					}
					m.invalidate()
				}
			})
		}
	}
}

func (m *mobileApp) scan() {
	if m.scanning || m.busy {
		return
	}
	m.stopRecentPresence()
	m.scanning = true
	m.error = ""
	generation := m.generation
	mobile.Scan(func(raw string, err error) {
		mygo.RunOnMain(func() {
			m.scanning = false
			if m.generation != generation {
				m.invalidate()
				return
			}
			if err != nil {
				m.error = err.Error()
			} else if raw != "" {
				m.connect(raw)
			}
			m.invalidate()
		})
	})
}

func (m *mobileApp) openSession(s rex.SessionInfo) {
	if m.client == nil {
		return
	}
	if m.selected.ID != s.ID {
		// A window's unlock holds for the session it released only.
		m.lockLost = false
	}
	m.detach()
	var stream *mobileStream
	locking := m.locksSize()
	m.lockSeen = false
	stream = newMobileStream(m.sessionStream(s.ID, locking), func() {
		mygo.RunOnMain(func() {
			if m.stream == stream && !stream.hasTransport() {
				if m.closingSession == m.selected.ID {
					return // The requested close is handled by its control response.
				}
				if m.selected.Exited {
					stream.Close()
					return
				}
				m.connectionLost()
			}
		})
	})
	stream.geometry(s.Cols, s.Rows)
	options := terminal.Options{Conn: stream, FixedCols: max(s.Cols, 1), FixedRows: max(s.Rows, 1), ReflowView: true}
	if locking {
		// The program draws for this view, as in a desktop pane.
		options = terminal.Options{Conn: stream}
	}
	options.Font, options.Theme, options.DarkTheme = terminal.Font{Family: termFont.Family, Size: 13, LineHeight: 1.2}, lightTerm, darkTerm
	options.AdaptiveColors, options.OptionAsAlt, options.SelectOnDrag, options.CopyRawText = true, true, true, true
	options.ActiveCursor, options.InputContext, options.OnPaste = true, true, m.pasteClipboard
	term, err := terminal.New(options)
	if err != nil {
		stream.Close()
		m.error = "无法打开终端：" + err.Error()
		return
	}
	m.term, m.stream, m.selected, m.creating, m.error = term, stream, s, false, ""
	m.resumeSID = ""
	m.rememberSession(s)
	m.invalidate()
}

func (m *mobileApp) create() {
	if m.client == nil || m.busy {
		return
	}
	m.busy, m.error = true, ""
	client, generation, directory := m.client, m.generation, strings.TrimSpace(m.directory)
	go func() {
		session, err := client.Create(rex.CreateOptions{Dir: directory, Cols: 48, Rows: 24})
		mygo.RunOnMain(func() {
			if m.generation != generation {
				return
			}
			m.busy = false
			if err != nil {
				m.error = "创建失败：" + err.Error()
			} else {
				m.sessions = append(m.sessions, session)
				m.openSession(session)
			}
			m.invalidate()
		})
	}()
}

func mobileSessionTitle(s rex.SessionInfo) string {
	prog := programOf(sessionProgramName(s))
	if title := programTitle(s.Title, prog); title != "" {
		return title
	}
	if s.Program != "" {
		if prog.Name != "" {
			return prog.Name
		}
		return s.Program
	}
	if s.Shell != "" {
		return filepath.Base(s.Shell)
	}
	return "终端"
}

func (m *mobileApp) view(c *ui.Context) {
	theme := *c.Theme()
	theme.FontSize, theme.Radius = 16, 12
	if theme.Dark {
		theme.Background = ui.Hex("#111315")
		theme.Surface = ui.Hex("#1e2126")
	} else {
		theme.Background = ui.Hex("#f5f6f8")
		theme.Surface = ui.Hex("#ffffff")
	}
	c.SetTheme(&theme)
	c.Root().Background(theme.Background)
	m.syncNavigation()
	m.navigation.InteractiveBack = (!m.busy || m.reconnecting) && !m.scanning
	ui.Column(c).Fill().Children(func() {
		m.navigation.View(c, func(page *ui.Route) {
			page.Page().Background(theme.Background)
			switch page.Path() {
			case "/sessions/terminal":
				m.terminalView(c)
			case "/sessions/new":
				m.createView(c)
			case "/sessions":
				m.sessionsView(c)
			default:
				m.connectView(c)
			}
		})
	})
	m.sessionEditor(c)
	m.sessionSettingsDialog(c)
	m.sessionEndDialog(c)
	m.connectionDialog(c)
	if page := m.navigation.Path(); page != m.navigationPage {
		// A completed edge gesture changes history. Release resources only
		// after the new page has built; a cancelled preview keeps them alive.
		switch page {
		case "/connect":
			m.goHome()
		case "/sessions":
			m.resumeSID = ""
			m.detach()
			m.creating = false
		}
		m.navigationPage = page
		// Closed terminal views cannot be revisited through forward history.
		m.navigation.Reset("/connect")
		if page == "/sessions" {
			m.navigation.Push(page)
		}
		c.Invalidate()
	}
}

// Keep asynchronous connection/session state and the navigation stack in sync.
func (m *mobileApp) syncNavigation() {
	page := "/connect"
	if !m.home && (m.client != nil || m.reconnecting) {
		page = "/sessions"
		if m.creating {
			page = "/sessions/new"
		}
	}
	if m.term != nil {
		page = "/sessions/terminal"
	}
	if m.navigation == nil {
		m.navigation = ui.NewRouter("/connect")
		// Edge drags still preview both pages. Ordinary page changes are
		// immediate so a detached terminal is never painted while leaving.
		m.navigation.Transition = ui.TransitionNone
	}
	if page == m.navigationPage {
		return
	}
	switch page {
	case "/connect":
		m.navigation.Reset(page)
	case "/sessions":
		m.navigation.Reset("/connect")
		m.navigation.Push(page)
	default:
		if m.navigation.Path() == "/sessions/new" && page == "/sessions/terminal" {
			m.navigation.Replace(page)
		} else {
			if m.navigation.Path() != "/sessions" {
				m.navigation.Reset("/connect")
				m.navigation.Push("/sessions")
			}
			m.navigation.Push(page)
		}
	}
	m.navigationPage = page
}

// Keep the full touch target while letting the title sit beside the visible
// chevron. Header text passes pointer events through the overlapping area.
func mobileBackButton(c *ui.Context) *ui.Element {
	button := ui.ButtonBase(c).Key("mobile-back").Label("返回").Role(ui.RoleButton).Size(44, 44).Margin(0, -20, 0, 0).Justify(ui.Start).Radius(12)
	if button.Pressed() {
		button.Background(c.Theme().SurfacePressed)
	}
	return button.Children(func() {
		ui.Icon(c, icon("chevron-left")).Size(24, 24).TextColor(c.Theme().Accent)
	})
}

func (m *mobileApp) header(c *ui.Context, title string, back func(), terminalPage bool) {
	ui.Row(c).Height(52).Padding(0, 8).AlignItems(ui.Center).Children(func() {
		if back != nil {
			if mobileBackButton(c).Clicked() {
				back()
			}
		} else {
			ui.Box(c).Size(44, 44).Shrink(0)
		}
		ui.Column(c).Grow(1).MinWidth(0).PassThrough().Children(func() {
			align := ui.Center
			if back != nil {
				align = ui.Start
			}
			size := float32(17)
			if terminalPage && m.needsRecovery() {
				size = 15
			}
			ui.Text(c, title).FontSize(size).Bold().TextAlign(align).FillWidth().SingleLine().Ellipsis("…").PassThrough()
			if terminalPage && m.needsRecovery() {
				ui.Text(c, m.recoveryStatus()).Label("正在重连").FontSize(11).TextColor(c.Theme().TextMuted).TextAlign(align).FillWidth().SingleLine().Ellipsis("…").PassThrough()
			}
		})
		if terminalPage && m.term != nil && m.needsRecovery() {
			if ui.ButtonBase(c).Label("连接恢复操作").Role(ui.RoleButton).Size(44, 44).Children(func() {
				ui.Text(c, "•••").FontSize(16).TextColor(c.Theme().TextMuted)
			}).Clicked() {
				m.connectionDetailsOpen = true
			}
		} else {
			ui.Box(c).Size(44, 44).Shrink(0)
		}
	})
}

func (m *mobileApp) errorView(c *ui.Context) {
	if m.needsRecovery() || m.connectionIssue != nil {
		if m.term == nil {
			m.connectionFeedback(c)
		}
		return
	}
	if m.error != "" {
		ui.Text(c, m.error).Key("connection-status").TextColor(c.Theme().Danger).FontSize(14).LineHeight(1.4).Margin(0, 16, 12, 16)
	}
}

func (m *mobileApp) connectView(c *ui.Context) {
	m.refreshRecentPresence(c)
	m.header(c, "GoRex", nil, false)
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().HideScrollbars().Padding(16).Gap(16).Children(func() {
		ui.Text(c, "电脑上的 GoRex · 设置 → 连接").FontSize(13).TextColor(c.Theme().TextMuted).FillWidth()
		if ui.PrimaryButton(c, "").Label("扫码连接桌面").Role(ui.RoleButton).Height(48).FillWidth().Disabled(m.busy || m.scanning || m.reconnecting).Children(func() {
			ui.Icon(c, icon("scan-line")).Size(20, 20)
			ui.Text(c, "扫码连接桌面").FontSize(16)
		}).Clicked() {
			m.scan()
		}
		m.connectionFeedback(c)
		if !m.reconnecting && m.busy {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "正在连接桌面…").Label("正在连接桌面").FontSize(14).TextColor(c.Theme().TextMuted).Grow(1)
				if mobileTextAction(c, "取消连接", "取消").Clicked() {
					m.disconnect(false)
				}
			})
		}
		ui.Column(c).FillWidth().Children(func() {
			mobileListSectionHeader(c, "最近连接", func() {
				if len(m.history) > 0 {
					label, glyph := "清除连接记录", "trash-2"
					if m.historySelection != nil {
						label, glyph = "确认清除连接记录", "check"
					}
					if mobileIconAction(c, label, glyph).Disabled(m.busy || m.scanning || m.reconnecting).Clicked() {
						if m.historySelection == nil {
							m.historySelection = make(map[string]bool)
						} else {
							m.clearSelectedDesktopHistory()
						}
					}
				}
			})
			mobileListGroup(c).Children(func() {
				if len(m.history) == 0 {
					mobileListEmpty(c, "连接过的桌面会显示在这里")
				}
				for i, entry := range m.history {
					entry := entry
					if i > 0 {
						mobileListDivider(c)
					}
					status := m.presence[entry.Link].state.label()
					label := "重新连接 " + entry.Name
					if m.isConnectedDesktop(entry) {
						status, label = "已连接", "打开桌面 "+entry.Name
					}
					selecting := m.historySelection != nil
					checked := m.historySelection[entry.Link]
					if selecting {
						label = "选择连接 " + entry.Name
					}
					row := mobileListRow(c, "desktop-"+entry.Link, label, 56).Value(status).Disabled(m.busy || m.scanning || m.reconnecting)
					if selecting {
						row.Role(ui.RoleCheckBox).Checked(checked)
					}
					row.Children(func() {
						if selecting {
							mobileHistoryCheckbox(c, checked)
						} else {
							platform := desktopPlatformProgram(entry.OS)
							mobileListIcon(c, platform.Glyph, colorsOf(c).iconMuted).Role(ui.RoleImage).Label(platform.Name + " icon")
						}
						mobileListText(c, mobileListTextOptions{Title: entry.Name})
						ui.Row(c).Gap(5).Shrink(0).AlignItems(ui.Center).Children(func() {
							color := c.Theme().TextMuted
							if m.presence[entry.Link].state == desktopOnline {
								color = colorsOf(c).busy.Mix(color, 0.25)
							}
							ui.Box(c).Size(6, 6).Radius(3).Background(color)
							ui.Text(c, status).Label("设备状态 " + entry.Name + " " + status).FontSize(12).TextColor(c.Theme().TextMuted)
						})
						if !selecting {
							mobileListChevron(c)
						}
					})
					if row.Clicked() {
						if selecting {
							m.historySelection[entry.Link] = !checked
							c.Invalidate()
						} else {
							m.openDesktop(entry)
						}
					}
				}
			})
		})
		m.recentSessionsView(c)
		if m.error != "" {
			ui.Text(c, m.error).Key("connection-status").TextColor(c.Theme().Danger).FontSize(14).LineHeight(1.4)
		}
		ui.Text(c, "通过 Tailcat 加密连接").FontSize(12).TextColor(c.Theme().TextMuted).TextAlign(ui.Center).FillWidth()
	})
}

func (m *mobileApp) sessionsView(c *ui.Context) {
	ui.Row(c).Height(52).Padding(0, 8).AlignItems(ui.Center).Children(func() {
		if mobileBackButton(c).Clicked() {
			m.goHome()
		}
		ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).PassThrough().Children(func() {
			ui.Text(c, "会话").FontSize(17).Bold().PassThrough()
			ui.Text(c, fmt.Sprint(len(m.sessions))).FontSize(13).TextColor(c.Theme().TextMuted).PassThrough()
		})
		if ui.ButtonBase(c).Label("新建会话").Disabled(m.reconnecting).Role(ui.RoleButton).Size(44, 44).Children(func() {
			ui.Icon(c, icon("plus")).Size(23, 23).TextColor(c.Theme().Accent)
		}).Clicked() {
			m.creating = true
			m.directory = m.hello.Host.Home
			m.error = ""
		}
	})
	ui.Row(c).FillWidth().Height(44).Padding(0, 8, 0, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		statusColor := colorsOf(c).busy.Mix(colorsOf(c).textMuted, 0.3)
		status := "桌面已连接"
		if m.needsRecovery() || m.connectionIssue != nil {
			statusColor, status = colorsOf(c).attention, "桌面连接已中断"
		}
		ui.Box(c).Label(status).Size(6, 6).Shrink(0).Radius(3).Background(statusColor)
		ui.Text(c, m.hello.Host.Name).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if ui.ButtonBase(c).Label("显示与提醒设置").Role(ui.RoleButton).Size(44, 44).Children(func() {
			ui.Icon(c, icon("settings-2")).Size(17, 17).TextColor(c.Theme().TextMuted)
			if m.pushError != "" && !m.pushDisabled {
				ui.Box(c).Absolute().Right(9).Top(9).Size(5, 5).Radius(3).Background(colorsOf(c).attention)
			}
		}).Clicked() {
			m.sessionSettingsOpen = true
		}
	})
	ui.Scroll(c).Key("mobile-sessions").Grow(1).MinHeight(0).FillWidth().TrackScroll(&m.scroll).HideScrollbars().Padding(8, 16, 16, 16).Gap(12).Children(func() {
		m.connectionFeedback(c)
		if !m.needsRecovery() && m.connectionIssue == nil {
			m.errorView(c)
		}
		m.sessionList(c)
	})
}

func (m *mobileApp) createView(c *ui.Context) {
	m.header(c, "新建会话", func() {
		if !m.busy {
			m.creating = false
			mobile.HideKeyboard()
		}
	}, false)
	ui.Column(c).Padding(20).Gap(16).FillWidth().Children(func() {
		ui.Text(c, "工作目录").Bold()
		ui.TextInput(c, &m.directory).Label("工作目录").Placeholder(m.hello.Host.Home).FillWidth().Height(50).InputOptions(ui.InputOptions{Keyboard: ui.KeyboardText, Return: ui.ReturnDone, Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone})
		ui.Text(c, "在桌面电脑上启动默认 Shell。").FontSize(14).TextColor(c.Theme().TextMuted)
		if ui.PrimaryButton(c, "创建并打开").Height(50).FillWidth().Disabled(m.busy).Clicked() {
			mobile.HideKeyboard()
			m.create()
		}
		if m.busy {
			ui.Text(c, "正在创建…")
		}
	})
	m.errorView(c)
}

func (m *mobileApp) terminalView(c *ui.Context) {
	m.header(c, m.sessionTitle(m.selected), func() { m.resumeSID = ""; m.detach() }, true)
	if m.term == nil {
		c.Invalidate()
		return
	}
	m.term.SetInputEnabled(!m.reconnecting && !m.background && (m.stream == nil || m.stream.inputReady()))
	m.errorView(c)
	if m.term == nil {
		c.Invalidate()
		return
	}
	if m.selected.Exited {
		ui.Text(c, "会话已结束").FontSize(13).Padding(6, 16).TextColor(c.Theme().TextMuted)
	}
	if m.imagePasteBusy {
		ui.Text(c, "正在粘贴图片…").FontSize(13).Padding(6, 16).TextColor(c.Theme().TextMuted)
	}
	var element *ui.Element
	ui.Box(c).Grow(1).MinHeight(0).FillWidth().Padding(4).Background(c.Theme().Background).Children(func() {
		keyboard := ui.KeyboardText
		if m.keyboardModifiers != 0 {
			keyboard = ui.KeyboardASCII
		}
		element = terminal.View(c, m.term).Key("mobile-terminal").Fill().InputOptions(ui.InputOptions{Keyboard: keyboard, Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone, Dismiss: ui.KeyboardDismissOnDrag}).InputModifiers(m.keyboardModifiers, m.consumeKeyboardModifiers).InputAccessory(m.keyboardActions(), func(id string) { m.keyboardAction(c, id) })
	})
	if !element.Focused() {
		m.clearKeyboardModifiers()
	}
	// Non-iOS previews use the same actions with Go controls. On iPhone,
	// UIKit owns their keyboard-attached view and the full reading viewport.
	if runtime.GOOS != "ios" && element.Focused() {
		m.keyboardPreview(c)
	}
	if m.reconnecting || m.stream != nil && !m.stream.inputReady() {
		c.Blur()
	}
	m.selectionMenu(c, element)
}

func (m *mobileApp) selectionMenu(c *ui.Context, element *ui.Element) {
	anchor, ok := m.term.SelectionAnchor()
	if !ok {
		return
	}
	bounds, viewport := element.Bounds(), c.Root().Bounds()
	x := max(8, min(bounds.X+anchor.X-74, viewport.W-156))
	y := bounds.Y + anchor.Y - 52
	if y < bounds.Y {
		y = bounds.Y + anchor.Y + anchor.H + 8
	}
	y = max(0, min(y, viewport.H-48))
	ui.Overlay(c, func() {
		ui.Row(c).Key("mobile-selection-menu").Absolute().Left(x).Top(y).Size(148, 44).KeepFocus().Radius(12).Background(c.Theme().Surface).Shadow(0, 2, 12, 0, ui.RGBA(0, 0, 0, .18)).Children(func() {
			if ui.ButtonBase(c).Label("复制").Role(ui.RoleButton).KeepFocus().Height(44).Grow(1).Children(func() { ui.Text(c, "复制").FontSize(15) }).Clicked() {
				if text, ok := m.term.SelectedText(); ok {
					c.WriteClipboard(text)
				}
			}
			if ui.ButtonBase(c).Label("全选").Role(ui.RoleButton).KeepFocus().Height(44).Grow(1).Children(func() { ui.Text(c, "全选").FontSize(15) }).Clicked() {
				m.term.SelectAll()
			}
		})
	})
}
