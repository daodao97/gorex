//go:build !gorex_cli

package remote

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
)

func prepareSystemNetwork(ctx context.Context, url string) error {
	err := mygo.Network.Prepare(ctx, url)
	var failure *mygo.NetworkError
	if !errors.As(err, &failure) {
		return err
	}
	kind := RelayUnavailable
	switch failure.Kind {
	case "offline":
		kind = NetworkUnavailable
	case "timeout":
		kind = ConnectionTimeout
	}
	return &ConnectionError{Kind: kind, Cause: err}
}

func systemNetworkCause(err error) string {
	var native *mygo.NetworkError
	if !errors.As(err, &native) {
		return ""
	}
	switch native.Kind {
	case "offline", "timeout", "canceled", "failed":
		return "system_network_" + native.Kind
	}
	return "system_network_failed"
}
