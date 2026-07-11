package git

import "testing"

func TestListFiles(t *testing.T) {
	r := grepFake{out: "a.go\ndir/b.ts\n\n"} // trailing blank tolerated
	got, err := ListFiles(r, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.go" || got[1] != "dir/b.ts" {
		t.Fatalf("got %v", got)
	}
}

func TestListFilesNoFilesNonZero(t *testing.T) {
	r := grepFake{out: "", err: &stubErr{msg: "exit status 1"}} // non-zero, empty output -> no files, no error
	got, err := ListFiles(r, "/repo")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v err %v", got, err)
	}
}
