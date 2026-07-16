package adapter

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	cfgMu         sync.RWMutex
	cfg           Config
	keywords      *keywordEngine
	provider      provider
	imageProvider provider
	store         *store
	cache         *decisionCache
	metrics       *metrics
	events        *eventStore
	randMu        sync.Mutex
	rand          *rand.Rand
	cleanupCancel context.CancelFunc
	adminUsername string
	adminPassword string
	adminSecret   []byte
}

func NewApp(cfg Config) (*App, error) {
	st, err := openStore(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if persisted, ok, err := st.LoadConfig(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	} else if ok {
		cfg = mergePersistedConfig(cfg, persisted)
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	cfg = normalized
	engine, err := newKeywordEngine(cfg.KeywordSets)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	p, err := newProvider(cfg.Provider)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	imageProvider, err := newProvider(effectiveImageProviderConfig(cfg))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	adminSecret := make([]byte, 32)
	if _, err := cryptorand.Read(adminSecret); err != nil {
		cleanupCancel()
		_ = st.Close()
		return nil, fmt.Errorf("generate admin session secret: %w", err)
	}
	adminUsername, adminPassword := adminCredentials()
	app := &App{
		cfg:           cfg,
		keywords:      engine,
		provider:      p,
		imageProvider: imageProvider,
		store:         st,
		cache:         newDecisionCache(),
		metrics:       newMetrics(),
		events:        newEventStore(cfg.EventRetention),
		rand:          rand.New(rand.NewSource(timeNow().UnixNano())),
		cleanupCancel: cleanupCancel,
		adminUsername: adminUsername,
		adminPassword: adminPassword,
		adminSecret:   adminSecret,
	}
	_, _ = st.PruneDecisionCache(context.Background(), maxDecisionCacheEntries)
	go app.runEventCleanupLoop(cleanupCtx)
	return app, nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}
	return a.store.Close()
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /readyz", a.handleReadyz)
	mux.HandleFunc("GET /metrics", a.handleMetrics)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/moderations", a.handleModeration)
	mux.HandleFunc("GET /admin", a.handleAdmin)
	mux.HandleFunc("POST /admin/api/login", a.handleAdminLogin)
	mux.HandleFunc("POST /admin/api/logout", a.handleAdminLogout)
	mux.HandleFunc("GET /admin/api/status", a.handleAdminStatus)
	mux.HandleFunc("POST /admin/api/secrets/sub2api-token", a.handleCopySub2APIToken)
	mux.HandleFunc("GET /admin/api/config", a.handleGetConfig)
	mux.HandleFunc("PUT /admin/api/config", a.handleUpdateConfig)
	mux.HandleFunc("POST /admin/api/config/import", a.handleImportConfig)
	mux.HandleFunc("GET /admin/api/config/export", a.handleExportConfig)
	mux.HandleFunc("POST /admin/api/config/reset", a.handleResetConfig)
	mux.HandleFunc("POST /admin/api/test", a.handleAdminTest)
	mux.HandleFunc("GET /admin/api/events", a.handleAdminEvents)
	mux.HandleFunc("POST /admin/api/events/clear", a.handleEventsClear)
	mux.HandleFunc("POST /admin/api/events/prune", a.handleEventsPrune)
	mux.HandleFunc("GET /admin/api/audits", a.handleAdminAudits)
	mux.HandleFunc("GET /admin/api/prompt/versions", a.handlePromptVersions)
	mux.HandleFunc("POST /admin/api/prompt/restore", a.handlePromptRestore)
	mux.HandleFunc("POST /admin/api/cache/clear", a.handleCacheClear)
	mux.HandleFunc("GET /admin/api/system/stats", a.handleSystemStats)
	mux.HandleFunc("GET /admin/api/system/update", a.handleSystemUpdateStatus)
	mux.HandleFunc("POST /admin/api/system/update", a.handleSystemUpdate)
	mux.HandleFunc("GET /admin/api/provider/test", a.handleProviderTest)
	mux.HandleFunc("POST /admin/api/provider/test", a.handleProviderTest)
	mux.HandleFunc("GET /admin/api/image-provider/test", a.handleImageProviderTest)
	mux.HandleFunc("POST /admin/api/image-provider/test", a.handleImageProviderTest)
	mux.HandleFunc("GET /admin/", a.handleAdmin)
	return securityHeaders(mux)
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleReadyz(w http.ResponseWriter, r *http.Request) {
	cfg := a.currentConfig()
	ready, message := providerRuntimeReady(cfg)
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ready": ready, "provider": a.currentProvider().Name(), "force_allow": cfg.ForceAllow, "message": message})
}

func providerRuntimeReady(cfg Config) (bool, string) {
	if cfg.ForceAllow {
		return true, "已开启紧急全量放行，服务可用但不会调用上游模型"
	}
	if cfg.Provider.Disabled {
		return false, "上游模型已禁用"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider.Type)) {
	case "", "mock":
		return true, "mock 上游可用，仅适合内部联调"
	case "chat_json", "qwen", "openai_compatible":
		if strings.TrimSpace(cfg.Provider.Endpoint) == "" {
			return false, "上游对话模型 Base URL 未配置"
		}
		if strings.TrimSpace(cfg.Provider.APIKey) == "" || isInsecureProviderAPIKey(cfg.Provider.APIKey) {
			return false, "上游对话模型 API Key 未配置"
		}
		if strings.TrimSpace(cfg.Provider.Model) == "" {
			return false, "上游对话模型名称未配置"
		}
		return true, "上游对话模型配置已就绪"
	case "http_json":
		if strings.TrimSpace(cfg.Provider.Endpoint) == "" {
			return false, "HTTP JSON 上游地址未配置"
		}
		return true, "HTTP JSON 上游配置已就绪"
	case "tencent_tms":
		if strings.TrimSpace(cfg.Provider.SecretID) == "" || isInsecureProviderSecret(cfg.Provider.SecretID) {
			return false, "腾讯云 SecretId 未配置"
		}
		if strings.TrimSpace(cfg.Provider.SecretKey) == "" || isInsecureProviderSecret(cfg.Provider.SecretKey) {
			return false, "腾讯云 SecretKey 未配置"
		}
		return true, "腾讯云内容安全配置已就绪"
	default:
		return false, "不支持的上游类型"
	}
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(a.metrics.Prometheus()))
}

