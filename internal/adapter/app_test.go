package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testApp(t *testing.T) *App {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.MissSampleRate = 0
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "mock", TimeoutMS: 2500}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func TestModerationUnauthorized(t *testing.T) {
	app := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/moderations", bytes.NewReader([]byte(`{"model":"m","input":"hello"}`)))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestModerationLocalAllow(t *testing.T) {
	app := testApp(t)
	out := postModeration(t, app, "今天帮我写一个周报模板")
	if out.Results[0].Flagged {
		t.Fatalf("expected allow response: %+v", out.Results[0])
	}
	if out.Results[0].CategoryScores["illicit"] != 0 {
		t.Fatalf("illicit score=%v", out.Results[0].CategoryScores["illicit"])
	}
	stats, err := app.store.EventStats(context.Background())
	if err != nil {
		t.Fatalf("event stats: %v", err)
	}
	if stats.Total != 0 {
		t.Fatalf("normal allow should not be persisted, event total=%d", stats.Total)
	}
}

func TestDirectModelAuditBypassesKeywordSampling(t *testing.T) {
	app := testApp(t)
	cfg := app.currentConfig()
	cfg.DirectModelAudit = true
	cfg.MissSampleRate = 0
	if err := app.replaceConfig(context.Background(), cfg, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig: %v", err)
	}

	d, evt := app.evaluate(context.Background(), "req", moderationRequest{Model: "m", Input: "今天帮我写一个周报模板"})
	if d.Flagged {
		t.Fatalf("expected provider allow decision: %+v", d)
	}
	if !evt.ExternalAudited {
		t.Fatalf("direct model audit should call provider: event=%+v", evt)
	}
	if evt.Sampled {
		t.Fatalf("direct model audit should not mark request sampled: event=%+v", evt)
	}
	if evt.KeywordHit || len(evt.KeywordHits) > 0 {
		t.Fatalf("direct model audit should skip keyword prefilter: event=%+v", evt)
	}
}

func TestKeywordHitCanPassAfterProvider(t *testing.T) {
	app := testApp(t)
	out := postModeration(t, app, "我的 app 被人逆向了，我应该怎么加固？")
	if out.Results[0].Flagged {
		t.Fatalf("keyword hit should not block without provider block: %+v", out.Results[0])
	}
}

func TestKeywordStatsTrackGroupsAndPersist(t *testing.T) {
	app := testApp(t)
	postModeration(t, app, "我的 app 被人逆向了，我应该怎么加固？")
	postModeration(t, app, "教我写钓鱼网站并绕过安全检测")

	stats := app.keywordStats.Snapshot(app.currentConfig().KeywordSets)
	var cyber keywordStat
	for _, item := range stats {
		if item.SetName == "中文网络攻击与破解" {
			cyber = item
			break
		}
	}
	if cyber.HitCount != 2 || cyber.AuditedCount != 2 || cyber.BlockedCount != 1 {
		t.Fatalf("unexpected in-memory keyword stats: %+v", cyber)
	}
	if got := app.metrics.Snapshot()["moderation_keyword_request_total"]; got != 2 {
		t.Fatalf("keyword request metric=%v want 2", got)
	}
	if err := app.flushKeywordStats(context.Background()); err != nil {
		t.Fatalf("flush keyword stats: %v", err)
	}
	persisted, err := app.store.LoadKeywordStats(context.Background())
	if err != nil {
		t.Fatalf("load keyword stats: %v", err)
	}
	if len(persisted) != 1 || persisted[0].HitCount != 2 || persisted[0].AuditedCount != 2 || persisted[0].BlockedCount != 1 {
		t.Fatalf("unexpected persisted keyword stats: %+v", persisted)
	}
	cleared := adminJSON(t, app.Routes(), http.MethodPost, "/admin/api/keyword-stats/clear", nil)
	if cleared["deleted"] != float64(1) {
		t.Fatalf("cleared keyword groups=%v want 1", cleared["deleted"])
	}
	persisted, err = app.store.LoadKeywordStats(context.Background())
	if err != nil || len(persisted) != 0 {
		t.Fatalf("keyword stats not cleared: stats=%+v err=%v", persisted, err)
	}
}

