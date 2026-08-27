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

// Check records a hit and reports whether it is allowed, plus how long to wait.
func (r *RateLimiter) Check(key string) (bool, time.Duration) {
	if r.limit <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-r.window)

	kept := r.hits[key][:0]
	for _, hit := range r.hits[key] {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	r.hits[key] = kept

	if len(kept) >= r.limit {
		return false, r.window - now.Sub(kept[0])
	}
	r.hits[key] = append(kept, now)
	return true, 0
}
