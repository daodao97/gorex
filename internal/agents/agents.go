// Package agents identifies coding agents from their executable or launcher.
package agents

import (
	"path"
	"strings"
)

type Agent struct {
	ID, Name          string
	Aliases, Packages []string
}

// All lists the supported terminal agents. Package names identify generic
// entry points such as node …/@google/gemini-cli/dist/index.js.
var All = []Agent{
	{"codex", "Codex", []string{"codex", "codex-cli"}, []string{"@openai/codex"}},
	{"claude", "Claude Code", []string{"claude", "claude-code"}, []string{"@anthropic-ai/claude-code"}},
	{"gemini", "Gemini CLI", []string{"gemini", "gemini-cli"}, []string{"@google/gemini-cli"}},
	{"cursor", "Cursor Agent", []string{"cursor-agent"}, nil},
	{"opencode", "OpenCode", []string{"opencode"}, []string{"opencode-ai"}},
	{"copilot", "GitHub Copilot", []string{"copilot"}, []string{"@github/copilot"}},
	{"aider", "Aider", []string{"aider", "aider-chat"}, []string{"aider"}},
	{"amp", "Amp", []string{"amp"}, []string{"@sourcegraph/amp"}},
	{"pi", "Pi", []string{"pi"}, []string{"@mariozechner/pi-coding-agent"}},
	{"omp", "Oh My Pi", []string{"omp"}, []string{"@oh-my-pi/pi-coding-agent"}},
	{"goose", "Goose", []string{"goose"}, nil},
	{"droid", "Droid", []string{"droid"}, nil},
	{"grok", "Grok", []string{"grok"}, nil},
	{"qwen", "Qwen Code", []string{"qwen", "qwen-code"}, []string{"@qwen-code/qwen-code"}},
	{"kimi", "Kimi Code", []string{"kimi", "kimi-code"}, []string{"kimi_cli"}},
	{"crush", "Crush", []string{"crush"}, nil},
	{"codebuddy", "CodeBuddy", []string{"codebuddy", "codebuddy-code"}, []string{"@tencent-ai/codebuddy-code"}},
	{"qodercli", "Qoder CLI", []string{"qoder", "qodercli"}, nil},
	{"qoderclicn", "Qoder CN CLI", []string{"qodercn", "qoderclicn", "qoder-cn"}, nil},
	{"traecli", "TraeCode", []string{"traecli", "traex"}, nil},
}

func Lookup(name string) (Agent, bool) {
	name = stem(name)
	for _, a := range All {
		for _, alias := range a.Aliases {
			if name == alias {
				return a, true
			}
		}
	}
	return Agent{}, false
}

func stem(name string) string {
	name = strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	for _, ext := range []string{".exe", ".cmd", ".js", ".mjs", ".cjs", ".ts", ".py"} {
		name = strings.TrimSuffix(name, ext)
	}
	return strings.TrimPrefix(name, "-")
}

func entryAgent(entry string) (Agent, bool) {
	entry = strings.ReplaceAll(entry, "\\", "/")
	for _, a := range All {
		for _, pkg := range a.Packages {
			if entry == pkg || strings.HasPrefix(entry, pkg+"@") || strings.Contains("/"+entry, "/node_modules/"+pkg+"/") ||
				strings.Contains("/"+entry, "/site-packages/"+pkg+"/") || strings.Contains("/"+entry, "/dist-packages/"+pkg+"/") {
				return a, true
			}
		}
	}
	return Lookup(entry)
}

// Detect examines only the launcher's entry point, never prompt text or
// later arguments (node app.js "ask claude" must remain Node).
func Detect(name string, args []string) (Agent, bool) {
	if a, ok := Lookup(name); ok {
		return a, true
	}
	if len(args) == 0 {
		return Agent{}, false
	}
	if a, ok := Lookup(args[0]); ok {
		return a, true
	}
	launcher := stem(args[0])
	switch launcher {
	case "node", "bun", "deno", "python", "python3", "npx", "npm", "pnpm", "yarn", "uv", "uvx":
	default:
		return Agent{}, false
	}
	for i := 1; i < len(args); i++ {
		token := args[i]
		switch token {
		case "-e", "--eval", "-c", "--command", "-p", "--print":
			return Agent{}, false
		case "-r", "--require", "--loader", "--import", "--conditions", "-C", "--directory", "--cwd", "--prefix", "--cache", "--registry":
			i++
			continue
		case "-m":
			if i+1 < len(args) && strings.HasPrefix(launcher, "python") {
				return entryAgent(args[i+1])
			}
			return Agent{}, false
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if (launcher == "npm" && token == "exec") || (launcher == "pnpm" && (token == "dlx" || token == "exec")) ||
			(launcher == "yarn" && (token == "dlx" || token == "exec")) || (launcher == "uv" && token == "tool") ||
			((launcher == "uv" || launcher == "bun" || launcher == "deno") && (token == "run" || token == "x")) {
			continue
		}
		return entryAgent(token)
	}
	return Agent{}, false
}
