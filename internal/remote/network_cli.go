//go:build gorex_cli

package remote

import "context"

func prepareSystemNetwork(ctx context.Context, _ string) error { return ctx.Err() }
func systemNetworkCause(error) string                          { return "" }
