package agents

import "testing"

func TestDetectLaunchers(t *testing.T) {
	for _, a := range All {
		for _, alias := range a.Aliases {
			got, ok := Detect(alias, []string{alias})
			if !ok || got.ID != a.ID {
				t.Fatalf("%s: got %+v, recognized %v", alias, got, ok)
			}
		}
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"node", []string{"node", "/opt/node_modules/@anthropic-ai/claude-code/cli.js", "--resume"}, "claude"},
		{"node", []string{"node", "/opt/node_modules/@google/gemini-cli/dist/index.js"}, "gemini"},
		{"node", []string{"node", "--require", "/tmp/codex.js", "/opt/node_modules/@github/copilot/index.js"}, "copilot"},
		{"node", []string{"node", "/opt/node_modules/@mariozechner/pi-coding-agent/dist/cli.js"}, "pi"},
		{"node", []string{"node", "/opt/node_modules/@oh-my-pi/pi-coding-agent/dist/cli.js"}, "omp"},
		{"node", []string{"node", "/opt/node_modules/@earendil-works/pi-coding-agent/dist/cli.js"}, "pi"},
		{"node", []string{"node", "/opt/node_modules/@deepseek-ai/dsh/bin/cli.js"}, "dsh"},
		{"node", []string{"node", "/opt/node_modules/@atomgit.com/atomcode/dist/index.js"}, "atomcode"},
		{"node", []string{"node", "/opt/node_modules/omo-ai/dist/cli.js"}, "omo"},
		{"pnpm", []string{"pnpm", "dlx", "cline@latest"}, "cline"},
		{"bun", []string{"bun", "x", "@charmland/crush@latest"}, "crush"},
		{"agy.exe", []string{`C:\tools\agy.exe`}, "agy"},
		{"python3", []string{"python3", "/usr/local/bin/hermes"}, "hermes"},
		{"node", []string{"node", "/opt/node_modules/@qwen-code/qwen-code/cli.js"}, "qwen"},
		{"npx", []string{"npx", "--yes", "@google/gemini-cli@latest"}, "gemini"},
		{"npm", []string{"npm", "exec", "--", "@openai/codex"}, "codex"},
		{"pnpm", []string{"pnpm", "dlx", "opencode-ai"}, "opencode"},
		{"bun", []string{"bun", "x", "@sourcegraph/amp"}, "amp"},
		{"python3", []string{"python3", "-m", "aider"}, "aider"},
		{"python3", []string{"python3", "/usr/lib/python3.12/site-packages/aider/__main__.py"}, "aider"},
		{"uv", []string{"uv", "tool", "run", "aider-chat"}, "aider"},
		{"cat", []string{"cat", "claude"}, ""},
		{"vim", []string{"vim", "/opt/node_modules/@google/gemini-cli/index.js"}, ""},
		{"node", []string{"node", "app.js", "@anthropic-ai/claude-code"}, ""},
		{"node", []string{"node", "app.js", "fix /tmp/codex.js"}, ""},
		{"node", []string{"node", "-e", "claude"}, ""},
		{"python3", []string{"python3", "-c", "aider"}, ""},
		{"python3", []string{"python3", "-m", "antigravity"}, ""},
		{"node", []string{"node", "/opt/node_modules/@google/gemini-cli-tools/index.js"}, ""},
		{"node", []string{"node", "--require", "codex.js", "app.js"}, ""},
		{"sh", []string{"sh", "-c", "echo claude"}, ""},
		{"cursor", []string{"cursor", "/work/repo"}, ""},
		{"cmd.exe", []string{"cmd.exe", "/c", "command-code"}, ""},
		{"cc", []string{"cc", "claude.c"}, ""},
		{"fx", []string{"fx", "input.json"}, ""},
		{"morph", []string{"morph", "image.png"}, ""},
		{"node", []string{"node", "app.js", "@deepseek-ai/dsh"}, ""},
		{"node", []string{"node", "/opt/node_modules/@atomgit.com/atomcode-tools/cli.js"}, ""},
	} {
		got, _ := Detect(tc.name, tc.args)
		if got.ID != tc.want {
			t.Errorf("%s %q: got %s, want %s", tc.name, tc.args, got.ID, tc.want)
		}
	}
}
