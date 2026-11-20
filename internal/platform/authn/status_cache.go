package authn

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// StatusChecker asks the account authority whether a subject is still active.
// Implementations should return (false, nil) for a known inactive account and
// a non-nil error only when the authority cannot answer.
type StatusChecker func(context.Context, string, string) (bool, error)

// StatusCache is a short-lived, per-process account-status cache. It keeps
// JWT verification local at the edge while bounding how long a disabled account
// can use an already issued access token on this instance. The authoritative
// service is still checked on a cache miss and by downstream write paths.
type StatusCache struct {
	check StatusChecker
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]statusEntry
}

type statusEntry struct {
	active  bool
	expires time.Time
}

func NewStatusCache(ttl time.Duration, check StatusChecker) *StatusCache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &StatusCache{
		check:   check,
		ttl:     ttl,
		now:     func() time.Time { return time.Now().UTC() },
		entries: make(map[string]statusEntry),
	}
}

// IsActive returns a cached status when present; otherwise it asks the
// configured authority once and caches both active and inactive answers.
func (c *StatusCache) IsActive(ctx context.Context, subject, token string) (bool, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return false, errors.New("account subject is required")
	}
	if active, ok := c.lookup(subject); ok {
		return active, nil
	}
	if c.check == nil {
		return false, errors.New("account status checker is unavailable")
	}
	active, err := c.check(ctx, subject, token)
	if err != nil {
		return false, err
	}
	c.store(subject, active)
	return active, nil
}

// Remember lets login/refresh paths seed an already verified status without an
// extra authority call. It is intentionally optional; a cold cache is valid.
func (c *StatusCache) Remember(subject string, active bool) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	c.store(subject, active)
}

func (c *StatusCache) lookup(subject string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[subject]
	if !ok {
		return false, false
	}
	if !c.now().Before(entry.expires) {
		delete(c.entries, subject)
		return false, false
	}
	return entry.active, true
}

func (c *StatusCache) store(subject string, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= 4096 {
		c.purgeExpiredLocked()
	}
	c.entries[subject] = statusEntry{active: active, expires: c.now().Add(c.ttl)}
}

func (c *StatusCache) purgeExpiredLocked() {
	now := c.now()
	for subject, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, subject)
		}
	}
}
