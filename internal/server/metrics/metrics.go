// Package metrics is fleet's dependency-free metrics registry: HTTP
// counters/histograms, an in-flight gauge, auth security counters, and a
// sampled DB-pool source, rendered in Prometheus text exposition format. It
// imports only the standard library so nothing pulls a new dependency, and
// every mutator is nil-safe so callers need not guard a missing registry.
package metrics

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// buckets are the histogram upper bounds in seconds (Prometheus-style).
var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// PoolStats is a snapshot of the DB connection pool. main adapts pgxpool.Stat
// to this so the metrics package stays free of a pgx dependency.
type PoolStats struct{ Total, Idle, Acquired int }

type httpKey struct{ route, method, status string }
type histKey struct{ route, method string }

// hist is a cumulative-bucket latency histogram for one route+method.
type hist struct {
	counts []uint64 // len == len(buckets)+1; last element is the +Inf bucket
	sum    float64
	count  uint64
}

func newHist() *hist { return &hist{counts: make([]uint64, len(buckets)+1)} }

// Metrics holds fleet's server metrics. All mutators are no-ops on a nil
// receiver so handlers and middleware can call unconditionally.
type Metrics struct {
	version, goVersion string

	mu    sync.Mutex
	reqs  map[httpKey]uint64
	hists map[histKey]*hist

	inFlight  atomic.Int64
	reuse     atomic.Int64
	rotations atomic.Int64
	logins    atomic.Int64

	poolMu sync.Mutex
	pool   func() PoolStats
}

// New builds a registry stamped with build metadata for fleet_build_info.
func New(version, goVersion string) *Metrics {
	return &Metrics{
		version:   version,
		goVersion: goVersion,
		reqs:      map[httpKey]uint64{},
		hists:     map[histKey]*hist{},
	}
}

// ObserveHTTP records one finished request. Labels are normalized to a bounded
// set (see normRoute/normMethod/statusClass) so a hostile client cannot grow
// the series cardinality with random paths or methods.
func (m *Metrics) ObserveHTTP(route, method string, code int, dur time.Duration) {
	if m == nil {
		return
	}
	r, me, st := normRoute(route), normMethod(method), statusClass(code)
	sec := dur.Seconds()
	m.mu.Lock()
	m.reqs[httpKey{r, me, st}]++
	hk := histKey{r, me}
	h := m.hists[hk]
	if h == nil {
		h = newHist()
		m.hists[hk] = h
	}
	h.count++
	h.sum += sec
	for i, b := range buckets {
		if sec <= b {
			h.counts[i]++
		}
	}
	h.counts[len(buckets)]++ // +Inf always increments
	m.mu.Unlock()
}

func (m *Metrics) IncInFlight() {
	if m != nil {
		m.inFlight.Add(1)
	}
}
func (m *Metrics) DecInFlight() {
	if m != nil {
		m.inFlight.Add(-1)
	}
}
func (m *Metrics) IncRefreshReuse() {
	if m != nil {
		m.reuse.Add(1)
	}
}
func (m *Metrics) IncRefreshRotation() {
	if m != nil {
		m.rotations.Add(1)
	}
}
func (m *Metrics) IncLogin() {
	if m != nil {
		m.logins.Add(1)
	}
}

// SetPoolSource registers a callback sampled at render time for the DB-pool
// gauges. A nil source (or nil Metrics) simply omits those gauges.
func (m *Metrics) SetPoolSource(fn func() PoolStats) {
	if m == nil {
		return
	}
	m.poolMu.Lock()
	m.pool = fn
	m.poolMu.Unlock()
}

// Handler returns the token-guarded /metrics handler. token must be non-empty
// (the caller only registers the endpoint when a token is configured); a
// missing or mismatched Bearer token yields 401 via a constant-time compare.
func (m *Metrics) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const pfx = "Bearer "
		h := r.Header.Get("Authorization")
		got := strings.TrimPrefix(h, pfx)
		if !strings.HasPrefix(h, pfx) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.Render(w)
	})
}

