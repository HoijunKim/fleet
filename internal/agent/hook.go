package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// hookDecision is the {approved, reason} JSON the ApprovalServer answers with.
type hookDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

// hookSpecific / hookOutput are the PreToolUse hook decision the claude CLI
// reads from the hook process's stdout.
type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

// RunHook is the PreToolUse hook body. It reads the tool call JSON from in,
// POSTs it to hookURL (the running fleet's loopback approval endpoint), and
// writes the user's allow/deny decision to out as the CLI's hook output. Any
// failure fails safe to deny. No dependency on bash/jq/curl; the caller owns
// client (and its timeout, human-scale so a person has time to approve).
func RunHook(in io.Reader, out io.Writer, hookURL string, client *http.Client) {
	body, _ := io.ReadAll(io.LimitReader(in, 1<<20))
	if hookURL == "" {
		emitHookDecision(out, false, "fleet approval endpoint unavailable")
		return
	}
	req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
	if err != nil {
		emitHookDecision(out, false, "fleet approval request failed: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		emitHookDecision(out, false, "fleet approval unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		emitHookDecision(out, false, "fleet approval denied (status "+resp.Status+")")
		return
	}
	var d hookDecision
	if err := json.Unmarshal(data, &d); err != nil {
		emitHookDecision(out, false, "fleet approval malformed response")
		return
	}
	reason := d.Reason
	if reason == "" {
		if d.Approved {
			reason = "approved in fleet"
		} else {
			reason = "rejected in fleet"
		}
	}
	emitHookDecision(out, d.Approved, reason)
}

// emitHookDecision writes the CLI's PreToolUse hook decision as JSON (exit 0).
func emitHookDecision(out io.Writer, approved bool, reason string) {
	pd := "deny"
	if approved {
		pd = "allow"
	}
	_ = json.NewEncoder(out).Encode(hookOutput{HookSpecificOutput: hookSpecific{
		HookEventName:            "PreToolUse",
		PermissionDecision:       pd,
		PermissionDecisionReason: reason,
	}})
}
