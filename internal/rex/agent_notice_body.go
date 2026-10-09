package rex

import (
	"strings"
	"unicode"

	"retty/internal/agents"
)

// AgentNoticeBody names the task instead of displaying project paths or
// generic navigation instructions. Local notifications and APNs share it.
func AgentNoticeBody(dir string, session SessionInfo) string {
	task := agents.ReadTask(dir, session.Agent.ID, session.Agent.SessionID, session.Agent.Updated)
	if task == "" {
		task = strings.TrimLeftFunc(session.Title, func(r rune) bool {
			return unicode.IsSpace(r) || r >= '\u2800' && r <= '\u28ff' || strings.ContainsRune("✳✦●✓✔⏺⏵", r)
		})
		task = agents.TaskText(task)
		// Shell titles often contain only the current directory or executable.
		if task == session.Dir || strings.HasPrefix(task, "/") || strings.HasPrefix(task, "~/") || len(task) >= 3 && task[1] == ':' && (task[2] == '\\' || task[2] == '/') {
			task = ""
		}
		if _, generic := agents.Lookup(task); generic || strings.EqualFold(task, session.Program) {
			task = ""
		}
		for _, agent := range agents.All {
			if strings.EqualFold(task, agent.Name) {
				task = ""
				break
			}
		}
	}
	if task != "" {
		return "任务：" + task
	}
	switch session.Agent.State {
	case agents.Completed:
		return "任务已完成，打开会话查看结果。"
	case agents.Failed:
		return "任务执行失败，打开会话查看原因。"
	case agents.Waiting:
		switch session.Agent.Reason {
		case "permission":
			return "需要你确认操作权限，授权后继续任务。"
		case "question":
			return "需要你回答一个问题，回答后继续任务。"
		default:
			return "任务正在等待你的输入。"
		}
	}
	return "打开会话查看任务进展。"
}
