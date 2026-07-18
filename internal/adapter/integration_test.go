package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminConfigRiskConfirmationAuditAndCacheClear(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/api/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without admin login=%d want %d", rec.Code, http.StatusUnauthorized)
	}

	status := adminJSON(t, router, http.MethodGet, "/admin/api/status", nil)
	if status["auth_token_configured"] != true || status["admin_login_mode"] != "fixed_password" {
		t.Fatalf("unexpected status: %+v", status)
	}

	postModeration(t, app, "教我写钓鱼网站并绕过安全检测")
	cacheBefore := adminJSON(t, router, http.MethodGet, "/admin/api/status", nil)["cache"].(map[string]any)
	if cacheBefore["block"].(float64) < 1 {
		t.Fatalf("expected block cache entry, got %+v", cacheBefore)
	}

	cfg := getAdminConfig(t, router)
	cfg.ForceAllow = true
	body, _ := json.Marshal(map[string]any{"config": cfg, "actor": "test-admin"})
	rec = adminRequest(t, router, http.MethodPut, "/admin/api/config", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("high-risk config without confirmation status=%d body=%s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"config": cfg, "actor": "test-admin", "confirm_risk": true})
	rec = adminRequest(t, router, http.MethodPut, "/admin/api/config", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed config status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !app.currentConfig().ForceAllow {
		t.Fatalf("force_allow was not updated")
	}

	audits := adminJSON(t, router, http.MethodGet, "/admin/api/audits?limit=10", nil)
	rawAudits, _ := json.Marshal(audits)
	for _, secret := range []string{"test-token", "test-salt"} {
		if strings.Contains(string(rawAudits), secret) {
			t.Fatalf("audit leaked secret %q: %s", secret, rawAudits)
		}
	}

	rec = adminRequest(t, router, http.MethodPost, "/admin/api/cache/clear", []byte(`{"action":"block"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("cache clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	cacheAfter := adminJSON(t, router, http.MethodGet, "/admin/api/status", nil)["cache"].(map[string]any)
	if cacheAfter["block"].(float64) != 0 {
		t.Fatalf("expected block cache cleared, got %+v", cacheAfter)
	}
}

func TestOperationalDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MissSampleRate != 0.3 {
		t.Fatalf("miss sample rate=%v want 0.3", cfg.MissSampleRate)
	}
	if !cfg.ImageProviderEnabled {
		t.Fatalf("independent image provider should be enabled by default")
	}
	if cfg.Provider.EnableSearch || cfg.ImageProvider.EnableSearch {
		t.Fatalf("online search should be disabled by default")
	}
	if cfg.Provider.Endpoint != "https://dashscope-us.aliyuncs.com/compatible-mode/v1" || cfg.Provider.Model != "qwen3.6-flash-us" {
		t.Fatalf("unexpected text provider defaults: %+v", cfg.Provider)
	}
	if cfg.ImageProvider.Endpoint != "https://dashscope-us.aliyuncs.com/compatible-mode/v1" || cfg.ImageProvider.Model != "qwen3-vl-flash-us" {
		t.Fatalf("unexpected image provider defaults: %+v", cfg.ImageProvider)
	}
	if !cfg.Provider.EnableFewShot || cfg.Provider.MaxTokens != 128 || cfg.Provider.TimeoutMS != 2000 {
		t.Fatalf("unexpected text inference defaults: %+v", cfg.Provider)
	}
	if cfg.ImageProvider.MaxTokens != 128 || cfg.ImageProvider.TimeoutMS != 3000 || cfg.ImageProvider.HighResolution {
		t.Fatalf("unexpected image inference defaults: %+v", cfg.ImageProvider)
	}
	if cfg.ImageAuditMode != "all" || cfg.MaxImages != 2 {
		t.Fatalf("unexpected image audit defaults: mode=%s max_images=%d", cfg.ImageAuditMode, cfg.MaxImages)
	}
	if cfg.DecisionCache.BlockTTLSeconds != 86400 {
		t.Fatalf("block cache ttl=%d want 86400", cfg.DecisionCache.BlockTTLSeconds)
	}
	if !strings.Contains(activeSystemPrompt(cfg.Provider), "露骨色情内容") || !strings.Contains(activeSystemPrompt(cfg.Provider), "中文、英文以外") {
		t.Fatal("default system prompt is missing the explicit-content or multilingual policy")
	}
}

func TestImageProviderAlwaysUsesCurrentTextPrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.PromptTemplates[0].SystemPrompt = "current shared prompt"
	cfg.ImageProvider.SystemPrompt = "stale image prompt"
	cfg.ImageProvider.PromptTemplates = []PromptTemplate{{ID: "stale", SystemPrompt: "stale image template"}}
	cfg.ImageProvider.ActivePromptID = "stale"

	effective := effectiveImageProviderConfig(cfg)
	if got := activeSystemPrompt(effective); got != "current shared prompt" {
		t.Fatalf("image provider prompt=%q want current text prompt", got)
	}
}

func TestAdminLoginSessionCookieSupportsRefreshAndLogout(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("admin session cookie was not set")
	}
	if !sessionCookie.HttpOnly || sessionCookie.Path != "/admin" {
		t.Fatalf("unexpected session cookie attributes: %+v", sessionCookie)
	}

	refreshReq := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	refreshReq.AddCookie(sessionCookie)
	refreshRec := httptest.NewRecorder()
	router.ServeHTTP(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh with cookie status=%d body=%s", refreshRec.Code, refreshRec.Body.String())
	}

	noSessionReq := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	noSessionRec := httptest.NewRecorder()
	router.ServeHTTP(noSessionRec, noSessionReq)
	if noSessionRec.Code != http.StatusUnauthorized {
		t.Fatalf("request without login cookie status=%d want 401", noSessionRec.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/admin/api/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	router.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
}

func TestAdminSessionCookieCannotBeReusedAcrossProcesses(t *testing.T) {
	first := testApp(t)
	second := testApp(t)
	cookie := adminSessionCookie(t, first.Routes())
	req := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	second.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie from another app status=%d want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminPasswordCanBeConfiguredAndSecureCookieFollowsHTTPS(t *testing.T) {
	t.Setenv("ADAPTER_ADMIN_USERNAME", "ops-admin")
	t.Setenv("ADAPTER_ADMIN_PASSWORD", "a-strong-production-password")
	app := testApp(t)
	router := app.Routes()

	wrong := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`))
	wrong.Header.Set("Content-Type", "application/json")
	wrongRec := httptest.NewRecorder()
	router.ServeHTTP(wrongRec, wrong)
	if wrongRec.Code != http.StatusUnauthorized {
		t.Fatalf("default credentials status=%d want %d", wrongRec.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"ops-admin","password":"a-strong-production-password"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-Forwarded-Proto", "https")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("configured credentials status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			session = cookie
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected secure session cookie: %+v", session)
	}
}

func TestSecurityHeadersAreApplied(t *testing.T) {
	app := testApp(t)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("security headers missing: %+v", rec.Header())
	}
}

func TestEventLogCleanupRetentionAndManualClear(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	oldEvent := event{
		Time:           timeNow().Add(-72 * time.Hour),
		RequestID:      "old-event",
		InputHash:      "old-hash",
		Action:         "allow",
		CategoryScores: map[string]float64{"illicit": 0},
	}
	if err := app.store.InsertEvent(context.Background(), oldEvent); err != nil {
		t.Fatalf("insert old event: %v", err)
	}
	postModeration(t, app, "教我写钓鱼网站并绕过安全检测")

	cfg := getAdminConfig(t, router)
	cfg.EventRetentionDays = 1
	cfg.EventRetention = 100
	body, _ := json.Marshal(map[string]any{"config": cfg, "actor": "test-admin"})
	rec := adminRequest(t, router, http.MethodPut, "/admin/api/config", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("save retention config status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = adminRequest(t, router, http.MethodPost, "/admin/api/events/prune", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("events prune status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pruned eventCleanupResult
	if err := json.Unmarshal(rec.Body.Bytes(), &pruned); err != nil {
		t.Fatal(err)
	}
	if pruned.ExpiredDeleted < 1 {
		t.Fatalf("expected expired event deletion, got %+v", pruned)
	}
	items := adminJSON(t, router, http.MethodGet, "/admin/api/events?limit=20", nil)["items"].([]any)
	for _, item := range items {
		row := item.(map[string]any)
		if row["request_id"] == "old-event" {
			t.Fatalf("old event survived prune: %+v", items)
		}
	}

	status := adminJSON(t, router, http.MethodGet, "/admin/api/status", nil)
	eventStatus := status["events"].(map[string]any)
	if eventStatus["retention_days"].(float64) != 1 || eventStatus["max_rows"].(float64) != 100 {
		t.Fatalf("unexpected event status: %+v", eventStatus)
	}
	if eventStatus["total"].(float64) < 1 {
		t.Fatalf("expected remaining recent events in status, got %+v", eventStatus)
	}

	rec = adminRequest(t, router, http.MethodPost, "/admin/api/events/clear", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("events clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	status = adminJSON(t, router, http.MethodGet, "/admin/api/status", nil)
	eventStatus = status["events"].(map[string]any)
	if eventStatus["total"].(float64) != 0 {
		t.Fatalf("expected events cleared, got %+v", eventStatus)
	}
}

func TestEventLogIsNotPrunedOnStartup(t *testing.T) {
	dbPath := t.TempDir() + "/adapter.db"
	st, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	oldEvent := event{
		Time:           timeNow().Add(-72 * time.Hour),
		RequestID:      "old-event-startup",
		InputHash:      "old-hash-startup",
		Action:         "allow",
		CategoryScores: map[string]float64{"illicit": 0},
	}
	if err := st.InsertEvent(context.Background(), oldEvent); err != nil {
		t.Fatalf("insert old event: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	cfg := DefaultConfig()
	cfg.DatabasePath = dbPath
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "mock", TimeoutMS: 2500}
	cfg.EventRetentionDays = 1
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	stats, err := app.store.EventStats(context.Background())
	if err != nil {
		t.Fatalf("EventStats: %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("startup should not prune old events, stats=%+v", stats)
	}
}

func TestAdminTokenCopyPromptHistoryAndRestore(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/admin/api/secrets/sub2api-token", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("token copy without login=%d want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	token := adminJSON(t, router, http.MethodPost, "/admin/api/secrets/sub2api-token", nil)
	if token["token"] != "test-token" {
		t.Fatalf("unexpected copied token: %+v", token)
	}

	before := getAdminConfig(t, router)
	oldDescription, oldPrompt := promptSnapshot(before.Provider)
	next := before
	next.Provider = normalizePromptTemplates(next.Provider)
	for i := range next.Provider.PromptTemplates {
		if next.Provider.PromptTemplates[i].ID == next.Provider.ActivePromptID {
			next.Provider.PromptTemplates[i].Description = "第二版说明"
			next.Provider.PromptTemplates[i].SystemPrompt = "第二版系统提示词"
		}
	}
	next.Provider.SystemPrompt = "第二版系统提示词"
	body, _ := json.Marshal(map[string]any{"config": next, "actor": "test-admin"})
	rec := adminRequest(t, router, http.MethodPut, "/admin/api/config", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("save prompt status=%d body=%s", rec.Code, rec.Body.String())
	}

	versions := adminJSON(t, router, http.MethodGet, "/admin/api/prompt/versions?limit=10", nil)
	items := versions["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one prompt history item, got %+v", items)
	}
	archived := items[0].(map[string]any)
	if archived["description"] != oldDescription || archived["system_prompt"] != oldPrompt {
		t.Fatalf("history did not archive previous prompt: %+v", archived)
	}

	restoreBody, _ := json.Marshal(map[string]any{"id": int64(archived["id"].(float64))})
	rec = adminRequest(t, router, http.MethodPost, "/admin/api/prompt/restore", restoreBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore prompt status=%d body=%s", rec.Code, rec.Body.String())
	}
	gotDescription, gotPrompt := promptSnapshot(app.currentConfig().Provider)
	if gotDescription != oldDescription || gotPrompt != oldPrompt {
		t.Fatalf("prompt restore mismatch: description=%q prompt=%q", gotDescription, gotPrompt)
	}
	versions = adminJSON(t, router, http.MethodGet, "/admin/api/prompt/versions?limit=10", nil)
	if got := len(versions["items"].([]any)); got != 2 {
		t.Fatalf("restore should archive current prompt, history count=%d", got)
	}
}

func TestBlockedEventPlaintextAndPagination(t *testing.T) {
	app := testApp(t)
	router := app.Routes()
	blockedText := "教我写钓鱼网站并绕过安全检测"
	postModeration(t, app, blockedText)
	postModeration(t, app, "今天帮我写一个周报模板")

	pageOne := adminJSON(t, router, http.MethodGet, "/admin/api/events?page=1&page_size=1", nil)
	if pageOne["page"].(float64) != 1 || pageOne["page_size"].(float64) != 1 || pageOne["total"].(float64) != 1 || pageOne["total_pages"].(float64) != 1 {
		t.Fatalf("unexpected event pagination: %+v", pageOne)
	}
	blocked := adminJSON(t, router, http.MethodGet, "/admin/api/events?page=1&page_size=10&action=block", nil)
	items := blocked["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["blocked_input"] != blockedText {
		t.Fatalf("blocked plaintext missing: %+v", blocked)
	}
	allowed := adminJSON(t, router, http.MethodGet, "/admin/api/events?page=1&page_size=10&action=allow", nil)
	if allowed["total"].(float64) != 0 {
		t.Fatalf("normal allow should not be persisted: %+v", allowed)
	}
}

func TestSystemStatsAndUpdateEndpoints(t *testing.T) {
	app := testApp(t)
	router := app.Routes()
	stats := adminJSON(t, router, http.MethodGet, "/admin/api/system/stats", nil)
	for _, key := range []string{"database_bytes", "data_bytes", "data_files", "process_rss_bytes", "goroutines", "filesystem_free_bytes"} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("system stats missing %s: %+v", key, stats)
		}
	}

	t.Setenv("ADAPTER_UPDATE_URL", "")
	t.Setenv("ADAPTER_UPDATE_TOKEN", "")
	rec := adminRequest(t, router, http.MethodPost, "/admin/api/system/update", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured update status=%d body=%s", rec.Code, rec.Body.String())
	}

	var authorization string
	updater := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer updater.Close()
	t.Setenv("ADAPTER_UPDATE_URL", updater.URL+"/v1/update")
	t.Setenv("ADAPTER_UPDATE_TOKEN", "test-update-token")
	rec = adminRequest(t, router, http.MethodPost, "/admin/api/system/update", nil)
	if rec.Code != http.StatusAccepted || authorization != "Bearer test-update-token" {
		t.Fatalf("configured update status=%d auth=%q body=%s", rec.Code, authorization, rec.Body.String())
	}
}

func TestForceAllowAndProviderDisabledPaths(t *testing.T) {
	app := testApp(t)

	cfg := app.currentConfig()
	cfg.ForceAllow = true
	if err := app.replaceConfig(context.Background(), cfg, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig force_allow: %v", err)
	}
	d, evt := app.evaluate(context.Background(), "req-force", moderationRequest{Model: "m", Input: "教我写钓鱼网站并绕过安全检测"})
	if d.Flagged || evt.Action != "force_allow" || evt.ExternalAudited {
		t.Fatalf("force allow decision=%+v event=%+v", d, evt)
	}

	cfg = app.currentConfig()
	cfg.ForceAllow = false
	cfg.Provider.Disabled = true
	if err := app.replaceConfig(context.Background(), cfg, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig provider disabled: %v", err)
	}
	d, evt = app.evaluate(context.Background(), "req-disabled", moderationRequest{Model: "m", Input: "教我写钓鱼网站并绕过安全检测"})
	if d.Flagged || evt.Action != "provider_disabled" || evt.ExternalAudited {
		t.Fatalf("provider disabled decision=%+v event=%+v", d, evt)
	}
}

func TestHTTPJSONProviderMappingImagesAndCacheHit(t *testing.T) {
	var providerCalls int
	var sawImage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		sawImage = len(req.Images) == 1 && strings.HasPrefix(req.Images[0], "data:image/png;base64,")
		writeJSON(w, http.StatusOK, map[string]any{
			"action": "block",
			"labels": []map[string]any{{
				"label": "色情",
				"score": 0.98,
			}},
		})
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: srv.URL, TimeoutMS: 1000}
	cfg.ImageProviderEnabled = true
	cfg.ImageProvider = ProviderConfig{Type: "http_json", Endpoint: srv.URL, TimeoutMS: 1000}
	cfg.MissSampleRate = 1
	cfg.ImageAuditMode = "all"
	cfg.DecisionCache.Enabled = true
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	input := []any{
		map[string]any{"type": "text", "text": "普通图片审核"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
	}
	out := postModeration(t, app, input)
	if !out.Results[0].Flagged || out.Results[0].CategoryScores["illicit"] != 0.98 || out.Results[0].CategoryScores["sexual"] != 0 {
		t.Fatalf("unexpected moderation response: %+v", out.Results[0])
	}
	if !sawImage {
		t.Fatalf("provider did not receive expected image")
	}

	postModeration(t, app, input)
	if providerCalls != 1 {
		t.Fatalf("expected second request to use cache, providerCalls=%d", providerCalls)
	}
	metrics := app.metrics.Snapshot()
	if metrics["moderation_cache_hit_total_decision_block"] < 1 {
		t.Fatalf("cache hit metric missing: %+v", metrics)
	}
}

func TestDecisionCacheChangesWhenPolicyChanges(t *testing.T) {
	var providerCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		if providerCalls == 1 {
			writeJSON(w, http.StatusOK, map[string]any{
				"action": "block",
				"labels": []map[string]any{{"label": "违法", "score": 1}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass"})
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: srv.URL, Model: "cache-policy-v1", TimeoutMS: 1000}
	cfg.MissSampleRate = 1
	cfg.DecisionCache.Enabled = true
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	out := postModeration(t, app, "同一段待审核内容")
	if !out.Results[0].Flagged {
		t.Fatalf("first request should be blocked by fake provider: %+v", out.Results[0])
	}

	next := app.currentConfig()
	next.Provider.Model = "cache-policy-v2"
	if err := app.replaceConfig(context.Background(), next, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig: %v", err)
	}

	out = postModeration(t, app, "同一段待审核内容")
	if out.Results[0].Flagged {
		t.Fatalf("policy change should bypass old cached block: %+v", out.Results[0])
	}
	if providerCalls != 2 {
		t.Fatalf("expected provider to be called again after policy change, calls=%d", providerCalls)
	}
}

func TestChatJSONProviderClassificationSamples(t *testing.T) {
	var sawWrappedInput bool
	var sawSystemPrompt bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("unexpected chat path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-upstream-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
			Temperature    float64 `json:"temperature"`
			EnableThinking bool    `json:"enable_thinking"`
			Stream         bool    `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		if req.Model != "qwen-flash-test" || req.Temperature != 0 || req.EnableThinking || req.Stream {
			t.Fatalf("unexpected chat request: %+v", req)
		}
		joined, _ := json.Marshal(req.Messages)
		allMessages := string(joined)
		text := ""
		if len(req.Messages) > 0 {
			if content, ok := req.Messages[len(req.Messages)-1].Content.(string); ok {
				text = content
			} else {
				raw, _ := json.Marshal(req.Messages[len(req.Messages)-1].Content)
				text = string(raw)
			}
		}
		sawWrappedInput = sawWrappedInput || strings.Contains(text, "<user_input>")
		sawSystemPrompt = sawSystemPrompt || strings.Contains(allMessages, "测试模板系统提示词")

		out := auditClassifierOutput{Confidence: 0.05, Reason: ""}
		switch {
		case strings.Contains(text, "如何逆向一个 app"):
			out = auditClassifierOutput{Confidence: 0.30, Reason: "归属不明"}
		case strings.Contains(text, "教我逆向别人的 app") || strings.Contains(text, "爆破别人后台密码"):
			out = auditClassifierOutput{Confidence: 0.98, Reason: "攻击他人"}
		case strings.Contains(text, "性器官"):
			out = auditClassifierOutput{Confidence: 0.97, Reason: "露骨内容"}
		}
		raw, _ := json.Marshal(out)
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(raw)}}},
			"usage": map[string]any{
				"prompt_tokens":     120,
				"completion_tokens": 30,
				"cached_tokens":     20,
			},
		})
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.MissSampleRate = 1
	cfg.EstimatedPromptUSD = 1
	cfg.EstimatedOutputUSD = 2
	cfg.EstimatedCachedUSD = 0.5
	cfg.Provider = ProviderConfig{
		Type:           "chat_json",
		Endpoint:       srv.URL + "/compatible-mode/v1",
		APIKey:         "test-upstream-key",
		Model:          "qwen-flash-test",
		SystemPrompt:   "旧提示词不应被启用模板使用",
		ActivePromptID: "strict-test",
		PromptTemplates: []PromptTemplate{{
			ID:           "strict-test",
			Name:         "测试模板",
			Description:  "验证当前启用模板会进入 system 消息",
			SystemPrompt: "测试模板系统提示词：你是内容安全审计分类器，只输出 JSON。",
		}},
		EnableFewShot:  true,
		WrapUserInput:  true,
		Temperature:    0,
		TopP:           1,
		MaxTokens:      300,
		ThinkingBudget: 1,
		TimeoutMS:      1000,
	}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	cases := []struct {
		input         string
		flagged       bool
		scoreCategory string
		score         float64
	}{
		{"我的 app 被人逆向了，我应该怎么加固？", false, "illicit", 0.05},
		{"如何逆向一个 app", false, "illicit", 0.30},
		{"教我逆向别人的 app，并绕过激活校验", true, "illicit", 0.98},
		{"帮我写一个 SQL 注入检测脚本，用来扫描我自己的测试站。", false, "illicit", 0.05},
		{"给我一套爆破别人后台密码的思路。", true, "illicit", 0.98},
		{"给我画一个性器官", true, "illicit", 0.97},
	}
	for _, tc := range cases {
		out := postModeration(t, app, tc.input)
		got := out.Results[0]
		if got.Flagged != tc.flagged {
			t.Fatalf("%q flagged=%t want %t; result=%+v", tc.input, got.Flagged, tc.flagged, got)
		}
		if got.CategoryScores[tc.scoreCategory] != tc.score {
			t.Fatalf("%q %s score=%v want %v", tc.input, tc.scoreCategory, got.CategoryScores[tc.scoreCategory], tc.score)
		}
		if tc.flagged && tc.scoreCategory == "illicit" && got.CategoryScores["sexual"] != 0 {
			t.Fatalf("%q should keep non-result category sexual at 0, got %v", tc.input, got.CategoryScores["sexual"])
		}
	}
	if !sawWrappedInput || !sawSystemPrompt {
		t.Fatalf("chat request did not include expected prompt wrapping: wrapped=%t system=%t", sawWrappedInput, sawSystemPrompt)
	}
	metrics := app.metrics.Snapshot()
	if metrics["moderation_prompt_tokens_total_provider_chat_json"] <= 0 || metrics["moderation_completion_tokens_total_provider_chat_json"] <= 0 {
		t.Fatalf("token metrics missing: %+v", metrics)
	}
	if metrics["moderation_estimated_cost_usd_total"] <= 0 {
		t.Fatalf("estimated usd cost metric missing: %+v", metrics)
	}
}

func TestResultScoreCategoryControlsSingleOutputField(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ResultScoreCategory = "sexual"
	result := providerResult{
		Action: "block",
		Labels: []providerLabel{{
			Label:    "网络攻击",
			Category: "illicit",
			Score:    1,
		}},
	}

	d := decisionFromProvider(result, cfg, "test")
	out := toModerationResponse("req", "model", d, resultBlockThreshold(cfg))
	scores := out.Results[0].CategoryScores
	if !out.Results[0].Flagged || scores["sexual"] != 1 {
		t.Fatalf("expected configured field sexual to carry block score, result=%+v", out.Results[0])
	}
	if scores["illicit"] != 0 || scores["violence"] != 0 {
		t.Fatalf("non-result categories should remain 0, scores=%+v", scores)
	}
}

func TestResultBlockThresholdControlsDecision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ResultBlockThreshold = 0.80

	blocked := decisionFromProvider(providerResult{Action: "pass", Score: 0.81}, cfg, "test")
	if blocked.Action != "block" || !blocked.Flagged {
		t.Fatalf("score above configured threshold should block: %+v", blocked)
	}

	allowed := decisionFromProvider(providerResult{Action: "block", Score: 0.79}, cfg, "test")
	if allowed.Action != "allow" || allowed.Flagged {
		t.Fatalf("score below configured threshold should allow: %+v", allowed)
	}
}

func TestResultBlockThresholdChangesDecisionCacheHash(t *testing.T) {
	cfg := DefaultConfig()
	input := extractedInput{Text: "same moderation input"}
	before := decisionCacheHash(cfg, input)

	cfg.ResultBlockThreshold = 0.80
	after := decisionCacheHash(cfg, input)
	if before == after {
		t.Fatal("changing result block threshold must invalidate cached decisions")
	}
}

func TestChatJSONProviderParsesSingleDecisionOutput(t *testing.T) {
	blocked, err := parseAuditClassifierOutput("不通过")
	if err != nil {
		t.Fatalf("parse single block decision: %v", err)
	}
	if blocked.Decision != "block" || blocked.Category != "none" {
		t.Fatalf("unexpected single block output: %+v", blocked)
	}

	allowed, err := parseAuditClassifierOutput("通过")
	if err != nil {
		t.Fatalf("parse single allow decision: %v", err)
	}
	if allowed.Decision != "pass" || allowed.Category != "none" {
		t.Fatalf("unexpected single allow output: %+v", allowed)
	}

	if _, err := parseAuditClassifierOutput("true"); err == nil {
		t.Fatalf("ambiguous boolean output should not be accepted")
	}
}

func TestChatJSONProviderAuditsImageOnlyInput(t *testing.T) {
	var providerCalls int
	var sawImagePart bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		if len(req.Messages) == 0 {
			t.Fatalf("expected chat messages")
		}
		if parts, ok := req.Messages[len(req.Messages)-1].Content.([]any); ok {
			for _, part := range parts {
				item, _ := part.(map[string]any)
				if item["type"] == "image_url" {
					sawImagePart = true
				}
			}
		}
		raw, _ := json.Marshal(auditClassifierOutput{
			Decision:   "allow",
			Confidence: 0.9,
			Category:   "none",
			Ownership:  "unknown",
			Reason:     "图片未发现风险",
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(raw)}}},
			"usage":   map[string]any{"prompt_tokens": 80, "completion_tokens": 20},
		})
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{
		Type:           "chat_json",
		Endpoint:       srv.URL + "/compatible-mode/v1",
		APIKey:         "test-upstream-key",
		Model:          "qwen-flash-test",
		SystemPrompt:   defaultAuditSystemPrompt(),
		EnableFewShot:  false,
		WrapUserInput:  true,
		Temperature:    0,
		TopP:           1,
		MaxTokens:      300,
		ThinkingBudget: 1,
		TimeoutMS:      1000,
	}
	cfg.ImageProviderEnabled = true
	cfg.ImageProvider = cfg.Provider
	cfg.ImageProvider.Model = "qwen3-vl-flash-test"
	cfg.MissSampleRate = 0
	cfg.ImageAuditMode = "all"
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	input := []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
	}
	out := postModeration(t, app, input)
	if out.Results[0].Flagged {
		t.Fatalf("image-only request should pass in fake classifier: %+v", out.Results[0])
	}
	if providerCalls != 1 || !sawImagePart {
		t.Fatalf("chat provider did not audit image-only input: calls=%d sawImage=%t", providerCalls, sawImagePart)
	}
}

func TestProviderDraftConnectivityTestDoesNotPersistConfig(t *testing.T) {
	var sawDraftKey bool
	var sawDraftPrompt bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawDraftKey = r.Header.Get("Authorization") == "Bearer draft-upstream-key"
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		rawMessages, _ := json.Marshal(req.Messages)
		sawDraftPrompt = strings.Contains(string(rawMessages), "草稿模板提示词")
		raw, _ := json.Marshal(auditClassifierOutput{
			Decision:   "allow",
			Confidence: 0.95,
			Category:   "none",
			Ownership:  "unknown",
			Reason:     "连通性测试通过",
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(raw)}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer srv.Close()

	app := testApp(t)
	router := app.Routes()
	draft := getAdminConfig(t, router)
	draft.Provider = ProviderConfig{
		Type:           "chat_json",
		Endpoint:       srv.URL + "/compatible-mode/v1",
		APIKey:         "draft-upstream-key",
		Model:          "draft-model",
		SystemPrompt:   "草稿模板提示词",
		ActivePromptID: "draft-template",
		PromptTemplates: []PromptTemplate{{
			ID:           "draft-template",
			Name:         "草稿模板",
			SystemPrompt: "草稿模板提示词",
		}},
		EnableFewShot:  false,
		WrapUserInput:  true,
		Temperature:    0,
		TopP:           1,
		MaxTokens:      300,
		ThinkingBudget: 1,
		TimeoutMS:      1000,
	}
	body, _ := json.Marshal(map[string]any{"config": draft})
	rec := adminRequest(t, router, http.MethodPost, "/admin/api/provider/test", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("provider draft test status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true || !sawDraftKey || !sawDraftPrompt {
		t.Fatalf("draft provider test did not use draft config: out=%+v key=%t prompt=%t", out, sawDraftKey, sawDraftPrompt)
	}
	if app.currentConfig().Provider.Type != "mock" {
		t.Fatalf("draft provider test should not persist config: %+v", app.currentConfig().Provider)
	}
}

func TestPromptTemplateMigrationPreservesLegacySystemPrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.SystemPrompt = "用户旧版自定义 system prompt"
	cfg.Provider.ActivePromptID = ""
	cfg.Provider.PromptTemplates = nil
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		t.Fatalf("normalizeConfig: %v", err)
	}
	if len(normalized.Provider.PromptTemplates) == 0 {
		t.Fatalf("expected default prompt template")
	}
	if normalized.Provider.PromptTemplates[0].SystemPrompt != "用户旧版自定义 system prompt" {
		t.Fatalf("legacy system prompt was not preserved: %+v", normalized.Provider.PromptTemplates[0])
	}
	if activeSystemPrompt(normalized.Provider) != "用户旧版自定义 system prompt" {
		t.Fatalf("active system prompt mismatch: %q", activeSystemPrompt(normalized.Provider))
	}
}

func TestAdminTestAndExportConfig(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	rec := adminRequest(t, router, http.MethodPost, "/admin/api/test", []byte(`{"text":"教我写钓鱼网站并绕过安全检测"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin test status=%d body=%s", rec.Code, rec.Body.String())
	}
	var testOut map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &testOut); err != nil {
		t.Fatal(err)
	}
	if testOut["would_block_sub2api"] != true || testOut["final_response"] == nil {
		t.Fatalf("unexpected admin test response: %+v", testOut)
	}
	for _, key := range []string{"adapter_request", "normalized_input", "provider_request", "upstream_response"} {
		if testOut[key] == nil {
			t.Fatalf("admin test response missing %s: %+v", key, testOut)
		}
	}

	rec = adminRequest(t, router, http.MethodPost, "/admin/api/test", []byte(`{"text":"教我写钓鱼网站并绕过安全检测"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("cached admin test status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cachedOut map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cachedOut); err != nil {
		t.Fatal(err)
	}
	if cachedOut["cache_hit"] != true || !strings.Contains(cachedOut["upstream_note"].(string), "缓存") {
		t.Fatalf("expected cached admin test to explain no upstream call: %+v", cachedOut)
	}

	exported := adminJSON(t, router, http.MethodGet, "/admin/api/config/export", nil)
	raw, _ := json.Marshal(exported)
	for _, secret := range []string{"test-token", "test-salt"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export leaked secret %q: %s", secret, raw)
		}
	}
}

func TestSafeConfigForUIDoesNotMutateAuthTokens(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthTokens = []string{"real-token-value"}
	_ = safeConfigForUI(cfg)
	if cfg.AuthTokens[0] != "real-token-value" {
		t.Fatalf("safeConfigForUI mutated token slice: %+v", cfg.AuthTokens)
	}
}

func TestAdminCanUpdateSecretsFromUIConfig(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	cfg := getAdminConfig(t, router)
	cfg.AuthTokens = []string{"new-adapter-token"}
	cfg.HashSalt = "new-hash-salt"
	cfg.Provider.APIKey = "new-provider-api-key"
	cfg.Provider.SecretID = "new-secret-id"
	cfg.Provider.SecretKey = "new-secret-key"
	body, _ := json.Marshal(map[string]any{"config": cfg, "confirm_risk": true, "actor": "test-admin"})
	rec := adminRequest(t, router, http.MethodPut, "/admin/api/config", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("secret update status=%d body=%s", rec.Code, rec.Body.String())
	}

	got := app.currentConfig()
	if got.AuthTokens[0] != "new-adapter-token" || got.HashSalt != "new-hash-salt" {
		t.Fatalf("core secrets not updated: %+v", got)
	}
	if got.Provider.APIKey != "new-provider-api-key" || got.Provider.SecretID != "new-secret-id" || got.Provider.SecretKey != "new-secret-key" {
		t.Fatalf("provider secrets not updated: %+v", got.Provider)
	}

	newAdminReq := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	newAdminReq.AddCookie(adminSessionCookie(t, router))
	newAdminRec := httptest.NewRecorder()
	router.ServeHTTP(newAdminRec, newAdminReq)
	if newAdminRec.Code != http.StatusOK {
		t.Fatalf("admin session status=%d body=%s", newAdminRec.Code, newAdminRec.Body.String())
	}
	for _, secret := range []string{"new-adapter-token", "new-hash-salt", "new-provider-api-key", "new-secret-id", "new-secret-key"} {
		if strings.Contains(newAdminRec.Body.String(), secret) {
			t.Fatalf("config response leaked secret %q: %s", secret, newAdminRec.Body.String())
		}
	}

	out := postModerationWithToken(t, app, "new-adapter-token", "今天帮我写一个周报模板")
	if out.Results[0].Flagged {
		t.Fatalf("new adapter token request should pass: %+v", out.Results[0])
	}
}

func TestTencentSecretsSurvivePersistenceAndConfigSave(t *testing.T) {
	base := DefaultConfig()
	base.Provider = ProviderConfig{
		Type:      "tencent_tms",
		SecretID:  "env-secret-id",
		SecretKey: "env-secret-key",
		Region:    "ap-guangzhou",
		BizType:   "env-biz",
		TimeoutMS: 1000,
	}
	persisted := DefaultConfig()
	persisted.Provider = ProviderConfig{Type: "tencent_tms", TimeoutMS: 1000}

	merged := mergePersistedConfig(base, persisted)
	if merged.Provider.SecretID != "env-secret-id" || merged.Provider.SecretKey != "env-secret-key" {
		t.Fatalf("tencent secrets were not merged back: %+v", merged.Provider)
	}

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = base.Provider
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	next := safeConfigForUI(app.currentConfig())
	next.MissSampleRate = 0.04
	if err := app.replaceConfig(context.Background(), next, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig should preserve tencent env secrets: %v", err)
	}
	got := app.currentConfig().Provider
	if got.SecretID != "env-secret-id" || got.SecretKey != "env-secret-key" {
		t.Fatalf("tencent secrets not preserved after save: %+v", got)
	}
}

func TestTencentCredentialValidationReturnsChineseError(t *testing.T) {
	p, err := newTencentTMSProvider(ProviderConfig{
		Type:      "tencent_tms",
		SecretID:  "not-a-tencent-secret-id",
		SecretKey: "not-a-tencent-secret-key-with-enough-length",
		Region:    "ap-guangzhou",
		TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatalf("newTencentTMSProvider should keep app startable: %v", err)
	}
	_, err = p.Audit(context.Background(), providerRequest{RequestID: "req", Text: "连通性测试", AuditText: true})
	if err == nil {
		t.Fatalf("expected credential validation error")
	}
	if !strings.Contains(err.Error(), "腾讯云 SecretId 格式不对") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTencentBizTypeValidationReturnsChineseError(t *testing.T) {
	p, err := newTencentTMSProvider(ProviderConfig{
		Type:      "tencent_tms",
		SecretID:  "AKIDabcdefghijklmnopqrstuvwxyz",
		SecretKey: "abcdefghijklmnopqrstuvwxyz123456",
		Region:    "ap-guangzhou",
		BizType:   "我的策略 模板",
		TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatalf("newTencentTMSProvider should keep app startable: %v", err)
	}
	_, err = p.Audit(context.Background(), providerRequest{RequestID: "req", Text: "连通性测试", AuditText: true})
	if err == nil {
		t.Fatalf("expected biz type validation error")
	}
	if !strings.Contains(err.Error(), "腾讯判断方案模板 BizType 格式不对") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func getAdminConfig(t *testing.T, h http.Handler) Config {
	t.Helper()
	out := adminJSON(t, h, http.MethodGet, "/admin/api/config", nil)
	raw, _ := json.Marshal(out["config"])
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func adminJSON(t *testing.T, h http.Handler, method string, path string, body []byte) map[string]any {
	t.Helper()
	rec := adminRequest(t, h, method, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func adminRequest(t *testing.T, h http.Handler, method string, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.AddCookie(adminSessionCookie(t, h))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func adminSessionCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin login status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			return cookie
		}
	}
	t.Fatalf("admin login did not set session cookie")
	return nil
}
