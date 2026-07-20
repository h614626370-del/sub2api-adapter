package adapter

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type keywordStat struct {
	SetName      string    `json:"set_name"`
	RiskDomain   string    `json:"risk_domain"`
	Enabled      bool      `json:"enabled"`
	HitCount     int64     `json:"hit_count"`
	AuditedCount int64     `json:"audited_count"`
	BlockedCount int64     `json:"blocked_count"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type keywordStatsTracker struct {
	mu      sync.Mutex
	totals  map[string]keywordStat
	pending map[string]keywordStat
}

func newKeywordStatsTracker(initial []keywordStat) *keywordStatsTracker {
	t := &keywordStatsTracker{
		totals:  make(map[string]keywordStat, len(initial)),
		pending: make(map[string]keywordStat),
	}
	for _, item := range initial {
		name := strings.TrimSpace(item.SetName)
		if name == "" {
			continue
		}
		item.SetName = name
		t.totals[name] = item
	}
	return t
}

func (t *keywordStatsTracker) Record(hits []keywordHit, audited bool, blocked bool) {
	if t == nil || len(hits) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(hits))
	now := timeNow()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, hit := range hits {
		name := strings.TrimSpace(hit.SetName)
		if name == "" {
			name = strings.TrimSpace(hit.RiskDomain)
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		total := t.totals[name]
		total.SetName = name
		total.RiskDomain = hit.RiskDomain
		total.HitCount++
		if audited {
			total.AuditedCount++
		}
		if blocked {
			total.BlockedCount++
		}
		total.UpdatedAt = now
		t.totals[name] = total

		delta := t.pending[name]
		delta.SetName = name
		delta.RiskDomain = hit.RiskDomain
		delta.HitCount++
		if audited {
			delta.AuditedCount++
		}
		if blocked {
			delta.BlockedCount++
		}
		delta.UpdatedAt = now
		t.pending[name] = delta
	}
}

func (t *keywordStatsTracker) Snapshot(sets []KeywordSet) []keywordStat {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]keywordStat, 0, len(sets))
	seen := make(map[string]struct{}, len(sets))
	for _, set := range sets {
		name := strings.TrimSpace(set.Name)
		if name == "" {
			name = strings.TrimSpace(set.RiskDomain)
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		item := t.totals[name]
		item.SetName = name
		item.RiskDomain = set.RiskDomain
		item.Enabled = set.Enabled
		out = append(out, item)
	}
	return out
}

func (t *keywordStatsTracker) TakePending() []keywordStat {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return nil
	}
	out := make([]keywordStat, 0, len(t.pending))
	for _, item := range t.pending {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetName < out[j].SetName })
	t.pending = make(map[string]keywordStat)
	return out
}

func (t *keywordStatsTracker) RestorePending(items []keywordStat) {
	if t == nil || len(items) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range items {
		current := t.pending[item.SetName]
		current.SetName = item.SetName
		current.RiskDomain = item.RiskDomain
		current.HitCount += item.HitCount
		current.AuditedCount += item.AuditedCount
		current.BlockedCount += item.BlockedCount
		if item.UpdatedAt.After(current.UpdatedAt) {
			current.UpdatedAt = item.UpdatedAt
		}
		t.pending[item.SetName] = current
	}
}

func (t *keywordStatsTracker) Reset(persist func() error) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := persist(); err != nil {
		return err
	}
	t.totals = make(map[string]keywordStat)
	t.pending = make(map[string]keywordStat)
	return nil
}
