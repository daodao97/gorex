package rex

import (
	"path/filepath"
	"strings"

	"retty/internal/agents"
)

// procInfo is what inspect finds of a process.
type procInfo struct {
	name string
	args []string
	dir  string
}

// interpreters run a script named by their first argument, which names
// the program better than they do: node …/codex.js is codex.
var interpreters = map[string]bool{
	"node": true, "bun": true, "deno": true, "python": true, "python3": true,
	"ruby": true, "perl": true, "php": true, "sh": true, "bash": true,
}

// programName returns the name a process is best known by.
func (p procInfo) programName() string {
	if agent, ok := agents.Detect(p.name, p.args); ok {
		return agent.ID
	}
	name := strings.TrimPrefix(p.name, "-")
	if len(p.args) > 0 {
		if base := strings.TrimPrefix(filepath.Base(p.args[0]), "-"); base != "" && len(base) > len(name) && strings.HasPrefix(base, name) {
			// p_comm is cut at 16 bytes.
			name = base
		}
	}
	if interpreters[name] && len(p.args) > 1 {
		for _, a := range p.args[1:] {
			if strings.HasPrefix(a, "-") {
				continue
			}
			script := filepath.Base(a)
			if ext := filepath.Ext(script); ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".ts" || ext == ".py" || ext == ".rb" {
				script = strings.TrimSuffix(script, ext)
			}
			// node …/bin/codex.js; but node demo/snake.mjs stays node.
			if strings.Contains(a, "/bin/") || strings.Contains(a, "node_modules") {
				return script
			}
			break
		}
	}
	return name
}
