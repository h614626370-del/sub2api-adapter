package adapter

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	serveAdminAsset(w, r)
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.Username)), []byte(a.adminUsername)) != 1 ||
		subtle.ConstantTimeCompare([]byte(in.Password), []byte(a.adminPassword)) != 1 {
		http.Error(w, "用户名或密码不正确", http.StatusUnauthorized)
		return
	}
	a.setAdminSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": a.adminUsername})
}

func (a *App) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	clearAdminSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	cfg := a.currentConfig()
	cacheStats, err := a.store.DecisionCacheStats(r.Context())
	if err != nil {
		cacheStats = a.cache.Stats()
	}
	evStats, err := a.store.EventStats(r.Context())
	if err != nil {
		evStats = eventStats{}
	}
	warnings := productionWarnings(cfg)
	if a.adminPassword == defaultAdminPassword {
		warnings = append(warnings, "管理员密码仍是公开的开发默认值；生产部署必须设置 ADAPTER_ADMIN_PASSWORD 或使用新版安装脚本生成随机密码")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":                  a.currentProvider().Name(),
		"force_allow":               cfg.ForceAllow,
		"provider_disabled":         cfg.Provider.Disabled,
		"cache":                     cacheStats,
		"metrics":                   a.metrics.Snapshot(),
		"keyword_sets":              len(cfg.KeywordSets),
		"started_at":                a.metrics.started,
		"adapter_version":           VersionInfo(),
		"auth_token_configured":     len(cfg.AuthTokens) > 0,
		"admin_login_mode":          "fixed_password",
		"hash_salt_configured":      cfg.HashSalt != "",
		"provider_key_status":       providerKeyStatus(cfg.Provider),
		"image_provider_key_status": providerKeyStatus(effectiveImageProviderConfig(cfg)),
		"image_provider_enabled":    cfg.ImageProviderEnabled,
		"production_warnings":       dedupeStrings(warnings),
		"database_path":             cfg.DatabasePath,
		"events": map[string]any{
			"total":          evStats.Total,
			"oldest":         evStats.Oldest,
			"newest":         evStats.Newest,
			"retention_days": cfg.EventRetentionDays,
			"max_rows":       cfg.EventRetention,
		},
	})
}

func (a *App) handleCopySub2APIToken(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	cfg := a.currentConfig()
	if len(cfg.AuthTokens) == 0 || strings.TrimSpace(cfg.AuthTokens[0]) == "" {
		http.Error(w, "sub2api 调用密钥尚未配置", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": cfg.AuthTokens[0]})
}

func providerKeyStatus(cfg ProviderConfig) string {
	if cfg.Type == "tencent_tms" {
		return maskConfigured(cfg.SecretID)
	}
	return maskConfigured(cfg.APIKey)
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": safeConfigForUI(a.currentConfig())})
}

func (a *App) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	var in struct {
		Config      Config `json:"config"`
		ConfirmRisk bool   `json:"confirm_risk"`
		Actor       string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}
	if requiresRiskConfirmation(a.currentConfig(), in.Config) && !in.ConfirmRisk {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "risk_confirmation_required",
			"message": "高风险配置变更需要二次确认",
		})
		return
	}
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		actor = "admin"
	}
	if err := a.replaceConfig(r.Context(), in.Config, actor, remoteIP(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": safeConfigForUI(a.currentConfig())})
}

func (a *App) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	var in struct {
		Config      Config `json:"config"`
		ConfirmRisk bool   `json:"confirm_risk"`
		Actor       string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}
	if requiresRiskConfirmation(a.currentConfig(), in.Config) && !in.ConfirmRisk {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "risk_confirmation_required"})
		return
	}
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		actor = "import"
	}
	if err := a.replaceConfig(r.Context(), in.Config, actor, remoteIP(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": safeConfigForUI(a.currentConfig())})
}

func (a *App) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=sub2api-adapter-config.json")
	writeJSON(w, http.StatusOK, safeConfigForUI(a.currentConfig()))
}