func (a *App) handleModeration(w http.ResponseWriter, r *http.Request) {
	start := timeNow()
	requestID := newRequestID()
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不允许", http.StatusMethodNotAllowed)
		return
	}
	if !a.authorized(r) {
		a.metrics.Inc("moderation_auth_fail_total", nil)
		http.Error(w, "未授权：请检查 sub2api 调用密钥", http.StatusUnauthorized)
		return
	}
	cfg := a.currentConfig()
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
	var req moderationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.metrics.Inc("moderation_bad_request_total", nil)
		http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}

	result, evt := a.evaluate(r.Context(), requestID, req)
	if evt.Action == "block" {
		blocked := extractModerationInput(req.Input, cfg.MaxTextChars, cfg.MaxImages, cfg.AllowDataURLImage)
		evt.BlockedInput = strings.TrimSpace(blocked.Text)
	}
	evt.LocalLatencyMS = time.Since(start).Milliseconds()
	a.events.Add(evt)
	if err := a.store.InsertEvent(r.Context(), evt); err != nil {
		slog.Warn("event_persist_failed", "request_id", evt.RequestID, "error", err)
	}
	a.metrics.Observe("moderation_local_latency_ms", nil, float64(evt.LocalLatencyMS))
	writeJSON(w, http.StatusOK, toModerationResponse(requestID, req.Model, result))
}

func (a *App) runEventCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cleanupEvents(ctx)
		}
	}
}

func (a *App) cleanupEvents(ctx context.Context) {
	a.cache.PruneExpired()
	if _, err := a.store.PruneDecisionCache(ctx, maxDecisionCacheEntries); err != nil {
		slog.Warn("decision_cache_cleanup_failed", "error", err)
	}
	cfg := a.currentConfig()
	result, err := a.store.PruneEvents(ctx, cfg.EventRetentionDays, cfg.EventRetention)
	if err != nil {
		slog.Warn("event_cleanup_failed", "error", err)
		return
	}
	if result.TotalDeleted > 0 {
		slog.Info("event_cleanup_done", "expired_deleted", result.ExpiredDeleted, "overflow_deleted", result.OverflowDeleted)
	}
}

