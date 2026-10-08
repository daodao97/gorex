package agents

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHookTaskKeepsOnlyBoundedLatestRequestPerConversation(t *testing.T) {
	for _, agent := range Integrated {
		t.Run(agent, func(t *testing.T) {
			dir, now := t.TempDir(), time.Now()
			event := "UserPromptSubmit"
			if agent == "gemini" {
				event = "BeforeAgent"
			}
			h := HookInput{Event: event, SessionID: "thread/../one"}
			data, _ := json.Marshal(map[string]any{"prompt": "\n修复通知文案\t显示具体任务 " + strings.Repeat("字", 200) + "secret-tail", "tool_input": "unrelated-secret"})
			if err := SaveHookTask(dir, agent, h, data, now); err != nil {
				t.Fatal(err)
			}
			got := ReadTask(dir, agent, h.SessionID, now.Add(time.Second))
			if !strings.HasPrefix(got, "修复通知文案 显示具体任务") || len([]rune(got)) != TaskTextLimit || !strings.HasSuffix(got, "…") {
				t.Fatal("excerpt lost Chinese text or its length bound", got)
			}
			path := taskPath(dir, agent, h.SessionID)
			saved, _ := os.ReadFile(path)
			if strings.Contains(string(saved), "secret-tail") || strings.Contains(string(saved), "unrelated-secret") {
				t.Fatal("full prompt or tool input was saved")
			}
			stat, _ := os.Stat(path)
			if stat.Mode().Perm() != 0600 {
				t.Fatal("task excerpt is not private")
			}
			if ReadTask(dir, agent, "another-thread", now) != "" || ReadTask(t.TempDir(), agent, h.SessionID, now) != "" || ReadTask(dir, agent, h.SessionID, now.Add(-time.Second)) != "" {
				t.Fatal("task leaked across conversations, profiles or older turns")
			}
			if err := SaveHookTask(dir, agent, h, []byte(`{"prompt":"添加移动端最近会话"}`), now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if got := ReadTask(dir, agent, h.SessionID, now.Add(2*time.Second)); got != "添加移动端最近会话" {
				t.Fatal("new request did not replace previous task", got)
			}
			SaveHookTask(dir, agent, h, []byte(`{"prompt":"继续吧。"}`), now.Add(3*time.Second))
			if got := ReadTask(dir, agent, h.SessionID, now.Add(4*time.Second)); got != "添加移动端最近会话" {
				t.Fatal("continuation reply lost the concrete task", got)
			}
		})
	}
}

func TestTaskIgnoresToolAndSubagentPromptsAndExpires(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	for _, h := range []HookInput{{Event: "PreToolUse", SessionID: "thread"}, {Event: "UserPromptSubmit", SessionID: "thread", AgentID: "child"}} {
		SaveHookTask(dir, "claude", h, []byte(`{"prompt":"not the user's task"}`), now)
		if got := ReadTask(dir, "claude", "thread", now); got != "" {
			t.Fatal("tool or subagent changed the main task", got)
		}
	}
	SaveHookTask(dir, "claude", HookInput{Event: "UserPromptSubmit", SessionID: "thread"}, []byte(`{"prompt":"task"}`), now.Add(-8*24*time.Hour))
	if ReadTask(dir, "claude", "thread", now) != "" {
		t.Fatal("expired task was attributed to a new completion")
	}
}
