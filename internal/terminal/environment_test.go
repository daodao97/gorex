package terminal

import (
	"os"
	"strings"
	"testing"
)

func TestColorEnvironment(t *testing.T) {
	keys := []string{"NO_COLOR", "FORCE_COLOR", "CLICOLOR", "CLICOLOR_FORCE"}
	for _, key := range keys {
		t.Setenv(key, "0")
	}
	t.Setenv("NO_COLOR", "1")
	for _, extra := range [][]string{nil, {"NO_COLOR=1", "FORCE_COLOR=0", "CLICOLOR=0", "CLICOLOR_FORCE=0"}} {
		values := make(map[string]string)
		for _, kv := range environment(extra) {
			key, value, _ := strings.Cut(kv, "=")
			values[key] = value
		}
		for i, key := range keys {
			value, exists := values[key]
			if len(extra) == 0 {
				if !exists || value != os.Getenv(key) {
					t.Errorf("lost explicit preference %s=%s", key, os.Getenv(key))
				}
			} else if want := strings.TrimPrefix(extra[i], key+"="); !exists || value != want {
				t.Errorf("explicit %s: got %q, want %q", key, value, want)
			}
		}
	}
}
