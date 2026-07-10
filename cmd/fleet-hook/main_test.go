package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoijun/fleet/internal/agent"
)

// hookSpecific / hookOutput mirror the JSON agent.RunHook writes, so the
// standalone-wrapper test can assert the decision shape without reaching into
// the agent package's unexported types.
type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

func decode(t *testing.T, b []byte) hookOutput {
	t.Helper()
	var o hookOutput
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("bad hook output %q: %v", b, err)
	}
	return o
}

func TestRunApprove(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"approved":true,"reason":"ok"}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	in := bytes.NewBufferString(`{"tool_name":"Edit","tool_input":{"file_path":"x"},"cwd":"/repo"}`)
	agent.RunHook(in, &out, srv.URL, srv.Client())

	o := decode(t, out.Bytes())
	if o.HookSpecificOutput.PermissionDecision != "allow" || o.HookSpecificOutput.PermissionDecisionReason != "ok" {
		t.Errorf("decision = %+v", o.HookSpecificOutput)
	}
	if o.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q", o.HookSpecificOutput.HookEventName)
	}
	if gotBody == "" || gotBody[0] != '{' {
		t.Errorf("server did not receive the tool JSON: %q", gotBody)
	}
}

func TestRunDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"approved":false,"reason":"blocked"}`)
	}))
	defer srv.Close()
	var out bytes.Buffer
	agent.RunHook(bytes.NewBufferString(`{}`), &out, srv.URL, srv.Client())
	o := decode(t, out.Bytes())
	if o.HookSpecificOutput.PermissionDecision != "deny" || o.HookSpecificOutput.PermissionDecisionReason != "blocked" {
		t.Errorf("decision = %+v", o.HookSpecificOutput)
	}
}

func TestRunNoURLDenies(t *testing.T) {
	var out bytes.Buffer
	agent.RunHook(bytes.NewBufferString(`{}`), &out, "", http.DefaultClient)
	if decode(t, out.Bytes()).HookSpecificOutput.PermissionDecision != "deny" {
		t.Error("missing FLEET_HOOK_URL must deny")
	}
}

func TestRunServerErrorDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	var out bytes.Buffer
	agent.RunHook(bytes.NewBufferString(`{}`), &out, srv.URL, srv.Client())
	if decode(t, out.Bytes()).HookSpecificOutput.PermissionDecision != "deny" {
		t.Error("5xx must deny")
	}
}
