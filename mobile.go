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
	"gorex/internal/agents"
	"gorex/internal/mobile"
	"gorex/internal/remote"
	"gorex/internal/rex"
	"gorex/internal/terminal"
)

type mobileApp struct {
	win                                     *mygo.Window
	client                                  *rex.Client
	closeTunnel                             func()
	cancel                                  context.CancelFunc
	generation                              int
	hello                                   rex.Hello
	sessions                                []rex.SessionInfo
	term                                    *terminal.Terminal
	stream                                  *mobileStream
	overview                                bool
	selected                                rex.SessionInfo
	link, error                             string
	history                                 []desktopRecent
	recentSessions                          []mobileRecentSession
	historyEpoch                            int
	home                                    bool
	pollCancel                              context.CancelFunc
	backgroundTimer                         *time.Timer
	backgroundAt                            time.Time
	busy, scanning, creating, focusTerminal bool
	directory                               string
	resumeLink, resumeSID                   string
	storage                                 chan func()
	store                                   *mygo.SecureStore
	scroll                                  ui.ScrollState
	navigation                              *ui.Router
	navigationPage                          string
	keyboardMore                            bool
	background                              bool
	presence                                map[string]desktopPresence
	presenceCancel                          context.CancelFunc
	presenceEpoch                           int
	reconnecting                            bool
	retryAttempt                            int
	retryTimer                              *time.Timer
	sessionPreferences                      map[string]mobileSessionPreference
	preferenceEpoch                         int
	editingSession                          string
	editingOpen                             bool
	editingName                             string
	editingPinned                           bool
	agentPrevious                           map[string]rex.SessionInfo
	notificationDenied                      bool
	agentNotify                             func(mobileAgentNotice)
	pendingDesktop, pendingSession          string
	pushDeviceID, pushToken, pushError      string
	pushDisabled                            bool
	pushRequesting                          bool
	pushSnapshot                            atomic.Pointer[rex.DeviceInfo]
	noticeReceipts                          map[string]time.Time
	noticeReceiptsLoaded                    bool
	preferenceTouched                       map[string]bool
	imagePasteBusy                          bool
	imagePasteCancel                        context.CancelFunc
	imagePasteEpoch                         int
}

