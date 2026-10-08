package remote

import (
	"context"
	"errors"
	"net"
	"syscall"
)

type FailureKind uint8

const (
	DesktopUnavailable FailureKind = iota
	InvalidLink
	NetworkUnavailable
	RelayUnavailable
	ConnectionTimeout
	ProtocolMismatch
)

// ConnectionError preserves the cause for classification without displaying
// transport internals or a case-sensitive pairing capability in the UI/logs.
type ConnectionError struct {
	Kind  FailureKind
	Cause error
}

func (e *ConnectionError) Unwrap() error { return e.Cause }
func (e *ConnectionError) Error() string {
	switch e.Kind {
	case InvalidLink:
		return "连接码无法识别"
	case NetworkUnavailable:
		return "当前网络不可用"
	case RelayUnavailable:
		return "无法访问连接服务"
	case ConnectionTimeout:
		return "连接超时"
	case ProtocolMismatch:
		return "桌面版本不兼容"
	default:
		return "暂时无法连接桌面"
	}
}

func Failure(err error) FailureKind {
	var failure *ConnectionError
	if errors.As(err, &failure) {
		return failure.Kind
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return ConnectionTimeout
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.ENETDOWN) {
		return NetworkUnavailable
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return RelayUnavailable
	}
	return DesktopUnavailable
}
