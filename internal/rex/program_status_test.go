package rex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"retty/internal/agents"
)

func statusOSC(body string) string { return "\x1b]7501;" + body + "\x1b\\" }
func statusB64(s string) string    { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestProgramStatusFields(t *testing.T) {
	for _, state := range []string{"idle", "working", "done", "blocked", "error", "clear"} {
		r, ok := parseProgramStatus("state=" + state)
		if !ok || r.State != state {
			t.Fatalf("state %s rejected", state)
		}
	}
	r, ok := parseProgramStatus("state=working:state=blocked: app = terraform :id=deploy/us-east:kind=auth:progress=0:title=" + statusB64("登录") + ":msg=" + strings.TrimRight(statusB64("Sign in to deploy."), "=") + ":future=value:broken:bad value=nope")
	if !ok || r.State != "blocked" || r.App != "terraform" || r.ID != "deploy/us-east" || r.Kind != "auth" || r.Title != "登录" || r.Message != "Sign in to deploy." || r.Progress == nil || *r.Progress != 0 {
		t.Fatalf("fields: %+v / %v", r, ok)
	}
	r, ok = parseProgramStatus("state=done:progress=50:kind=permission")
	if !ok || r.Progress != nil || r.Kind != "" {
		t.Fatal("irrelevant fields retained", r)
	}
	for _, value := range []string{"-1", "+1", "1.5", "101", "", "abc", strings.Repeat("9", 500)} {
		r, ok = parseProgramStatus("state=working:kind=unknown:progress=" + value)
		if !ok || r.Progress != nil || r.Kind != "" {
			t.Fatalf("invalid progress %q changed state", value)
		}
	}
	r, ok = parseProgramStatus("state=blocked:kind=unknown:app=bad/name:progress=100")
	if !ok || r.Kind != "" || r.App != "" || r.Progress == nil || *r.Progress != 100 {
		t.Fatal("invalid optional fields", r)
	}
	r, ok = parseProgramStatus("state=working:state=malformed;:state=done")
	if !ok || r.State != "done" {
		t.Fatal("malformed pairs prevented subsequent valid pairs")
	}
}

func TestProgramStatusRejectsWholeReport(t *testing.T) {
	bad := []string{
		"app=build", "state=unknown", "state=working:id=", "state=working:id=/a", "state=working:id=a/", "state=working:id=a//b", "state=working:id=a,b",
		"state=working:id=" + strings.Repeat("a", 33), "state=working:id=" + strings.Repeat("a/", 8) + "a",
		"state=working:id=" + strings.Repeat(strings.Repeat("a", 32)+"/", 4) + "a",
		"state=working:app=" + strings.Repeat("a", 33), "state=working:" + strings.Repeat("x", 17) + "=future",
		"state=done:msg=not-base64", "state=done:msg=not-base64:msg=" + statusB64("valid"),
		"state=done:title=" + statusB64(strings.Repeat("a", 193)),
		"state=done:msg=" + statusB64(strings.Repeat("a", 2049)),
		"state=done:msg=" + statusB64(string([]byte{0xff})),
	}
	for _, c := range []rune{0, '\n', '\t', 0x1b, 0x7f, 0x80, 0x9f} {
		bad = append(bad, "state=done:msg="+statusB64("bad"+string(c)))
	}
	for _, body := range bad {
		t.Run(body[:min(len(body), 60)], func(t *testing.T) {
			var tracker programStatusTracker
			tracker.feed([]byte(statusOSC("state=working:app=build")), time.Now())
			before := tracker.snapshot()
			tracker.feed([]byte(statusOSC(body)), time.Now())
			if !reflect.DeepEqual(before, tracker.snapshot()) {
				t.Fatal("invalid report modified records")
			}
		})
	}
	for key, size := range map[string]int{"title": 192, "msg": 2048} {
		if _, ok := parseProgramStatus("state=done:" + key + "=" + statusB64(strings.Repeat("a", size))); !ok {
			t.Fatalf("legal %s boundary rejected", key)
		}
	}
}

func TestProgramStatusFragmentationQueryAndControlStrings(t *testing.T) {
	input := "text" + statusOSC("?") + "\x1b]7501;state=working:app=build\a" + statusOSC("state=blocked:kind=permission:msg="+statusB64("Apply?"))
	for split := 0; split <= len(input); split++ {
		var tracker programStatusTracker
		replies := tracker.feed([]byte(input[:split]), time.Unix(1, 0)) + tracker.feed([]byte(input[split:]), time.Unix(1, 0))
		state := tracker.agentState("build")
		if replies != 1 || len(tracker.records) != 1 || state.State != agents.Waiting || state.Message != "Apply?" {
			t.Fatalf("split %d: replies=%d state=%+v", split, replies, state)
		}
	}
	var tracker programStatusTracker
	replies := 0
	for _, b := range []byte(input) {
		replies += tracker.feed([]byte{b}, time.Unix(1, 0))
	}
	if replies != 1 || tracker.agentState("build").State != agents.Waiting {
		t.Fatal("byte-by-byte parsing failed")
	}
	for _, prefix := range []string{"\x1bP", "\x1b_", "\x1b^", "\x1bX"} {
		var tracker programStatusTracker
		if tracker.feed([]byte(prefix+statusOSC("?")+statusOSC("state=done")), time.Now()) != 0 || len(tracker.records) != 1 {
			t.Fatal("opaque string parsing failed", prefix)
		}
		// The first ST closes the opaque string; only the report after it applies.
		if tracker.records[""].State != "done" {
			t.Fatal("following OSC lost")
		}
	}
	for _, cancel := range []byte{0x18, 0x1a} {
		var tracker programStatusTracker
		tracker.feed(append([]byte("\x1b]7501;state=done"), cancel), time.Now())
		tracker.feed([]byte("\a"), time.Now())
		if len(tracker.records) != 0 {
			t.Fatal("cancelled OSC applied")
		}
	}
}

func TestProgramStatusLimitsBoundMemoryAndRecover(t *testing.T) {
	for _, st := range []string{"\a", "\x1b\\"} {
		for _, size := range []int{4096, 4097, 1 << 20} {
			var tracker programStatusTracker
			prefix := "\x1b]7501;state=working:future="
			input := prefix + strings.Repeat("a", size-len(prefix)-len(st)) + st
			tracker.feed([]byte(input), time.Now())
			if len(tracker.osc) > 4096 || (len(tracker.records) != 0) != (size == 4096) {
				t.Fatalf("sequence size %d with %q", size, st)
			}
			tracker.feed([]byte(statusOSC("state=done")), time.Now())
			if tracker.records[""].State != "done" {
				t.Fatal("parser did not recover from oversized OSC")
			}
		}
	}
}

func TestProgramStatusRecordsInheritanceClearingAndLifetimes(t *testing.T) {
	var tracker programStatusTracker
	feed := func(body string) { tracker.feed([]byte(statusOSC(body)), time.Now()) }
	feed("state=working:app=deploy:msg=" + statusB64("Deploying"))
	feed("state=working:id=us-east:progress=40:title=" + statusB64("US East"))
	feed("state=blocked:kind=auth:id=eu/west")
	state := tracker.agentState("shell")
	if state.ID != "deploy" || state.SessionID != "osc7501:eu/west" || state.Reason != "auth" {
		t.Fatal("nearest/root ancestor not inherited", state)
	}
	feed("state=idle:app=region:id=eu")
	if tracker.agentState("shell").ID != "region" {
		t.Fatal("ancestor update did not change inherited app")
	}
	beforeApp := SessionInfo{ID: "fixture", Agent: state}
	afterApp := SessionInfo{ID: "fixture", Agent: tracker.agentState("shell")}
	if AgentNoticeTransition(beforeApp, afterApp) || AgentNoticeID("host", beforeApp) != AgentNoticeID("host", afterApp) {
		t.Fatal("inherited app change replayed an existing waiting event")
	}
	feed("state=clear:id=eu")
	if _, exists := tracker.records["eu/west"]; exists || tracker.agentState("shell").State != agents.Running {
		t.Fatal("subtree clear failed")
	}
	feed("state=done:id=us-east")
	if r := tracker.records["us-east"]; r.Progress != nil || r.Title != "" || r.Message != "" {
		t.Fatal("record replacement retained missing fields")
	}
	feed("state=error:id=failed")
	feed("state=idle:id=idle")
	feed("state=blocked:id=waiting")
	before := tracker.snapshot()
	tracker.feed([]byte("\x1b[?1049h\x1b[?1049l\x1b[!p"), time.Now())
	if !reflect.DeepEqual(before, tracker.snapshot()) {
		t.Fatal("screen switch or soft reset cleared records")
	}
	tracker.feed([]byte("\x1b]133;A;aid=1\a"), time.Now())
	if len(tracker.records) != 3 || tracker.records["us-east"].State != "done" || tracker.records["failed"].State != "error" || tracker.records["idle"].State != "idle" {
		t.Fatal("prompt lifetimes", tracker.snapshot())
	}
	feed("state=working:id=running")
	tracker.processExit(time.Now())
	if len(tracker.records) != 3 {
		t.Fatal("process exit did not clear transient records")
	}
	feed("state=working:id=worker")
	tracker.feed([]byte("\x1b]133;C\a"), time.Now())
	if len(tracker.records) != 1 || tracker.records["worker"].State != "working" {
		t.Fatal("next command retained old results or cleared ongoing work")
	}
	tracker.feed([]byte("\x1bc"), time.Now())
	if len(tracker.records) != 0 {
		t.Fatal("RIS retained records")
	}
	feed("state=working:id=a")
	feed("state=done:id=ab")
	feed("state=clear:id=a")
	if len(tracker.records) != 1 {
		t.Fatal("clear incorrectly matched a sibling prefix")
	}
	feed("state=clear")
	if len(tracker.records) != 0 {
		t.Fatal("root clear did not clear everything")
	}
}

func TestProgramStatusEvictsLeastRecentlyUpdatedAndReconnectIsQuiet(t *testing.T) {
	var tracker programStatusTracker
	feed := func(body string) { tracker.feed([]byte(statusOSC(body)), time.Unix(1, 0)) }
	for i := 0; i < 256; i++ {
		feed(fmt.Sprintf("state=idle:id=job%d", i))
	}
	feed("state=idle:id=job0")
	feed("state=blocked:id=job256:app=claude-code:kind=question:msg=" + statusB64("Continue?"))
	if len(tracker.records) != 256 {
		t.Fatal("record cap")
	}
	if _, exists := tracker.records["job1"]; exists {
		t.Fatal("wrong LRU record evicted")
	}
	if _, exists := tracker.records["job0"]; !exists {
		t.Fatal("updated record evicted")
	}
	state := tracker.agentState("shell")
	if state.ID != "claude" || state.WaitRevision == 0 {
		t.Fatal("program alias or waiting revision", state)
	}
	in := SessionInfo{ID: "fixture", Agent: state, ProgramStatuses: tracker.snapshot()}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var restored SessionInfo
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if AgentNoticeTransition(in, restored) || AgentNoticeID("host", in) != AgentNoticeID("host", restored) || len(restored.ProgramStatuses) != 256 {
		t.Fatal("reconnect lost state or repeated notification")
	}
	feed("state=blocked:id=job256:app=claude-code:kind=question:progress=60:msg=" + statusB64("Still waiting"))
	next := SessionInfo{ID: "fixture", Agent: tracker.agentState("shell")}
	if AgentNoticeTransition(in, next) {
		t.Fatal("text/progress update repeated a waiting notification")
	}
	feed("state=working:id=job256:app=claude-code")
	feed("state=done:id=job256:app=claude-code")
	completed := SessionInfo{ID: "fixture", Agent: tracker.agentState("shell")}
	if !AgentNoticeTransition(next, completed) || completed.Agent.CompletionRevision == 0 {
		t.Fatal("completion lost")
	}
	feed("state=done:id=job256:app=claude-code:msg=" + statusB64("Done"))
	duplicate := SessionInfo{ID: "fixture", Agent: tracker.agentState("shell")}
	if AgentNoticeTransition(completed, duplicate) || AgentNoticeID("host", completed) != AgentNoticeID("host", duplicate) {
		t.Fatal("duplicate completion replayed")
	}
}

func TestProgramStatusPresentationAndNotificationLimits(t *testing.T) {
	var tracker programStatusTracker
	tracker.feed([]byte(statusOSC("state=done:app=brew:msg="+statusB64("plain <b>result</b>\u202efake\u200b"))), time.Now())
	in := SessionInfo{Agent: tracker.agentState("shell")}
	if got := AgentNoticeBody(t.TempDir(), in); got != "plain <b>result</b>fake" {
		t.Fatal("unsafe formatting or markup interpreted", got)
	}
	var limiter ProgramNoticeLimiter
	now := time.Now()
	if !limiter.Allow("pane", in.Agent, now) || limiter.Allow("pane", in.Agent, now.Add(time.Second)) || !limiter.Allow("pane", in.Agent, now.Add(2*time.Second)) || !limiter.Allow("other-pane", in.Agent, now) {
		t.Fatal("program notice limiter")
	}
	if !limiter.Allow("pane", AgentState{}, now) {
		t.Fatal("hook reminder rate-limited")
	}
}

func FuzzProgramStatusStream(f *testing.F) {
	for _, input := range []string{statusOSC("?"), statusOSC("state=working:app=cargo:progress=0"), statusOSC("state=blocked:kind=auth"), "\x1bP" + statusOSC("state=done"), "\x1b]7501;" + strings.Repeat("x", 5000) + "\a"} {
		f.Add([]byte(input), uint16(7))
	}
	f.Fuzz(func(t *testing.T, data []byte, split uint16) {
		at := min(int(split), len(data))
		var tracker programStatusTracker
		replies := tracker.feed(data[:at], time.Unix(1, 0)) + tracker.feed(data[at:], time.Unix(1, 0))
		var whole programStatusTracker
		if want := whole.feed(data, time.Unix(1, 0)); replies != want || !reflect.DeepEqual(tracker.snapshot(), whole.snapshot()) {
			t.Fatal("PTY chunk boundaries changed query replies or records")
		}
		if len(tracker.osc) > 4096 || len(tracker.records) > 256 {
			t.Fatal("unbounded parser")
		}
		tracker.agentState("fixture")
	})
}
