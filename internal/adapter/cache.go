package adapter

import (
	"sync"
	"time"
)

type decisionCache struct {
	mu    sync.Mutex
	items map[string]cacheItem
}

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
	c.items[key] = cacheItem{Decision: d, ExpiresAt: timeNow().Add(ttl)}
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
	for key, item := range c.items {
		if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
			delete(c.items, key)
			continue
		}
		out["total"]++
		out[item.Decision.Action]++
	}
	return out
}