func (a *App) evaluate(ctx context.Context, requestID string, req moderationRequest) (decision, event) {
	d, evt, _ := a.evaluateWithTrace(ctx, requestID, req)
	return d, evt
}

func (a *App) evaluateWithTrace(ctx context.Context, requestID string, req moderationRequest) (decision, event, evaluationTrace) {
	cfg := a.currentConfig()
	p := a.currentProvider()
	a.metrics.Inc("moderation_requests_total", nil)
	input := extractModerationInput(req.Input, cfg.MaxTextChars, cfg.MaxImages, cfg.AllowDataURLImage)
	if len(input.Images) > 0 {
		a.metrics.Inc("moderation_image_requests_total", nil)
	}
	inputHash := riskHash(cfg.HashSalt, input)
	cacheHash := decisionCacheHash(cfg, input)
	trace := evaluationTrace{}
	evt := event{
		Time:         timeNow(),
		RequestID:    requestID,
		InputHash:    inputHash,
		InputExcerpt: inputExcerpt(input.Text, cfg.LogRawInput),
		ImageCount:   len(input.Images),
	}

	if cfg.ForceAllow {
		d := allowDecision("force_allow")
		evt.Action = "force_allow"
		evt.CategoryScores = d.CategoryScores
		a.metrics.Inc("moderation_force_allow_total", nil)
		return d, evt, trace
	}

	if cfg.DecisionCache.Enabled {
		if cached, ok, err := a.store.GetDecision(ctx, cacheHash); err == nil && ok {
			evt.Action = cached.Action
			evt.CacheHit = true
			evt.ExternalAudited = false
			evt.Provider = cached.Provider
			evt.HighestCategory = cached.HighestCategory
			evt.HighestScore = cached.HighestScore
			evt.CategoryScores = cached.CategoryScores
			trace.CacheNote = "命中决策缓存，本次没有请求上游模型；如刚调整过策略或提示词，可以清理缓存后重测。"
			a.metrics.Inc("moderation_cache_hit_total", map[string]string{"decision": cached.Action})
			return cached, evt, trace
		} else if err != nil {
			slog.Warn("decision_cache_lookup_failed", "error", err)
		}
		if cached, ok := a.cache.Get(cacheHash); ok {
			evt.Action = cached.Action
			evt.CacheHit = true
			evt.ExternalAudited = false
			evt.Provider = cached.Provider
			evt.HighestCategory = cached.HighestCategory
			evt.HighestScore = cached.HighestScore
			evt.CategoryScores = cached.CategoryScores
			trace.CacheNote = "命中内存决策缓存，本次没有请求上游模型；如刚调整过策略或提示词，可以清理缓存后重测。"
			a.metrics.Inc("moderation_cache_hit_total", map[string]string{"decision": cached.Action})
			return cached, evt, trace
		}
	}

	prefilterEnabled := !cfg.DirectModelAudit
	var hits []keywordHit
	if prefilterEnabled {
		hits = a.currentKeywords().Match(input.Text)
		evt.KeywordHits = hits
		evt.KeywordHit = len(hits) > 0
		for _, hit := range hits {
			a.metrics.Inc("moderation_keyword_hit_total", map[string]string{"category": hit.RiskDomain})
		}
	}

	sampled := prefilterEnabled && len(hits) == 0 && a.roll(cfg.MissSampleRate)
	if sampled {
		evt.Sampled = true
		a.metrics.Inc("moderation_miss_sample_total", nil)
	}

	textLen := len([]rune(strings.TrimSpace(input.Text)))
	shouldAuditText := textLen >= cfg.MinTextChars && (cfg.DirectModelAudit || ((len(hits) > 0 && cfg.AuditOnKeywordHit) || sampled))
	shouldAuditImage := a.shouldAuditImage(len(input.Images) > 0, len(hits) > 0, sampled, cfg.DirectModelAudit && textLen >= cfg.MinTextChars)
	if shouldAuditImage {
		a.metrics.Inc("moderation_image_audit_total", nil)
	}

	if !shouldAuditText && !shouldAuditImage {
		d := allowDecision("local")
		evt.Action = "allow"
		evt.CategoryScores = d.CategoryScores
		a.metrics.Inc("moderation_local_allow_total", nil)
		return d, evt, trace
	}

	usingImageProvider := shouldAuditImage && cfg.ImageProviderEnabled && !cfg.ImageProvider.Disabled
	if usingImageProvider {
		p = a.currentImageProvider()
	}
	providerDisabled := cfg.Provider.Disabled
	if usingImageProvider {
		providerDisabled = cfg.ImageProvider.Disabled
	}
	if providerDisabled {
		d := allowDecision("provider_disabled")
		evt.Action = "provider_disabled"
		evt.CategoryScores = d.CategoryScores
		a.metrics.Inc("moderation_provider_disabled_total", nil)
		return d, evt, trace
	}

	providerImages := []string(nil)
	if shouldAuditImage {
		providerImages = redactImagesForProvider(input.Images)
	}
	providerReq := providerRequest{
		RequestID:       requestID,
		Text:            input.Text,
		Images:          providerImages,
		KeywordHits:     hits,
		AuditText:       shouldAuditText,
		AuditImage:      shouldAuditImage,
		NormalizedInput: input.Text,
	}
	trace.ProviderRequest = &providerReq
	trace.UpstreamRequest = debugUpstreamRequest(p, providerReq)
	evt.ExternalAudited = true
	evt.Provider = p.Name()
	a.metrics.Inc("moderation_provider_calls_total", map[string]string{"provider": p.Name()})

	providerStart := timeNow()
	providerResult, err := p.Audit(ctx, providerReq)
	evt.ProviderLatencyMS = time.Since(providerStart).Milliseconds()
	a.metrics.Observe("moderation_provider_latency_ms", map[string]string{"provider": p.Name()}, float64(evt.ProviderLatencyMS))
	if err != nil {
		d := allowDecision("fail_open")
		evt.Action = "fail_open"
		evt.ErrorSummary = safeSummary(err.Error(), 240)
		evt.CategoryScores = d.CategoryScores
		trace.UpstreamResponse = map[string]any{"ok": false, "error": evt.ErrorSummary, "latency_ms": evt.ProviderLatencyMS}
		a.metrics.Inc("moderation_provider_errors_total", map[string]string{"provider": p.Name()})
		a.metrics.Inc("moderation_fail_open_total", nil)
		slog.Warn("moderation_provider_fail_open", "request_id", requestID, "provider", p.Name(), "error", err)
		return d, evt, trace
	}
	trace.UpstreamResponse = debugProviderResult(providerResult)

	d := decisionFromProvider(providerResult, cfg, p.Name())
	if providerResult.PromptTokens > 0 {
		a.metrics.Add("moderation_prompt_tokens_total", map[string]string{"provider": p.Name()}, float64(providerResult.PromptTokens))
	}
	if providerResult.CompletionTokens > 0 {
		a.metrics.Add("moderation_completion_tokens_total", map[string]string{"provider": p.Name()}, float64(providerResult.CompletionTokens))
	}
	if providerResult.CachedTokens > 0 {
		a.metrics.Add("moderation_cached_tokens_total", map[string]string{"provider": p.Name()}, float64(providerResult.CachedTokens))
	}
	if d.Action == "block" {
		a.metrics.Inc("moderation_flagged_total", map[string]string{"category": d.HighestCategory})
		a.metrics.Inc("moderation_block_total", map[string]string{"category": d.HighestCategory})
	}
	if d.Action == "allow" {
		a.metrics.Inc("moderation_provider_allow_total", map[string]string{"provider": p.Name()})
	}
	evt.Action = d.Action
	evt.ProviderRawSummary = d.RawSummary
	evt.HighestCategory = d.HighestCategory
	evt.HighestScore = d.HighestScore
	evt.CategoryScores = d.CategoryScores
	evt.EstimatedCostUSD = a.estimatedTokenCostUSD(providerResult)
	a.metrics.Add("moderation_estimated_cost_usd_total", nil, evt.EstimatedCostUSD)

	if cfg.DecisionCache.Enabled {
		a.cache.Set(cacheHash, d, a.ttlFor(d.Action))
		if err := a.store.SaveDecision(ctx, cacheHash, d, a.ttlFor(d.Action)); err != nil {
			slog.Warn("decision_cache_save_failed", "error", err)
		}
	}
	return d, evt, trace
}

