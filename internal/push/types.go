// Package push binds GoRex's paired devices to task reminders. APNs transport
// and native notification delivery are provided by MyGo; this package owns only
// subscription, receipt and session policies.
package push

import (
	"encoding/hex"
	"errors"
	"gorex/internal/rex"
	"strings"
	"time"
)

// Registration contains a public installation identifier and a private APNs
// device token. It is sent only through the already authenticated Tailcat tunnel.
type Registration rex.PushRegistration

func (r Registration) Validate() error {
	if len(r.ID) != 32 {
		return errors.New("invalid notification device ID")
	}
	if _, err := hex.DecodeString(r.ID); err != nil {
		return errors.New("invalid notification device ID")
	}
	if !r.Disabled && (len(r.Token) < 2 || len(r.Token) > 512 || len(r.Token)%2 != 0) {
		return errors.New("invalid notification token")
	}
	if !r.Disabled {
		if _, err := hex.DecodeString(r.Token); err != nil {
			return errors.New("invalid notification token")
		}
	}
	if len(r.Receipts) > 128 {
		return errors.New("too many notification receipts")
	}
	for _, id := range r.Receipts {
		if !strings.HasPrefix(id, "gorex-agent-") || len(id) > 64 {
			return errors.New("invalid notification receipt")
		}
	}
	return nil
}

type Status struct {
	Configured    bool      `json:"configured"`
	Devices       int       `json:"devices"`
	Sent          uint64    `json:"sent"`
	LastError     string    `json:"lastError,omitempty"`
	LastSent      time.Time `json:"lastSent,omitzero"`
	DesktopActive bool      `json:"desktopActive"`
}

// DesktopActivity is a short-lived foreground lease from a local GUI window.
// Sequence orders focus/blur updates; a crashed GUI stops renewing its lease.
type DesktopActivity struct {
	ID       string `json:"id"`
	Sequence uint64 `json:"sequence"`
	Active   bool   `json:"active"`
}
