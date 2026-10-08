//go:build darwin || linux

package rex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coder/websocket"
	"gorex/internal/agents"
)

// WatchCodexThread attaches metadata-only observation to an already running
// CLI. It neither resumes the thread nor changes its instructions/approvals.
// The caller supplies the verified pane capability and exact thread ID.
func WatchCodexThread(thread string, parent int) error {
	if thread == "" || parent <= 0 || osPaneContextMissing() {
		return errors.New("missing Codex pane context")
	}
	socket, err := codexSocket()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := newCodexObserver(ctx)
	for processAlive(parent) {
		if err := pollCodexThread(ctx, socket, thread, parent, observer); err != nil {
			time.Sleep(time.Second)
		}
	}
	return nil
}

func pollCodexThread(ctx context.Context, socket, thread string, parent int, observer *codexObserver) error {
	connect, cancel := context.WithTimeout(ctx, 3*time.Second)
	c, err := dialCodex(connect, socket)
	cancel()
	if err != nil {
		return err
	}
	defer c.CloseNow()
	var sequence int
	call := func(method string, params any, result any) error {
		sequence++
		request, _ := json.Marshal(map[string]any{"id": sequence, "method": method, "params": params})
		deadline, done := context.WithTimeout(ctx, 3*time.Second)
		defer done()
		if err := c.Write(deadline, websocket.MessageText, request); err != nil {
			return err
		}
		for {
			_, raw, err := c.Read(deadline)
			if err != nil {
				return err
			}
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(raw, &response) != nil || response.ID != sequence {
				continue
			}
			if len(response.Error) > 0 {
				return errors.New("Codex metadata unavailable")
			}
			if result == nil {
				return nil
			}
			return json.Unmarshal(response.Result, result)
		}
	}
	if err := call("initialize", map[string]any{"clientInfo": map[string]string{"name": "gorex_status_observer", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"method":"initialized"}`)); err != nil {
		return err
	}
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for processAlive(parent) {
		var metadata struct {
			Thread codexThread `json:"thread"`
		}
		if err := call("thread/read", map[string]any{"threadId": thread, "includeTurns": false}, &metadata); err != nil {
			return err
		}
		var turns struct {
			Data []codexTurn `json:"data"`
		}
		if err := call("thread/turns/list", map[string]any{"threadId": thread, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}, &turns); err != nil {
			return err
		}
		if metadata.Thread.ID != thread || metadata.Thread.Parent != "" {
			return errors.New("not the requested root thread")
		}
		var turn codexTurn
		if len(turns.Data) > 0 {
			turn = turns.Data[0]
		}
		observer.snapshot(metadata.Thread, turn)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func (o *codexObserver) snapshot(thread codexThread, turn codexTurn) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.thread.ID == "" {
		o.thread = thread
		o.emit(agents.HookInput{Event: "SessionStart", SessionID: thread.ID, CWD: thread.CWD, Source: "resume"})
	}
	if thread.ID != o.thread.ID {
		return
	}
	if turn.ID != "" && turn.Status == "inProgress" && (!o.active || o.turn != turn.ID) {
		o.turn, o.active, o.wait = turn.ID, true, ""
		o.event("UserPromptSubmit", "")
	}
	o.waiting(thread.Status)
	if o.active && turn.ID == o.turn {
		event := ""
		switch turn.Status {
		case "completed":
			event = "Stop"
		case "failed":
			event = "StopFailure"
		case "interrupted":
			event = "Interrupt"
		}
		if event != "" {
			o.active, o.wait = false, ""
			o.event(event, "")
		}
	}
}
