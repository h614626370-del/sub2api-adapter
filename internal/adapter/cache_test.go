package adapter

import (
	"context"
	"testing"
	"time"
)

func TestDecisionCachePrunesExpiredEntries(t *testing.T) {
	originalTimeNow := timeNow
	base := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return base }
	t.Cleanup(func() { timeNow = originalTimeNow })

	cache := newDecisionCache()
	cache.Set("expired", allowDecision("test"), time.Minute)
	base = base.Add(2 * time.Minute)
	if deleted := cache.PruneExpired(); deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}
	if stats := cache.Stats(); stats["total"] != 0 {
		t.Fatalf("expired cache remains: %+v", stats)
	}
}

func TestDecisionCacheHasHardEntryLimit(t *testing.T) {
	cache := newDecisionCache()
	for i := 0; i < maxDecisionCacheEntries+25; i++ {
		cache.Set(string(rune(i+1)), allowDecision("test"), time.Hour)
	}
	if stats := cache.Stats(); stats["total"] != maxDecisionCacheEntries {
		t.Fatalf("cache total=%d want %d", stats["total"], maxDecisionCacheEntries)
	}
}

func TestStorePrunesExpiredDecisionCache(t *testing.T) {
	store, err := openStore(t.TempDir() + "/adapter.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(context.Background(), `INSERT INTO decision_cache(input_hash, action, decision_json, expires_at, created_at)
		VALUES(?, ?, ?, ?, ?)`, "expired", "allow", `{}`, timeNow().Add(-time.Hour), timeNow().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := store.PruneDecisionCache(context.Background(), maxDecisionCacheEntries)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}
}

func TestStoreDecisionCacheHasHardEntryLimit(t *testing.T) {
	store, err := openStore(t.TempDir() + "/adapter.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for i := 0; i < 5; i++ {
		_, err = store.db.ExecContext(context.Background(), `INSERT INTO decision_cache(input_hash, action, decision_json, expires_at, created_at)
			VALUES(?, ?, ?, ?, ?)`, string(rune('a'+i)), "allow", `{}`, timeNow().Add(time.Duration(i+1)*time.Hour), timeNow())
		if err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := store.PruneDecisionCache(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d want 2", deleted)
	}
	stats, err := store.DecisionCacheStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats["total"] != 3 {
		t.Fatalf("cache total=%d want 3", stats["total"])
	}
}

func TestNormalizeConfigAppliesResourceBounds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBodyBytes = 1 << 40
	cfg.MaxImages = 1000
	cfg.EventRetention = 1000000
	cfg.Provider.TimeoutMS = 99999
	cfg.Provider.MaxTokens = 99999
	cfg.ImageProvider.TimeoutMS = 99999
	cfg.ImageProvider.MaxTokens = 99999
	cfg.DecisionCache.AllowTTLSeconds = 999999
	cfg.DecisionCache.BlockTTLSeconds = 99999999
	got, err := normalizeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxBodyBytes != 32<<20 || got.MaxImages != 8 || got.EventRetention != 10000 {
		t.Fatalf("request/event bounds not applied: %+v", got)
	}
	if got.Provider.TimeoutMS != 30000 || got.Provider.MaxTokens != 4096 || got.ImageProvider.TimeoutMS != 30000 || got.ImageProvider.MaxTokens != 4096 {
		t.Fatalf("provider bounds not applied: text=%+v image=%+v", got.Provider, got.ImageProvider)
	}
	if got.DecisionCache.AllowTTLSeconds != 86400 || got.DecisionCache.BlockTTLSeconds != 7776000 {
		t.Fatalf("cache TTL bounds not applied: %+v", got.DecisionCache)
	}
}
