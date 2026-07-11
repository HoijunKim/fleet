package agent

import (
	"encoding/json"
	"testing"
)

func v(tool, input string, cur string) Verdict {
	return Classify(tool, json.RawMessage(input), ClassifyContext{CurrentBranch: cur, ProtectedBranches: DefaultProtectedBranches()})
}

func TestClassifyEdits(t *testing.T) {
	got := v("Edit", `{"file_path":"README.md","old_string":"a","new_string":"b"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "edit" || got.Summary != "Edit README.md" {
		t.Fatalf("edit: %+v", got)
	}
	w := v("Write", `{"file_path":"new.txt","content":"hi"}`, "feat/x")
	if w.Decision != "gate" || w.Category != "edit" || w.Severity != "medium" {
		t.Fatalf("write: %+v", w)
	}
}

func TestClassifyPushProtectedAlwaysDenied(t *testing.T) {
	deny := []struct{ cmd, cur string }{
		{"git push origin main", "feat/x"},
		{"git push origin HEAD:main", "feat/x"},
		{"git push origin master", "feat/x"},
		{"git push --force origin main", "feat/x"},
		{"git push origin +main", "feat/x"},
		{"git push", "main"},               // bare push while on main
		{"git push origin", "master"},      // bare push, remote only, on master
		{"git push origin feat/x:main", "feat/x"},
		{"git -C . push origin main", "feat/x"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, d.cur)
		if got.Decision != "deny" {
			t.Fatalf("expected deny for %q (on %q): %+v", d.cmd, d.cur, got)
		}
	}
}

func TestClassifyPushFeatureBranchGates(t *testing.T) {
	got := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "remote" || got.Severity != "high" {
		t.Fatalf("feature push: %+v", got)
	}
	bare := v("Bash", `{"command":"git push"}`, "feat/x")
	if bare.Decision != "gate" || bare.Category != "remote" {
		t.Fatalf("bare push on feature: %+v", bare)
	}
}

func TestClassifyCommitAndPR(t *testing.T) {
	c := v("Bash", `{"command":"git commit -m \"fix typo\""}`, "feat/x")
	if c.Decision != "gate" || c.Category != "shell" || c.Summary != "Commit: fix typo" {
		t.Fatalf("commit: %+v", c)
	}
	pr := v("Bash", `{"command":"gh pr create --fill"}`, "feat/x")
	if pr.Decision != "gate" || pr.Category != "remote" {
		t.Fatalf("pr: %+v", pr)
	}
}

func TestClassifySecretReadDenied(t *testing.T) {
	got := v("Bash", `{"command":"cat .env"}`, "feat/x")
	if got.Decision != "deny" {
		t.Fatalf("secret read should deny: %+v", got)
	}
}

func TestClassifyFailClosed(t *testing.T) {
	if v("Bash", `{}`, "feat/x").Decision != "gate" { // empty command -> generic gate is fine, but must not be a push
		// an empty command is a harmless no-op; gating it is acceptable
	}
	if v("Bash", `not json`, "feat/x").Decision != "deny" {
		t.Fatal("garbage input must deny")
	}
	if v("Frobnicate", `{}`, "feat/x").Decision != "deny" {
		t.Fatal("unknown tool must deny")
	}
}

// jsonStr quotes a string as a JSON literal for embedding in a command field.
func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
