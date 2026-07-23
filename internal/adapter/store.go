package adapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type store struct {
	db             *sql.DB
	decisionWrites atomic.Uint64
}

type configAudit struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	SourceIP  string          `json:"source_ip"`
	Summary   string          `json:"summary"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
	CreatedAt time.Time       `json:"created_at"`
}

type promptVersion struct {
	ID           int64     `json:"id"`
	Description  string    `json:"description"`
	SystemPrompt string    `json:"system_prompt"`
	Actor        string    `json:"actor"`
	SourceIP     string    `json:"source_ip"`
	CreatedAt    time.Time `json:"created_at"`
}

type eventStats struct {
	Total  int       `json:"total"`
	Oldest time.Time `json:"oldest,omitempty"`
	Newest time.Time `json:"newest,omitempty"`
}

type eventCleanupResult struct {
	ExpiredDeleted  int64 `json:"expired_deleted"`
	OverflowDeleted int64 `json:"overflow_deleted"`
	TotalDeleted    int64 `json:"total_deleted"`
}

func openStore(path string) (*store, error) {
	if path == "" {
		path = "data/adapter.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			action TEXT NOT NULL,
			keyword_hit INTEGER NOT NULL,
			keyword_hits TEXT NOT NULL,
			sampled INTEGER NOT NULL,
			external_audited INTEGER NOT NULL,
			provider TEXT NOT NULL,
			highest_category TEXT NOT NULL,
			highest_score REAL NOT NULL,
			category_scores TEXT NOT NULL,
			local_latency_ms INTEGER NOT NULL,
			provider_latency_ms INTEGER NOT NULL,
			error_summary TEXT NOT NULL,
			input_excerpt TEXT NOT NULL,
			blocked_input TEXT NOT NULL DEFAULT '',
			image_count INTEGER NOT NULL,
			estimated_cost_cny REAL NOT NULL,
			estimated_cost_usd REAL NOT NULL DEFAULT 0,
			provider_raw_summary TEXT NOT NULL,
			provider_failures TEXT NOT NULL DEFAULT '[]',
			provider_calls INTEGER NOT NULL DEFAULT 0,
			segment_count INTEGER NOT NULL DEFAULT 0,
			segment_cache_hits INTEGER NOT NULL DEFAULT 0,
			context_reviewed INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_events_action_created_at ON events(action, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_events_input_hash ON events(input_hash);`,
		`CREATE TABLE IF NOT EXISTS decision_cache (
			input_hash TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			decision_json TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_decision_cache_expires_at ON decision_cache(expires_at);`,
		`CREATE TABLE IF NOT EXISTS config_audits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor TEXT NOT NULL,
			source_ip TEXT NOT NULL,
			summary TEXT NOT NULL,
			before_json TEXT NOT NULL,
			after_json TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS prompt_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			description TEXT NOT NULL,
			system_prompt TEXT NOT NULL,
			actor TEXT NOT NULL,
			source_ip TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_versions_created_at ON prompt_versions(created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS keyword_stats (
			set_name TEXT PRIMARY KEY,
			risk_domain TEXT NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 0,
			audited_count INTEGER NOT NULL DEFAULT 0,
			blocked_count INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "events", "estimated_cost_usd", `REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "events", "blocked_input", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for column, definition := range map[string]string{
		"provider_failures":  `TEXT NOT NULL DEFAULT '[]'`,
		"provider_calls":     `INTEGER NOT NULL DEFAULT 0`,
		"segment_count":      `INTEGER NOT NULL DEFAULT 0`,
		"segment_cache_hits": `INTEGER NOT NULL DEFAULT 0`,
		"context_reviewed":   `INTEGER NOT NULL DEFAULT 0`,
	} {
		if err := s.ensureColumn(ctx, "events", column, definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) ensureColumn(ctx context.Context, table string, column string, definition string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition))
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

