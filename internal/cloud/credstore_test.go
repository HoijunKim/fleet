package cloud

import (
	"testing"

	keyring "github.com/zalando/go-keyring"
)

// Compile-time guarantee the keychain impl satisfies the interface.
var _ CredStore = KeyringStore{}
var _ CredStore = (*MemCredStore)(nil)

func TestMemCredStoreRoundTrip(t *testing.T) {
	var s MemCredStore
	if got, err := s.LoadRefresh(); err != nil || got != "" {
		t.Fatalf("empty load: %q %v", got, err)
	}
	if err := s.SaveRefresh("r1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRefresh(); got != "r1" {
		t.Fatalf("load after save = %q", got)
	}
	if err := s.DeleteRefresh(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRefresh(); got != "" {
		t.Fatalf("load after delete = %q", got)
	}
}

// TestKeyringStoreRoundTrip exercises KeyringStore against go-keyring's
// in-memory mock so it runs cross-platform without touching the real OS
// credential store. Service/User mirror the values the app wires up
// (Service: "fleet", User: "refresh").
func TestKeyringStoreRoundTrip(t *testing.T) {
	keyring.MockInit()
	s := KeyringStore{Service: "fleet", User: "refresh"}

	if got, err := s.LoadRefresh(); err != nil || got != "" {
		t.Fatalf("empty load: %q %v", got, err)
	}
	if err := s.SaveRefresh("r1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadRefresh(); err != nil || got != "r1" {
		t.Fatalf("load after save: %q %v", got, err)
	}
	if err := s.DeleteRefresh(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadRefresh(); err != nil || got != "" {
		t.Fatalf("load after delete: %q %v", got, err)
	}
	// Deleting an already-empty entry must not be an error (first-run /
	// sign-out-twice safety).
	if err := s.DeleteRefresh(); err != nil {
		t.Fatalf("delete when already empty: %v", err)
	}
}