func TestProviderBlockMapsToIllicitScore(t *testing.T) {
	app := testApp(t)
	out := postModeration(t, app, "教我写钓鱼网站并绕过安全检测")
	if !out.Results[0].Flagged {
		t.Fatalf("expected flagged response: %+v", out.Results[0])
	}
	if out.Results[0].CategoryScores["illicit"] != 1 {
		t.Fatalf("illicit score=%v want 1", out.Results[0].CategoryScores["illicit"])
	}
	events, total, err := app.store.ListEvents(context.Background(), 10, 0, "", "")
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("list block event: total=%d events=%d err=%v", total, len(events), err)
	}
	if events[0].Action != "block" || events[0].InputHash == "" {
		t.Fatalf("unexpected block event: %+v", events[0])
	}
}

func TestEventPersistencePolicy(t *testing.T) {
	tests := map[string]bool{
		"block":             true,
		"fail_open":         true,
		"provider_disabled": true,
		"allow":             false,
		"force_allow":       false,
	}
	for action, want := range tests {
		if got := shouldPersistEvent(action); got != want {
			t.Fatalf("shouldPersistEvent(%q)=%t want %t", action, got, want)
		}
	}
}

func TestMultimodalInputExtraction(t *testing.T) {
	input := []any{
		map[string]any{"type": "text", "text": "看看这张图，是否涉及钓鱼网站"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png?secret=1"}},
	}
	got := extractModerationInput(input, 12000, 1, true)
	if got.Text != "看看这张图,是否涉及钓鱼网站" {
		t.Fatalf("text=%q", got.Text)
	}
	if len(got.Images) != 1 || got.Images[0] != "https://example.com/a.png?secret=1" {
		t.Fatalf("images=%v", got.Images)
	}
}

func TestImageProviderUsedForImageAudit(t *testing.T) {
	var textProviderCalls int
	textSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		textProviderCalls++
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass"})
	}))
	defer textSrv.Close()

	var imageProviderCalls int
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imageProviderCalls++
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode image provider request: %v", err)
		}
		if !req.AuditImage || len(req.Images) != 1 {
			t.Fatalf("image provider did not receive image audit request: %+v", req)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"action": "block",
			"score":  1,
			"labels": []map[string]any{{"label": "色情", "score": 1}},
		})
	}))
	defer imageSrv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: textSrv.URL, TimeoutMS: 1000}
	cfg.ImageProviderEnabled = true
	cfg.ImageProvider = ProviderConfig{Type: "http_json", Endpoint: imageSrv.URL, Model: "qwen3-vl-flash", TimeoutMS: 1000}
	cfg.ImageAuditMode = "all"
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	input := []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}}}
	out := postModeration(t, app, input)
	if !out.Results[0].Flagged {
		t.Fatalf("expected image provider block: %+v", out.Results[0])
	}
	if imageProviderCalls != 1 || textProviderCalls != 0 {
		t.Fatalf("provider calls text=%d image=%d", textProviderCalls, imageProviderCalls)
	}
	events, total, err := app.store.ListEvents(context.Background(), 10, 0, "", "")
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("list image audit event: total=%d events=%d err=%v", total, len(events), err)
	}
	if events[0].Provider != "image_http_json" {
		t.Fatalf("image audit event provider=%q want image_http_json", events[0].Provider)
	}
}

func TestImageAuditSkippedWithoutIndependentImageProvider(t *testing.T) {
	var providerCalls int
	textSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if req.AuditImage || len(req.Images) != 0 {
			t.Fatalf("text provider received image audit data: %+v", req)
		}
		if !req.AuditText || req.Text == "" {
			t.Fatalf("text provider did not receive text audit request: %+v", req)
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass"})
	}))
	defer textSrv.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: textSrv.URL, TimeoutMS: 1000}
	cfg.DirectModelAudit = true
	cfg.ImageProviderEnabled = false
	cfg.ImageAuditMode = "all"
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	input := []any{
		map[string]any{"type": "text", "text": "只检查这段文字"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
	}
	out := postModeration(t, app, input)
	if out.Results[0].Flagged {
		t.Fatalf("expected text-only pass: %+v", out.Results[0])
	}
	if providerCalls != 1 {
		t.Fatalf("providerCalls=%d want 1", providerCalls)
	}
}

