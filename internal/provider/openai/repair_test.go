package openai

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/stream"
)

func TestOpenAIStream_FlushNextToolCall_RepairsArgs(t *testing.T) {
	s := &openaiStream{
		toolCalls: map[int64]*toolCallState{
			0: {
				id:   "call_1",
				name: "fs.edit",
				// Malformed: wrapped in code fence + trailing comma
				args: "```json\n{\"path\":\"/x\",\"old_string\":\"a\",\"new_string\":\"b\",}\n```",
			},
		},
	}

	ev, ok := s.flushNextToolCall()
	if !ok {
		t.Fatal("flushNextToolCall returned ok=false with pending call")
	}
	if ev.Type != stream.EventToolCallDone {
		t.Errorf("event type = %v, want EventToolCallDone", ev.Type)
	}
	if ev.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q", ev.ToolCallID)
	}
	if !json.Valid(ev.Args) {
		t.Fatalf("Args is not valid JSON after repair: %q", string(ev.Args))
	}
	var parsed map[string]string
	if err := json.Unmarshal(ev.Args, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["path"] != "/x" || parsed["old_string"] != "a" || parsed["new_string"] != "b" {
		t.Errorf("data lost in repair: %v", parsed)
	}

	// Second call: queue empty.
	if _, ok := s.flushNextToolCall(); ok {
		t.Error("flushNextToolCall returned ok=true on empty queue")
	}
}

func TestOpenAIStream_FlushNextToolCall_ValidArgsPassThrough(t *testing.T) {
	original := `{"path":"/x"}`
	s := &openaiStream{
		toolCalls: map[int64]*toolCallState{
			0: {id: "call_1", name: "fs.read", args: original},
		},
	}
	ev, ok := s.flushNextToolCall()
	if !ok {
		t.Fatal("flushNextToolCall returned ok=false")
	}
	if string(ev.Args) != original {
		t.Errorf("valid args mutated: %q → %q", original, string(ev.Args))
	}
}

func TestRepairArgs_ValidPassesThrough(t *testing.T) {
	cases := []string{
		`{"path":"/foo.go"}`,
		`{}`,
		`{"a":1,"b":[1,2,3],"c":{"d":"e"}}`,
		`{"text":"contains \"quoted\" inner"}`,
	}
	for _, in := range cases {
		got, repaired := repairArgs(in)
		if repaired {
			t.Errorf("repairArgs(%q): repaired=true on valid input", in)
		}
		if string(got) != in {
			t.Errorf("repairArgs(%q): mutated valid input → %q", in, string(got))
		}
		if !json.Valid(got) {
			t.Errorf("repairArgs(%q): output not valid JSON: %q", in, string(got))
		}
	}
}

func TestRepairArgs_EmptyInput(t *testing.T) {
	got, repaired := repairArgs("")
	if repaired {
		t.Error("empty input should not be marked repaired")
	}
	if string(got) != "" {
		t.Errorf("empty input → %q, want empty", string(got))
	}
}

func TestRepairArgs_TrimsTrailingComma(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"a":1,}`, `{"a":1}`},
		{`{"a":1, "b":2,}`, `{"a":1, "b":2}`},
		{`{"a":[1,2,3,]}`, `{"a":[1,2,3]}`},
		{`{"a":1 , }`, `{"a":1  }`},
	}
	for _, tc := range cases {
		got, repaired := repairArgs(tc.in)
		if !repaired {
			t.Errorf("repairArgs(%q): repaired=false, want true", tc.in)
		}
		if !json.Valid(got) {
			t.Errorf("repairArgs(%q): output not valid JSON: %q", tc.in, string(got))
		}
		if string(got) != tc.want {
			t.Errorf("repairArgs(%q) = %q, want %q", tc.in, string(got), tc.want)
		}
	}
}

func TestRepairArgs_StripsCodeFences(t *testing.T) {
	cases := []string{
		"```json\n{\"path\":\"/x\"}\n```",
		"```\n{\"path\":\"/x\"}\n```",
		"```json\n{\"path\":\"/x\"}",
		"  ```json {\"path\":\"/x\"} ```  ",
	}
	for _, in := range cases {
		got, repaired := repairArgs(in)
		if !repaired {
			t.Errorf("repairArgs(%q): repaired=false, want true", in)
		}
		if !json.Valid(got) {
			t.Errorf("repairArgs(%q): output not valid JSON: %q", in, string(got))
		}
		var parsed map[string]any
		if err := json.Unmarshal(got, &parsed); err != nil {
			t.Errorf("repairArgs(%q): unmarshal: %v", in, err)
			continue
		}
		if parsed["path"] != "/x" {
			t.Errorf("repairArgs(%q): lost data, got %v", in, parsed)
		}
	}
}

func TestRepairArgs_ExtractsFromProse(t *testing.T) {
	cases := []string{
		`Here are the arguments: {"path":"/x"}`,
		`{"path":"/x"} -- that's the call`,
		`Sure, calling with {"path":"/x"} now.`,
	}
	for _, in := range cases {
		got, repaired := repairArgs(in)
		if !repaired {
			t.Errorf("repairArgs(%q): repaired=false, want true", in)
		}
		if !json.Valid(got) {
			t.Errorf("repairArgs(%q): output not valid JSON: %q", in, string(got))
		}
	}
}

func TestRepairArgs_HandlesBracesInsideStrings(t *testing.T) {
	in := `{"snippet":"if x { return y }","other":"a}b"}`
	got, _ := repairArgs(in)
	if !json.Valid(got) {
		t.Fatalf("output not valid JSON: %q", string(got))
	}
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["snippet"] != "if x { return y }" {
		t.Errorf("snippet corrupted: %q", parsed["snippet"])
	}
	if parsed["other"] != "a}b" {
		t.Errorf("other corrupted: %q", parsed["other"])
	}
}

func TestRepairArgs_TakesFirstBalancedBlock(t *testing.T) {
	// Some small models emit two JSON objects back-to-back; take the first.
	in := `{"path":"/a"} {"path":"/b"}`
	got, _ := repairArgs(in)
	if !json.Valid(got) {
		t.Fatalf("not valid: %q", string(got))
	}
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["path"] != "/a" {
		t.Errorf("expected first block, got %q", parsed["path"])
	}
}

func TestRepairArgs_UnrepairableFails(t *testing.T) {
	cases := []string{
		`{"a":`,                  // truncated
		`not json at all`,        // no JSON
		`{{{`,                    // unbalanced
		`{"a":1`,                 // missing close
	}
	for _, in := range cases {
		got, repaired := repairArgs(in)
		// Either: returns valid JSON (we got lucky) or returns original + repaired=false
		if json.Valid(got) {
			continue // acceptable — we managed to repair
		}
		if repaired {
			t.Errorf("repairArgs(%q): claims repaired but output invalid: %q", in, string(got))
		}
	}
}

func TestRepairArgs_FencesAndTrailingCommaCombined(t *testing.T) {
	in := "```json\n{\"path\":\"/x\",}\n```"
	got, repaired := repairArgs(in)
	if !repaired {
		t.Fatal("expected repaired=true")
	}
	if !json.Valid(got) {
		t.Fatalf("not valid: %q", string(got))
	}
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["path"] != "/x" {
		t.Errorf("lost data: %v", parsed)
	}
}
