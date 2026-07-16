package adapter

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type systemStats struct {
	CollectedAt       time.Time         `json:"collected_at"`
	Version           map[string]string `json:"version"`
	UptimeSeconds     float64           `json:"uptime_seconds"`
	ProcessRSSBytes   int64             `json:"process_rss_bytes"`
	HeapAllocBytes    uint64            `json:"heap_alloc_bytes"`
	HeapSysBytes      uint64            `json:"heap_sys_bytes"`
	RuntimeSysBytes   uint64            `json:"runtime_sys_bytes"`
	Goroutines        int               `json:"goroutines"`
	DatabaseBytes     int64             `json:"database_bytes"`
	DatabaseWALBytes  int64             `json:"database_wal_bytes"`
	DatabaseSHMBytes  int64             `json:"database_shm_bytes"`
	DataDirectory     string            `json:"data_directory"`
	DataBytes         int64             `json:"data_bytes"`
	DataFiles         int               `json:"data_files"`
	FilesystemTotal   uint64            `json:"filesystem_total_bytes"`
	FilesystemFree    uint64            `json:"filesystem_free_bytes"`
	EventRows         int               `json:"event_rows"`
	DecisionCacheRows int               `json:"decision_cache_rows"`
	RequestsTotal     float64           `json:"requests_total"`
	BlocksTotal       float64           `json:"blocks_total"`
	FailOpenTotal     float64           `json:"fail_open_total"`
	ProviderP95MS     float64           `json:"provider_p95_ms"`
}

func (a *App) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, a.collectSystemStats(r))
}

func (a *App) collectSystemStats(r *http.Request) systemStats {
	cfg := a.currentConfig()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	metrics := a.metrics.Snapshot()
	dataDir := filepath.Dir(cfg.DatabasePath)
	dataBytes, dataFiles := directoryUsage(dataDir)
	fsTotal, fsFree := filesystemUsage(dataDir)
	events, _ := a.store.EventStats(r.Context())
	cache, _ := a.store.DecisionCacheStats(r.Context())
	return systemStats{
		CollectedAt:       timeNow(),
		Version:           VersionInfo(),
		UptimeSeconds:     metrics["adapter_uptime_seconds"],
		ProcessRSSBytes:   processRSSBytes(),
		HeapAllocBytes:    memory.HeapAlloc,
		HeapSysBytes:      memory.HeapSys,
		RuntimeSysBytes:   memory.Sys,
		Goroutines:        runtime.NumGoroutine(),
		DatabaseBytes:     fileSize(cfg.DatabasePath),
		DatabaseWALBytes:  fileSize(cfg.DatabasePath + "-wal"),
		DatabaseSHMBytes:  fileSize(cfg.DatabasePath + "-shm"),
		DataDirectory:     dataDir,
		DataBytes:         dataBytes,
		DataFiles:         dataFiles,
		FilesystemTotal:   fsTotal,
		FilesystemFree:    fsFree,
		EventRows:         events.Total,
		DecisionCacheRows: cache["total"],
		RequestsTotal:     metrics["moderation_requests_total"],
		BlocksTotal:       metricSum(metrics, "moderation_block_total"),
		FailOpenTotal:     metrics["moderation_fail_open_total"],
		ProviderP95MS:     metricMax(metrics, "moderation_provider_latency_ms", "_p95"),
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

func directoryUsage(root string) (int64, int) {
	var total int64
	files := 0
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		files++
		return nil
	})
	return total, files
}

func metricSum(values map[string]float64, prefix string) float64 {
	total := 0.0
	for key, value := range values {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			total += value
		}
	}
	return total
}

func metricMax(values map[string]float64, prefix string, suffix string) float64 {
	max := 0.0
	for key, value := range values {
		if len(key) >= len(prefix)+len(suffix) && key[:len(prefix)] == prefix && key[len(key)-len(suffix):] == suffix && value > max {
			max = value
		}
	}
	return max
}
