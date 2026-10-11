package main

import (
	"github.com/egoist/mygo/ui"
	"retty/internal/rex"
)

// Stream EOF has no process exit code. Ask the daemon before deciding whether
// to close a resumed pane; a CLI error must keep its output and retry action.
func (a *App) checkAgentRestoreExit(p *Pane, stream *rex.Stream) {
	p.streamEnded = true
	sid := p.SID
	go func() {
		infos, err := a.client.List()
		var info *rex.SessionInfo
		for _, in := range infos {
			if in.ID == sid {
				copy := in
				info = &copy
				break
			}
		}
		a.post(func() {
			if p.closed || a.quitting || p.stream != stream {
				return
			}
			if err == nil && info != nil && info.Exited && info.ExitCode == 0 {
				a.closePane(p)
				return
			}
			p.recoverySID = sid
			p.recoveryError = "Agent 恢复进程已退出，请检查终端输出后重试"
			if err != nil || info == nil || !info.Exited {
				p.recoveryError = "会话连接已断开，可重试连接原会话"
			}
			if info != nil {
				p.info = *info
			}
			a.changed()
		})
	}()
}

func (a *App) restoreAgentPane(saved *savedNode) *Pane {
	r, err := a.client.RestoreAgent(saved.SID, saved.Cols, saved.Rows, false)
	if err == nil && r.Session == nil && r.Error == "" {
		return nil
	}
	p := &Pane{ID: a.id(), startDir: saved.Dir, restored: true}
	if err != nil {
		r.Error = err.Error()
	}
	a.applyAgentRestore(p, saved.SID, saved.Cols, saved.Rows, r)
	return p
}

func (a *App) applyAgentRestore(p *Pane, sid string, cols, rows int, r rex.RestoreResult) {
	p.recoveryBusy = false
	if r.Session != nil {
		p.SID, p.info = r.Session.ID, *r.Session
		p.recoverySID, p.recoveryError = "", ""
		if r.Error != "" {
			p.recoverySID, p.recoveryError = sid, r.Error
		}
		a.attach(p, r.Session.Cols, r.Session.Rows)
		return
	}
	if r.Error == "" {
		r.Error = "原 Agent 会话已结束，无法恢复"
	}
	p.SID, p.recoverySID, p.recoveryError = "", sid, r.Error
	if r.Agent != "" {
		p.info.Program = r.Agent
	}
	p.info.Exited = true
	a.attach(p, cols, rows)
}

func (a *App) retryAgentRestore(p *Pane) {
	if p.closed || p.recoveryBusy || p.recoverySID == "" {
		return
	}
	sid := p.recoverySID
	cols, rows := 80, 24
	if p.term != nil {
		cols, rows = p.term.Size()
	}
	p.recoveryBusy = true
	go func() {
		r, err := a.client.RestoreAgent(sid, cols, rows, true)
		if err != nil {
			r.Error = err.Error()
		}
		a.post(func() {
			p.recoveryBusy = false
			// Ordinary GUI quit detaches and must preserve a restore already
			// started by the daemon. Only an explicit pane close ends it.
			if a.quitting {
				return
			}
			if p.closed {
				if r.Session != nil {
					go a.client.Kill(r.Session.ID)
				}
				return
			}
			a.closePaneTerminal(p)
			p.stream = nil
			a.applyAgentRestore(p, sid, cols, rows, r)
			a.focusReq = p
			a.changed()
		})
	}()
}

func (a *App) agentRecoveryBar(c *ui.Context, k *colors, p *Pane) {
	ui.Row(c).Height(30).Padding(0, 10).AlignItems(ui.Center).Gap(8).Background(k.panel).Children(func() {
		message := "恢复失败"
		if p.recoveryBusy {
			message = "正在恢复会话…"
		}
		ui.Text(c, message).FontSize(12).TextColor(k.textMuted).Grow(1).Tooltip(p.recoveryError)
		if !p.recoveryBusy && iconButton(c, k, "rotate-ccw", "重试恢复会话", 24, 13).Clicked() {
			a.retryAgentRestore(p)
		}
	})
}
