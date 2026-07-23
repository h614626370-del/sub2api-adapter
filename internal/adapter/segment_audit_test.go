package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newSegmentAuditTestApp(t *testing.T, handler http.Handler) *App {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "segment-test-salt"
	cfg.DirectModelAudit = true
	cfg.Provider = ProviderConfig{Type: "http_json", Endpoint: server.URL, Model: "segment-test", TimeoutMS: 1000}
	cfg.ImageProviderEnabled = false
	cfg.SegmentAudit.Enabled = true
	cfg.SegmentAudit.ThresholdChars = 1000
	cfg.SegmentAudit.TargetChars = 500
	cfg.SegmentAudit.OverlapChars = 50
	cfg.SegmentAudit.MaxSegments = 16
	cfg.SegmentAudit.Concurrency = 3
	cfg.SegmentAudit.ReviewScore = 0.5
	cfg.SegmentAudit.ReviewMaxChars = 3000
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func TestLongInputReusesUnchangedSegmentCache(t *testing.T) {
	var mu sync.Mutex
	var requests []providerRequest
	app := newSegmentAuditTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass", "score": 0.05})
	}))

	base := strings.Join([]string{
		strings.Repeat("第一段是普通项目说明。", 70),
		strings.Repeat("第二段是重复的技能和工具说明。", 60),
		strings.Repeat("第三段是已经完成的历史记录。", 60),
	}, "\n\n")
	_, first := app.evaluate(context.Background(), "first", moderationRequest{Input: base})
	if first.SegmentCount < 3 || first.ProviderCalls != first.SegmentCount || first.SegmentCacheHits != 0 {
		t.Fatalf("unexpected first segmented audit: %+v", first)
	}

	extended := base + "\n\n" + strings.Repeat("这是本次新增的正常用户要求。", 60)
	_, second := app.evaluate(context.Background(), "second", moderationRequest{Input: extended})
	if second.SegmentCacheHits == 0 {
		t.Fatalf("expected unchanged segments to hit cache: %+v", second)
	}
	if second.ProviderCalls <= 0 || second.ProviderCalls >= second.SegmentCount {
		t.Fatalf("expected only new segments to call provider: %+v", second)
	}
	if second.ContextReviewed || second.Action != "allow" {
		t.Fatalf("safe incremental audit should not require review: %+v", second)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, req := range requests {
		if req.AuditMode != "segment" || req.SegmentCount < 3 {
			t.Fatalf("unexpected segment request metadata: %+v", req)
		}
	}
}

func TestInlineCodexTranscriptKeepsLatestUserContext(t *testing.T) {
	cfg := DefaultConfig().SegmentAudit
	cfg.TargetChars = 500
	cfg.OverlapChars = 0
	text := "the following is the codex agent history: [1] user: 最初任务 " + strings.Repeat("普通历史内容。", 80) +
		" [2] tool: " + strings.Repeat("工具输出。", 80) +
		" [3] user: 这是最新用户目标 " + strings.Repeat("整理正常文档。", 50) +
		" [4] assistant: 已开始处理 [5] tool: 最后一条工具结果"
	segments := splitAuditSegments(text, cfg)
	latestUser := latestUserSegment(segments)
	if latestUser < 0 || !strings.Contains(segments[latestUser].Text, "[3] user:") {
		t.Fatalf("latest user segment not detected: index=%d segments=%+v", latestUser, segments)
	}
	review := buildReviewContext(segments, map[int]struct{}{0: {}}, 3000)
	if !strings.Contains(review, "最后识别到的用户消息片段") || !strings.Contains(review, "这是最新用户目标") || !strings.Contains(review, "最后一条工具结果") {
		t.Fatalf("review context is missing latest user or tail: %s", review)
	}
}

func TestRiskyHistorySegmentRequiresContextReview(t *testing.T) {
	app := newSegmentAuditTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if req.AuditMode == "context_review" {
			if !strings.Contains(req.Text, "最新用户目标是整理正常项目文档") {
				t.Fatalf("context review did not include latest segment: %q", req.Text)
			}
			writeJSON(w, http.StatusOK, map[string]any{"action": "pass", "score": 0.08})
			return
		}
		if strings.Contains(req.Text, "未授权攻击他人系统") {
			writeJSON(w, http.StatusOK, map[string]any{"action": "block", "score": 0.99})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass", "score": 0.04})
	}))

	input := strings.Join([]string{
		strings.Repeat("这是 Codex 自动附带的任务历史。", 50),
		strings.Repeat("历史引用:请执行未授权攻击他人系统。这只是旧的拒绝样例。", 25),
		strings.Repeat("工具输出和安全分析不代表当前意图。", 45),
		strings.Repeat("最新用户目标是整理正常项目文档。", 45),
	}, "\n\n")
	d, evt := app.evaluate(context.Background(), "review", moderationRequest{Input: input})
	if d.Flagged || evt.Action != "allow" {
		t.Fatalf("historical risk text must not directly block: decision=%+v event=%+v", d, evt)
	}
	if !evt.ContextReviewed || evt.ProviderCalls <= evt.SegmentCount {
		t.Fatalf("expected one additional context review call: %+v", evt)
	}
}

