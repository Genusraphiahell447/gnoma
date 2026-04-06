package hook

import (
	"context"
	"testing"
)

// mockExecutor is a test Executor with configurable return values and payload capture.
type mockExecutor struct {
	result          HookResult
	err             error
	receivedPayload []byte
}

func (m *mockExecutor) Execute(_ context.Context, payload []byte) (HookResult, error) {
	m.receivedPayload = append([]byte(nil), payload...)
	return m.result, m.err
}

func newAllow() *mockExecutor { return &mockExecutor{result: HookResult{Action: Allow}} }
func newDeny() *mockExecutor  { return &mockExecutor{result: HookResult{Action: Deny}} }
func newSkip() *mockExecutor  { return &mockExecutor{result: HookResult{Action: Skip}} }
func newTransform(output []byte) *mockExecutor {
	return &mockExecutor{result: HookResult{Action: Allow, Output: output}}
}
func newError(failOpen bool) *mockExecutor {
	return &mockExecutor{
		result: HookResult{},
		err:    context.DeadlineExceeded,
	}
}

func makeHandler(event EventType, ex Executor) Handler {
	return Handler{
		def:      HookDef{Name: "h", Event: event, Command: CommandTypeShell, Exec: "x"},
		executor: ex,
	}
}

func makePatternHandler(pattern string, ex Executor) Handler {
	return Handler{
		def:      HookDef{Name: "h", Event: PreToolUse, Command: CommandTypeShell, Exec: "x", ToolPattern: pattern},
		executor: ex,
	}
}

// --- resolveAction unit tests ---

func TestResolveAction_EmptyResults_Allow(t *testing.T) {
	if got := resolveAction(nil, PreToolUse); got != Allow {
		t.Errorf("resolveAction(nil) = %v, want Allow", got)
	}
}

func TestResolveAction_AllAllow(t *testing.T) {
	results := []HookResult{
		{Action: Allow},
		{Action: Allow},
	}
	if got := resolveAction(results, PreToolUse); got != Allow {
		t.Errorf("all Allow: got %v, want Allow", got)
	}
}

func TestResolveAction_OneDeny(t *testing.T) {
	results := []HookResult{
		{Action: Allow},
		{Action: Deny},
		{Action: Allow},
	}
	if got := resolveAction(results, PreToolUse); got != Deny {
		t.Errorf("one Deny: got %v, want Deny", got)
	}
}

func TestResolveAction_SkipAbstains(t *testing.T) {
	results := []HookResult{
		{Action: Allow},
		{Action: Skip},
	}
	if got := resolveAction(results, PreToolUse); got != Allow {
		t.Errorf("skip abstains: got %v, want Allow", got)
	}
}

func TestResolveAction_AllSkip(t *testing.T) {
	results := []HookResult{
		{Action: Skip},
		{Action: Skip},
	}
	// all abstain → Allow (no one objected)
	if got := resolveAction(results, PreToolUse); got != Allow {
		t.Errorf("all Skip: got %v, want Allow", got)
	}
}

func TestResolveAction_PostToolUseDenyBecomesSkip(t *testing.T) {
	results := []HookResult{
		{Action: Deny},
	}
	if got := resolveAction(results, PostToolUse); got != Allow {
		t.Errorf("PostToolUse Deny treated as Skip: got %v, want Allow", got)
	}
}

// --- Dispatcher.Fire tests ---