func (a *App) handleResetConfig(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	next := DefaultConfig()
	if err := a.replaceConfig(r.Context(), next, "reset", remoteIP(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": safeConfigForUI(a.currentConfig())})
}

func (a *App) handleAdminTest(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	var in struct {
		Model  string   `json:"model"`
		Text   string   `json:"text"`
		Images []string `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}
	input := any(in.Text)
	if len(in.Images) > 0 {
		parts := []any{}
		if in.Text != "" {
			parts = append(parts, map[string]any{"type": "text", "text": in.Text})
		}
		for _, image := range in.Images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": image}})
		}
		input = parts
	}
	req := moderationRequest{Model: in.Model, Input: input}
	if req.Model == "" {
		req.Model = "llm-audit-adapter-v1"
	}
	requestID := newRequestID()
	normalized := extractModerationInput(req.Input, a.currentConfig().MaxTextChars, a.currentConfig().MaxImages, a.currentConfig().AllowDataURLImage)
	d, evt, trace := a.evaluateWithTrace(r.Context(), requestID, req)
	upstreamNote := ""
	if !evt.ExternalAudited {
		upstreamNote = "本次没有请求上游模型：命中缓存、本地放行、总开关放行，或上游模型已禁用。"
	}
	if trace.CacheNote != "" {
		upstreamNote = trace.CacheNote
	}
	finalResponse := toModerationResponse(requestID, req.Model, d)
	cfg := a.currentConfig()
	resultCategory := cfg.ResultScoreCategory
	resultScore := d.CategoryScores[resultCategory]
	blockThreshold := sub2APIBlockThreshold(resultCategory)
	wouldBlock := wouldBlockSub2API(d.CategoryScores)
	if len(finalResponse.Results) > 0 {
		wouldBlock = wouldBlockSub2API(finalResponse.Results[0].CategoryScores)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":                requestID,
		"adapter_request":           moderationRequestDebug(req),
		"normalized_input":          normalizedInputDebug(normalized),
		"normalized_text":           normalized.Text,
		"keyword_prefilter_enabled": !cfg.DirectModelAudit,
		"keyword_hits":              evt.KeywordHits,
		"sampled":                   evt.Sampled,
		"cache_hit":                 evt.CacheHit,
		"cache_note":                trace.CacheNote,
		"external_audited":          evt.ExternalAudited,
		"provider":                  evt.Provider,
		"provider_request":          redactDebugValue(trace.ProviderRequest),
		"upstream_request":          trace.UpstreamRequest,
		"upstream_response":         trace.UpstreamResponse,
		"upstream_note":             upstreamNote,
		"provider_raw_summary":      evt.ProviderRawSummary,
		"category_scores":           d.CategoryScores,
		"result_score_category":     resultCategory,
		"result_score":              resultScore,
		"sub2api_block_threshold":   blockThreshold,
		"final_response":            finalResponse,
		"would_block_sub2api":       wouldBlock,
		"event":                     evt,
	})
}

func (a *App) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	inputHash := strings.TrimSpace(r.URL.Query().Get("input_hash"))
	items, total, err := a.store.ListEvents(r.Context(), pageSize, (page-1)*pageSize, action, inputHash)
	if err != nil {
		fallback := a.events.List()
		writeJSON(w, http.StatusOK, map[string]any{"items": fallback, "page": 1, "page_size": len(fallback), "total": len(fallback), "total_pages": 1, "warning": err.Error()})
		return
	}
	totalPages := (total + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "page_size": pageSize, "total": total, "total_pages": totalPages})
}

func (a *App) handleEventsClear(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	deleted, err := a.store.ClearEvents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (a *App) handleEventsPrune(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	cfg := a.currentConfig()
	result, err := a.store.PruneEvents(r.Context(), cfg.EventRetentionDays, cfg.EventRetention)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleAdminAudits(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := a.store.ListAudits(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handlePromptVersions(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := a.store.ListPromptVersions(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	description, prompt := promptSnapshot(a.currentConfig().Provider)
	writeJSON(w, http.StatusOK, map[string]any{
		"current": map[string]any{"description": description, "system_prompt": prompt},
		"items":   items,
	})
}

func (a *App) handlePromptRestore(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID <= 0 {
		http.Error(w, "历史版本 ID 无效", http.StatusBadRequest)
		return
	}
	version, err := a.store.GetPromptVersion(r.Context(), in.ID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "提示词历史版本不存在", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg := a.currentConfig()
	cfg.Provider = normalizePromptTemplates(cfg.Provider)
	templates := append([]PromptTemplate(nil), cfg.Provider.PromptTemplates...)
	for i := range templates {
		if templates[i].ID == cfg.Provider.ActivePromptID {
			templates[i].Description = version.Description
			templates[i].SystemPrompt = version.SystemPrompt
			break
		}
	}
	cfg.Provider.PromptTemplates = templates
	cfg.Provider.SystemPrompt = version.SystemPrompt
	if err := a.replaceConfig(r.Context(), cfg, "admin-ui-prompt-restore", remoteIP(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": safeConfigForUI(a.currentConfig()), "restored": version})
}

func (a *App) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	memDeleted := a.cache.Clear(in.Action)
	dbDeleted, err := a.store.ClearDecisionCache(r.Context(), in.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": int64(memDeleted) + dbDeleted})
}

func (a *App) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	provider := a.currentProvider()
	if r.Method == http.MethodPost {
		draftProvider, err := a.providerFromTestDraft(r, false)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": 0, "error": safeSummary(err.Error(), 240)})
			return
		}
		provider = draftProvider
	}
	start := timeNow()
	result, err := provider.Audit(r.Context(), providerRequest{
		RequestID: "provider-test-" + strconv.FormatInt(timeNow().Unix(), 10),
		Text:      "今天帮我写一个周报模板",
		AuditText: true,
	})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": elapsed, "error": safeSummary(err.Error(), 240)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "latency_ms": elapsed, "result": result})
}

func (a *App) handleImageProviderTest(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	provider := a.currentImageProvider()
	if r.Method == http.MethodPost {
		draftProvider, err := a.providerFromTestDraft(r, true)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": 0, "error": safeSummary(err.Error(), 240)})
			return
		}
		provider = draftProvider
	}
	start := timeNow()
	result, err := provider.Audit(r.Context(), providerRequest{
		RequestID:  "image-provider-test-" + strconv.FormatInt(timeNow().Unix(), 10),
		Text:       "请判断这张图片是否合规，只输出 JSON 审核结果。",
		Images:     []string{"https://dashscope.oss-cn-beijing.aliyuncs.com/images/dog_and_girl.jpeg"},
		AuditText:  true,
		AuditImage: true,
	})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": elapsed, "error": safeSummary(err.Error(), 240)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "latency_ms": elapsed, "result": result})
}

func (a *App) providerFromTestDraft(r *http.Request, image bool) (provider, error) {
	var in struct {
		Config Config `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return nil, err
	}
	before := a.currentConfig()
	next := in.Config
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
		return nil, err
	}
	if image {
		return newProvider(effectiveImageProviderConfig(normalized))
	}
	return newProvider(normalized.Provider)
}

func requiresRiskConfirmation(before Config, after Config) bool {
	return before.ForceAllow != after.ForceAllow ||
		before.Provider.Disabled != after.Provider.Disabled ||
		before.ResultScoreCategory != after.ResultScoreCategory ||
		before.DirectModelAudit != after.DirectModelAudit ||
		before.ImageProviderEnabled != after.ImageProviderEnabled ||
		before.ImageProvider.Model != after.ImageProvider.Model ||
		before.MissSampleRate != after.MissSampleRate ||
		before.ImageAuditMode != after.ImageAuditMode
}

func remoteIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > -1 {
		return host[:idx]
	}
	return host
}