// Render writes the full Prometheus text exposition. Series are sorted for
// deterministic output; the pool source is sampled fresh here.
func (m *Metrics) Render(w io.Writer) {
	if m == nil {
		return
	}
	var b strings.Builder

	b.WriteString("# HELP fleet_build_info Build metadata (constant 1).\n# TYPE fleet_build_info gauge\n")
	fmt.Fprintf(&b, "fleet_build_info{version=\"%s\",go_version=\"%s\"} 1\n",
		escapeLabel(m.version), escapeLabel(m.goVersion))

	m.mu.Lock()
	reqKeys := make([]httpKey, 0, len(m.reqs))
	for k := range m.reqs {
		reqKeys = append(reqKeys, k)
	}
	sort.Slice(reqKeys, func(i, j int) bool {
		a, c := reqKeys[i], reqKeys[j]
		if a.route != c.route {
			return a.route < c.route
		}
		if a.method != c.method {
			return a.method < c.method
		}
		return a.status < c.status
	})
	b.WriteString("# HELP fleet_http_requests_total Total HTTP requests by route, method, status class.\n# TYPE fleet_http_requests_total counter\n")
	for _, k := range reqKeys {
		fmt.Fprintf(&b, "fleet_http_requests_total{route=\"%s\",method=\"%s\",status=\"%s\"} %d\n",
			escapeLabel(k.route), escapeLabel(k.method), escapeLabel(k.status), m.reqs[k])
	}

	histKeys := make([]histKey, 0, len(m.hists))
	for k := range m.hists {
		histKeys = append(histKeys, k)
	}
	sort.Slice(histKeys, func(i, j int) bool {
		a, c := histKeys[i], histKeys[j]
		if a.route != c.route {
			return a.route < c.route
		}
		return a.method < c.method
	})
	b.WriteString("# HELP fleet_http_request_duration_seconds HTTP request latency in seconds.\n# TYPE fleet_http_request_duration_seconds histogram\n")
	for _, k := range histKeys {
		h := m.hists[k]
		for i, bound := range buckets {
			fmt.Fprintf(&b, "fleet_http_request_duration_seconds_bucket{route=\"%s\",method=\"%s\",le=\"%s\"} %d\n",
				escapeLabel(k.route), escapeLabel(k.method), formatFloat(bound), h.counts[i])
		}
		fmt.Fprintf(&b, "fleet_http_request_duration_seconds_bucket{route=\"%s\",method=\"%s\",le=\"+Inf\"} %d\n",
			escapeLabel(k.route), escapeLabel(k.method), h.counts[len(buckets)])
		fmt.Fprintf(&b, "fleet_http_request_duration_seconds_sum{route=\"%s\",method=\"%s\"} %s\n",
			escapeLabel(k.route), escapeLabel(k.method), formatFloat(h.sum))
		fmt.Fprintf(&b, "fleet_http_request_duration_seconds_count{route=\"%s\",method=\"%s\"} %d\n",
			escapeLabel(k.route), escapeLabel(k.method), h.count)
	}
	m.mu.Unlock()

	writeGauge(&b, "fleet_http_in_flight", "In-flight HTTP requests.", m.inFlight.Load())
	writeCounter(&b, "fleet_auth_refresh_reuse_total", "Refresh-token reuse events detected.", m.reuse.Load())
	writeCounter(&b, "fleet_auth_refresh_rotations_total", "Refresh-token rotations.", m.rotations.Load())
	writeCounter(&b, "fleet_auth_logins_total", "Successful logins.", m.logins.Load())

	m.poolMu.Lock()
	fn := m.pool
	m.poolMu.Unlock()
	if fn != nil {
		ps := fn()
		writeGauge(&b, "fleet_db_pool_total_connections", "Total DB pool connections.", int64(ps.Total))
		writeGauge(&b, "fleet_db_pool_idle_connections", "Idle DB pool connections.", int64(ps.Idle))
		writeGauge(&b, "fleet_db_pool_acquired_connections", "Acquired DB pool connections.", int64(ps.Acquired))
	}

	_, _ = io.WriteString(w, b.String())
}

func writeGauge(b *strings.Builder, name, help string, v int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, v)
}
func writeCounter(b *strings.Builder, name, help string, v int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
}

func normRoute(p string) string {
	if p == "" {
		return "other"
	}
	return p
}

var knownMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

func normMethod(m string) string {
	if knownMethods[m] {
		return m
	}
	return "other"
}

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
