package action

import (
	"runtime"
	"strings"
	"testing"
)

func TestEditorCmd(t *testing.T) {
	c := EditorCmd("code", "/repo/x")
	if len(c.Args) < 2 || c.Args[0] != "code" || c.Args[len(c.Args)-1] != "/repo/x" {
		t.Errorf("args=%v", c.Args)
	}
}

func TestTerminalCmdSetsDir(t *testing.T) {
	c := TerminalCmd("wt", "/repo/x")
	if c.Dir != "/repo/x" {
		t.Errorf("dir=%q", c.Dir)
	}
}

func TestRunInDirCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	var line string
	if runtime.GOOS == "windows" {
		line = "echo hello"
	} else {
		line = "echo hello"
	}
	out, err := RunInDir(dir, line)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("out=%q", out)
	}
}

func TestRunInDirReportsFailure(t *testing.T) {
	dir := t.TempDir()
	_, err := RunInDir(dir, "definitely-not-a-real-command-xyz")
	if err == nil {
		t.Error("expected error for missing command")
	}
}