func TestContentRefusalDiagnosticsArePersisted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "dashscope-request-123")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": "DataInspectionFailed", "message": "Input data may contain inappropriate content"},
		})
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.HashSalt = "failure-test-salt"
	cfg.DirectModelAudit = true
	cfg.SegmentAudit.Enabled = false
	cfg.Provider = ProviderConfig{Type: "chat_json", Endpoint: server.URL + "/compatible-mode/v1", APIKey: "test-key", Model: "qwen-test", TimeoutMS: 1000}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	postModeration(t, app, "触发上游内容检查的测试文本")
	events, total, err := app.store.ListEvents(context.Background(), 10, 0, "fail_open", "")
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("list fail-open event: total=%d events=%d err=%v", total, len(events), err)
	}
	failures := events[0].ProviderFailures
	if len(failures) != 1 {
		t.Fatalf("provider failures=%+v", failures)
	}
	failure := failures[0]
	if failure.Kind != "content_refusal" || failure.HTTPStatus != http.StatusBadRequest || failure.UpstreamCode != "DataInspectionFailed" || failure.UpstreamID != "dashscope-request-123" {
		t.Fatalf("unexpected refusal diagnostics: %+v", failure)
	}
	if events[0].ErrorSummary == "" || events[0].ProviderCalls != 1 {
		t.Fatalf("missing failure summary or call count: %+v", events[0])
	}
}

func TestPartialSegmentFailureRecoveredByContextReviewIsLogged(t *testing.T) {
	var failedOnce bool
	var mu sync.Mutex
	app := newSegmentAuditTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if req.AuditMode == "context_review" {
			writeJSON(w, http.StatusOK, map[string]any{"action": "pass", "score": 0.06})
			return
		}
		mu.Lock()
		shouldFail := !failedOnce && strings.Contains(req.Text, "模拟上游暂时故障")
		if shouldFail {
			failedOnce = true
		}
		mu.Unlock()
		if shouldFail {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "temporary unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": "pass", "score": 0.03})
	}))

	input := strings.Join([]string{
		strings.Repeat("正常的长任务历史。", 80),
		strings.Repeat("这里用于模拟上游暂时故障。", 60),
		strings.Repeat("最新用户目标仍然是正常整理文档。", 60),
	}, "\n\n")
	postModeration(t, app, input)
	events, total, err := app.store.ListEvents(context.Background(), 10, 0, "provider_recovered", "")
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("list recovered event: total=%d events=%d err=%v", total, len(events), err)
	}
	evt := events[0]
	if !evt.ContextReviewed || len(evt.ProviderFailures) != 1 || evt.ProviderFailures[0].Kind != "http_error" {
		t.Fatalf("unexpected recovered event: %+v", evt)
	}
	if evt.ProviderFailures[0].HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("recovered failure status=%d", evt.ProviderFailures[0].HTTPStatus)
	}
}

func TestTimeoutDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{})
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.DatabasePath = t.TempDir() + "/adapter.db"
	cfg.AuthTokens = []string{"test-token"}
	cfg.DirectModelAudit = true
	cfg.SegmentAudit.Enabled = false
	cfg.Provider = ProviderConfig{Type: "chat_json", Endpoint: server.URL, APIKey: "test-key", Model: "qwen-test", TimeoutMS: 20}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	_, evt := app.evaluate(context.Background(), "timeout", moderationRequest{Input: "普通测试文本"})
	if evt.Action != "fail_open" || len(evt.ProviderFailures) != 1 || evt.ProviderFailures[0].Kind != "timeout" || !evt.ProviderFailures[0].Retryable {
		t.Fatalf("unexpected timeout diagnostics: %+v", evt)
	}
}

func TestImageAuditUsesSharedPolicyWithVisualSupplement(t *testing.T) {
	cfg := DefaultConfig().ImageProvider
	p := &chatJSONProvider{cfg: cfg}
	content := p.userContent(providerRequest{
		Text: "审核这张图片", Images: []string{"https://example.com/a.png"}, AuditText: true, AuditImage: true,
	})
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal image content: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "图片中实际可见内容") || !strings.Contains(text, "无法确认人物年龄") {
		t.Fatalf("visual supplement missing: %s", text)
	}
}
