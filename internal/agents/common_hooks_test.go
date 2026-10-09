package agents

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestGeminiAndQwenLifecycleHooksAndTimeoutUnits(t *testing.T) {
	for _, test := range []struct{ agent, event, notification, state, reason string }{
		{"gemini", "BeforeAgent", "", Running, ""}, {"gemini", "AfterAgent", "", Completed, ""},
		{"gemini", "Notification", "ToolPermission", Waiting, "permission"}, {"gemini", "AfterTool", "", Running, ""},
		{"qwen", "UserPromptSubmit", "", Running, ""}, {"qwen", "PermissionRequest", "", Waiting, "permission"},
		{"qwen", "Stop", "", Completed, ""}, {"qwen", "Notification", "idle_prompt", Waiting, "input"},
	} {
		input := HookInput{SessionID: "thread", Event: test.event, Notification: test.notification}
		state, reason := HookStatus(test.agent, input)
		if state != test.state || reason != test.reason {
			t.Fatalf("%+v: %s %s", test, state, reason)
		}
		input.AgentID = "subagent"
		if state, _ := HookStatus(test.agent, input); state != "" {
			t.Fatal("subagent finished parent")
		}
	}
	if state, reason := HookStatus("gemini", HookInput{SessionID: "thread", Event: "BeforeTool", Tool: "ask_user"}); state != Waiting || reason != "question" {
		t.Fatal("Gemini question not recognized", state, reason)
	}
	for _, agent := range []string{"gemini", "qwen"} {
		original := []byte(`{"theme":"mine","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"user-hook"}]}]}}`)
		installed, e := MergeHooks(agent, original, true)
		if e != nil {
			t.Fatal(e)
		}
		var root map[string]any
		json.Unmarshal(installed, &root)
		groups := root["hooks"].(map[string]any)[HookEvents(agent)[0]].([]any)
		timeout := groups[len(groups)-1].(map[string]any)["hooks"].([]any)[0].(map[string]any)["timeout"].(float64)
		expected := float64(2)
		if agent == "gemini" {
			expected = 2000
		}
		if timeout != expected {
			t.Fatal("incorrect timeout unit", agent, timeout)
		}
		again, _ := MergeHooks(agent, installed, true)
		if !bytes.Equal(again, installed) {
			t.Fatal("duplicate hook installation")
		}
		removed, _ := MergeHooks(agent, installed, false)
		if !bytes.Contains(removed, []byte("user-hook")) || !bytes.Contains(removed, []byte("mine")) || bytes.Contains(removed, []byte("RETTY_HOOK")) {
			t.Fatal("removal changed foreign settings")
		}
	}
}