type desktopRecent struct {
	Link string
	Name string
	ID   string `json:",omitempty"`
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
	seen := make(map[string]bool)
	for _, entries := range [][]desktopRecent{first, second} {
		for _, entry := range entries {
			if _, err := remote.ParseLink(entry.Link); err != nil || seen[entry.Link] {
				continue
			}
			if strings.TrimSpace(entry.Name) == "" {
				entry.Name = "桌面"
			}
			duplicate := false
			for i, kept := range history {
				if sameDesktop(entry, kept) {
					// Migrate older records without IDs while keeping the newest URL.
					if kept.ID == "" || strings.HasPrefix(entry.ID, "machine:") && !strings.HasPrefix(kept.ID, "legacy:") {
						history[i].ID = entry.ID
					}
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			seen[entry.Link] = true
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
	m := &mobileApp{storage: make(chan func(), 32)}
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
			if saved, err := m.store.Get("recent-sessions"); err == nil {
				json.Unmarshal(saved, &recentSessions)
			}
			if saved, err := m.store.Get("history"); err == nil {
				json.Unmarshal(saved, &history)
			}
			if len(history) == 0 {
				if saved, err := m.store.Get("recent"); err == nil {
					history = []desktopRecent{{Link: string(saved), Name: "桌面"}}
				}
			}
			mygo.RunOnMain(func() {
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
	m.cancelImagePaste()
	m.keyboardMore = false
	if m.term != nil {
		m.term.Close()
		m.term = nil
	}
	m.stream = nil
	m.selected = rex.SessionInfo{}
	mobile.HideKeyboard()
}

func (m *mobileApp) disconnect(forget bool) {
	m.stopRecentPresence()
	m.stopBackgroundTimer()
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
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.detach()
	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	if m.closeTunnel != nil {
		close := m.closeTunnel
		m.closeTunnel = nil
		go close()
	}
	m.busy, m.creating = false, false
	m.sessions = nil
	m.resumeLink, m.resumeSID = "", ""
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
		m.error = err.Error()
		m.invalidate()
		return
	}
	link := remote.Link(addr)
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
	if m.background {
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
			err = fmt.Errorf("请更新桌面端 GoRex 后重新连接")
		}
		if err == nil {
			sessions, err = client.List()
		}
		stop()
		if err == nil {
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
					m.busy = false
					m.cancel = nil
					m.error = err.Error()
					if recovering {
						m.scheduleRetry()
					}
					log.Printf("GoRex desktop connection failed: %s", m.error)
					m.invalidate()
				}
				return
			}
			m.client, m.closeTunnel, m.hello = client, closeTunnel, hello
			m.reconnecting, m.retryAttempt = false, 0
			currentDesktop := desktopRecent{Link: m.link, Name: m.hello.Host.Name, ID: m.hello.Host.ID}
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
						m.stream.replace(client.ViewStream(session.ID))
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
			var sessions []rex.SessionInfo
			var err error
			if info := m.pushSnapshot.Load(); info != nil {
				sessions, err = client.ListFrom(*info)
			} else {
				sessions, err = client.List()
			}
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
								m.openSession(s)
							} else {
								m.selected = s
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
	m.detach()
	var stream *mobileStream
	stream = newMobileStream(m.client.ViewStream(s.ID), func() {
		mygo.RunOnMain(func() {
			if m.stream == stream && !stream.hasTransport() {
				if m.selected.Exited {
					stream.Close()
					return
				}
				m.connectionLost()
			}
		})
	})
	stream.geometry(s.Cols, s.Rows)
	term, err := terminal.New(terminal.Options{Conn: stream, FixedCols: max(s.Cols, 1), FixedRows: max(s.Rows, 1), ReflowView: true, FitToView: m.overview, Font: terminal.Font{Family: termFont.Family, Size: 13, LineHeight: 1.2}, Theme: lightTerm, DarkTheme: darkTerm, AdaptiveColors: true, SelectOnDrag: true, CopyRawText: true, ActiveCursor: true, OnPaste: m.pasteClipboard})
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
	if strings.TrimSpace(s.Title) != "" {
		return s.Title
	}
	if s.Program != "" {
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
	m.navigation.InteractiveBack = !m.busy && !m.scanning
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

func (m *mobileApp) header(c *ui.Context, title string, back func(), terminalPage bool) {
	ui.Row(c).Height(52).Padding(0, 8).AlignItems(ui.Center).Children(func() {
		if back != nil {
			button := ui.ButtonBase(c).Key("mobile-back").Label("返回").Role(ui.RoleButton).Size(44, 44).Radius(12)
			if button.Pressed() {
				button.Background(c.Theme().SurfacePressed)
			}
			button.Children(func() { ui.Icon(c, icon("chevron-left")).Size(24, 24).TextColor(c.Theme().Accent) })
			if button.Clicked() {
				back()
			}
		} else {
			ui.Box(c).Size(44, 44).Shrink(0)
		}
		ui.Text(c, title).FontSize(17).Bold().TextAlign(ui.Center).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if terminalPage && m.term != nil {
			if ui.ButtonBase(c).Key("mobile-keyboard").Label("键盘").Role(ui.RoleButton).KeepFocus().Size(44, 44).Children(func() {
				ui.Icon(c, icon("keyboard")).Size(22, 22).TextColor(c.Theme().Accent)
			}).Clicked() {
				m.focusTerminal = true
				c.Invalidate()
			}
			label := "完整"
			if m.overview {
				label = "适应"
			}
			if ui.ButtonBase(c).Label(label).Role(ui.RoleButton).KeepFocus().Size(44, 44).Children(func() {
				ui.Text(c, label).FontSize(14).TextColor(c.Theme().Accent)
			}).Clicked() {
				m.overview = !m.overview
				m.term.SetFitToView(m.overview)
			}
		} else {
			ui.Box(c).Size(44, 44).Shrink(0)
		}
	})
}

func (m *mobileApp) errorView(c *ui.Context) {
	if m.reconnecting {
		ui.Row(c).FillWidth().Padding(0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "正在重连…").Label("正在重连").FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
			if mobileTextAction(c, "取消重连", "取消").Clicked() {
				m.disconnect(false)
			}
		})
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
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(40, 40).Shrink(0).Radius(11).Background(c.Theme().Surface).Center().Children(func() {
				ui.Icon(c, icon("terminal")).Size(22, 22).TextColor(c.Theme().Accent)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, "连接桌面").FontSize(20).Bold()
				ui.Text(c, "电脑上的 GoRex · 设置 → 连接").FontSize(13).TextColor(c.Theme().TextMuted)
			})
			if m.client != nil && mobileTextAction(c, "断开桌面连接", "断开").Disabled(m.busy || m.reconnecting).Clicked() {
				m.disconnect(false)
			}
		})
		if ui.PrimaryButton(c, "").Label("扫码连接桌面").Role(ui.RoleButton).Height(48).FillWidth().Disabled(m.busy || m.scanning || m.reconnecting).Children(func() {
			ui.Icon(c, icon("scan-line")).Size(20, 20)
			ui.Text(c, "扫码连接桌面").FontSize(16)
		}).Clicked() {
			m.scan()
		}
		if m.reconnecting {
			m.errorView(c)
		} else if m.busy {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "正在连接桌面…").Label("正在连接桌面").FontSize(14).TextColor(c.Theme().TextMuted).Grow(1)
				if mobileTextAction(c, "取消连接", "取消").Clicked() {
					m.disconnect(false)
				}
			})
		}
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).FillWidth().Height(44).Padding(0, 4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "最近连接").FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
				if len(m.history) > 0 && mobileTextAction(c, "清除连接记录", "清除").Disabled(m.busy).Clicked() {
					m.disconnect(true)
				}
			})
			ui.Column(c).FillWidth().Radius(12).Clip().Background(c.Theme().Surface).Children(func() {
				if len(m.history) == 0 {
					ui.Text(c, "连接过的桌面会显示在这里").FontSize(14).TextColor(c.Theme().TextMuted).Padding(16)
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
					row := mobileListRow(c, "desktop-"+entry.Link, label, 56).Value(status).Disabled(m.busy || m.scanning || m.reconnecting)
					row.Children(func() {
						mobileListIcon(c, "monitor")
						ui.Text(c, entry.Name).FontSize(15).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						ui.Row(c).Gap(5).Shrink(0).AlignItems(ui.Center).Children(func() {
							color := c.Theme().TextMuted
							if m.presence[entry.Link].state == desktopOnline {
								color = colorsOf(c).busy.Mix(color, 0.25)
							}
							ui.Box(c).Size(6, 6).Radius(3).Background(color)
							ui.Text(c, status).Label("设备状态 " + entry.Name + " " + status).FontSize(12).TextColor(c.Theme().TextMuted)
						})
						ui.Icon(c, icon("chevron-right")).Size(16, 16).TextColor(colorsOf(c).iconMuted)
					})
					if row.Clicked() {
						m.openDesktop(entry)
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

func mobileTextAction(c *ui.Context, label, text string) *ui.Element {
	return ui.ButtonBase(c).Label(label).Role(ui.RoleButton).MinWidth(44).Height(44).Padding(0, 8).Children(func() {
		ui.Text(c, text).FontSize(14).TextColor(c.Theme().Accent)
	})
}

func mobileListRow(c *ui.Context, key, label string, height float32) *ui.Element {
	row := ui.ButtonBase(c).Key(key).Label(label).Role(ui.RoleButton).FillWidth().Height(height).Padding(0, 12).Gap(12)
	if row.Pressed() {
		row.Background(c.Theme().SurfacePressed)
	}
	return row
}

func mobileListIcon(c *ui.Context, name string) {
	ui.Box(c).Size(32, 32).Shrink(0).Center().Children(func() {
		ui.Icon(c, icon(name)).Size(21, 21).TextColor(colorsOf(c).iconMuted)
	})
}

func mobileListDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Margin(0, 0, 0, 56).Background(colorsOf(c).hover)
}

func (m *mobileApp) sessionsView(c *ui.Context) {
	ui.Row(c).Height(52).Padding(0, 8).AlignItems(ui.Center).Children(func() {
		if ui.ButtonBase(c).Label("返回").Role(ui.RoleButton).Size(44, 44).Children(func() {
			ui.Icon(c, icon("chevron-left")).Size(24, 24).TextColor(c.Theme().Accent)
		}).Clicked() {
			m.goHome()
		}
		ui.Text(c, "会话").FontSize(17).Bold().TextAlign(ui.Center).Grow(1)
		if ui.ButtonBase(c).Label("新建会话").Disabled(m.reconnecting).Role(ui.RoleButton).Size(44, 44).Children(func() {
			ui.Icon(c, icon("plus")).Size(23, 23).TextColor(c.Theme().Accent)
		}).Clicked() {
			m.creating = true
			m.directory = m.hello.Host.Home
			m.error = ""
		}
	})
	ui.Row(c).FillWidth().Padding(4, 20, 12, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Shrink(0).Radius(3).Background(colorsOf(c).busy.Mix(colorsOf(c).textMuted, 0.3))
		ui.Text(c, m.hello.Host.Name).FontSize(14).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		ui.Text(c, fmt.Sprintf("%d 个会话", len(m.sessions))).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
		ui.Text(c, "提醒").FontSize(13).TextColor(c.Theme().TextMuted)
		enabled := !m.pushDisabled
		if ui.Switch(c, &enabled).Label("后台任务提醒").Changed() {
			m.setPushEnabled(enabled)
		}
	})
	if m.pushError != "" && !m.pushDisabled {
		ui.Row(c).FillWidth().Padding(0, 20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.pushError).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
			if m.notificationDenied && mobileTextAction(c, "打开通知设置", "设置").Clicked() {
				go mygo.Permissions.OpenSettings()
			}
		})
	}
	m.errorView(c)
	ui.Scroll(c).Key("mobile-sessions").Grow(1).MinHeight(0).FillWidth().TrackScroll(&m.scroll).HideScrollbars().Padding(0, 16, 16, 16).Children(func() {
		ui.Column(c).FillWidth().Radius(12).Clip().Background(c.Theme().Surface).Children(func() {
			if len(m.sessions) == 0 {
				ui.Column(c).FillWidth().Padding(24, 16).Gap(6).Children(func() {
					ui.Text(c, "暂无会话").FontSize(16)
					ui.Text(c, "轻点右上角 ＋ 新建终端").FontSize(14).TextColor(c.Theme().TextMuted)
				})
			}
			for i, session := range m.orderedSessions() {
				s := session
				if i > 0 {
					mobileListDivider(c)
				}
				row := mobileListRow(c, "session-"+s.ID, "打开会话 "+s.ID, 72).Disabled(m.reconnecting).Value(m.sessionValue(s)).TouchSelection().HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputLongPress {
						m.editSession(s)
						c.Invalidate()
						return true
					}
					return false
				})
				row.Children(func() {
					glyph := "terminal"
					if agent, ok := agents.Detect(s.Program, s.Args); ok {
						glyph = programOf(agent.ID).Glyph
					}
					mobileListIcon(c, glyph)
					ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, m.sessionTitle(s)).FontSize(16).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							if m.preference(s.ID).Pinned {
								ui.Text(c, "置顶").FontSize(11).TextColor(c.Theme().TextMuted)
							}
							color := c.Theme().TextMuted
							if sessionAgentState(s).State == "waiting" {
								color = colorsOf(c).attention
							}
							ui.Text(c, m.sessionStatus(s)).FontSize(12).TextColor(color).Shrink(0)
						})
						dir := s.Dir
						if home := strings.TrimRight(m.hello.Host.Home, "/\\"); home != "" && (dir == home || strings.HasPrefix(dir, home+"/") || strings.HasPrefix(dir, home+"\\")) {
							dir = "~" + strings.TrimPrefix(dir, home)
						}
						ui.Text(c, dir).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…")
					})
					ui.Icon(c, icon("chevron-right")).Size(16, 16).TextColor(colorsOf(c).iconMuted)
				})
				if row.Clicked() && !m.editingOpen {
					m.openSession(s)
				}
			}
		})
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
		element = terminal.View(c, m.term).Key("mobile-terminal").Fill().Disabled(m.reconnecting).InputOptions(ui.InputOptions{Keyboard: ui.KeyboardText, Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone, Dismiss: ui.KeyboardDismissOnDrag}).InputAccessory(mobileKeyboardActions, func(id string) { m.keyboardAction(c, id) })
		if m.focusTerminal {
			element.Focus()
			m.focusTerminal = false
		}
	})
	// Non-iOS previews use the same actions with Go controls. On iPhone,
	// UIKit owns their keyboard-attached view and the full reading viewport.
	if runtime.GOOS != "ios" && element.Focused() {
		m.keyboardPreview(c)
	}
	if m.reconnecting {
		c.Blur()
	}
	m.selectionMenu(c, element)
}