func (a *App) shouldAuditImage(hasImage bool, keywordHit bool, sampled bool, directTextAudit bool) bool {
	cfg := a.currentConfig()
	if !hasImage {
		return false
	}
	if !cfg.ImageProviderEnabled || cfg.ImageProvider.Disabled {
		return false
	}
	switch cfg.ImageAuditMode {
	case "off":
		return false
	case "all":
		return true
	case "sampled":
		return a.roll(cfg.ImageSampleRate)
	default:
		return directTextAudit || keywordHit || sampled || a.roll(cfg.ImageSampleRate)
	}
}

func (a *App) ttlFor(action string) time.Duration {
	cfg := a.currentConfig()
	switch action {
	case "block":
		return cfg.DecisionCache.blockTTL
	default:
		return cfg.DecisionCache.allowTTL
	}
}

func (a *App) estimatedTokenCostUSD(result providerResult) float64 {
	cfg := a.currentConfig()
	promptTokens := result.PromptTokens
	cachedTokens := result.CachedTokens
	if cachedTokens > promptTokens {
		cachedTokens = promptTokens
	}
	uncachedPromptTokens := promptTokens - cachedTokens
	var costUSD float64
	if uncachedPromptTokens > 0 && cfg.EstimatedPromptUSD > 0 {
		costUSD += float64(uncachedPromptTokens) / 1000000 * cfg.EstimatedPromptUSD
	}
	if cachedTokens > 0 && cfg.EstimatedCachedUSD > 0 {
		costUSD += float64(cachedTokens) / 1000000 * cfg.EstimatedCachedUSD
	}
	if result.CompletionTokens > 0 && cfg.EstimatedOutputUSD > 0 {
		costUSD += float64(result.CompletionTokens) / 1000000 * cfg.EstimatedOutputUSD
	}
	return costUSD
}

