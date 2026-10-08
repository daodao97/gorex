package rex

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLaunchdServiceDoesNotInheritAutomationEnvironment(t *testing.T) {
	// Exercise real launchd with only disposable jobs and fixture executables.
	// No normal GoRex service, socket or session is used by this test.
	for _, preference := range []string{"", "user-preference"} {
		t.Run("login-preference-"+preference, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("GOREX_AUTOMATION_FIXTURE", "must-not-leak")
			dir := t.TempDir()
			shell := filepath.Join(dir, "login-shell")
			startup := "#!/bin/sh\nshift\nexport STARTUP_MARKER=from-login\n"
			if preference != "" {
				startup += "export NO_COLOR=user-preference\n"
			}
			startup += "exec /bin/sh \"$@\"\n"
			if err := os.WriteFile(shell, []byte(startup), 0o700); err != nil {
				t.Fatal(err)
			}
			exe := filepath.Join(dir, "fixture")
			fixture := "#!/bin/sh\nprintf '%s\\n' \"color:${NO_COLOR-unset}\" \"automation:${GOREX_AUTOMATION_FIXTURE-unset}\" \"startup:$STARTUP_MARKER\" \"dir:$GOREX_DIR\" \"argument:$1\" >> \"$GOREX_DIR/result\"\n"
			fixture += "while [ ! -f \"$GOREX_DIR/release\" ]; do sleep 0.05; done\n"
			if err := os.WriteFile(exe, []byte(fixture), 0o700); err != nil {
				t.Fatal(err)
			}
			args := []string{"argument with spaces & quotes '"}
			sum := sha256.Sum256([]byte(strings.Join(append([]string{dir, exe, shell}, args...), "\x00")))
			service := fmt.Sprintf("gui/%d/dev.gorex.background.%x", os.Getuid(), sum[:12])
			t.Cleanup(func() {
				// This is exclusively the job registered with this test's fixture.
				if err := exec.Command("/bin/launchctl", "bootout", service).Run(); err != nil {
					t.Errorf("cleaning up fixture job: %v", err)
				}
			})
			if err := startLaunchdBackground(exe, args, dir, "fixture.log", shell); err != nil {
				t.Fatal(err)
			}
			wantColor := "unset"
			if preference != "" {
				wantColor = preference
			}
			want := fmt.Sprintf("color:%s\nautomation:unset\nstartup:from-login\ndir:%s\nargument:%s\n", wantColor, dir, args[0])
			wait := func(want string) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					got, _ := os.ReadFile(filepath.Join(dir, "result"))
					if string(got) == want {
						return
					}
					if time.Now().After(deadline) {
						log, _ := os.ReadFile(filepath.Join(dir, "fixture.log"))
						t.Fatalf("fixture result %q, want %q; log %q", got, want, log)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			wait(want)
			// Starting the same job while it runs must not replace its process.
			if err := startLaunchdBackground(exe, args, dir, "fixture.log", shell); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			if got, _ := os.ReadFile(filepath.Join(dir, "result")); string(got) != want {
				t.Fatalf("restarted a running service: %q", got)
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			// An exited job can be started again using the same registration.
			deadline := time.Now().Add(10 * time.Second)
			for {
				out, err := exec.Command("/bin/launchctl", "print", service).Output()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(out), "pid = ") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fixture did not exit after release")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := startLaunchdBackground(exe, args, dir, "fixture.log", shell); err != nil {
				t.Fatal(err)
			}
			wait(want + want)
		})
	}
}