func (s *store) LoadConfig(ctx context.Context) (Config, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func (s *store) SaveConfig(ctx context.Context, cfg Config, actor string, sourceIP string, summary string, before Config) error {
	persisted := configForPersistence(cfg)
	afterRaw, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	beforeRaw, err := json.Marshal(safeConfigForUI(before))
	if err != nil {
		return err
	}
	auditAfterRaw, err := json.Marshal(safeConfigForUI(cfg))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := timeNow()
	beforeDescription, beforePrompt := promptSnapshot(before.Provider)
	afterDescription, afterPrompt := promptSnapshot(cfg.Provider)
	if beforeDescription != afterDescription || beforePrompt != afterPrompt {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prompt_versions(description, system_prompt, actor, source_ip, created_at)
			VALUES(?, ?, ?, ?, ?)`, beforeDescription, beforePrompt, actor, sourceIP, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES('config', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, string(afterRaw), now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO config_audits(actor, source_ip, summary, before_json, after_json, created_at)
		VALUES(?, ?, ?, ?, ?, ?)`, actor, sourceIP, summary, string(beforeRaw), string(auditAfterRaw), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) ListPromptVersions(ctx context.Context, limit int) ([]promptVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, description, system_prompt, actor, source_ip, created_at
		FROM prompt_versions ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []promptVersion
	for rows.Next() {
		var item promptVersion
		if err := rows.Scan(&item.ID, &item.Description, &item.SystemPrompt, &item.Actor, &item.SourceIP, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) GetPromptVersion(ctx context.Context, id int64) (promptVersion, error) {
	var item promptVersion
	err := s.db.QueryRowContext(ctx, `SELECT id, description, system_prompt, actor, source_ip, created_at
		FROM prompt_versions WHERE id = ?`, id).Scan(
		&item.ID, &item.Description, &item.SystemPrompt, &item.Actor, &item.SourceIP, &item.CreatedAt,
	)
	return item, err
}

func (s *store) InsertEvent(ctx context.Context, e event) error {
	keywordHits, _ := json.Marshal(e.KeywordHits)
	categoryScores, _ := json.Marshal(e.CategoryScores)
	providerFailures, _ := json.Marshal(e.ProviderFailures)
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(
		request_id, input_hash, action, keyword_hit, keyword_hits, sampled, external_audited,
		provider, highest_category, highest_score, category_scores, local_latency_ms,
		provider_latency_ms, error_summary, input_excerpt, blocked_input, image_count, estimated_cost_cny,
		estimated_cost_usd, provider_raw_summary, provider_failures, provider_calls, segment_count,
		segment_cache_hits, context_reviewed, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.RequestID, e.InputHash, e.Action, boolInt(e.KeywordHit), string(keywordHits), boolInt(e.Sampled),
		boolInt(e.ExternalAudited), e.Provider, e.HighestCategory, e.HighestScore, string(categoryScores),
		e.LocalLatencyMS, e.ProviderLatencyMS, e.ErrorSummary, e.InputExcerpt, e.BlockedInput, e.ImageCount,
		e.EstimatedCostCNY, e.EstimatedCostUSD, e.ProviderRawSummary, string(providerFailures), e.ProviderCalls,
		e.SegmentCount, e.SegmentCacheHits, boolInt(e.ContextReviewed), e.Time)
	return err
}

func (s *store) LoadKeywordStats(ctx context.Context) ([]keywordStat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT set_name, risk_domain, hit_count, audited_count, blocked_count, updated_at
		FROM keyword_stats ORDER BY set_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []keywordStat
	for rows.Next() {
		var item keywordStat
		if err := rows.Scan(&item.SetName, &item.RiskDomain, &item.HitCount, &item.AuditedCount, &item.BlockedCount, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) AddKeywordStats(ctx context.Context, items []keywordStat) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO keyword_stats(
			set_name, risk_domain, hit_count, audited_count, blocked_count, updated_at
		) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(set_name) DO UPDATE SET
			risk_domain = excluded.risk_domain,
			hit_count = keyword_stats.hit_count + excluded.hit_count,
			audited_count = keyword_stats.audited_count + excluded.audited_count,
			blocked_count = keyword_stats.blocked_count + excluded.blocked_count,
			updated_at = excluded.updated_at`,
			item.SetName, item.RiskDomain, item.HitCount, item.AuditedCount, item.BlockedCount, item.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *store) ClearKeywordStats(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM keyword_stats`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *store) ListEvents(ctx context.Context, limit int, offset int, action string, inputHash string) ([]event, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var args []any
	var where []string
	if action != "" {
		where = append(where, "action = ?")
		args = append(args, action)
	}
	if inputHash != "" {
		where = append(where, "input_hash = ?")
		args = append(args, inputHash)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + joinSQLAnd(where)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT request_id, input_hash, action, keyword_hit, keyword_hits, sampled, external_audited,
		provider, highest_category, highest_score, category_scores, local_latency_ms, provider_latency_ms,
		error_summary, input_excerpt, blocked_input, image_count, estimated_cost_cny, estimated_cost_usd, provider_raw_summary,
		provider_failures, provider_calls, segment_count, segment_cache_hits, context_reviewed, created_at
		FROM events` + whereSQL + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	queryArgs := append(append([]any(nil), args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []event
	for rows.Next() {
		var e event
		var hitsRaw, scoresRaw, failuresRaw string
		var keywordHit, sampled, externalAudited, contextReviewed int
		if err := rows.Scan(&e.RequestID, &e.InputHash, &e.Action, &keywordHit, &hitsRaw, &sampled,
			&externalAudited, &e.Provider, &e.HighestCategory, &e.HighestScore, &scoresRaw,
			&e.LocalLatencyMS, &e.ProviderLatencyMS, &e.ErrorSummary, &e.InputExcerpt, &e.BlockedInput, &e.ImageCount,
			&e.EstimatedCostCNY, &e.EstimatedCostUSD, &e.ProviderRawSummary, &failuresRaw, &e.ProviderCalls,
			&e.SegmentCount, &e.SegmentCacheHits, &contextReviewed, &e.Time); err != nil {
			return nil, 0, err
		}
		e.KeywordHit = keywordHit == 1
		e.Sampled = sampled == 1
		e.ExternalAudited = externalAudited == 1
		e.ContextReviewed = contextReviewed == 1
		_ = json.Unmarshal([]byte(hitsRaw), &e.KeywordHits)
		_ = json.Unmarshal([]byte(scoresRaw), &e.CategoryScores)
		_ = json.Unmarshal([]byte(failuresRaw), &e.ProviderFailures)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *store) ClearEvents(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *store) PruneEvents(ctx context.Context, retentionDays int, maxRows int) (eventCleanupResult, error) {
	var out eventCleanupResult
	if retentionDays > 0 {
		cutoff := timeNow().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, cutoff)
		if err != nil {
			return out, err
		}
		out.ExpiredDeleted, _ = res.RowsAffected()
	}
	if maxRows > 0 {
		res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id NOT IN (
			SELECT id FROM events ORDER BY created_at DESC, id DESC LIMIT ?
		)`, maxRows)
		if err != nil {
			return out, err
		}
		out.OverflowDeleted, _ = res.RowsAffected()
	}
	out.TotalDeleted = out.ExpiredDeleted + out.OverflowDeleted
	return out, nil
}

func (s *store) EventStats(ctx context.Context) (eventStats, error) {
	var stats eventStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&stats.Total); err != nil {
		return stats, err
	}
	if stats.Total == 0 {
		return stats, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM events ORDER BY created_at ASC, id ASC LIMIT 1`).Scan(&stats.Oldest); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM events ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&stats.Newest); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *store) SaveDecision(ctx context.Context, inputHash string, d decision, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO decision_cache(input_hash, action, decision_json, expires_at, created_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(input_hash) DO UPDATE SET action = excluded.action, decision_json = excluded.decision_json,
			expires_at = excluded.expires_at`, inputHash, d.Action, string(raw), timeNow().Add(ttl), timeNow())
	if err != nil {
		return err
	}
	if s.decisionWrites.Add(1)%100 == 0 {
		_, err = s.PruneDecisionCache(ctx, maxDecisionCacheEntries)
	}
	return err
}

func (s *store) GetDecision(ctx context.Context, inputHash string) (decision, bool, error) {
	var raw string
	var expires time.Time
	err := s.db.QueryRowContext(ctx, `SELECT decision_json, expires_at FROM decision_cache WHERE input_hash = ?`, inputHash).Scan(&raw, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return decision{}, false, nil
	}
	if err != nil {
		return decision{}, false, err
	}
	if timeNow().After(expires) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM decision_cache WHERE input_hash = ?`, inputHash)
		return decision{}, false, nil
	}
	var d decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return decision{}, false, err
	}
	return d, true, nil
}

func (s *store) ClearDecisionCache(ctx context.Context, action string) (int64, error) {
	var res sql.Result
	var err error
	if action == "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM decision_cache`)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM decision_cache WHERE action = ?`, action)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *store) DecisionCacheStats(ctx context.Context) (map[string]int, error) {
	_, _ = s.PruneDecisionCache(ctx, maxDecisionCacheEntries)
	rows, err := s.db.QueryContext(ctx, `SELECT action, COUNT(*) FROM decision_cache GROUP BY action`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{"allow": 0, "block": 0, "total": 0}
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		out[action] = count
		out["total"] += count
	}
	return out, rows.Err()
}

func (s *store) PruneDecisionCache(ctx context.Context, maxRows int) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM decision_cache WHERE expires_at < ?`, timeNow())
	if err != nil {
		return 0, err
	}
	deleted, _ := res.RowsAffected()
	if maxRows <= 0 {
		return deleted, nil
	}
	res, err = s.db.ExecContext(ctx, `DELETE FROM decision_cache WHERE input_hash NOT IN (
		SELECT input_hash FROM decision_cache ORDER BY expires_at DESC, created_at DESC LIMIT ?
	)`, maxRows)
	if err != nil {
		return deleted, err
	}
	overflow, _ := res.RowsAffected()
	return deleted + overflow, nil
}

func (s *store) ListAudits(ctx context.Context, limit int) ([]configAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor, source_ip, summary, before_json, after_json, created_at
		FROM config_audits ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []configAudit
	for rows.Next() {
		var item configAudit
		var before, after string
		if err := rows.Scan(&item.ID, &item.Actor, &item.SourceIP, &item.Summary, &before, &after, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Before = redactConfigRaw(before)
		item.After = redactConfigRaw(after)
		out = append(out, item)
	}
	return out, rows.Err()
}

func joinSQLAnd(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " AND "
		}
		out += part
	}
	return out
}

