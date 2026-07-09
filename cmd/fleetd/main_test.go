package main

import (
	"os"
	"testing"
)

func TestEnvOr(t *testing.T) {
	if got := envOr("FLEETD_TEST_MISSING", "def"); got != "def" {
		t.Fatalf("missing -> %q, want def", got)
	}
	os.Setenv("FLEETD_TEST_SET", "val")
	defer os.Unsetenv("FLEETD_TEST_SET")
	if got := envOr("FLEETD_TEST_SET", "def"); got != "val" {
		t.Fatalf("set -> %q, want val", got)
	}
}

func TestEnvBool(t *testing.T) {
	if got := envBool("FLEETD_TEST_BOOL_MISSING"); got != false {
		t.Fatalf("missing -> %v, want false", got)
	}
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"0":     false,
		"false": false,
		"yes":   false,
		"":      false,
	}
	for val, want := range cases {
		os.Setenv("FLEETD_TEST_BOOL", val)
		if got := envBool("FLEETD_TEST_BOOL"); got != want {
			t.Fatalf("val %q -> %v, want %v", val, got, want)
		}
	}
	os.Unsetenv("FLEETD_TEST_BOOL")
}