func TestDispatcher_NilReceiver_Allow(t *testing.T) {
	var d *Dispatcher
	payload := []byte(`{"tool":"bash"}`)
	got, action, err := d.Fire(PreToolUse, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != Allow {
		t.Errorf("nil dispatcher: action = %v, want Allow", action)
	}
	if string(got) != string(payload) {
		t.Errorf("nil dispatcher: payload mutated")
	}
}

func TestDispatcher_EmptyChain_Allow(t *testing.T) {
	d := &Dispatcher{chains: make(map[EventType][]Handler)}
	payload := []byte(`{}`)
	_, action, err := d.Fire(PreToolUse, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != Allow {
		t.Errorf("empty chain: action = %v, want Allow", action)
	}
}

func TestDispatcher_SingleHandler_Allow(t *testing.T) {
	ex := newAllow()
	d := dispatcherWith(PreToolUse, makeHandler(PreToolUse, ex))
	_, action, err := d.Fire(PreToolUse, []byte(`{}`))
	if err != nil || action != Allow {
		t.Errorf("got action=%v err=%v, want Allow nil", action, err)
	}
}

func TestDispatcher_SingleHandler_Deny(t *testing.T) {
	d := dispatcherWith(PreToolUse, makeHandler(PreToolUse, newDeny()))
	_, action, _ := d.Fire(PreToolUse, []byte(`{}`))
	if action != Deny {
		t.Errorf("action = %v, want Deny", action)
	}
}

func TestDispatcher_SingleHandler_Skip(t *testing.T) {
	d := dispatcherWith(PreToolUse, makeHandler(PreToolUse, newSkip()))
	_, action, _ := d.Fire(PreToolUse, []byte(`{}`))
	if action != Allow {
		t.Errorf("skip abstains: action = %v, want Allow", action)
	}
}

func TestDispatcher_AllMustAllow_TwoAllowOneDeny(t *testing.T) {
	d := dispatcherWith(PreToolUse,
		makeHandler(PreToolUse, newAllow()),
		makeHandler(PreToolUse, newDeny()),
		makeHandler(PreToolUse, newAllow()),
	)
	_, action, _ := d.Fire(PreToolUse, []byte(`{}`))
	if action != Deny {
		t.Errorf("action = %v, want Deny", action)
	}
}

func TestDispatcher_TransformChaining(t *testing.T) {
	// Handler A transforms payload; handler B receives the transformed version.
	transformed := []byte(`{"tool":"transformed"}`)
	exA := newTransform(transformed)
	exB := newAllow()

	d := dispatcherWith(PreToolUse,
		makeHandler(PreToolUse, exA),
		makeHandler(PreToolUse, exB),
	)
	d.Fire(PreToolUse, []byte(`{"tool":"original"}`))

	if string(exB.receivedPayload) != string(transformed) {
		t.Errorf("exB received %q, want %q", exB.receivedPayload, transformed)
	}
}

func TestDispatcher_TransformChaining_EmptyOutputPassesThrough(t *testing.T) {
	// Handler A returns no output (no transform); handler B should receive the original payload.
	original := []byte(`{"tool":"original"}`)
	exA := newAllow() // no Output
	exB := newAllow()

	d := dispatcherWith(PreToolUse,
		makeHandler(PreToolUse, exA),
		makeHandler(PreToolUse, exB),
	)
	d.Fire(PreToolUse, original)

	if string(exB.receivedPayload) != string(original) {
		t.Errorf("exB received %q, want %q", exB.receivedPayload, original)
	}
}

func TestDispatcher_ToolPattern_Match(t *testing.T) {
	ex := newDeny()
	payload := MarshalPreToolPayload("bash", nil)
	d := dispatcherWith(PreToolUse, makePatternHandler("bash*", ex))
	_, action, _ := d.Fire(PreToolUse, payload)
	if action != Deny {
		t.Errorf("pattern matched: action = %v, want Deny", action)
	}
}

func TestDispatcher_ToolPattern_NoMatch(t *testing.T) {
	ex := newDeny()
	payload := MarshalPreToolPayload("fs.read", nil)
	d := dispatcherWith(PreToolUse, makePatternHandler("bash*", ex))
	_, action, _ := d.Fire(PreToolUse, payload)
	// handler skipped — no deniers → Allow
	if action != Allow {
		t.Errorf("pattern no match: action = %v, want Allow", action)
	}
}

func TestDispatcher_ToolPattern_Empty_MatchesAll(t *testing.T) {
	ex := newDeny()
	payload := MarshalPreToolPayload("fs.read", nil)
	d := dispatcherWith(PreToolUse, makePatternHandler("", ex))
	_, action, _ := d.Fire(PreToolUse, payload)
	if action != Allow {
		// empty pattern + Deny → resolveAction sees Deny → Deny
		// wait, empty pattern means fire for all tools
	}
	// correct: empty pattern fires → Deny
	if action != Deny {
		t.Errorf("empty pattern matches all: action = %v, want Deny", action)
	}
}

func TestDispatcher_ErrorFailOpenTrue(t *testing.T) {
	ex := &mockExecutor{result: HookResult{}, err: context.DeadlineExceeded}
	d := dispatcherWith(PreToolUse, Handler{
		def:      HookDef{Name: "h", Event: PreToolUse, Command: CommandTypeShell, Exec: "x", FailOpen: true},
		executor: ex,
	})
	_, action, _ := d.Fire(PreToolUse, []byte(`{}`))
	if action != Allow {
		t.Errorf("error+fail_open=true: action = %v, want Allow", action)
	}
}

func TestDispatcher_ErrorFailOpenFalse(t *testing.T) {
	ex := &mockExecutor{result: HookResult{}, err: context.DeadlineExceeded}
	d := dispatcherWith(PreToolUse, Handler{
		def:      HookDef{Name: "h", Event: PreToolUse, Command: CommandTypeShell, Exec: "x", FailOpen: false},
		executor: ex,
	})
	_, action, _ := d.Fire(PreToolUse, []byte(`{}`))
	if action != Deny {
		t.Errorf("error+fail_open=false: action = %v, want Deny", action)
	}
}

func TestDispatcher_PostToolUseDenyTreatedAsSkip(t *testing.T) {
	d := dispatcherWith(PostToolUse, makeHandler(PostToolUse, newDeny()))
	_, action, _ := d.Fire(PostToolUse, []byte(`{}`))
	if action != Allow {
		t.Errorf("PostToolUse deny → skip → Allow: got %v", action)
	}
}

func TestDispatcher_FinalTransformedPayloadReturned(t *testing.T) {
	finalPayload := []byte(`{"command":"final"}`)
	d := dispatcherWith(PreToolUse, makeHandler(PreToolUse, newTransform(finalPayload)))
	got, _, _ := d.Fire(PreToolUse, []byte(`{"command":"original"}`))
	if string(got) != string(finalPayload) {
		t.Errorf("Fire() returned %q, want %q", got, finalPayload)
	}
}

func TestDispatcher_NoTransform_OriginalPayloadReturned(t *testing.T) {
	original := []byte(`{"command":"original"}`)
	d := dispatcherWith(PreToolUse, makeHandler(PreToolUse, newAllow()))
	got, _, _ := d.Fire(PreToolUse, original)
	if string(got) != string(original) {
		t.Errorf("Fire() returned %q, want %q", got, original)
	}
}

// dispatcherWith builds a Dispatcher with pre-built handlers for a single event.
func dispatcherWith(event EventType, handlers ...Handler) *Dispatcher {
	d := &Dispatcher{chains: make(map[EventType][]Handler)}
	d.chains[event] = handlers
	return d
}
