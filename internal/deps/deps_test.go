package deps

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProduces(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module github.com/me/a\n\ngo 1.22\n")
	write(t, d, "package.json", `{"name":"pkg-a","version":"1.0.0"}`)
	gm, js := Produces(d)
	if gm != "github.com/me/a" || js != "pkg-a" {
		t.Errorf("Produces=%q,%q", gm, js)
	}
}

func TestRequires(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module x\n\nrequire (\n\tgithub.com/me/b v1.2.3\n\tgithub.com/other/c v0.1.0\n)\n")
	write(t, d, "package.json", `{"dependencies":{"pkg-b":"^1.0.0"},"devDependencies":{"vite":"^5"}}`)
	got := Requires(d)
	want := map[string]bool{"github.com/me/b": true, "github.com/other/c": true, "pkg-b": true, "vite": true}
	if len(got) != 4 {
		t.Fatalf("Requires=%v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected require %q", g)
		}
	}
}

func TestBuildGraphEdges(t *testing.T) {
	da := t.TempDir()
	write(t, da, "go.mod", "module github.com/me/a\nrequire github.com/me/b v1.0.0\n")
	db := t.TempDir()
	write(t, db, "go.mod", "module github.com/me/b\n")
	g := BuildGraph([]RepoRef{{ID: "a", Path: da, Name: "a"}, {ID: "b", Path: db, Name: "b"}})
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%v", g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].From != "a" || g.Edges[0].To != "b" {
		t.Errorf("edges=%v (want a->b)", g.Edges)
	}
}
