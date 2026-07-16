package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type metrics struct {
	mu       sync.Mutex
	counters map[string]float64
	latency  map[string][]float64
	started  time.Time
}

func newMetrics() *metrics {
	return &metrics{
		counters: map[string]float64{},
		latency:  map[string][]float64{},
		started:  timeNow(),
	}
}

func (m *metrics) Inc(name string, labels map[string]string) {
	m.Add(name, labels, 1)
}

func (m *metrics) Add(name string, labels map[string]string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[metricKey(name, labels)] += value
}

func (m *metrics) Observe(name string, labels map[string]string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	m.latency[key] = append(m.latency[key], value)
	if len(m.latency[key]) > 2048 {
		m.latency[key] = m.latency[key][len(m.latency[key])-2048:]
	}
}

func (m *metrics) Snapshot() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]float64{}
	for key, value := range m.counters {
		out[key] = value
	}
	out["adapter_uptime_seconds"] = time.Since(m.started).Seconds()
	for key, values := range m.latency {
		if len(values) == 0 {
			continue
		}
		cp := append([]float64(nil), values...)
		sort.Float64s(cp)
		out[key+"_p50"] = percentile(cp, 0.50)
		out[key+"_p95"] = percentile(cp, 0.95)
		out[key+"_p99"] = percentile(cp, 0.99)
	}
	return out
}

func (m *metrics) Prometheus() string {
	snap := m.Snapshot()
	keys := make([]string, 0, len(snap))
	for key := range snap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s %g\n", sanitizeMetricKey(key), snap[key])
	}
	return b.String()
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, name)
	for _, key := range keys {
		parts = append(parts, key+"_"+labels[key])
	}
	return strings.Join(parts, "_")
}

func sanitizeMetricKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
