package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

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
