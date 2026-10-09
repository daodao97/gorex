package rex

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"retty/internal/agents"
)

func TestAgentHookCapturesTaskOnlyAfterSuccessfulReport(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{true: "accepted", false: "rejected"}[authorized], func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("RETTY_DIR", dir)
			t.Setenv("RETTY_SESSION", "pane")
			t.Setenv("RETTY_AGENT_TOKEN", "fixture-token")
			t.Setenv("RETTY_AGENT_SERVER_TOKEN", "")
			t.Setenv("RETTY_AGENT_SOCKET", SocketPath())
			listener, err := net.Listen("unix", SocketPath())
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			reported := make(chan Request, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				var req Request
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}
				reported <- req
				response := Response{ID: req.ID}
				if !authorized {
					response.Error = "invalid agent token"
				}
				json.NewEncoder(conn).Encode(response)
			}()
			RunAgentHook("codex", strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"thread","prompt":"让通知显示具体任务，删除项目路径"}`))
			select {
			case req := <-reported:
				if req.AgentEvent == nil || req.AgentEvent.Input.SessionID != "thread" {
					t.Fatal("hook did not report lifecycle metadata")
				}
				encoded, _ := json.Marshal(req.AgentEvent.Input)
				if strings.Contains(string(encoded), "prompt") {
					t.Fatal("prompt was included in the lifecycle protocol")
				}
			case <-time.After(time.Second):
				t.Fatal("hook did not report")
			}
			got := agents.ReadTask(dir, "codex", "thread", time.Now())
			if authorized && got != "让通知显示具体任务，删除项目路径" || !authorized && got != "" {
				t.Fatal("task was lost or saved for a rejected hook", got)
			}
		})
	}
}
