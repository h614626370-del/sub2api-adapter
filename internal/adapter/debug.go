package adapter

import (
	"encoding/json"
	"strings"
)

func debugUpstreamRequest(p provider, in providerRequest) any {
	switch typed := p.(type) {
	case *chatJSONProvider:
		return typed.debugRequest(in)
	case *httpJSONProvider:
		return typed.debugRequest(in)
	case *tencentTMSProvider:
		return typed.debugRequest(in)
	default:
		return nil
	}
}

func debugProviderResult(result providerResult) map[string]any {
	return map[string]any{
		"action":            result.Action,
		"labels":            result.Labels,
		"raw_summary":       result.RawSummary,
		"latency_ms":        result.LatencyMS,
		"prompt_tokens":     result.PromptTokens,
		"completion_tokens": result.CompletionTokens,
		"cached_tokens":     result.CachedTokens,
	}
}

func (p *chatJSONProvider) debugRequest(in providerRequest) map[string]any {
	return map[string]any{
		"method":  "POST",
		"url":     chatCompletionsURL(p.cfg.Endpoint),
		"headers": safeDebugHeaders(p.cfg),
		"body":    redactDebugValue(p.chatRequest(in)),
	}
}

func (p *httpJSONProvider) debugRequest(in providerRequest) map[string]any {
	return map[string]any{
		"method":  "POST",
		"url":     p.cfg.Endpoint,
		"headers": safeDebugHeaders(p.cfg),
		"body":    redactDebugValue(in),
	}
}

func (p *tencentTMSProvider) debugRequest(in providerRequest) map[string]any {
	region := p.cfg.Region
	if region == "" {
		region = "ap-guangzhou"
	}
	body := map[string]any{
		"content_base64":   "已隐藏，实际发送的是待审核文本的 base64",
		"content_chars":    len([]rune(in.Text)),
		"data_id":          trimRunes(in.RequestID, 64),
		"source_language":  "zh",
		"type":             "TEXT",
		"biz_type":         p.cfg.BizType,
		"audit_text":       in.AuditText,
		"audit_image":      in.AuditImage,
		"normalized_input": in.NormalizedInput,
	}
	return map[string]any{
		"sdk":      "TencentCloud TextModeration",
		"action":   "TextModeration",
		"region":   region,
		"endpoint": strings.TrimSpace(p.cfg.Endpoint),
		"body":     redactDebugValue(body),
	}
}

func safeDebugHeaders(cfg ProviderConfig) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if strings.TrimSpace(cfg.APIKey) != "" {
		headers["Authorization"] = "Bearer ***已隐藏***"
	}
	for key := range cfg.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		headers[key] = "***已隐藏***"
	}
	return headers
}

func moderationRequestDebug(req moderationRequest) map[string]any {
	return map[string]any{
		"method": "POST",
		"path":   "/v1/moderations",
		"body": map[string]any{
			"model": req.Model,
			"input": redactDebugValue(req.Input),
		},
	}
}

func normalizedInputDebug(input extractedInput) map[string]any {
	return map[string]any{
		"text":        input.Text,
		"images":      redactImagesForEvent(input.Images),
		"image_count": len(input.Images),
	}
}

func redactDebugValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return safeSummary(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\n", " "), "\r", " ")), 240)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return safeSummary(string(raw), 240)
	}
	return redactDecodedDebugValue(decoded)
}

func redactDecodedDebugValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			out[key] = redactDecodedDebugValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, redactDecodedDebugValue(item))
		}
		return out
	case string:
		if strings.HasPrefix(x, "data:") {
			return redactImage(x)
		}
		return trimRunes(x, 4000)
	default:
		return v
	}
}