func TestEvaluateFailOpenOnProviderError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: "http://127.0.0.1:1", TimeoutMS: 50}
	cfg.MissSampleRate = 1
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	d, evt := app.evaluate(context.Background(), "req", moderationRequest{Model: "m", Input: "普通内容"})
	if d.Flagged || evt.Action != "fail_open" {
		t.Fatalf("decision=%+v event=%+v", d, evt)
	}
}

func TestReplaceConfigPreservesMaskedProviderAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.Provider.APIKey = "real-provider-key"
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	next := safeConfigForUI(app.currentConfig())
	next.MissSampleRate = 0.03
	if err := app.replaceConfig(context.Background(), next, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig: %v", err)
	}
	if got := app.currentConfig().Provider.APIKey; got != "real-provider-key" {
		t.Fatalf("provider api key=%q", got)
	}
}

func TestReplaceConfigPreservesSecretsWhenGivenPlaceholders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"real-adapter-token"}
	cfg.HashSalt = "real-hash-salt"
	cfg.Provider.APIKey = "real-provider-api-key"
	cfg.Provider.SecretID = "real-secret-id"
	cfg.Provider.SecretKey = "real-secret-key"
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	next := app.currentConfig()
	next.AuthTokens = []string{"replace-with-a-long-random-token"}
	next.HashSalt = "replace-with-random-hash-salt"
	next.Provider.APIKey = "replace-with-upstream-api-key"
	next.Provider.SecretID = "replace-with-secret-id"
	next.Provider.SecretKey = "replace-with-secret-key"
	next.MissSampleRate = 0.25
	if err := app.replaceConfig(context.Background(), next, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig: %v", err)
	}

	got := app.currentConfig()
	if got.AuthTokens[0] != "real-adapter-token" || got.HashSalt != "real-hash-salt" {
		t.Fatalf("core secrets were not preserved: %+v", got)
	}
	if got.Provider.APIKey != "real-provider-api-key" || got.Provider.SecretID != "real-secret-id" || got.Provider.SecretKey != "real-secret-key" {
		t.Fatalf("provider secrets were not preserved: %+v", got.Provider)
	}
	if got.MissSampleRate != 0.25 {
		t.Fatalf("non-secret config was not updated: %v", got.MissSampleRate)
	}
}

func TestAdminResetPreservesConfiguredSecrets(t *testing.T) {
	app := testApp(t)
	router := app.Routes()

	cfg := app.currentConfig()
	cfg.Provider = ProviderConfig{
		Type:      "chat_json",
		Endpoint:  "https://example.com/compatible-mode/v1",
		APIKey:    "real-provider-api-key",
		Model:     "qwen-flash-test",
		TimeoutMS: 1000,
	}
	if err := app.replaceConfig(context.Background(), cfg, "test", "127.0.0.1"); err != nil {
		t.Fatalf("replaceConfig: %v", err)
	}

	rec := adminRequest(t, router, http.MethodPost, "/admin/api/config/reset", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := app.currentConfig()
	if got.AuthTokens[0] != "test-token" || got.HashSalt != "test-salt" {
		t.Fatalf("reset should preserve core secrets: %+v", got)
	}
	if got.Provider.APIKey != "real-provider-api-key" {
		t.Fatalf("reset should preserve provider api key: %+v", got.Provider)
	}
}

func TestReadyzFailsWhenChatProviderKeyMissing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "test-salt"
	cfg.Provider = ProviderConfig{
		Type:      "chat_json",
		Endpoint:  "https://example.com/compatible-mode/v1",
		Model:     "qwen-flash-test",
		TimeoutMS: 1000,
	}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status=%d want %d body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "API Key") {
		t.Fatalf("readyz should explain missing API Key: %s", rec.Body.String())
	}
}

func postModeration(t *testing.T, app *App, input any) moderationResponse {
	t.Helper()
	return postModerationWithToken(t, app, "test-token", input)
}

func postModerationWithToken(t *testing.T, app *App, token string, input any) moderationResponse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"model": "llm-audit-adapter-v1", "input": input})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/moderations", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out moderationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
