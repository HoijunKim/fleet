package git

import "testing"

const samplePorcelain = `# branch.oid abc123
# branch.head main
# branch.upstream origin/main
# branch.ab +2 -1
1 .M N... 100644 100644 100644 aaa bbb src/app.ts
1 M. N... 100644 100644 100644 ccc ddd README.md
? tmp.log
`

func TestParseStatus(t *testing.T) {
	r := parseStatus(samplePorcelain)
	if r.Branch != "main" {
		t.Errorf("branch=%q", r.Branch)
	}
	if !r.HasUpstream || r.Ahead != 2 || r.Behind != 1 {
		t.Errorf("upstream=%v ahead=%d behind=%d", r.HasUpstream, r.Ahead, r.Behind)
	}
	if !r.Dirty || r.Modified != 3 {
		t.Errorf("dirty=%v modified=%d", r.Dirty, r.Modified)
	}
	if len(r.Files) != 3 || r.Files[0] != "src/app.ts" || r.Files[2] != "tmp.log" {
		t.Errorf("files=%v", r.Files)
	}
}

func TestParseStatusCleanDetached(t *testing.T) {
	const s = `# branch.oid abc123
# branch.head (detached)
`
	r := parseStatus(s)
	if r.Dirty || r.Modified != 0 {
		t.Errorf("expected clean, got dirty=%v modified=%d", r.Dirty, r.Modified)
	}
	if r.HasUpstream {
		t.Errorf("detached head should have no upstream")
	}
	if r.Branch != "(detached)" {
		t.Errorf("branch=%q", r.Branch)
	}
}

func TestParseLastCommit(t *testing.T) {
	const out = "a1b2c3\x1fhoijun\x1f2026-07-01T14:00:00+09:00\x1ffix scaffold bug"
	c := parseLastCommit(out)
	if c.Hash != "a1b2c3" || c.Author != "hoijun" || c.Message != "fix scaffold bug" {
		t.Errorf("commit=%+v", c)
	}
	if c.When.Year() != 2026 || c.When.Month() != 7 {
		t.Errorf("time parsed wrong: %v", c.When)
	}
}

func TestParseTodoCount(t *testing.T) {
	const out = "src/a.go:2\nsrc/b.go:5\n"
	if got := parseTodoCount(out); got != 7 {
		t.Errorf("todo count=%d want 7", got)
	}
	if got := parseTodoCount(""); got != 0 {
		t.Errorf("empty todo count=%d want 0", got)
	}
}

func TestParseStatusRenameAndUnmerged(t *testing.T) {
	s := "# branch.head main\n" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new/path.ts\told/path.ts\n" +
		"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflicted.go\n"
	r := parseStatus(s)
	if r.Modified != 2 {
		t.Fatalf("modified=%d want 2", r.Modified)
	}
	if len(r.Files) != 2 {
		t.Fatalf("files=%v", r.Files)
	}
	if r.Files[0] != "new/path.ts" {
		t.Errorf("rename path=%q want new/path.ts", r.Files[0])
	}
	if r.Files[1] != "conflicted.go" {
		t.Errorf("unmerged path=%q want conflicted.go", r.Files[1])
	}
}