var mobileKeyboardActions = []ui.InputAction{
	{ID: "escape", Label: "Esc"}, {ID: "tab", Label: "Tab"}, {ID: "interrupt", Label: "Ctrl+C"},
	{ID: "up", Label: "↑", Symbol: "arrow.up"}, {ID: "down", Label: "↓", Symbol: "arrow.down"},
	{ID: "more", Label: "更多", Symbol: "ellipsis", Items: []ui.InputAction{
		{ID: "left", Label: "←", Symbol: "arrow.left"}, {ID: "right", Label: "→", Symbol: "arrow.right"},
		{ID: "eof", Label: "Ctrl+D"}, {ID: "search", Label: "Ctrl+R"}, {ID: "paste", Label: "粘贴"},
		{ID: "/", Label: "/"}, {ID: "-", Label: "-"}, {ID: "|", Label: "|"}, {ID: "~", Label: "~"}, {ID: "\\", Label: "\\"},
	}},
	{ID: "dismiss", Label: "收起", Symbol: "keyboard.chevron.compact.down"},
}

func (m *mobileApp) keyboardAction(c *ui.Context, id string) {
	if m.term == nil || m.reconnecting {
		return
	}
	if data, ok := map[string]string{"escape": "\x1b", "tab": "\t", "interrupt": "\x03", "up": "\x1b[A", "down": "\x1b[B", "left": "\x1b[D", "right": "\x1b[C", "eof": "\x04", "search": "\x12", "/": "/", "-": "-", "|": "|", "~": "~", "\\": "\\"}[id]; ok {
		m.term.Send([]byte(data))
		return
	}
	switch id {
	case "paste":
		m.pasteClipboard(c)
	case "more":
		m.keyboardMore = !m.keyboardMore
	case "dismiss":
		m.keyboardMore = false
		c.Blur()
		mobile.HideKeyboard()
	}
	c.Invalidate()
}

func (m *mobileApp) keyboardPreview(c *ui.Context) {
	button := func(action ui.InputAction) {
		b := ui.ButtonBase(c).Label(action.Label).Role(ui.RoleButton).KeepFocus().Height(44).MinWidth(44).Grow(1).Shrink(1).Radius(8)
		if b.Pressed() {
			b.Background(c.Theme().SurfacePressed)
		}
		b.Children(func() { ui.Text(c, action.Label).FontSize(14) })
		if b.Clicked() {
			m.keyboardAction(c, action.ID)
		}
	}
	ui.Column(c).FillWidth().Padding(0, 8).Background(c.Theme().Surface).Children(func() {
		if m.keyboardMore {
			items := mobileKeyboardActions[5].Items
			for i := 0; i < len(items); i += 5 {
				ui.Row(c).FillWidth().Gap(2).Children(func() {
					for _, action := range items[i:min(i+5, len(items))] {
						button(action)
					}
				})
			}
		}
		ui.Row(c).FillWidth().Gap(2).Children(func() {
			for _, action := range mobileKeyboardActions {
				button(action)
			}
		})
	})
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
