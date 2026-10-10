package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"retty/internal/push"
	"retty/internal/rex"
	"strings"
	"time"
)

type noticeRequest struct {
	Op       string              `json:"op"`
	Activity rex.DesktopActivity `json:"activity"`
	Notice   push.Notice         `json:"notice"`
}

type noticeResponse struct {
	Route push.Route `json:"route,omitempty"`
	Error string     `json:"error,omitempty"`
}

func (b *Bridge) serveNotice(conn net.Conn, line []byte) {
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	response := noticeResponse{}
	var req noticeRequest
	if len(line) > 16384 || json.Unmarshal(line, &req) != nil {
		response.Error = "invalid notification claim"
	} else if b.onNotice == nil {
		response.Error = "notification routing unsupported"
	} else {
		var err error
		response.Route, err = b.onNotice(req.Activity, req.Notice)
		if err != nil {
			response.Error = "notification routing unavailable"
		}
	}
	_ = json.NewEncoder(conn).Encode(response)
}

// ClaimNotification uses the existing encrypted tunnel. The source host owns
// both desktop claims and phone delivery, even when several desktops view it.
func ClaimNotification(ctx context.Context, client *rex.Client, activity rex.DesktopActivity, n push.Notice) (push.Route, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := client.DialExtension(ctx)
	if err != nil {
		return push.RouteQuiet, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if err = json.NewEncoder(conn).Encode(noticeRequest{Op: "notification-claim", Activity: activity, Notice: n}); err != nil {
		return push.RouteQuiet, err
	}
	var response noticeResponse
	if err = json.NewDecoder(io.LimitReader(conn, 16384)).Decode(&response); err != nil {
		return push.RouteQuiet, err
	}
	if response.Error != "" {
		if response.Error == "notification routing unsupported" || strings.HasPrefix(response.Error, "unknown op ") {
			return push.RouteQuiet, push.ErrLegacyRouting
		}
		return push.RouteQuiet, errors.New("remote notification routing unavailable")
	}
	if response.Route != push.RouteQuiet && response.Route != push.RouteDesktop && response.Route != push.RoutePhone {
		return push.RouteQuiet, errors.New("invalid notification route")
	}
	return response.Route, nil
}
