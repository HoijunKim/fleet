package tui

import "testing"

func TestDefaultKeysBound(t *testing.T) {
	k := defaultKeys()
	if len(k.Quit.Keys()) == 0 {
		t.Error("Quit unbound")
	}
	if len(k.Fetch.Keys()) == 0 || k.Fetch.Keys()[0] != "f" {
		t.Errorf("Fetch keys=%v", k.Fetch.Keys())
	}
	if len(k.ShortHelp()) == 0 {
		t.Error("ShortHelp empty")
	}
}
