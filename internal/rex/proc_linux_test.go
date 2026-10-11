package rex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInspectLinuxStoppedAndZombieAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.Symlink("/bin/sleep", path); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait() // Reap only this fixture after inspecting its zombie state.
	})
	pid := cmd.Process.Pid
	waitState := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(string(b)[strings.LastIndexByte(string(b), ')')+1:])
			if len(fields) > 0 && fields[0] == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("fixture did not enter process state %s", want)
	}
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitState("T")
	if got := inspect(pid).programName(); got != "claude" {
		t.Fatalf("stopped Agent must remain identifiable, got %q", got)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitState("Z")
	if p := inspect(pid); p.name != "" || len(p.args) != 0 || p.dir != "" {
		t.Fatalf("zombie Agent must be treated as exited, got %+v", p)
	}
}
