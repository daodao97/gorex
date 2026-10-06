package agents

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestHookWorkingDirectoryValidation(t *testing.T) {
	for _, tc := range []struct {
		cwd   string
		valid bool
	}{
		{"/work/project", true}, {strings.Repeat("a", 512), true},
		{strings.Repeat("a", 4097), false}, {"/work\x00project", false}, {"/work\nproject", false},
	} {
		data, _ := json.Marshal(HookInput{SessionID: "thread", Event: "SessionStart", CWD: tc.cwd})
		h, valid := ParseHook("codex", data)
		if valid != tc.valid || (valid && h.CWD != tc.cwd) {
			t.Fatal("incorrect cwd validation")
		}
	}
}

func TestHookLifecycleInputs(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		for _, tc := range []struct{ event, tool, state, reason string }{
			{"SessionStart", "", Ready, ""}, {"UserPromptSubmit", "", Running, ""},
			{"PreToolUse", "Bash", Running, ""}, {"PermissionRequest", "Bash", Waiting, "permission"},
			{"PreToolUse", "request_user_input", Waiting, "question"},
			{"PreToolUse", "AskUserQuestion", Waiting, "question"},
			{"PostToolUse", "Bash", Running, ""}, {"Stop", "", Completed, ""}, {"SessionEnd", "", Ended, ""},
		} {
			t.Run(agent+"/"+tc.event+"/"+tc.tool, func(t *testing.T) {
				data := []byte(fmt.Sprintf(`{"session_id":"main","hook_event_name":%q,"tool_name":%q,"prompt":"not retained"}`, tc.event, tc.tool))
				h, valid := ParseHook(agent, data)
				if !valid {
					t.Fatal("valid lifecycle input rejected")
				}
				if state, reason := HookStatus(agent, h); state != tc.state || reason != tc.reason {
					t.Fatalf("%s/%s", state, reason)
				}
			})
		}
	}
	for _, data := range []string{
		`{"session_id":"main","hook_event_name":"SubagentStop"}`,
		`{"session_id":"main","hook_event_name":"Stop","agent_id":"child"}`,
		`{"session_id":"main","hook_event_name":"SessionStart","source":"compact"}`,
		`{"session_id":"main","hook_event_name":"Notification","notification_type":"auth_success"}`,
		`{"hook_event_name":"Stop"}`, `broken`, `null`,
	} {
		if _, valid := ParseHook("claude", []byte(data)); valid {
			t.Fatalf("misleading event accepted: %s", data)
		}
	}
	for _, kind := range []string{"permission_prompt", "idle_prompt"} {
		h := HookInput{SessionID: "main", Event: "Notification", Notification: kind}
		if state, _ := HookStatus("claude", h); state != Waiting {
			t.Fatal(kind)
		}
		if state, _ := HookStatus("codex", h); state != "" {
			t.Fatal("Codex has no Notification event")
		}
	}
}
