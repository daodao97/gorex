package main

import (
	"log"

	"github.com/egoist/mygo/ui"
	"gorex/internal/remote"
)

type mobileConnectionIssue struct {
	title, body      string
	automatic, retry bool
}

func connectionIssueFor(err error) *mobileConnectionIssue {
	issue := &mobileConnectionIssue{title: "暂时无法连接桌面", body: "请确认电脑上的 GoRex 正在运行，并检查两端网络。若电脑曾停止连接，请重新扫码。", automatic: true, retry: true}
	switch remote.Failure(err) {
	case remote.InvalidLink:
		issue.title, issue.body = "连接码无法识别", "请在电脑的 GoRex「设置 → 连接」中显示二维码，重新扫描。"
		issue.automatic, issue.retry = false, false
	case remote.NetworkUnavailable:
		issue.title, issue.body = "当前网络不可用", "请检查 Wi-Fi、蜂窝网络，以及 GoRex 的联网权限，再重试。"
	case remote.RelayUnavailable:
		issue.title, issue.body = "无法访问连接服务", "请检查当前网络或 VPN，再重试。"
	case remote.ConnectionTimeout:
		issue.title = "连接超时"
	case remote.ProtocolMismatch:
		issue.title, issue.body = "桌面版本不兼容", "请更新电脑上的 GoRex，再重试连接。"
		issue.automatic = false
	}
	return issue
}

func (m *mobileApp) connectionFailed(err error, recovering bool) {
	m.busy, m.cancel, m.error = false, nil, ""
	m.connectionIssue = connectionIssueFor(err)
	log.Printf("mobile connect: %s", m.connectionIssue.title)
	if recovering {
		m.scheduleRetry()
	}
	m.invalidate()
}

func (m *mobileApp) retryConnection() {
	if m.background || m.busy || m.link == "" {
		return
	}
	recovering := m.reconnecting || m.term != nil || m.client != nil
	m.pauseConnection()
	m.reconnecting = recovering
	m.connectionIssue, m.connectionDetailsOpen = nil, false
	m.retryAttempt, m.error = 0, ""
	m.startConnection(recovering)
}

func (m *mobileApp) rescanConnection() {
	m.disconnect(false)
	m.scan()
}

func (m *mobileApp) needsRecovery() bool {
	return m.reconnecting || m.stream != nil && !m.stream.inputReady()
}

func (m *mobileApp) recoveryAnimating() bool {
	return !m.background && m.needsRecovery() && (m.connectionIssue == nil || m.connectionIssue.automatic)
}

func (m *mobileApp) recoveryStatus() string {
	if m.connectionIssue != nil && !m.connectionIssue.automatic {
		return m.connectionIssue.title
	}
	if !m.reconnecting {
		return "正在载入会话…"
	}
	return "正在重连…"
}

// Inline feedback is used on the home/session pages. The terminal keeps its
// viewport unchanged and exposes the same actions through its fixed header.
func (m *mobileApp) connectionFeedback(c *ui.Context) {
	if m.connectionIssue == nil && !m.needsRecovery() {
		return
	}
	title, body := "正在恢复连接", "恢复后将继续当前会话，可先阅读已有内容。"
	if m.connectionIssue != nil {
		title, body = m.connectionIssue.title, m.connectionIssue.body
	}
	mobileCard(c).Key("connection-feedback").Label("连接状态").Padding(12).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if m.recoveryAnimating() {
				mobileReconnectIcon(c, 14, c.Theme().Accent)
			}
			ui.Text(c, title).FontSize(14).Bold().Grow(1)
		})
		ui.Text(c, body).FontSize(13).LineHeight(1.4).TextColor(c.Theme().TextMuted)
		m.connectionActions(c)
	})
}

func (m *mobileApp) connectionActions(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(4).Children(func() {
		if (m.connectionIssue == nil || m.connectionIssue.retry) && m.link != "" {
			if mobileIconAction(c, "重试连接", "rotate-ccw").Disabled(m.busy).Clicked() {
				m.retryConnection()
			}
		}
		if mobileIconAction(c, "重新扫码连接", "scan-line").Disabled(m.scanning).Clicked() {
			m.rescanConnection()
		}
		if m.needsRecovery() && mobileIconAction(c, "取消重连", "x").Clicked() {
			m.disconnect(false)
		}
	})
}

func (m *mobileApp) connectionDialog(c *ui.Context) {
	ui.DialogBase(c, &m.connectionDetailsOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, .35))
		panel.Width(340).MaxWidthPercent(95).Padding(20).Radius(18).Background(c.Theme().Surface).Column().Gap(12)
		title, body := "正在恢复连接", "恢复后将继续当前会话，可先阅读已有内容。"
		closeText := "继续阅读"
		if m.term == nil {
			title, body = "正在重新连接", "会话列表已保留，连接恢复后即可打开会话。"
			closeText = "返回列表"
		}
		if m.connectionIssue != nil {
			title, body = m.connectionIssue.title, m.connectionIssue.body
		}
		ui.Text(c, title).FontSize(18).Bold()
		ui.Text(c, body).FontSize(14).LineHeight(1.4).TextColor(c.Theme().TextMuted)
		m.connectionActions(c)
		if ui.Button(c, closeText).Label("关闭连接提示").FillWidth().Height(44).Clicked() {
			m.connectionDetailsOpen = false
		}
	})
}
