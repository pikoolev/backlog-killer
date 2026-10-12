package httpops

import (
	"crypto/sha256"
	"encoding/hex"
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

const maxLimiterKeys = 10000

func NewLimiter() *Limiter { return &Limiter{values: map[string]bucket{}} }
func (l *Limiter) Allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	value, exists := l.values[key]
	if !exists && len(l.values) >= maxLimiterKeys {
		for k, v := range l.values {
			if now.Sub(v.start) > time.Minute {
				delete(l.values, k)
			}
		}
		if len(l.values) >= maxLimiterKeys {
			return false
		}
	}
	if now.Sub(value.start) >= time.Minute {
		value = bucket{start: now}
	}
	if value.count >= limit {
		return false
	}
	value.count++
	l.values[key] = value
	return true
}
func credentialKey(r *http.Request) string {
	raw := r.Header.Get("Authorization")
	if raw == "" {
		return ClientIP(r)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
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
			scope = "mcp:" + credentialKey(r)
		case strings.Contains(r.URL.Path, "/attachments"):
			limit = 10
			scope = "upload:" + credentialKey(r)
		case (r.Method == "POST" || r.Method == "PATCH") && strings.Contains(r.URL.Path, "/tickets"):
			limit = 60
			scope = "mutation:" + credentialKey(r)
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
