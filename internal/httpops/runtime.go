package httpops

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type bucket struct {
	start time.Time
	count int
}
type Limiter struct {
	mu     sync.Mutex
	values map[string]bucket
}

func NewLimiter() *Limiter { return &Limiter{values: map[string]bucket{}} }
func (l *Limiter) Allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	value := l.values[key]
	if now.Sub(value.start) >= time.Minute {
		value = bucket{start: now}
	}
	if value.count >= limit {
		return false
	}
	value.count++
	l.values[key] = value
	if len(l.values) > 10000 {
		for k, v := range l.values {
			if now.Sub(v.start) > 2*time.Minute {
				delete(l.values, k)
			}
		}
	}
	return true
}
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		scope := ""
		switch {
		case strings.HasPrefix(r.URL.Path, "/embed/"):
			limit = 10
			scope = "embed:" + ClientIP(r)
		case r.URL.Path == "/mcp":
			limit = 120
			scope = "mcp:" + r.Header.Get("Authorization")
		case strings.Contains(r.URL.Path, "/attachments"):
			limit = 10
			scope = "upload:" + r.Header.Get("Authorization")
		case (r.Method == "POST" || r.Method == "PATCH") && strings.Contains(r.URL.Path, "/tickets"):
			limit = 60
			scope = "mutation:" + r.Header.Get("Authorization")
		}
		if limit > 0 && !l.Allow(scope, limit) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type Metrics struct {
	Requests    atomic.Uint64
	Failures    atomic.Uint64
	RateLimited atomic.Uint64
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.Requests.Add(1); next.ServeHTTP(w, r) })
}
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "backlog_killer_http_requests_total %d\nbacklog_killer_http_failures_total %d\nbacklog_killer_rate_limited_total %d\n", m.Requests.Load(), m.Failures.Load(), m.RateLimited.Load())
}
