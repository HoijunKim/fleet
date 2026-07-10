package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func joined(args []string) string { return strings.Join(args, " ") }

func TestBuildArgs(t *testing.T) {
	o := Options{
		Prompt:       "what is off?",
		SystemPrompt: "role text",
		Policy:       Policy{Allowed: []string{"Read"}, Disallowed: []string{"Bash(rm:*)"}},
		SettingsPath: "/tmp/s.json",
		ResumeID:     "sess-9",
		MaxTurns:     24,
	}
	got := joined(BuildArgs(o))
	for _, want := range []string{
		"-p what is off?",
		"--output-format stream-json",
		"--include-partial-messages",
		"--verbose",
		"--append-system-prompt role text",
		"--allowedTools Read",
		"--disallowedTools Bash(rm:*)",
		"--settings /tmp/s.json",
		"--max-turns 24",
		"--resume sess-9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q\n%s", want, got)
		}
	}
}

func TestBuildArgsMinimal(t *testing.T) {
	got := joined(BuildArgs(Options{Prompt: "hi"}))
	if strings.Contains(got, "--resume") || strings.Contains(got, "--max-turns") || strings.Contains(got, "--append-system-prompt") {
		t.Errorf("optional flags must be omitted when empty: %s", got)
	}
}

func TestWriteHookSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := WriteHookSettings(p, "C:/fleet/fleet-hook.exe"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"PreToolUse"`, `"Edit|Write|Bash"`, "fleet-hook", `"command"`} {
		if !strings.Contains(s, want) {
			t.Errorf("settings missing %q\n%s", want, s)
		}
	}
}

// buildStub compiles a throwaway "claude" replacement whose behavior is the Go
// source in src, returning its path. It exercises the driver's spawn+stream
// pipeline (and, in Task 8, the gate) without the real CLI.
func buildStub(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "stub.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("build stub: %v\n%s", err, out)
	}
	return bin
}

const streamStub = `package main
import ("fmt";"os";"strings")
func main(){
	if p:=os.Getenv("FLEET_STUB_ARGV"); p!=""{ _=os.WriteFile(p,[]byte(strings.Join(os.Args[1:],"\n")),0644) }
	fmt.Println(` + "`" + `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-x"}` + "`" + `)
	fmt.Println(` + "`" + `{"type":"assistant","message":{"content":[{"type":"text","text":"Looking"}]}}` + "`" + `)
	fmt.Println("")
	fmt.Println(` + "`" + `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"app.go"}}]}}` + "`" + `)
	fmt.Println(` + "`" + `{"type":"result","subtype":"success","result":"done","total_cost_usd":0.012,"session_id":"sess-1"}` + "`" + `)
}
`

func TestRunStreamsEvents(t *testing.T) {
	bin := buildStub(t, streamStub)
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("FLEET_STUB_ARGV", argvFile)

	var got []Event
	d := Driver{Bin: bin, WaitDelay: time.Second}
	err := d.Run(context.Background(), Options{Prompt: "q", HookURL: "http://127.0.0.1:1/approve"}, func(ev Event) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(got), got)
	}
	if got[0].Kind != KindInit || got[0].SessionID != "sess-1" {
		t.Errorf("init = %+v", got[0])
	}
	if got[1].Kind != KindText || got[1].Text != "Looking" {
		t.Errorf("text = %+v", got[1])
	}
	if got[2].Kind != KindTool || got[2].ToolName != "Read" {
		t.Errorf("tool = %+v", got[2])
	}
	if got[3].Kind != KindResult || got[3].CostUSD != 0.012 {
		t.Errorf("result = %+v", got[3])
	}
	argv, _ := os.ReadFile(argvFile)
	if !strings.Contains(string(argv), "stream-json") {
		t.Errorf("stub did not receive expected argv: %s", argv)
	}
}

const sleepStub = `package main
import ("fmt";"time")
func main(){
	fmt.Println(` + "`" + `{"type":"system","subtype":"init","session_id":"s","model":"m"}` + "`" + `)
	time.Sleep(30*time.Second)
}
`

func TestRunCancel(t *testing.T) {
	bin := buildStub(t, sleepStub)
	ctx, cancel := context.WithCancel(context.Background())
	d := Driver{Bin: bin, WaitDelay: 300 * time.Millisecond}

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx, Options{Prompt: "q"}, func(ev Event) {
			if ev.Kind == KindInit {
				cancel() // cancel as soon as the process is alive and streaming
			}
		})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("want cancelled error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
