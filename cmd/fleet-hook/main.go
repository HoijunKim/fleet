// Command fleet-hook is the PreToolUse hook helper the fleet app registers with
// the claude CLI (via a run-scoped --settings file). It reads the tool call
// JSON on stdin, forwards it to the running fleet app over the loopback URL in
// FLEET_HOOK_URL, and prints the user's allow/deny decision as the CLI's hook
// output. Any failure fails safe to deny. No dependency on bash/jq/curl.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

type decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

func main() {
	// Generous timeout: the app holds the request open while a human approves.
	client := &http.Client{Timeout: 15 * time.Minute}
	run(os.Stdin, os.Stdout, os.Getenv("FLEET_HOOK_URL"), client)
}

// run reads the tool JSON from in, POSTs it to hookURL, and writes the hook
// decision to out. It always writes a valid decision; on any error it denies
// with an explanatory reason (fail-safe).
func run(in io.Reader, out io.Writer, hookURL string, client *http.Client) {
	body, _ := io.ReadAll(io.LimitReader(in, 1<<20))
	if hookURL == "" {
		emit(out, false, "fleet approval endpoint unavailable")
		return
	}
	req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
	if err != nil {
		emit(out, false, "fleet approval request failed: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		emit(out, false, "fleet approval unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		emit(out, false, "fleet approval denied (status "+resp.Status+")")
		return
	}
	var d decision
	if err := json.Unmarshal(data, &d); err != nil {
		emit(out, false, "fleet approval malformed response")
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
	emit(out, d.Approved, reason)
}

// emit writes the CLI's PreToolUse hook decision as JSON (exit 0).
func emit(out io.Writer, approved bool, reason string) {
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
