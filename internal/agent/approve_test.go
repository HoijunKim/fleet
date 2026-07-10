package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func postApprove(t *testing.T, url, body string) map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApprovalServerAllow(t *testing.T) {
	coord := NewCoordinator()
	gotCh := make(chan ActionRequest, 1)
	srv := NewApprovalServer(nil, coord, time.Second, func(a ActionRequest) { gotCh <- a })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)

	resCh := make(chan map[string]any, 1)
	go func() {
		resCh <- postApprove(t, srv.URL(), `{"tool_name":"Edit","tool_input":{"file_path":"x"},"cwd":"/r","session_id":"s"}`)
	}()

	req := <-gotCh
	if req.ToolName != "Edit" || req.Cwd != "/r" || req.SessionID != "s" || req.ID == "" {
		t.Fatalf("action req = %+v", req)
	}
	if !coord.Decide(req.ID, true, "yes") {
		t.Fatal("Decide failed")
	}
	res := <-resCh
	if res["approved"] != true || res["reason"] != "yes" {
		t.Errorf("response = %+v", res)
	}
}

func TestApprovalServerTimeout(t *testing.T) {
	coord := NewCoordinator()
	srv := NewApprovalServer(nil, coord, 20*time.Millisecond, func(a ActionRequest) {})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)
	res := postApprove(t, srv.URL(), `{"tool_name":"Bash"}`)
	if res["approved"] != false {
		t.Errorf("timeout must deny: %+v", res)
	}
}

// gateStub is a fake claude that reads FLEET_HOOK_URL (set by Driver.Run),
// POSTs a mutating tool call to fleet's loopback ApprovalServer exactly as the
// real PreToolUse hook would, and emits a result reflecting the decision. It
// uses only escaped double-quoted strings so it embeds cleanly as a raw string.
const gateStub = `package main
import ("bytes";"encoding/json";"fmt";"io";"net/http";"os")
func main(){
	url:=os.Getenv("FLEET_HOOK_URL")
	resp,err:=http.Post(url,"application/json",bytes.NewReader([]byte("{\"tool_name\":\"Edit\",\"tool_input\":{},\"session_id\":\"s\",\"cwd\":\"/r\"}")))
	if err!=nil{ fmt.Println("post failed"); os.Exit(1) }
	defer resp.Body.Close()
	data,_:=io.ReadAll(resp.Body)
	var d map[string]any
	json.Unmarshal(data,&d)
	res:="denied"
	if a,ok:=d["approved"].(bool); ok && a { res="approved" }
	out,_:=json.Marshal(map[string]any{"type":"result","subtype":"success","result":res})
	os.Stdout.Write(append(out,'\n'))
}
`

// TestDriverGateEndToEnd wires the real Driver + ApprovalServer + Coordinator:
// the stub POSTs through the loopback endpoint, the coordinator decision flows
// back, and the stub's emitted result proves the whole driver->hook->gate->
// driver pipeline works (this would fail under the old delete-before-send
// Coordinator, which lost a Decide that raced ahead of Await).
func TestDriverGateEndToEnd(t *testing.T) {
	coord := NewCoordinator()
	srv := NewApprovalServer(nil, coord, 2*time.Second, func(a ActionRequest) {
		coord.Decide(a.ID, true, "approved in test") // stand in for the GUI approving
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)

	bin := buildStub(t, gateStub)
	var got []Event
	err := Driver{Bin: bin, WaitDelay: time.Second}.Run(
		context.Background(),
		Options{Prompt: "q", HookURL: srv.URL()},
		func(ev Event) { got = append(got, ev) },
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no events streamed from the gated run")
	}
	last := got[len(got)-1]
	if last.Kind != KindResult || last.Result != "approved" {
		t.Fatalf("gate decision did not flow driver->hook->coordinator->stub: %+v", got)
	}
}
