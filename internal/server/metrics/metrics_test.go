package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func render(m *Metrics) string {
	var b strings.Builder
	m.Render(&b)
	return b.String()
}

func TestObserveHTTPCountersAndHistogram(t *testing.T) {
	m := New("v1", "go1.22")
	m.ObserveHTTP("/sync", "GET", 200, 7*time.Millisecond)   // <= 0.01
	m.ObserveHTTP("/sync", "GET", 200, 300*time.Millisecond) // <= 0.5
	m.ObserveHTTP("/sync", "GET", 500, 5*time.Millisecond)

	out := render(m)
	if !strings.Contains(out, `fleet_http_requests_total{route="/sync",method="GET",status="2xx"} 2`) {
		t.Fatalf("2xx counter wrong:\n%s", out)
	}
	if !strings.Contains(out, `fleet_http_requests_total{route="/sync",method="GET",status="5xx"} 1`) {
		t.Fatalf("5xx counter wrong:\n%s", out)
	}
	// Cumulative histogram: le="0.01" should include the 7ms and 5ms obs = 2.
	if !strings.Contains(out, `fleet_http_request_duration_seconds_bucket{route="/sync",method="GET",le="0.01"} 2`) {
		t.Fatalf("le=0.01 bucket wrong:\n%s", out)
	}
	// le="+Inf" and _count are the total number of observations = 3.
	if !strings.Contains(out, `fleet_http_request_duration_seconds_bucket{route="/sync",method="GET",le="+Inf"} 3`) {
		t.Fatalf("+Inf bucket wrong:\n%s", out)
	}
	if !strings.Contains(out, `fleet_http_request_duration_seconds_count{route="/sync",method="GET"} 3`) {
		t.Fatalf("_count wrong:\n%s", out)
	}
}

func TestCardinalityBounded(t *testing.T) {
	m := New("v", "go")
	m.ObserveHTTP("", "FROBNICATE", 999, time.Millisecond) // unmatched route, weird method+status
	m.ObserveHTTP("/x?a=1", "GET", 204, time.Millisecond)  // a "raw path" is NOT normalized away by us, but...
	out := render(m)
	if !strings.Contains(out, `route="other"`) || !strings.Contains(out, `method="other"`) || !strings.Contains(out, `status="other"`) {
		t.Fatalf("expected other-buckets for hostile input:\n%s", out)
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{100: "other", 204: "2xx", 302: "3xx", 404: "4xx", 503: "5xx", 700: "other", 0: "other"}
	for code, want := range cases {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestNormMethodRoute(t *testing.T) {
	if normMethod("GET") != "GET" || normMethod("get") != "other" || normMethod("TRACE") != "other" {
		t.Fatal("normMethod")
	}
	if normRoute("") != "other" || normRoute("/sync") != "/sync" {
		t.Fatal("normRoute")
	}
}

func TestAuthAndBuildInfoAndPool(t *testing.T) {
	m := New("v9", "go1.22")
	m.IncRefreshReuse()
	m.IncRefreshReuse()
	m.IncRefreshRotation()
	m.IncLogin()
	m.IncRefreshPruned(3)
	m.IncRefreshPruned(0)  // ignored
	m.IncRefreshPruned(-1) // ignored
	m.IncInFlight()
	m.SetPoolSource(func() PoolStats { return PoolStats{Total: 5, Idle: 3, Acquired: 2} })

	out := render(m)
	for _, want := range []string{
		`fleet_build_info{version="v9",go_version="go1.22"} 1`,
		"fleet_auth_refresh_reuse_total 2",
		"fleet_auth_refresh_rotations_total 1",
		"fleet_auth_logins_total 1",
		"fleet_auth_refresh_pruned_total 3",
		"fleet_http_in_flight 1",
		"fleet_db_pool_total_connections 5",
		"fleet_db_pool_idle_connections 3",
		"fleet_db_pool_acquired_connections 2",
		"# TYPE fleet_http_requests_total counter",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestNoPoolSourceOmitsGauges(t *testing.T) {
	out := render(New("v", "go"))
	if strings.Contains(out, "fleet_db_pool_total_connections") {
		t.Fatal("pool gauges must be omitted without a source")
	}
}

func TestNilMetricsNoop(t *testing.T) {
	var m *Metrics // nil
	// none of these must panic
	m.ObserveHTTP("/x", "GET", 200, time.Millisecond)
	m.IncInFlight()
	m.DecInFlight()
	m.IncRefreshReuse()
	m.IncRefreshRotation()
	m.IncLogin()
	m.IncRefreshPruned(5)
	m.SetPoolSource(func() PoolStats { return PoolStats{} })
	var b strings.Builder
	m.Render(&b)
	if b.Len() != 0 {
		t.Fatal("nil Render should write nothing")
	}
}

func TestHandlerTokenAuth(t *testing.T) {
	m := New("v", "go")
	h := m.Handler("s3cr3t")

	// no token -> 401
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rr.Code)
	}
	// wrong token -> 401
	rr = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer nope")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rr.Code)
	}
	// correct token -> 200 + body
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "fleet_build_info") {
		t.Fatalf("correct token: %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestConcurrentObserveHTTP(t *testing.T) {
	m := New("v", "go")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.ObserveHTTP("/sync", "GET", 200, time.Millisecond)
			}
		}()
	}
	// concurrent render must not race with writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			_ = render(m)
		}
	}()
	wg.Wait()
	if !strings.Contains(render(m), `fleet_http_requests_total{route="/sync",method="GET",status="2xx"} 5000`) {
		t.Fatalf("lost updates under concurrency:\n%s", render(m))
	}
}
