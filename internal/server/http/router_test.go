package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/server/metrics"
)

func TestHealthzOK(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Options{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want %q", string(body), "ok")
	}
}

func TestReadyzOK(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Options{Store: &syncFakeStore{}}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ready" {
		t.Fatalf("body = %q, want %q", string(body), "ready")
	}
}

func TestReadyzDBDown(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Options{Store: &syncFakeStore{pingErr: errors.New("db down")}}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestMetricsEndpointRecordsRoutePattern(t *testing.T) {
	m := metrics.New("test", "go")
	srv := httptest.NewServer(NewRouter(Options{Store: &syncFakeStore{}, Metrics: m, MetricsToken: "tok"}))
	defer srv.Close()

	// A request through the router records the route PATTERN, not the raw path.
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// /metrics without a token -> 401.
	resp, _ = http.Get(srv.URL + "/metrics")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// With the token -> 200, and the earlier request is recorded as /healthz.
	req, _ := http.NewRequest("GET", srv.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with token: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `route="/healthz"`) {
		t.Fatalf("route pattern not recorded:\n%s", string(body))
	}
}

func TestMetricsDisabledWithoutToken(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Options{Store: &syncFakeStore{}, Metrics: metrics.New("t", "t")}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("metrics must be unregistered without a token: %d", resp.StatusCode)
	}
}
