//go:build !ios

package remote

import (
	"context"
	"github.com/tailscale/tailcat"
)

func prepareNetwork(context.Context, tailcat.Addr) error { return nil }
