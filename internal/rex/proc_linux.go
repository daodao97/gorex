package rex

import (
	"fmt"
	"os"
	"strings"
)

// inspect returns the name, arguments and working directory of a process.
func inspect(pid int) (p procInfo) {
	if pid <= 0 {
		return
	}
	// An exited child can retain its Agent comm until its parent reaps it.
	// Exclude zombies/dead processes, while preserving stopped/background jobs.
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return
	}
	// comm is parenthesized and may itself contain spaces or parentheses.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) == 0 || fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		p.name = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		p.args = strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	}
	p.dir, _ = os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	return p
}

func sysctlString(string) string { return "" }

func sysctlUint64(string) uint64 { return 0 }
