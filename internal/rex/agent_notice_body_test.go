package rex

import (
	"strings"
	"testing"
	"time"

	"gorex/internal/agents"
)

func TestAgentNoticeBodyNamesTaskAndOmitsProjectPath(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	s := SessionInfo{Title: "⠏ 优化手机端连接保持", Dir: "/Users/private/project", Program: "codex", Agent: AgentState{ID: "codex", SessionID: "thread", State: agents.Completed, Updated: now}}
	if got := AgentNoticeBody(dir, s); got != "任务：优化手机端连接保持" || strings.Contains(got, s.Dir) {
		t.Fatal("task title was lost or replaced by its path", got)
	}
	err := agents.SaveHookTask(dir, "codex", agents.HookInput{Event: "UserPromptSubmit", SessionID: "thread"}, []byte(`{"prompt":"通知显示具体任务，不需要项目路径"}`), now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got := AgentNoticeBody(dir, s); got != "任务：通知显示具体任务，不需要项目路径" {
		t.Fatal("notice used an old session title instead of the latest task", got)
	}
	// Older completions cannot accidentally describe the next user's request.
	s.Agent.Updated = now.Add(-2 * time.Second)
	if got := AgentNoticeBody(dir, s); got != "任务：优化手机端连接保持" {
		t.Fatal("older completion was labeled with a newer task", got)
	}
}

func TestAgentNoticeBodyFallsBackHonestlyWithoutTaskMetadata(t *testing.T) {
	for _, title := range []string{"Codex", "Claude Code", "/Users/private/project", "~/work/project", `C:\work\project`, ""} {
		s := SessionInfo{Title: title, Dir: "/Users/private/project", Program: "codex", Agent: AgentState{State: agents.Waiting, Reason: "question"}}
		if got := AgentNoticeBody(t.TempDir(), s); got != "需要你回答一个问题，回答后继续任务。" {
			t.Fatal("generic executable or path was presented as a task", got)
		}
	}
}
