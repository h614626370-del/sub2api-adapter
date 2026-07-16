package adapter

import (
	"sync"
	"time"
)

type decisionCache struct {
	mu        sync.Mutex
	items     map[string]cacheItem
	lastPrune time.Time
}

const maxDecisionCacheEntries = 10000

type cacheItem struct {
	Decision  decision
	ExpiresAt time.Time
}

func newDecisionCache() *decisionCache {
	return &decisionCache{items: map[string]cacheItem{}}
}

func (c *decisionCache) Get(key string) (decision, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok {
		return decision{}, false
	}
	if !item.ExpiresAt.IsZero() && timeNow().After(item.ExpiresAt) {
		delete(c.items, key)
		return decision{}, false
	}
	return item.Decision, true
}

func (c *decisionCache) Set(key string, d decision, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := timeNow()
	if c.lastPrune.IsZero() || now.Sub(c.lastPrune) >= time.Minute {
		c.pruneExpiredLocked(now)
	}
	if _, exists := c.items[key]; !exists && len(c.items) >= maxDecisionCacheEntries {
		c.evictOneLocked()
	}
	c.items[key] = cacheItem{Decision: d, ExpiresAt: now.Add(ttl)}
}

func (c *decisionCache) PruneExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pruneExpiredLocked(timeNow())
}

func (c *decisionCache) pruneExpiredLocked(now time.Time) int {
	c.lastPrune = now
	deleted := 0
	for key, item := range c.items {
		if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
			delete(c.items, key)
			deleted++
		}
	}
	return deleted
}

func (c *decisionCache) evictOneLocked() {
	for key := range c.items {
		delete(c.items, key)
		return
	}
}

func (c *decisionCache) Clear(action string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for key, item := range c.items {
		if action == "" || item.Decision.Action == action {
			delete(c.items, key)
			count++
		}
	}
	return count
}

func (c *decisionCache) Stats() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{"allow": 0, "block": 0, "total": 0}
	now := timeNow()
	c.pruneExpiredLocked(now)
	for _, item := range c.items {
		out["total"]++
		out[item.Decision.Action]++
	}
	return out
}
