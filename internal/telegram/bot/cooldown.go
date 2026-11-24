package bot

import (
	"sync"
	"time"
)

// cooldownRule is one limit a mention has to pass: key identifies the throttled thing
// (a group or a user in a chat) and window is how long it stays throttled after a mention.
type cooldownRule struct {
	key    string
	window time.Duration
}

type cooldownEntry struct {
	until   time.Time
	noticed bool // the throttle notice for this window was already sent
}

// cooldownTracker is an in-memory limiter; its state resets on restart.
type cooldownTracker struct {
	mu      sync.Mutex
	entries map[string]*cooldownEntry
}

// throttleResult describes why a mention was held back.
type throttleResult struct {
	blocked bool
	wait    time.Duration
	key     string // the rule with the longest remaining wait
}

// reserve checks all rules. If none is active it starts every window and reports not blocked.
// If any is active nothing changes; the result carries the rule with the longest wait.
// It never calls out while holding the lock, so callers can do slow checks (admin lookups) afterwards.
func (c *cooldownTracker) reserve(now time.Time, rules []cooldownRule) throttleResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[string]*cooldownEntry)
	}

	// Bound memory: drop expired entries once the map grows
	if len(c.entries) > 1024 {
		for key, entry := range c.entries {
			if !now.Before(entry.until) {
				delete(c.entries, key)
			}
		}
	}

	var worst throttleResult

	for _, rule := range rules {
		if rule.window <= 0 {
			continue
		}

		entry := c.entries[rule.key]
		if entry == nil || !now.Before(entry.until) {
			continue
		}

		if wait := entry.until.Sub(now); wait > worst.wait {
			worst = throttleResult{blocked: true, wait: wait, key: rule.key}
		}
	}

	if worst.blocked {
		return worst
	}

	for _, rule := range rules {
		if rule.window > 0 {
			c.entries[rule.key] = &cooldownEntry{until: now.Add(rule.window)}
		}
	}

	return throttleResult{}
}

// firstNotice reports whether the throttle notice for the rule's current window was not sent yet,
// and marks it as sent.
func (c *cooldownTracker) firstNotice(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[key]
	if entry == nil || entry.noticed {
		return false
	}

	entry.noticed = true

	return true
}
