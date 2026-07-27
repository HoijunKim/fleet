package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hoijun/fleet/internal/winhide"
)

// RebaseAction is one commit's fate in an interactive rebase: kept in place
// ("pick"), folded into the commit above it ("fixup"), or removed ("drop").
type RebaseAction struct {
	Hash string `json:"hash"`
	Op   string `json:"op"` // "pick" | "fixup" | "drop"
}

// BuildRebaseTodo renders the git-rebase todo for the given actions, in order.
// A "drop" omits its line; "pick"/"fixup" emit `<op> <hash>`. An all-drop list
// yields "" - there is nothing to rebase onto.
func BuildRebaseTodo(actions []RebaseAction) string {
	var b strings.Builder
	for _, a := range actions {
		if a.Op == "drop" {
			continue
		}
		op := a.Op
		if op != "fixup" {
			op = "pick"
		}
		b.WriteString(op)
		b.WriteByte(' ')
		b.WriteString(a.Hash)
		b.WriteByte('\n')
	}
	return b.String()
}

// InteractiveRebase replays base..HEAD according to actions (reorder/drop/fixup).
// It drives `git rebase -i` non-interactively: the desired todo is written to a
// temp file exposed as FLEET_REBASE_TODO, and seqEditor (the GIT_SEQUENCE_EDITOR
// command - the fleet --rebase-seq sentinel in production) overwrites git's todo
// with it. Like a merge/rebase/cherry-pick, a real conflict is KEPT (returns
// ErrConflict, state left for the conflict panel); any other in-progress failure
// is rolled back so the tree is never stranded.
func InteractiveRebase(dir, base, seqEditor string, actions []RebaseAction) error {
	todo := BuildRebaseTodo(actions)
	if strings.TrimSpace(todo) == "" {
		return fmt.Errorf("nothing to rebase")
	}
	f, err := os.CreateTemp("", "fleet-rebase-todo-*.txt")
	if err != nil {
		return err
	}
	todoPath := f.Name()
	_, werr := f.WriteString(todo)
	f.Close()
	defer os.Remove(todoPath)
	if werr != nil {
		return werr
	}

	cmd := exec.Command("git", "-c", "core.editor=true", "rebase", "-i", base)
	cmd.Dir = dir
	winhide.Apply(cmd)
	cmd.Env = append(os.Environ(),
		"GIT_SEQUENCE_EDITOR="+seqEditor,
		"FLEET_REBASE_TODO="+todoPath,
		"GIT_EDITOR=true",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	unmerged, _ := (ExecRunner{}).Run(dir, "ls-files", "-u")
	if strings.TrimSpace(unmerged) != "" {
		return fmt.Errorf("rebase stopped on a conflict: %w", ErrConflict)
	}
	// Not a conflict: roll back so the repo is never left mid-rebase.
	if OperationInProgress(dir) == "rebase" {
		_, _ = (ExecRunner{}).Run(dir, "rebase", "--abort")
	}
	return fmt.Errorf("rebase failed: %s", strings.TrimSpace(string(out)))
}

// ApplyRebaseSeq is the --rebase-seq sentinel body: git invokes fleet as the
// sequence editor with the todo path it generated; fleet overwrites that file
// with the contents of FLEET_REBASE_TODO. Pure file I/O, so no reliance on a
// shell or cp. dst is git's todo path (os.Args[2]); src is FLEET_REBASE_TODO.
func ApplyRebaseSeq(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