func (a *App) roll(rate float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	a.randMu.Lock()
	defer a.randMu.Unlock()
	return a.rand.Float64() < rate
}

func (a *App) authorized(r *http.Request) bool {
	cfg := a.currentConfig()
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return false
	}
	got := strings.TrimSpace(auth[len("Bearer "):])
	for _, token := range cfg.AuthTokens {
		token = strings.TrimSpace(token)
		if token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (a *App) adminAuthorized(w http.ResponseWriter, r *http.Request) bool {
	_ = w
	return a.validAdminSessionCookie(r)
}

const adminSessionCookieName = "sub2api_admin_session"
const defaultAdminUsername = "admin"
const defaultAdminPassword = "admin123456"

func adminCredentials() (string, string) {
	username := strings.TrimSpace(os.Getenv("ADAPTER_ADMIN_USERNAME"))
	if username == "" {
		username = defaultAdminUsername
	}
	password := os.Getenv("ADAPTER_ADMIN_PASSWORD")
	if password == "" {
		password = defaultAdminPassword
	}
	return username, password
}

func (a *App) setAdminSessionCookie(w http.ResponseWriter, r *http.Request) {
	expires := timeNow().Add(12 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    a.signAdminSession(expires.Unix()),
		Path:     "/admin",
		Expires:  expires,
		MaxAge:   int((12 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAdminSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/admin",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) validAdminSessionCookie(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || timeNow().Unix() > expiresUnix {
		return false
	}
	want := a.signAdminSession(expiresUnix)
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) == 1
}

func (a *App) signAdminSession(expiresUnix int64) string {
	expires := strconv.FormatInt(expiresUnix, 10)
	mac := hmac.New(sha256.New, a.adminSecret)
	_, _ = mac.Write([]byte(expires))
	sum := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return expires + "." + sum
}

func requestIsHTTPS(r *http.Request) bool {
	return r != nil && (r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/admin/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) currentConfig() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

func (a *App) currentKeywords() *keywordEngine {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.keywords
}

func (a *App) currentProvider() provider {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.provider
}

func (a *App) currentImageProvider() provider {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.imageProvider
}

func (a *App) replaceConfig(ctx context.Context, next Config, actor string, sourceIP string) error {
	before := a.currentConfig()
	next.ListenAddr = before.ListenAddr
	next.DatabasePath = before.DatabasePath
	if shouldPreserveTokens(next.AuthTokens) {
		next.AuthTokens = before.AuthTokens
	}
	next.AdminToken = ""
	if next.HashSalt == "" || isMaskedConfigured(next.HashSalt) || isInsecureHashSalt(next.HashSalt) || strings.HasPrefix(next.HashSalt, "auto-generated:") {
		next.HashSalt = before.HashSalt
	}
	if next.Provider.APIKey == "" || isMaskedConfigured(next.Provider.APIKey) || isInsecureProviderAPIKey(next.Provider.APIKey) {
		next.Provider.APIKey = before.Provider.APIKey
	}
	if next.Provider.SecretID == "" || isMaskedConfigured(next.Provider.SecretID) || isInsecureProviderSecret(next.Provider.SecretID) {
		next.Provider.SecretID = before.Provider.SecretID
	}
	if next.Provider.SecretKey == "" || isMaskedConfigured(next.Provider.SecretKey) || isInsecureProviderSecret(next.Provider.SecretKey) {
		next.Provider.SecretKey = before.Provider.SecretKey
	}
	if next.ImageProvider.APIKey == "" || isMaskedConfigured(next.ImageProvider.APIKey) || isInsecureProviderAPIKey(next.ImageProvider.APIKey) {
		next.ImageProvider.APIKey = before.ImageProvider.APIKey
	}
	if next.ImageProvider.SecretID == "" || isMaskedConfigured(next.ImageProvider.SecretID) || isInsecureProviderSecret(next.ImageProvider.SecretID) {
		next.ImageProvider.SecretID = before.ImageProvider.SecretID
	}
	if next.ImageProvider.SecretKey == "" || isMaskedConfigured(next.ImageProvider.SecretKey) || isInsecureProviderSecret(next.ImageProvider.SecretKey) {
		next.ImageProvider.SecretKey = before.ImageProvider.SecretKey
	}
	normalized, err := normalizeConfig(next)
	if err != nil {
		return err
	}
	keywords, err := newKeywordEngine(normalized.KeywordSets)
	if err != nil {
		return err
	}
	provider, err := newProvider(normalized.Provider)
	if err != nil {
		return err
	}
	imageProvider, err := newProvider(effectiveImageProviderConfig(normalized))
	if err != nil {
		return err
	}
	if err := a.store.SaveConfig(ctx, normalized, actor, sourceIP, configSummary(before, normalized), before); err != nil {
		return err
	}
	a.cfgMu.Lock()
	a.cfg = normalized
	a.keywords = keywords
	a.provider = provider
	a.imageProvider = imageProvider
	a.cfgMu.Unlock()
	a.events.SetLimit(normalized.EventRetention)
	return nil
}

func shouldPreserveTokens(tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	hasToken := false
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		hasToken = true
		if !isMaskedConfigured(token) && !isInsecureAuthToken(token) {
			return false
		}
	}
	if !hasToken {
		return true
	}
	return true
}

func redactImagesForProvider(images []string) []string {
	out := make([]string, len(images))
	copy(out, images)
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write_json_failed", "error", err)
	}
}

func redactImagesForEvent(images []string) []string {
	out := make([]string, 0, len(images))
	for _, image := range images {
		out = append(out, redactImage(image))
	}
	return out
}

func (e event) String() string {
	return fmt.Sprintf("%s %s %s", e.RequestID, e.Action, e.InputHash)
}