func promptSnapshot(provider ProviderConfig) (string, string) {
	description := ""
	for _, template := range provider.PromptTemplates {
		if template.ID == provider.ActivePromptID {
			description = strings.TrimSpace(template.Description)
			break
		}
	}
	if description == "" && len(provider.PromptTemplates) > 0 {
		description = strings.TrimSpace(provider.PromptTemplates[0].Description)
	}
	return description, activeSystemPrompt(provider)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func mergePersistedConfig(base Config, persisted Config) Config {
	persisted.ListenAddr = base.ListenAddr
	persisted.DatabasePath = base.DatabasePath
	if len(persisted.AuthTokens) == 0 || hasInsecureAuthToken(persisted.AuthTokens) || os.Getenv("ADAPTER_AUTH_TOKEN") != "" {
		persisted.AuthTokens = base.AuthTokens
	}
	persisted.AdminToken = ""
	if isInsecureHashSalt(persisted.HashSalt) || strings.HasPrefix(persisted.HashSalt, "auto-generated:") || os.Getenv("ADAPTER_HASH_SALT") != "" {
		persisted.HashSalt = base.HashSalt
	}
	if os.Getenv("FORCE_ALLOW") != "" {
		persisted.ForceAllow = base.ForceAllow
	}
	if os.Getenv("MISS_SAMPLE_RATE") != "" {
		persisted.MissSampleRate = base.MissSampleRate
	}
	if os.Getenv("RESULT_BLOCK_THRESHOLD") != "" {
		persisted.ResultBlockThreshold = base.ResultBlockThreshold
	}
	if os.Getenv("DIRECT_MODEL_AUDIT") != "" {
		persisted.DirectModelAudit = base.DirectModelAudit
	}
	if os.Getenv("PROVIDER_TYPE") != "" || os.Getenv("UPSTREAM_PROVIDER") != "" {
		persisted.Provider.Type = base.Provider.Type
	}
	if strings.TrimSpace(persisted.Provider.Type) == "" {
		persisted.Provider.Type = base.Provider.Type
	}
	if base.Provider.Type == "chat_json" && persisted.Provider.Type == "tencent_tms" {
		persisted.Provider = base.Provider
		persisted.MissSampleRate = base.MissSampleRate
		persisted.AuditOnKeywordHit = base.AuditOnKeywordHit
	}
	if os.Getenv("PROVIDER_DISABLED") != "" || os.Getenv("MODEL_DISABLED") != "" {
		persisted.Provider.Disabled = base.Provider.Disabled
	}
	if os.Getenv("PROVIDER_API_KEY") != "" || os.Getenv("UPSTREAM_API_KEY") != "" || persisted.Provider.APIKey == "" || isInsecureProviderAPIKey(persisted.Provider.APIKey) {
		persisted.Provider.APIKey = base.Provider.APIKey
	}
	if os.Getenv("IMAGE_PROVIDER_ENABLED") != "" {
		persisted.ImageProviderEnabled = base.ImageProviderEnabled
	}
	if os.Getenv("IMAGE_PROVIDER_API_KEY") != "" {
		persisted.ImageProvider.APIKey = base.ImageProvider.APIKey
	}
	if os.Getenv("IMAGE_PROVIDER_MODEL") != "" || strings.TrimSpace(persisted.ImageProvider.Model) == "" {
		persisted.ImageProvider.Model = base.ImageProvider.Model
	}
	if os.Getenv("IMAGE_PROVIDER_ENDPOINT") != "" || strings.TrimSpace(persisted.ImageProvider.Endpoint) == "" {
		persisted.ImageProvider.Endpoint = base.ImageProvider.Endpoint
	}
	if os.Getenv("TENCENT_SECRET_ID") != "" || persisted.Provider.SecretID == "" || isInsecureProviderSecret(persisted.Provider.SecretID) {
		persisted.Provider.SecretID = base.Provider.SecretID
	}
	if os.Getenv("TENCENT_SECRET_KEY") != "" || persisted.Provider.SecretKey == "" || isInsecureProviderSecret(persisted.Provider.SecretKey) {
		persisted.Provider.SecretKey = base.Provider.SecretKey
	}
	if os.Getenv("PROVIDER_ENDPOINT") != "" || os.Getenv("UPSTREAM_BASE_URL") != "" || persisted.Provider.Endpoint == "" {
		persisted.Provider.Endpoint = base.Provider.Endpoint
	}
	if os.Getenv("TENCENT_REGION") != "" || persisted.Provider.Region == "" {
		persisted.Provider.Region = base.Provider.Region
	}
	if os.Getenv("TENCENT_BIZ_TYPE") != "" || persisted.Provider.BizType == "" {
		persisted.Provider.BizType = base.Provider.BizType
	}
	if os.Getenv("PROVIDER_TIMEOUT_MS") != "" || os.Getenv("UPSTREAM_TIMEOUT_MS") != "" {
		persisted.Provider.TimeoutMS = base.Provider.TimeoutMS
	}
	if os.Getenv("UPSTREAM_MODEL") != "" || persisted.Provider.Model == "" {
		persisted.Provider.Model = base.Provider.Model
	}
	if strings.TrimSpace(persisted.Provider.SystemPrompt) == "" {
		persisted.Provider.SystemPrompt = base.Provider.SystemPrompt
	}
	if len(persisted.Provider.PromptTemplates) == 0 {
		persisted.Provider.PromptTemplates = defaultPromptTemplates(persisted.Provider.SystemPrompt)
	}
	if strings.TrimSpace(persisted.Provider.ActivePromptID) == "" {
		persisted.Provider.ActivePromptID = persisted.Provider.PromptTemplates[0].ID
	}
	return persisted
}

func safeConfigForUI(cfg Config) Config {
	if len(cfg.AuthTokens) > 0 {
		tokens := make([]string, 0, len(cfg.AuthTokens))
		for _, token := range cfg.AuthTokens {
			tokens = append(tokens, maskConfigured(token))
		}
		cfg.AuthTokens = tokens
	}
	cfg.AdminToken = ""
	cfg.HashSalt = maskConfigured(cfg.HashSalt)
	cfg.Provider.APIKey = maskConfigured(cfg.Provider.APIKey)
	cfg.Provider.SecretID = maskConfigured(cfg.Provider.SecretID)
	cfg.Provider.SecretKey = maskConfigured(cfg.Provider.SecretKey)
	cfg.ImageProvider.APIKey = maskConfigured(cfg.ImageProvider.APIKey)
	cfg.ImageProvider.SecretID = maskConfigured(cfg.ImageProvider.SecretID)
	cfg.ImageProvider.SecretKey = maskConfigured(cfg.ImageProvider.SecretKey)
	return cfg
}

func configForPersistence(cfg Config) Config {
	return cfg
}

func redactConfigRaw(raw string) json.RawMessage {
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return json.RawMessage(`{}`)
	}
	out, err := json.Marshal(safeConfigForUI(cfg))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func maskConfigured(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "configured"
	}
	return "configured:****" + value[len(value)-4:]
}

