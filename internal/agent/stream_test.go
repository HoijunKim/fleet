package agent

import "testing"

func TestParseInit(t *testing.T) {
	evs := Parse([]byte(`{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-x"}`))
	if len(evs) != 1 || evs[0].Kind != KindInit || evs[0].SessionID != "sess-1" || evs[0].Model != "claude-x" {
		t.Fatalf("init parse = %+v", evs)
	}
}

func TestParseAssistantTextAndTool(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Looking"},{"type":"tool_use","name":"Read","input":{"file_path":"app.go"}}]}}`
	evs := Parse([]byte(line))
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(evs), evs)
	}
	// A complete assistant text block is NOT partial (deltas carry the stream).
	if evs[0].Kind != KindText || evs[0].Text != "Looking" || evs[0].Partial {
		t.Errorf("text event = %+v", evs[0])
	}
	if evs[1].Kind != KindTool || evs[1].ToolName != "Read" || string(evs[1].ToolInput) != `{"file_path":"app.go"}` {
		t.Errorf("tool event = %+v", evs[1])
	}
}

func TestParsePartialTextDelta(t *testing.T) {
	line := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"abc"}}}`
	evs := Parse([]byte(line))
	if len(evs) != 1 || evs[0].Kind != KindText || evs[0].Text != "abc" || !evs[0].Partial {
		t.Fatalf("delta parse = %+v", evs)
	}

	// The real CLI carries session_id as a sibling of the inner "event" object,
	// not inside it. Parse must backfill SessionID from the outer envelope onto
	// the partial delta produced by recursing into the inner event.
	lineWithSession := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}},"session_id":"abc-123"}`
	evsWithSession := Parse([]byte(lineWithSession))
	if len(evsWithSession) != 1 || evsWithSession[0].Kind != KindText || evsWithSession[0].Text != "hi" || !evsWithSession[0].Partial {
		t.Fatalf("delta parse with session = %+v", evsWithSession)
	}
	if evsWithSession[0].SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want %q", evsWithSession[0].SessionID, "abc-123")
	}
}

func TestParseResult(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done","total_cost_usd":0.012,"session_id":"sess-1","usage":{"input_tokens":10,"output_tokens":20}}`
	evs := Parse([]byte(line))
	if len(evs) != 1 || evs[0].Kind != KindResult {
		t.Fatalf("result parse = %+v", evs)
	}
	if evs[0].Result != "done" || evs[0].CostUSD != 0.012 || evs[0].InputTokens != 10 || evs[0].OutputTokens != 20 {
		t.Errorf("result fields = %+v", evs[0])
	}
}

func TestParseTolerant(t *testing.T) {
	for _, bad := range []string{"", "   ", "not json", "{bad", `{"type":"unknown_thing"}`, `[]`} {
		if evs := Parse([]byte(bad)); evs != nil {
			t.Errorf("Parse(%q) = %+v, want nil", bad, evs)
		}
	}
}
