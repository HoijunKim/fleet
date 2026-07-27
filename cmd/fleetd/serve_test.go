package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeGracefulShutdown verifies serve accepts requests, then on ctx cancel
// drains and returns nil, leaving the listener closed.
func TestServeGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Handler: mux}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, ln, 10*time.Second) }()

	url := "http://" + ln.Addr().String() + "/"
	var resp *http.Response
	for i := 0; i < 100; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel() // trigger graceful shutdown
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error on clean shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after cancel")
	}

	if _, err := http.Get(url); err == nil {
		t.Fatal("expected request to fail after shutdown")
	}
}

// TestServeReturnsServeError verifies a serve failure (closed listener) is
// returned rather than swallowed.
func TestServeReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // Serve on a closed listener fails immediately
	srv := &http.Server{Handler: http.NewServeMux()}
	if err := serve(context.Background(), srv, ln, 10*time.Second); err == nil {
		t.Fatal("expected serve error on closed listener")
	}
}