func isMaskedConfigured(value string) bool {
	return value == "configured" || strings.HasPrefix(value, "configured:****")
}

func productionWarnings(cfg Config) []string {
	var warnings []string
	for _, token := range cfg.AuthTokens {
		if isInsecureAuthToken(token) {
			warnings = append(warnings, "sub2api 调用密钥仍是空值或开发默认值，请在“密钥与认证”页面替换为强随机密钥")
		}
	}
	if isInsecureHashSalt(cfg.HashSalt) {
		warnings = append(warnings, "风险哈希盐未配置或仍是开发默认值，请在“密钥与认证”页面替换为稳定强随机值")
	}
	if strings.HasPrefix(cfg.HashSalt, "auto-generated:") {
		warnings = append(warnings, "风险哈希盐当前是进程临时生成值，重启后会变化；生产前请在“密钥与认证”页面保存稳定密钥")
	}
	if cfg.Provider.Type == "mock" {
		warnings = append(warnings, "当前上游模型是 mock，只适合内部联调；正式测试请切换到对话模型分类器 chat_json")
	}
	if cfg.Provider.Type == "http_json" {
		warnings = append(warnings, "当前上游模型是 http_json，只适合兼容旧网关；正式测试请切换到对话模型分类器 chat_json")
	}
	if cfg.Provider.Type == "tencent_tms" && (strings.TrimSpace(cfg.Provider.SecretID) == "" || strings.TrimSpace(cfg.Provider.SecretKey) == "") {
		warnings = append(warnings, "腾讯云 SecretId 或 SecretKey 尚未配置，请在“密钥”页面填写后再进行官方连通性测试")
	}
	if (cfg.Provider.Type == "chat_json" || cfg.Provider.Type == "qwen" || cfg.Provider.Type == "openai_compatible") && (strings.TrimSpace(cfg.Provider.APIKey) == "" || isInsecureProviderAPIKey(cfg.Provider.APIKey)) {
		warnings = append(warnings, "上游对话模型 API Key 尚未配置，请在“密钥与认证”页面填写后再进行连通性测试")
	}
	if (cfg.Provider.Type == "chat_json" || cfg.Provider.Type == "qwen" || cfg.Provider.Type == "openai_compatible") && strings.TrimSpace(cfg.Provider.Endpoint) == "" {
		warnings = append(warnings, "上游对话模型 Base URL 尚未配置，请在“对话模型接入”页面填写")
	}
	if cfg.ImageProviderEnabled && strings.TrimSpace(effectiveImageProviderConfig(cfg).APIKey) == "" {
		warnings = append(warnings, "图片审核模型已启用，但图片模型 API Key 尚未配置，也无法复用文本模型 Key")
	}
	if cfg.ImageProviderEnabled && strings.TrimSpace(cfg.ImageProvider.Model) == "" {
		warnings = append(warnings, "图片审核模型已启用，但模型名称为空；请在“图片模型”页面选择阿里视觉模型")
	}
	if cfg.ForceAllow {
		warnings = append(warnings, "紧急全量放行 FORCE_ALLOW 已开启，所有请求都会直接通过")
	}
	if cfg.ListenAddr != "" && !strings.HasPrefix(cfg.ListenAddr, "127.0.0.1:") && !strings.HasPrefix(cfg.ListenAddr, "localhost:") {
		warnings = append(warnings, "Adapter 当前不是只监听本机地址；如果对外暴露，请务必配置 HTTPS 和来源 IP 白名单")
	}
	return dedupeStrings(warnings)
}

