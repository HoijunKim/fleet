package main

import (
	"os"
	"strings"
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

func TestValidateSigningKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty", "", true},
		{"short but random", "k9#Lq2vXz7!Pw4Rt6Ym1Bn8Cd3Fg5Hj", true}, // 31 bytes
		{"long but one repeated byte", strings.Repeat("a", 64), true},
		{"long but too few distinct bytes", strings.Repeat("abcdefg", 10), true}, // 70 bytes, 7 distinct
		{"exactly 32 bytes, enough variety", "k9#Lq2vXz7!Pw4Rt6Ym1Bn8Cd3Fg5HjK", false},
		{"long random", "8f3c1e9a7b2d4f60c5a8e13b7d92f4a6c0b3e857d192f4a6", false},
	}
	for _, tc := range cases {
		err := validateSigningKey([]byte(tc.key))
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
