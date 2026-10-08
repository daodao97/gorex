//go:build darwin || linux

package rex

import (
	"encoding/json"
	"testing"
	"time"

	"gorex/internal/agents"
)

func TestCodexBridgeOnlyTracksItsCLIThread(t *testing.T) {
	var tracker agentTracker
	var events []agents.HookInput
	o := &codexObserver{pending: map[string]string{}, emit: func(h agents.HookInput) {
		events = append(events, h)
		tracker.apply(AgentEvent{Agent: "codex", Input: h, At: time.Now()})
	}}
	message := func(client bool, value string) { t.Helper(); o.message([]byte(value), client) }
	// Thread notifications broadcast by the daemon cannot claim this pane.
	message(false, `{"method":"thread/started","params":{"thread":{"id":"foreign","cwd":"/same"}}}`)
	message(false, `{"method":"turn/started","params":{"threadId":"foreign","turn":{"id":"foreign-turn"}}}`)
	if len(events) != 0 {
		t.Fatal("foreign daemon thread claimed pane")
	}
	message(true, `{"id":1,"method":"thread/start","params":{"cwd":"/same"}}`)
	// Server requests use a separate ID namespace and may overlap client IDs.
	message(false, `{"id":1,"method":"item/tool/requestUserInput","params":{"threadId":"foreign"}}`)
	message(false, `{"id":1,"result":{"thread":{"id":"own","cwd":"/same","status":{"type":"idle"}}}}`)
	message(false, `{"method":"turn/started","params":{"threadId":"own","turn":{"id":"turn","status":"inProgress"}}}`)
	if tracker.state.SessionID != "own" || tracker.state.State != agents.Running {
		t.Fatalf("unbound turn: %+v", tracker.state)
	}
	message(false, `{"method":"thread/status/changed","params":{"threadId":"own","status":{"type":"active","activeFlags":["waitingOnApproval"]}}}`)
	message(false, `{"method":"thread/status/changed","params":{"threadId":"own","status":{"type":"active","activeFlags":["waitingOnApproval"]}}}`)
	if tracker.state.State != agents.Waiting || tracker.state.Reason != "permission" || tracker.state.WaitRevision != 1 {
		t.Fatalf("permission status: %+v", tracker.state)
	}
	message(false, `{"method":"thread/status/changed","params":{"threadId":"own","status":{"type":"active","activeFlags":["waitingOnUserInput"]}}}`)
	if tracker.state.Reason != "question" {
		t.Fatalf("question status: %+v", tracker.state)
	}
	message(false, `{"method":"thread/status/changed","params":{"threadId":"own","status":{"type":"active","activeFlags":[]}}}`)
	if tracker.state.State != agents.Running {
		t.Fatalf("unresolved wait: %+v", tracker.state)
	}
	message(false, `{"method":"turn/completed","params":{"threadId":"own","turn":{"id":"old-turn","status":"completed"}}}`)
	message(false, `{"method":"turn/completed","params":{"threadId":"foreign","turn":{"id":"turn","status":"completed"}}}`)
	if tracker.state.State != agents.Running {
		t.Fatal("stale/foreign completion accepted")
	}
	message(false, `{"method":"turn/completed","params":{"threadId":"own","turn":{"id":"turn","status":"completed"}}}`)
	message(false, `{"method":"turn/completed","params":{"threadId":"own","turn":{"id":"turn","status":"completed"}}}`)
	if tracker.state.State != agents.Completed || tracker.state.CompletionRevision != 1 {
		t.Fatalf("completion: %+v", tracker.state)
	}
	for _, finish := range []struct{ status, want string }{{"failed", agents.Failed}, {"interrupted", agents.Ready}} {
		message(false, `{"method":"turn/started","params":{"threadId":"own","turn":{"id":"next","status":"inProgress"}}}`)
		data, _ := json.Marshal(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "own", "turn": map[string]string{"id": "next", "status": finish.status}}})
		o.message(data, false)
		if tracker.state.State != finish.want {
			t.Fatalf("%s treated as %s", finish.status, tracker.state.State)
		}
	}
	// An error reply and a subagent response cannot replace the root owner.
	message(true, `{"id":"resume","method":"thread/resume","params":{"threadId":"missing"}}`)
	message(false, `{"id":"resume","error":{"code":-1,"message":"not found"}}`)
	message(true, `{"id":2,"method":"thread/start","params":{}}`)
	message(false, `{"id":2,"result":{"thread":{"id":"child","parentThreadId":"own","cwd":"/same"}}}`)
	if o.thread.ID != "own" {
		t.Fatal("failed resume/subagent replaced root")
	}
}

func TestCodexBridgeKeepsSameDirectoryPanesIndependent(t *testing.T) {
	for _, id := range []string{"first", "second"} {
		var seen []agents.HookInput
		o := &codexObserver{pending: map[string]string{}, emit: func(h agents.HookInput) { seen = append(seen, h) }}
		o.message([]byte(`{"id":1,"method":"thread/start"}`), true)
		bound, _ := json.Marshal(map[string]any{"id": 1, "result": map[string]any{"thread": map[string]string{"id": id, "cwd": "/same"}}})
		o.message(bound, false)
		for _, thread := range []string{"first", "second"} {
			raw, _ := json.Marshal(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": thread, "turn": map[string]string{"id": "turn"}}})
			o.message(raw, false)
		}
		if len(seen) != 2 || seen[1].SessionID != id {
			t.Fatal("same cwd crossed pane ownership")
		}
	}
}

func TestCodexMetadataWatchIgnoresHistoricalCompletion(t *testing.T) {
	var tracker agentTracker
	o := &codexObserver{emit: func(h agents.HookInput) { tracker.apply(AgentEvent{Agent: "codex", Input: h, At: time.Now()}) }}
	thread := codexThread{ID: "existing", CWD: "/same", Status: codexStatus{Type: "idle"}}
	o.snapshot(thread, codexTurn{ID: "old", Status: "completed"})
	if tracker.state.State != agents.Ready || tracker.state.CompletionRevision != 0 {
		t.Fatal("history produced a fresh completion")
	}
	thread.Status = codexStatus{Type: "active", Flags: []string{"waitingOnApproval"}}
	o.snapshot(thread, codexTurn{ID: "new", Status: "inProgress"})
	if tracker.state.State != agents.Waiting || tracker.state.Reason != "permission" {
		t.Fatal("active existing task not observed")
	}
	thread.Status = codexStatus{Type: "idle"}
	o.snapshot(thread, codexTurn{ID: "new", Status: "completed"})
	o.snapshot(thread, codexTurn{ID: "new", Status: "completed"})
	if tracker.state.State != agents.Completed || tracker.state.CompletionRevision != 1 {
		t.Fatal("polled completion missing or duplicated")
	}
}
