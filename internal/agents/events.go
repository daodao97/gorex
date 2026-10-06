package agents

import (
	"encoding/json"
	"strings"
)

// HookInput is the small, common part of Claude and Codex lifecycle input.
// Prompt text, transcripts and tool arguments are deliberately not retained.
type HookInput struct {
	Event        string `json:"hook_event_name"`
	SessionID    string `json:"session_id"`
	CWD          string `json:"cwd,omitempty"`
	TurnID       string `json:"turn_id"`
	Tool         string `json:"tool_name"`
	ToolID       string `json:"tool_use_id"`
	Notification string `json:"notification_type"`
	Source       string `json:"source"`
	AgentID      string `json:"agent_id"`
}

const (
	Ready     = "ready"
	Running   = "running"
	Waiting   = "waiting"
	Completed = "completed"
	Failed    = "failed"
	Ended     = "ended"
)

// HookStatus only recognizes actual lifecycle events, never silence or
// arbitrary terminal output. Subagent lifecycle events cannot finish a tab.
func HookStatus(agent string, h HookInput) (state, reason string) {
	if (agent != "claude" && agent != "codex") || h.SessionID == "" || h.AgentID != "" {
		return "", ""
	}
	switch h.Event {
	case "SessionStart":
		if h.Source == "compact" {
			return "", ""
		}
		return Ready, ""
	case "UserPromptSubmit":
		return Running, ""
	case "PreToolUse":
		if h.Tool == "AskUserQuestion" || h.Tool == "request_user_input" {
			return Waiting, "question"
		}
		return Running, ""
	case "PermissionRequest":
		return Waiting, "permission"
	case "PostToolUse":
		return Running, ""
	case "Stop":
		return Completed, ""
	case "Interrupt":
		if agent == "codex" {
			return Ready, ""
		}
	case "SessionEnd":
		return Ended, ""
	}
	if agent == "claude" {
		switch h.Event {
		case "PostToolUseFailure", "ElicitationResult":
			return Running, ""
		case "Elicitation":
			return Waiting, "question"
		case "StopFailure":
			return Failed, ""
		case "Notification":
			if h.Notification == "permission_prompt" {
				return Waiting, "permission"
			}
			if h.Notification == "idle_prompt" {
				return Waiting, "input"
			}
		}
	}
	return "", ""
}

func ParseHook(agent string, data []byte) (HookInput, bool) {
	var h HookInput
	if json.Unmarshal(data, &h) != nil {
		return h, false
	}
	if len(h.CWD) > 4096 || strings.ContainsAny(h.CWD, "\x00\r\n") {
		return h, false
	}
	for _, value := range []string{h.Event, h.SessionID, h.TurnID, h.Tool, h.ToolID, h.Notification, h.Source, h.AgentID} {
		if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return h, false
		}
	}
	state, _ := HookStatus(agent, h)
	return h, state != ""
}

func HookEvents(agent string) []string {
	common := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "Stop", "SessionEnd"}
	if agent == "claude" {
		return append(common, "PostToolUseFailure", "Notification", "Elicitation", "ElicitationResult", "StopFailure")
	}
	if agent == "codex" {
		return append(common, "Interrupt")
	}
	return nil
}
