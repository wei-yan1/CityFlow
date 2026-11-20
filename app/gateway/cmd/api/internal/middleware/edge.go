// Package middleware contains the transport-level concerns owned by the
// public Gateway edge: request IDs, access logs, panic recovery and admission
// control. Domain rules and data ownership stay in the downstream services.
package middleware

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest"
	"golang.org/x/time/rate"
)

type requestIDKey struct{}

var requestSequence atomic.Uint64

// RequestID adds a stable per-request identifier to the response and the
// request context. It deliberately generates the ID instead of trusting an
// arbitrary inbound value; upstream proxies may still carry it for correlation.
func RequestID() rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set("X-Request-ID", id)
			next(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		}
	}
}

// AccessLog records method, path, status and duration after the handler runs.
// The default form writes synchronously; gateway production wiring uses
// AccessLogWithWriter to hand lines to a bounded batch writer.
func AccessLog() rest.Middleware {
	return AccessLogWithWriter(nil)
}

func AccessLogWithWriter(batch *AccessLogWriter) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next(writer, r)
			status := writer.status
			if writer.hijacked {
				status = http.StatusSwitchingProtocols
			}
			line := fmt.Sprintf("request_id=%s method=%s path=%s status=%d duration=%s",
				RequestIDFrom(r.Context()), r.Method, r.URL.Path, status,
				time.Since(started).Round(time.Millisecond))
			if batch != nil && batch.Write(line) {
				return
			}
			log.Print(line)
		}
	}
}

// Recover keeps a malformed downstream reply or handler bug from killing the
// whole Gateway process. It only writes a body when no response was started.
func Recover() rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("panic request_id=%s path=%s error=%v",
						RequestIDFrom(r.Context()), r.URL.Path, recovered)
					if writer, ok := w.(*statusWriter); ok && writer.written {
						return
					}
					httpkit.WriteError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next(w, r)
		}
	}
}

// RateLimit applies one process-wide admission limit. A single-instance
// development Gateway only needs a coarse guard; production should move this
// to a shared limiter (or the edge proxy) before running multiple instances.
func RateLimit(rps, burst int) rest.Middleware {
	if rps <= 0 || burst <= 0 {
		return func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next(w, r)
				return
			}
			if !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				httpkit.WriteError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next(w, r)
		}
	}
}

// Chain applies the same edge middleware to a standalone handler such as the
// embedded page/static-resource handler, which is outside the go-zero route table.
func Chain(handler http.Handler, middlewares ...rest.Middleware) http.Handler {
	if handler == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httpkit.WriteError(w, http.StatusBadGateway, "upstream service unavailable")
		})
	}
	next := handler.ServeHTTP
	for i := len(middlewares) - 1; i >= 0; i-- {
		next = middlewares[i](next)
	}
	return http.HandlerFunc(next)
}

// RequestIDFrom returns the ID placed by RequestID, or an empty string when the
// request bypassed the edge middleware (for example a direct unit test).
func RequestIDFrom(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDKey{}).(string); ok {
		return value
	}
	return ""
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%s-%016x", time.Now().UTC().Format("20060102T150405.000000000"), requestSequence.Add(1))
}

// statusWriter preserves optional ResponseWriter interfaces needed by the
// Tracking WebSocket proxy while allowing access logs to observe the final status.
type statusWriter struct {
	http.ResponseWriter
	status   int
	written  bool
	hijacked bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.hijacked || w.written {
		return
	}
	w.status = status
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.hijacked {
		return 0, fmt.Errorf("response already hijacked")
	}
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	w.hijacked = true
	conn, readWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	return newIdleConn(conn, hijackedIdleTimeout), readWriter, nil
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *statusWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *statusWriter) String() string {
	return strings.TrimSpace(fmt.Sprintf("status=%d written=%t", w.status, w.written))
}

// hijackedIdleTimeout is the edge-side guard for a WebSocket connection that
// stops sending client frames. Tracking also has its own read deadline; the
// edge guard prevents a silently dead TCP connection from living forever if a
// backend implementation forgets it. Browser and Go clients answer WebSocket
// pings automatically while their reader is active, so normal connections keep
// refreshing this timer.
const hijackedIdleTimeout = 60 * time.Second

type idleConn struct {
	net.Conn
	idle  time.Duration
	timer *time.Timer
}

func newIdleConn(conn net.Conn, idle time.Duration) net.Conn {
	wrapped := &idleConn{Conn: conn, idle: idle}
	wrapped.timer = time.AfterFunc(idle, func() {
		_ = wrapped.Conn.Close()
	})
	return wrapped
}

func (c *idleConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.timer.Reset(c.idle)
	}
	return n, err
}

func (c *idleConn) Close() error {
	c.timer.Stop()
	return c.Conn.Close()
}
