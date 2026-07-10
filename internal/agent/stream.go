package agent

import (
	"bytes"
	"encoding/json"
)

// EventKind classifies a normalized stream event.
type EventKind string

const (
	KindInit   EventKind = "init"
	KindText   EventKind = "text"
	KindTool   EventKind = "tool_use"
	KindResult EventKind = "result"
)

// Event is one normalized item extracted from a stream-json line. A single line
// may yield several events (an assistant message can carry a text block and a
// tool_use block at once). Partial is true only for streamed text_delta chunks;
// a complete assistant "text" block is Partial=false, so a consumer streaming
// deltas can ignore the redundant complete block that --include-partial-messages
// also emits.
type Event struct {
	Kind         EventKind
	SessionID    string
	Model        string
	Text         string
	Partial      bool
	ToolName     string
	ToolInput    json.RawMessage
	Result       string
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

// block is one content block inside an assistant/user message.
type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// envelope is the tolerant shape every line is decoded into. Unknown fields are
// ignored; missing fields stay zero.
type envelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Session string `json:"session_id"`
	Model   string `json:"model"`
	Message struct {
		Content []block `json:"content"`
		Model   string  `json:"model"`
	} `json:"message"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Event   json.RawMessage `json:"event"`
	Result  string          `json:"result"`
	CostUSD float64         `json:"total_cost_usd"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Parse decodes one newline-delimited JSON line into zero or more normalized
// events. Blank lines, non-object lines, malformed JSON, and unrecognized
// shapes yield nil - it never panics, so a noisy stream cannot crash the driver.
func Parse(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return nil
	}
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			return []Event{{Kind: KindInit, SessionID: e.Session, Model: e.Model}}
		}
		return nil
	case "assistant":
		model := e.Message.Model
		if model == "" {
			model = e.Model
		}
		var out []Event
		for _, b := range e.Message.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					out = append(out, Event{Kind: KindText, Text: b.Text, Model: model, SessionID: e.Session})
				}
			case "tool_use":
				out = append(out, Event{Kind: KindTool, ToolName: b.Name, ToolInput: b.Input, SessionID: e.Session})
			}
		}
		return out
	case "stream_event":
		if len(e.Event) > 0 {
			return Parse(e.Event)
		}
		return nil
	case "content_block_delta":
		if e.Delta.Type == "text_delta" && e.Delta.Text != "" {
			return []Event{{Kind: KindText, Text: e.Delta.Text, Partial: true, SessionID: e.Session}}
		}
		return nil
	case "result":
		return []Event{{
			Kind:         KindResult,
			Result:       e.Result,
			CostUSD:      e.CostUSD,
			SessionID:    e.Session,
			InputTokens:  e.Usage.InputTokens,
			OutputTokens: e.Usage.OutputTokens,
		}}
	default:
		return nil
	}
}
