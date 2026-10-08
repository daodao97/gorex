package agents

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const TaskTextLimit = 160

// TaskText keeps a short, plain-text excerpt, never a transcript or tool input.
func TaskText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u202a' || r == '\u202b' || r == '\u202c' || r == '\u202d' || r == '\u202e' || r >= '\u2066' && r <= '\u2069' {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > TaskTextLimit {
		return string(runes[:TaskTextLimit-1]) + "…"
	}
	return text
}

type taskExcerpt struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func taskPath(dir, agent, session string) string {
	sum := sha256.Sum256([]byte(agent + "\x00" + session))
	return filepath.Join(dir, "agent-tasks", fmt.Sprintf("%x.json", sum[:]))
}

// SaveHookTask runs only after an authenticated lifecycle report succeeds.
// Keeping this small sidecar also works with already-running session daemons
// whose protocol intentionally carries lifecycle metadata only.
func SaveHookTask(dir, agent string, input HookInput, data []byte, at time.Time) error {
	if NormalizeHook(agent, input).Event != "UserPromptSubmit" || input.AgentID != "" || input.SessionID == "" {
		return nil
	}
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	text := TaskText(payload.Prompt)
	// Continuation/approval replies describe no new task. Keep the previous
	// request instead of notifying with just "继续" or "实现吧".
	switch strings.ToLower(strings.Trim(text, " 。.!！?？")) {
	case "继续", "继续吧", "实现吧", "好的", "好", "可以", "嗯", "ok", "yes", "continue", "go ahead":
		return nil
	}
	path := taskPath(dir, agent, input.SessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	saved, err := json.Marshal(taskExcerpt{Text: text, At: at})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".task-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(saved)
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), path)
}

// ReadTask cannot attribute a newer prompt to an older completion. Expired
// excerpts and missing metadata fall back to the terminal's task title.
func ReadTask(dir, agent, session string, updated time.Time) string {
	if agent == "" || session == "" || updated.IsZero() {
		return ""
	}
	f, err := os.Open(taskPath(dir, agent, session))
	if err != nil {
		return ""
	}
	defer f.Close()
	var saved taskExcerpt
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&saved) != nil || saved.At.IsZero() || saved.At.After(updated) || updated.Sub(saved.At) > 7*24*time.Hour {
		return ""
	}
	return TaskText(saved.Text)
}
