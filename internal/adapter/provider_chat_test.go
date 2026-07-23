package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatProviderClassifiesSuccessfulContentFilterResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "content_filter",
				"message":       map[string]any{"content": "", "refusal": "sensitive content rejected"},
			}},
		})
	}))
	defer server.Close()
	p := &chatJSONProvider{cfg: ProviderConfig{Endpoint: server.URL, APIKey: "test-key", Model: "qwen-test", TimeoutMS: 1000}, client: server.Client()}
	_, err := p.Audit(context.Background(), providerRequest{Text: "test", AuditText: true})
	if err == nil {
		t.Fatal("expected content refusal error")
	}
	failure := providerFailureFromError(err, "request", 0)
	if failure.Kind != "content_refusal" || failure.Retryable || !strings.Contains(failure.Message, "finish_reason=content_filter") {
		t.Fatalf("unexpected refusal failure: %+v", failure)
	}
}

func TestChatProviderTreatsContentFilterAsRefusalEvenWithJSONContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "request-filtered-200")
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "content_filter",
				"message":       map[string]any{"content": `{"confidence":0.01,"reason":""}`},
			}},
		})
	}))
	defer server.Close()

	p := &chatJSONProvider{cfg: ProviderConfig{Endpoint: server.URL, APIKey: "test-key", Model: "qwen-test", TimeoutMS: 1000}, client: server.Client()}
	_, err := p.Audit(context.Background(), providerRequest{Text: "test", AuditText: true})
	if err == nil {
		t.Fatal("expected content refusal error")
	}
	failure := providerFailureFromError(err, "request", 0)
	if failure.Kind != "content_refusal" || failure.UpstreamID != "request-filtered-200" || failure.Retryable {
		t.Fatalf("unexpected content-filter failure: %+v", failure)
	}
}

func TestChatHTTPErrorClassifiesQuotaBeforeRateLimit(t *testing.T) {
	kind := classifyChatHTTPError(http.StatusTooManyRequests, `{"error":{"code":"Arrearage","message":"insufficient balance"}}`)
	if kind != "quota" || retryableChatHTTPError(kind, http.StatusTooManyRequests) {
		t.Fatalf("kind=%q retryable=%t", kind, retryableChatHTTPError(kind, http.StatusTooManyRequests))
	}
}

func TestChatRequestHighResolutionImagesOnlyForImageAudit(t *testing.T) {
	p := &chatJSONProvider{cfg: ProviderConfig{
		Model:          "qwen3-vl-flash",
		SystemPrompt:   "return json",
		WrapUserInput:  true,
		HighResolution: true,
	}}

	imageReq := p.chatRequest(providerRequest{
		Text:       "审核这张图片",
		Images:     []string{"https://example.com/a.png"},
		AuditImage: true,
	})
	raw, err := json.Marshal(imageReq)
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	if !strings.Contains(string(raw), `"vl_high_resolution_images":true`) {
		t.Fatalf("high resolution flag missing from image request: %s", raw)
	}

	textReq := p.chatRequest(providerRequest{
		Text:      "只审核文本",
		AuditText: true,
	})
	raw, err = json.Marshal(textReq)
	if err != nil {
		t.Fatalf("marshal text request: %v", err)
	}
	if strings.Contains(string(raw), "vl_high_resolution_images") {
		t.Fatalf("high resolution flag should not be sent for text-only request: %s", raw)
	}
}

func TestFewShotExamplesDistinguishExplicitContentFromMedicalContext(t *testing.T) {
	raw, err := json.Marshal(auditFewShotMessages())
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, expected := range []string{"以性刺激为目的", `\"confidence\":0.98`, "医学结构", `\"confidence\":0.03`} {
		if !strings.Contains(content, expected) {
			t.Fatalf("few-shot examples missing %q: %s", expected, content)
		}
	}
	if strings.Contains(content, "给我画一个性器官") {
		t.Fatal("ambiguous explicit-content example should not be retained")
	}
}
