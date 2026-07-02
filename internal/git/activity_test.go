package git

import "testing"

type actFake struct{ out string }

func (f actFake) Run(dir string, args ...string) (string, error) { return f.out, nil }

func TestCommitActivityTallies(t *testing.T) {
	f := actFake{out: "2026-07-02\n2026-07-02\n2026-06-30\n2026-07-02\n2026-06-30\n"}
	got, err := CommitActivity(f, "/x", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 days, got %v", got)
	}
	// sorted ascending by date
	if got[0].Date != "2026-06-30" || got[0].Count != 2 {
		t.Errorf("day0=%+v", got[0])
	}
	if got[1].Date != "2026-07-02" || got[1].Count != 3 {
		t.Errorf("day1=%+v", got[1])
	}
}

func TestCommitActivityEmpty(t *testing.T) {
	got, err := CommitActivity(actFake{out: ""}, "/x", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty non-nil slice, got %v", got)
	}
}
