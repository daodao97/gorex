package rex

import "testing"

func TestAgentProgramName(t *testing.T) {
	for _, tc := range []struct {
		proc procInfo
		want string
	}{
		{procInfo{name: "node", args: []string{"node", "/opt/node_modules/@anthropic-ai/claude-code/cli.js"}}, "claude"},
		{procInfo{name: "node", args: []string{"node", "/opt/node_modules/@google/gemini-cli/dist/index.js"}}, "gemini"},
		{procInfo{name: "python3", args: []string{"python3", "-m", "aider"}}, "aider"},
		{procInfo{name: "cursor-agent", args: []string{"/opt/bin/cursor-agent"}}, "cursor"},
		{procInfo{name: "node", args: []string{"node", "/tmp/app.js", "claude"}}, "node"},
		{procInfo{name: "python3", args: []string{"python3", "-c", "print('aider')"}}, "python3"},
	} {
		if got := tc.proc.programName(); got != tc.want {
			t.Errorf("%+v: got %s, want %s", tc.proc, got, tc.want)
		}
	}
}
