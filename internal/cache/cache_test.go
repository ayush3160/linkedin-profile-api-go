package cache

import (
	"testing"
	"time"
)

func TestTTLEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewTTL(time.Minute, 2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	if _, ok := c.Get("a"); ok {
		t.Error("a should have been evicted")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("c should be present")
	}
	if got := c.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
}

func TestTTLExpires(t *testing.T) {
	c := NewTTL(time.Minute, 8)
	now := time.Now()
	c.now = func() time.Time { return now }
	c.Set("a", 1)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should be live")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Error("a should have expired")
	}
}

func TestZeroTTLDisablesCaching(t *testing.T) {
	c := NewTTL(0, 8)
	c.Set("a", 1)
	if _, ok := c.Get("a"); ok {
		t.Error("ttl=0 should disable the cache")
	}
}

func TestRateLimiterWindow(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := range 2 {
		if allowed, _ := limiter.Check("ip"); !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	allowed, retryAfter := limiter.Check("ip")
	if allowed {
		t.Error("third request should be limited")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want positive", retryAfter)
	}

	// A different caller has its own window.
	if allowed, _ := limiter.Check("other-ip"); !allowed {
		t.Error("a different key should not be limited")
	}
	// The window rolls forward.
	now = now.Add(2 * time.Minute)
	if allowed, _ := limiter.Check("ip"); !allowed {
		t.Error("window should have rolled over")
	}
}

func TestZeroLimitDisablesRateLimiting(t *testing.T) {
	limiter := NewRateLimiter(0, time.Minute)
	for range 100 {
		if allowed, _ := limiter.Check("ip"); !allowed {
			t.Fatal("limit=0 should allow everything")
		}
	}
}
