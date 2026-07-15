package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoijun/fleet/internal/server/auth"
)

func TestDeleteAccountAuthedSucceeds(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()
	client, tok := authedClient(t, srv, key)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/account", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if store.deletedID != "user-1" {
		t.Errorf("DeleteAccount got userID %q, want user-1", store.deletedID)
	}
}

func TestDeleteAccountUnauthenticated(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/account", nil)
	resp, err := srv.Client().Do(req) // no Authorization header
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if store.deletedID != "" {
		t.Errorf("unauthenticated request must not delete, got %q", store.deletedID)
	}
}

func TestDeleteAccountStoreErrorIs500(t *testing.T) {
	key := []byte("k")
	store := &syncFakeStore{deleteErr: errors.New("boom")}
	srv := httptest.NewServer(NewRouter(Options{Store: store, Auth: auth.New(auth.Config{Store: store, SigningKey: key}), SigningKey: key}))
	defer srv.Close()
	client, tok := authedClient(t, srv, key)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/account", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
