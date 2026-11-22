package handler

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	loginAccountFailureLimit  = 5
	loginAccountFailureWindow = 15 * time.Minute
	loginIPFailureLimit       = 30
	loginIPFailureWindow      = 10 * time.Minute
	maxLoginThrottleEntries   = 4096
)

// loginGate throttles password exchange by account and by client IP. The
// process-wide rate limiter alone lets an attacker spread guesses across
// accounts; this gate makes repeated guesses for one account expensive.
type loginGate struct {
	account *failureWindow
	ip      *failureWindow
}

var loginThrottle = newLoginGate()

func newLoginGate() *loginGate {
	return &loginGate{
		account: newFailureWindow(loginAccountFailureLimit, loginAccountFailureWindow),
		ip:      newFailureWindow(loginIPFailureLimit, loginIPFailureWindow),
	}
}

func (g *loginGate) blocked(email, ip string, now time.Time) (time.Duration, bool) {
	if wait := g.account.retryAfter(accountKey(email), now); wait > 0 {
		return wait, true
	}
	if wait := g.ip.retryAfter(ipKey(ip), now); wait > 0 {
		return wait, true
	}
	return 0, false
}

func (g *loginGate) failed(email, ip string, now time.Time) {
	g.account.record(accountKey(email), now)
	g.ip.record(ipKey(ip), now)
}

func (g *loginGate) succeeded(email string) {
	g.account.clear(accountKey(email))
}

func accountKey(email string) string {
	return "email:" + strings.ToLower(strings.TrimSpace(email))
}

func ipKey(ip string) string {
	return "ip:" + ip
}

type failureWindow struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]*failureEntry
}

type failureEntry struct {
	count int
	first time.Time
}

func newFailureWindow(max int, window time.Duration) *failureWindow {
	return &failureWindow{max: max, window: window, entries: make(map[string]*failureEntry)}
}

func (w *failureWindow) retryAfter(key string, now time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.entries[key]
	if !ok {
		return 0
	}
	if now.Sub(entry.first) >= w.window {
		delete(w.entries, key)
		return 0
	}
	if entry.count < w.max {
		return 0
	}
	return entry.first.Add(w.window).Sub(now)
}

func (w *failureWindow) record(key string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.entries) >= maxLoginThrottleEntries {
		w.pruneLocked(now)
	}
	entry, ok := w.entries[key]
	if !ok || now.Sub(entry.first) >= w.window {
		w.entries[key] = &failureEntry{count: 1, first: now}
		return
	}
	entry.count++
}

func (w *failureWindow) clear(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.entries, key)
}

func (w *failureWindow) pruneLocked(now time.Time) {
	for key, entry := range w.entries {
		if now.Sub(entry.first) >= w.window {
			delete(w.entries, key)
		}
	}
	if len(w.entries) < maxLoginThrottleEntries {
		return
	}
	// Still full of live entries: evict the oldest quarter instead of clearing
	// everything, so flooding the map cannot reset an active lockout.
	type agedEntry struct {
		key   string
		first time.Time
	}
	aged := make([]agedEntry, 0, len(w.entries))
	for key, entry := range w.entries {
		aged = append(aged, agedEntry{key: key, first: entry.first})
	}
	sort.Slice(aged, func(i, j int) bool { return aged[i].first.Before(aged[j].first) })
	for _, entry := range aged[:len(aged)/4+1] {
		delete(w.entries, entry.key)
	}
}

// clientIP prefers X-Forwarded-For only when the request arrives from a
// loopback/private proxy on this host. A public peer cannot spoof the header
// to escape the per-IP bucket.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if forwarded := firstForwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
	}
	return host
}

func firstForwardedIP(header string) string {
	for _, part := range strings.Split(header, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			return ip.String()
		}
	}
	return ""
}
