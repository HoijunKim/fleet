package git

import "testing"

type grepFake struct {
	out string
	err error
}

func (f grepFake) Run(dir string, args ...string) (string, error) { return f.out, f.err }

func TestGrepParses(t *testing.T) {
	f := grepFake{out: "src/a.go:12:func Foo() {\ninternal/b.go:3:// Foo helper\n"}
	hits, err := Grep(f, "/r", "Foo", GrepOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].File != "src/a.go" || hits[0].Line != 12 || hits[0].Text != "func Foo() {" {
		t.Errorf("hit0=%+v", hits[0])
	}
	if hits[1].File != "internal/b.go" || hits[1].Line != 3 {
		t.Errorf("hit1=%+v", hits[1])
	}
}

func TestGrepTextWithColons(t *testing.T) {
	f := grepFake{out: "cfg.yaml:5:url: http://x:8080/path\n"}
	hits, _ := Grep(f, "/r", "url", GrepOpts{})
	if len(hits) != 1 || hits[0].File != "cfg.yaml" || hits[0].Line != 5 {
		t.Fatalf("hit=%+v", hits)
	}
	if hits[0].Text != "url: http://x:8080/path" {
		t.Errorf("text=%q (must keep colons after the line number)", hits[0].Text)
	}
}

func TestGrepNoMatch(t *testing.T) {
	// git grep exits 1 with empty output when nothing matches.
	f := grepFake{out: "", err: &stubErr{msg: "exit status 1"}}
	hits, err := Grep(f, "/r", "zzz", GrepOpts{})
	if err != nil {
		t.Errorf("no-match must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits=%v", hits)
	}
}

// argFake records the args passed to Run so the -i wiring can be asserted.
type argFake struct{ got []string }

func (f *argFake) Run(dir string, args ...string) (string, error) {
	f.got = args
	return "", nil
}

func TestGrepIgnoreCasePassesFlag(t *testing.T) {
	f := &argFake{}
	_, _ = Grep(f, "/r", "todo", GrepOpts{IgnoreCase: true})
	found := false
	for _, a := range f.got {
		if a == "-i" {
			found = true
		}
	}
	if !found {
		t.Errorf("ignoreCase must add -i, got args %v", f.got)
	}

	f2 := &argFake{}
	_, _ = Grep(f2, "/r", "todo", GrepOpts{})
	for _, a := range f2.got {
		if a == "-i" {
			t.Errorf("case-sensitive grep must not pass -i, got %v", f2.got)
		}
	}
}

func TestGrepOptsFlags(t *testing.T) {
	has := func(args []string, flag string) bool {
		for _, a := range args {
			if a == flag {
				return true
			}
		}
		return false
	}
	cases := []struct {
		name string
		opts GrepOpts
		want []string
		deny []string
	}{
		{"default fixed", GrepOpts{}, []string{"-F"}, []string{"-E", "-i", "-w"}},
		{"regex", GrepOpts{Regex: true}, []string{"-E"}, []string{"-F"}},
		{"whole word", GrepOpts{WholeWord: true}, []string{"-w", "-F"}, []string{"-E"}},
		{"ignore case", GrepOpts{IgnoreCase: true}, []string{"-i"}, nil},
		{"regex + word + case", GrepOpts{Regex: true, WholeWord: true, IgnoreCase: true}, []string{"-E", "-w", "-i"}, []string{"-F"}},
	}
	for _, tc := range cases {
		f := &argFake{}
		_, _ = Grep(f, "/r", "q", tc.opts)
		for _, w := range tc.want {
			if !has(f.got, w) {
				t.Errorf("%s: missing %q in %v", tc.name, w, f.got)
			}
		}
		for _, d := range tc.deny {
			if has(f.got, d) {
				t.Errorf("%s: unexpected %q in %v", tc.name, d, f.got)
			}
		}
	}
}
