//go:build darwin || linux

package rex

import (
	"os"
	"strings"
	"testing"
)

func TestSessionColorEnvironment(t *testing.T) {
	keys := []string{"NO_COLOR", "FORCE_COLOR", "CLICOLOR", "CLICOLOR_FORCE"}
	for _, key := range keys {
		t.Setenv(key, "0")
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	t.Setenv("COLORTERM", "")
	for _, test := range []struct {
		name  string
		extra []string
	}{
		{name: "user login preferences"},
		{name: "explicit color preferences", extra: []string{"NO_COLOR=1", "FORCE_COLOR=0", "CLICOLOR=0", "CLICOLOR_FORCE=0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := make(map[string]string)
			for _, kv := range sessionEnv("color-test", test.extra) {
				key, value, _ := strings.Cut(kv, "=")
				values[key] = value
			}
			for i, key := range keys {
				value, exists := values[key]
				if len(test.extra) == 0 {
					if !exists || value != os.Getenv(key) {
						t.Errorf("lost login preference %s=%s", key, os.Getenv(key))
					}
				} else if want := strings.TrimPrefix(test.extra[i], key+"="); !exists || value != want {
					t.Errorf("explicit %s: got %q, want %q", key, value, want)
				}
			}
			if values["TERM"] != "xterm-256color" || values["COLORTERM"] != "truecolor" {
				t.Errorf("terminal capabilities: TERM=%q COLORTERM=%q", values["TERM"], values["COLORTERM"])
			}
		})
	}
}
