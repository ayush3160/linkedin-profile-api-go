// Package cache provides an in-process TTL cache and a fixed-window rate
// limiter.
//
// Deliberately in-memory: the service is a single process and a cached profile
// is cheap to re-fetch. Swap in Redis behind multiple replicas; the interfaces
// here are small enough to make that a drop-in.
package cache

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key       string
	value     any
	expiresAt time.Time
}

// TTL is an LRU cache with per-entry expiry.
type TTL struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	items      map[string]*list.Element
	order      *list.List // front = most recently used
	now        func() time.Time
}

// NewTTL builds a cache. A ttl of zero disables caching entirely.
func NewTTL(ttl time.Duration, maxEntries int) *TTL {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &TTL{
		ttl: ttl, maxEntries: maxEntries,
		items: make(map[string]*list.Element, maxEntries),
		order: list.New(), now: time.Now,
	}
}

// Get returns the cached value for key, if present and unexpired.
func (c *TTL) Get(key string) (any, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return nil, false
	}
	item := element.Value.(*entry)
	if c.now().After(item.expiresAt) {
		c.order.Remove(element)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(element)
	return item.value, true
}

// Set stores a value under key.
func (c *TTL) Set(key string, value any) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		item := element.Value.(*entry)
		item.value, item.expiresAt = value, c.now().Add(c.ttl)
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(&entry{key: key, value: value, expiresAt: c.now().Add(c.ttl)})
	c.items[key] = element
	for c.order.Len() > c.maxEntries {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*entry).key)
	}
}

// Len is the number of live entries.
func (c *TTL) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// RateLimiter is a fixed-window limiter keyed by caller.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

// NewRateLimiter builds a limiter. A limit of zero or less disables it.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// Available reports whether a hit would be allowed, without recording one.
//
// Callers that only spend the allowance when work actually happens need to
// answer the caller early but charge them late.
func (r *RateLimiter) Available(key string) (bool, time.Duration) {
	if r.limit <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	kept := r.prune(key, now)
	if len(kept) >= r.limit {
		return false, r.window - now.Sub(kept[0])
	}
	return true, 0
}

// prune drops hits outside the window and returns what is left. Caller holds
// the lock.
func (r *RateLimiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-r.window)
	kept := r.hits[key][:0]
	for _, hit := range r.hits[key] {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	r.hits[key] = kept
	return kept
}

// Check records a hit and reports whether it is allowed, plus how long to wait.
func (r *RateLimiter) Check(key string) (bool, time.Duration) {
	if r.limit <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	kept := r.prune(key, now)
	if len(kept) >= r.limit {
		return false, r.window - now.Sub(kept[0])
	}
	r.hits[key] = append(kept, now)
	return true, 0
}

// Budget is a global rolling-window cap on an action, independent of caller.
//
// The per-IP limiter protects the service; a budget protects the LinkedIn
// account behind it. Someone who rotates source IPs defeats the former and
// still cannot spend more of the session than a budget allows.
type Budget struct {
	mu     sync.Mutex
	name   string
	limit  int
	window time.Duration
	hits   []time.Time
	now    func() time.Time
}

// NewBudget builds a budget. A limit of zero or less disables it.
func NewBudget(name string, limit int, window time.Duration) *Budget {
	return &Budget{name: name, limit: limit, window: window, now: time.Now}
}

// Name identifies the budget in error messages.
func (b *Budget) Name() string { return b.name }

// prune drops hits that have fallen out of the window. Caller holds the lock.
func (b *Budget) prune(now time.Time) {
	cutoff := now.Add(-b.window)
	kept := b.hits[:0]
	for _, hit := range b.hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	b.hits = kept
}

// Available reports whether a unit could be spent, without spending it, plus
// how long until the oldest unit falls out of the window.
func (b *Budget) Available() (bool, time.Duration) {
	if b.limit <= 0 {
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.prune(now)
	if len(b.hits) >= b.limit {
		return false, b.window - now.Sub(b.hits[0])
	}
	return true, 0
}

// Spend records one unit against the budget.
func (b *Budget) Spend() {
	if b.limit <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.prune(now)
	b.hits = append(b.hits, now)
}

// Remaining is how many units are left in the current window. A disabled
// budget reports -1.
func (b *Budget) Remaining() int {
	if b.limit <= 0 {
		return -1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(b.now())
	if remaining := b.limit - len(b.hits); remaining > 0 {
		return remaining
	}
	return 0
}
