//go:build !gorex_cli

package remote

import (
	"github.com/egoist/mygo"
	"testing"
)

func TestDiagnosticSystemNetworkErrorCodes(t *testing.T) {
	for _, tt := range []struct{ kind, code string }{
		{"timeout", "system_network_timeout"},
		{"secret-token", "system_network_failed"},
	} {
		if got := diagnosticCause(&mygo.NetworkError{Kind: tt.kind}); got != tt.code {
			t.Fatalf("got %q, want %q", got, tt.code)
		}
	}
}