func hasInsecureAuthToken(tokens []string) bool {
	for _, token := range tokens {
		if isInsecureAuthToken(token) {
			return true
		}
	}
	return false
}

func isInsecureAuthToken(token string) bool {
	switch strings.TrimSpace(token) {
	case "", "change-me-before-production", "dev-adapter-token-change-me", "replace-with-a-long-random-token", "replace-with-long-random-token":
		return true
	default:
		return false
	}
}

func isInsecureHashSalt(salt string) bool {
	switch strings.TrimSpace(salt) {
	case "", "dev-only-change-before-production", "replace-with-random-hash-salt", "replace-with-random-salt":
		return true
	default:
		return false
	}
}

func isInsecureProviderAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || isMaskedConfigured(key) {
		return key == ""
	}
	switch key {
	case "replace-with-upstream-api-key", "replace-with-provider-api-key", "change-me", "change-me-before-production":
		return true
	default:
		return false
	}
}

func isInsecureProviderSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" || isMaskedConfigured(secret) {
		return secret == ""
	}
	switch secret {
	case "replace-with-secret-id", "replace-with-secret-key", "change-me", "change-me-before-production":
		return true
	default:
		return false
	}
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func configSummary(before Config, after Config) string {
	return fmt.Sprintf("provider %s->%s, prompt_template %s->%s, result_score_category %s->%s, result_block_threshold %.4f->%.4f, force_allow %t->%t, direct_model_audit %t->%t, segment_audit %t/%d/%d->%t/%d/%d, image_provider %t/%s/highres=%t->%t/%s/highres=%t, miss_sample_rate %.4f->%.4f, keyword_sets %d->%d",
		before.Provider.Type, after.Provider.Type, before.Provider.ActivePromptID, after.Provider.ActivePromptID,
		before.ResultScoreCategory, after.ResultScoreCategory, before.ResultBlockThreshold, after.ResultBlockThreshold,
		before.ForceAllow, after.ForceAllow,
		before.DirectModelAudit, after.DirectModelAudit, before.SegmentAudit.Enabled, before.SegmentAudit.ThresholdChars,
		before.SegmentAudit.TargetChars, after.SegmentAudit.Enabled, after.SegmentAudit.ThresholdChars, after.SegmentAudit.TargetChars,
		before.ImageProviderEnabled, before.ImageProvider.Model,
		before.ImageProvider.HighResolution, after.ImageProviderEnabled, after.ImageProvider.Model,
		after.ImageProvider.HighResolution, before.MissSampleRate, after.MissSampleRate, len(before.KeywordSets), len(after.KeywordSets))
}
