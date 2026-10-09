package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/remote"
	"retty/internal/rex"
)

func TestMobileDesktopDiagnosticsIdentifyStalledRequestsAndProtocol(t *testing.T) {
	for _, stage := range []remote.ConnectionStage{remote.StageHello, remote.StageSessions, remote.StageProtocol, ""} {
		t.Run(string(stage), func(t *testing.T) {
			conn, peer := net.Pipe()
			client := rex.NewClient(conn, nil)
			defer client.Close()
			defer peer.Close()
			go func() {
				in, out := json.NewDecoder(peer), json.NewEncoder(peer)
				for {
					var req rex.Request
					if in.Decode(&req) != nil {
						return
					}
					if stage == remote.StageHello || stage == remote.StageSessions && req.Op == "list" {
						return
					}
					version := rex.ProtocolVersion
					if stage == remote.StageProtocol {
						version = 0
					}
					var data any = rex.Hello{Version: version}
					if req.Op == "list" {
						data = []rex.SessionInfo{{ID: "private-session", Title: "private-title"}}
					}
					body, _ := json.Marshal(data)
					if out.Encode(rex.Response{ID: req.ID, Data: body}) != nil {
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			trace := remote.NewDiagnostics(false, 1, time.Second)
			_, sessions, err := remote.ReadDesktop(ctx, client, rex.DeviceInfo{Name: "private-device", Push: nil}, trace)
			report := trace.Finish(err)
			if report.Stage != stage {
				t.Fatalf("stage %s, want %s", report.Stage, stage)
			}
			if stage == remote.StageHello || stage == remote.StageSessions {
				if !errors.Is(err, context.DeadlineExceeded) || remote.Failure(err) != remote.ConnectionTimeout {
					t.Fatal("deadline replaced by socket-close failure")
				}
			}
			if stage == remote.StageProtocol && remote.Failure(err) != remote.ProtocolMismatch {
				t.Fatal("protocol mismatch misclassified")
			}
			if stage == "" && (err != nil || len(sessions) != 1) {
				t.Fatal("successful handshake changed")
			}
			for _, secret := range []string{"private-session", "private-title", "private-device"} {
				if strings.Contains(report.Text, secret) {
					t.Fatal("metadata leaked")
				}
			}
		})
	}
}

func TestMobileDiagnosticsOnlyVisibleOnFailureAndCopyAtAllSizes(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{320, 568}, {390, 750}, {750, 390}} {
		m := &mobileApp{home: true}
		for i := 0; i < 7; i++ {
			trace := remote.NewDiagnostics(false, 1, 30*time.Second)
			var err error
			if i == 5 {
				err = trace.Measure(remote.StageTunnel, func() error { return context.DeadlineExceeded })
			}
			m.recordConnectionDiagnostic(trace.Finish(err))
		}
		if len(m.connectionDiagnostics) != 6 || !strings.Contains(m.connectionDiagnosticsText(), "失败阶段：建立加密隧道") {
			t.Fatal("success erased prior failure")
		}
		tt := ui.NewTester(m.view, size[0], size[1])
		if _, ok := tt.Find("查看连接诊断"); ok {
			t.Fatal("successful connection showed old diagnostics")
		}
		m.connectionIssue = connectionIssueFor(context.DeadlineExceeded)
		tt.Frame()
		tt.Click("查看连接诊断")
		tt.Frame()
		if !m.connectionDiagnosticsOpen {
			t.Fatal("failed connection did not expose diagnostics")
		}
		for _, label := range []string{"复制连接诊断", "关闭连接诊断"} {
			r, ok := tt.Find(label)
			if !ok || r.H < 44 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
				t.Fatalf("action %s clipped at %v: %v", label, size, r)
			}
		}
		tt.Click("复制连接诊断")
		tt.Frame()
		if tt.Clipboard() != m.connectionDiagnosticsText() || !tt.HasText("已复制") {
			t.Fatal("copy lost diagnostic history")
		}
		if size[0] == 390 {
			saveSettingsImage(t, tt, "mobile-connection-diagnostics")
		}
		tt.Click("关闭连接诊断")
		tt.Frame()
		if m.connectionDiagnosticsOpen {
			t.Fatal("close did not dismiss diagnostics")
		}
		tt.Click("查看连接诊断")
		tt.Frame()
		m.connectionIssue = nil // Recovery succeeds while diagnostics are open.
		tt.Frame()
		if m.connectionDiagnosticsOpen {
			t.Fatal("successful recovery left diagnostics open")
		}
		for _, label := range []string{"查看连接诊断", "连接诊断面板"} {
			if _, ok := tt.Find(label); ok {
				t.Fatal("successful recovery retained diagnostic UI", label)
			}
		}
	}
}

func TestMobileStageFailureMessage(t *testing.T) {
	err := &remote.ConnectionError{Kind: remote.ConnectionTimeout, Stage: remote.StageHello, Cause: fmt.Errorf("secret: %w", context.DeadlineExceeded)}
	issue := connectionIssueFor(err)
	if !strings.Contains(issue.body, string(remote.StageHello)) || strings.Contains(issue.body, "secret") {
		t.Fatal("stage hint unsafe or absent")
	}
}

func TestMobileDiagnosticsMergeEarlyURLAttemptWithStoredFailure(t *testing.T) {
	saved := remote.DiagnosticReport{Started: time.Unix(1, 0), Result: "连接超时", Text: "stored failure"}
	current := remote.DiagnosticReport{Started: time.Unix(2, 0), Result: "连接成功", Text: "early URL success"}
	m := &mobileApp{connectionDiagnostics: []remote.DiagnosticReport{current}}
	m.restoreConnectionDiagnostics([]remote.DiagnosticReport{saved})
	if len(m.connectionDiagnostics) != 2 || m.connectionDiagnostics[0].Text != saved.Text || m.connectionDiagnostics[1].Text != current.Text {
		t.Fatal("early URL completion erased saved failure")
	}
	m.restoreConnectionDiagnostics([]remote.DiagnosticReport{saved, current})
	if len(m.connectionDiagnostics) != 2 {
		t.Fatal("restore duplicated an attempt")
	}
}
