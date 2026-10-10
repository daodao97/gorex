// Package notification defines the shared desktop/APNs routing contract.
// It has no delivery or UI dependencies, so headless gateways can use it.
package notification

import "errors"

// Notice describes one event shared by desktop delivery and APNs.
type Notice struct {
	ID      string `json:"id"`
	Desktop string `json:"desktop"`
	Session string `json:"session"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Viewed  bool   `json:"viewed,omitempty"`
	Caller  string `json:"-"`
}

type Route string

const (
	RouteQuiet   Route = "quiet"
	RouteDesktop Route = "desktop"
	RoutePhone   Route = "phone"
)

var ErrLegacyRouting = errors.New("notification worker needs routing update")
