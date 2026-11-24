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
	key     string
	notify  bool // true only for the first blocked attempt of a window
}

// reserve checks all rules. If none is active, it starts every window and reports not blocked.
// If any is active, nothing is started; the result carries the longest wait and whether this
// is the first blocked attempt for that rule (so the notice is sent once per window).
// A caller for whom exempt returns true is never blocked and leaves no trace (no window started).
func (c *cooldownTracker) reserve(now time.Time, rules []cooldownRule, exempt func() bool) throttleResult {
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
			worst = throttleResult{blocked: true, wait: wait, key: rule.key, notify: !entry.noticed}
		}
	}

	if worst.blocked {
		// Admins bypass the limit without starting or touching any window
		if exempt != nil && exempt() {
			return throttleResult{}
		}

		c.entries[worst.key].noticed = true

		return worst
	}

	for _, rule := range rules {
		if rule.window > 0 {
			c.entries[rule.key] = &cooldownEntry{until: now.Add(rule.window)}
		}
	}

	return throttleResult{}
}
