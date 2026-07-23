package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type provider interface {
	Name() string
	Audit(ctx context.Context, in providerRequest) (providerResult, error)
}

type providerRequest struct {
	RequestID       string       `json:"request_id"`
	Text            string       `json:"text"`
	Images          []string     `json:"images,omitempty"`
	KeywordHits     []keywordHit `json:"keyword_hits,omitempty"`
	AuditText       bool         `json:"audit_text"`
	AuditImage      bool         `json:"audit_image"`
	NormalizedInput string       `json:"normalized_input,omitempty"`
	AuditMode       string       `json:"audit_mode,omitempty"`
	SegmentIndex    int          `json:"segment_index,omitempty"`
	SegmentCount    int          `json:"segment_count,omitempty"`
}

type upstreamCallError struct {
	Kind         string
	HTTPStatus   int
	UpstreamCode string
	UpstreamID   string
	Retryable    bool
	Message      string
	Cause        error
}

func (e *upstreamCallError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *upstreamCallError) Unwrap() error { return e.Cause }

func providerFailureFromError(err error, stage string, segmentIndex int) providerFailure {
	failure := providerFailure{Stage: stage, SegmentIndex: segmentIndex, Kind: "internal", Message: safeSummary(err.Error(), 1200)}
	var upstream *upstreamCallError
	if errors.As(err, &upstream) {
		failure.Kind = upstream.Kind
		failure.HTTPStatus = upstream.HTTPStatus
		failure.UpstreamCode = upstream.UpstreamCode
		failure.UpstreamID = upstream.UpstreamID
		failure.Retryable = upstream.Retryable
		failure.Message = safeSummary(upstream.Message, 1200)
		return failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Kind = "timeout"
		failure.Retryable = true
		return failure
	}
	if errors.Is(err, context.Canceled) {
		failure.Kind = "canceled"
		return failure
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		failure.Kind = "network"
		failure.Retryable = netErr.Timeout() || netErr.Temporary()
	}
	return failure
}

type providerResult struct {
	Action           string          `json:"action"`
	Score            float64         `json:"score,omitempty"`
	Labels           []providerLabel `json:"labels,omitempty"`
	RawSummary       string          `json:"raw_summary,omitempty"`
	LatencyMS        int64           `json:"latency_ms"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	CachedTokens     int             `json:"cached_tokens,omitempty"`
}

type providerLabel struct {
	Label    string  `json:"label"`
	Category string  `json:"category,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

func newProvider(cfg ProviderConfig) (provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "", "mock":
		return mockProvider{}, nil
	case "chat_json", "qwen", "openai_compatible":
		return &chatJSONProvider{cfg: cfg, client: &http.Client{}}, nil
	case "http_json":
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return nil, errors.New("HTTP JSON 上游地址未配置，请在后台填写 Endpoint")
		}
		return &httpJSONProvider{cfg: cfg, client: &http.Client{}}, nil
	case "tencent_tms":
		return newTencentTMSProvider(cfg)
	default:
		return nil, fmt.Errorf("不支持的上游类型 %q", cfg.Type)
	}
}

type mockProvider struct{}

func (mockProvider) Name() string { return "mock" }

func (mockProvider) Audit(ctx context.Context, in providerRequest) (providerResult, error) {
	_ = ctx
	start := timeNow()
	text := compactForMatch(in.Text)
	result := providerResult{Action: "pass", RawSummary: "模拟上游：放行"}
	switch {
	case strings.Contains(text, "钓鱼网站") || strings.Contains(text, "盗号") || strings.Contains(text, "撞库") || strings.Contains(text, "绕过安全检测"):
		result.Action = "block"
		result.Score = 1
		result.Labels = []providerLabel{{Label: "黑产/诈骗", Category: "illicit", Score: 1}}
		result.RawSummary = "模拟上游：违法/违规"
	case strings.Contains(text, "未成年") && (strings.Contains(text, "裸照") || strings.Contains(text, "色情")):
		result.Action = "block"
		result.Score = 1
		result.Labels = []providerLabel{{Label: "未成年色情", Category: "sexual/minors", Score: 1}}
		result.RawSummary = "模拟上游：未成年人性内容"
	case strings.Contains(text, "裸照") || strings.Contains(text, "色情"):
		result.Action = "block"
		result.Score = 1
		result.Labels = []providerLabel{{Label: "色情", Category: "sexual", Score: 1}}
		result.RawSummary = "模拟上游：色情/性内容"
	case strings.Contains(text, "自杀") || strings.Contains(text, "自残"):
		result.Action = "pass"
		result.RawSummary = "模拟上游：放行"
	default:
		result.Action = "pass"
		result.RawSummary = "模拟上游：放行"
	}
	result.LatencyMS = time.Since(start).Milliseconds()
	return result, nil
}

type httpJSONProvider struct {
	cfg    ProviderConfig
	client *http.Client
}

func (p *httpJSONProvider) Name() string { return "http_json" }

func (p *httpJSONProvider) Audit(ctx context.Context, in providerRequest) (providerResult, error) {
	timeout := time.Duration(p.cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(in)
	if err != nil {
		return providerResult{}, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return providerResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	for key, value := range p.cfg.Headers {
		req.Header.Set(key, value)
	}

	start := timeNow()
	resp, err := p.client.Do(req)
	if err != nil {
		return providerResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	latency := time.Since(start).Milliseconds()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerResult{}, &upstreamCallError{
			Kind: "http_error", HTTPStatus: resp.StatusCode, Retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			Message: fmt.Sprintf("HTTP JSON 上游返回状态码 %d：%s", resp.StatusCode, safeSummary(string(raw), 1200)),
		}
	}
	out := parseProviderResponse(raw)
	out.LatencyMS = latency
	if out.Action == "" {
		out.Action = "pass"
	}
	return out, nil
}

func parseProviderResponse(raw []byte) providerResult {
	var generic map[string]any
	if json.Unmarshal(raw, &generic) != nil {
		return providerResult{Action: "pass", RawSummary: safeSummary(string(raw), 240)}
	}
	out := providerResult{
		Action:     canonicalProviderAction(firstString(generic, "action", "suggestion", "result", "conclusion", "decision")),
		Score:      clamp01(firstFloat(generic, "score", "confidence", "risk_score", "final_score")),
		RawSummary: safeSummary(string(raw), 480),
	}
	if labels, ok := generic["labels"].([]any); ok {
		out.Labels = append(out.Labels, parseLabels(labels)...)
	}
	if labels, ok := generic["categories"].([]any); ok {
		out.Labels = append(out.Labels, parseLabels(labels)...)
	}
	if label := firstString(generic, "label", "category", "risk_label"); label != "" {
		out.Labels = append(out.Labels, providerLabel{Label: label, Category: mapProviderLabel(label), Score: 1})
	}
	if out.Action == "" && len(out.Labels) > 0 {
		out.Action = "block"
	}
	if out.Score == 0 {
		for _, label := range out.Labels {
			if label.Score > out.Score {
				out.Score = clamp01(label.Score)
			}
		}
	}
	return out
}

func safeSummary(text string, maxRunes int) string {
	text = redactSecrets(text)
	if strings.Contains(text, "data:") {
		text = dataURLPrefix.ReplaceAllString(text, "data:...;base64,")
	}
	return trimRunes(text, maxRunes)
}

func parseLabels(items []any) []providerLabel {
	out := make([]providerLabel, 0, len(items))
	for _, item := range items {
		switch x := item.(type) {
		case string:
			out = append(out, providerLabel{Label: x, Category: mapProviderLabel(x), Score: 1})
		case map[string]any:
			label := firstString(x, "label", "name", "category", "risk_label")
			category := firstString(x, "target_category", "openai_category", "mapped_category")
			if category == "" {
				category = mapProviderLabel(label)
			}
			out = append(out, providerLabel{Label: label, Category: category, Score: firstFloat(x, "score", "confidence")})
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstFloat(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return 0
}

func canonicalProviderAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "pass", "allow", "normal", "safe", "通过":
		return "pass"
	case "block", "reject", "deny", "unsafe", "违规", "拦截":
		return "block"
	default:
		return ""
	}
}

func mapProviderLabel(label string) string {
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(label, "未成年") && (strings.Contains(label, "色情") || strings.Contains(label, "性")):
		return "sexual/minors"
	case strings.Contains(label, "色情") || strings.Contains(label, "涉黄") || strings.Contains(lower, "porn") || strings.Contains(lower, "sexual"):
		return "sexual"
	case strings.Contains(label, "辱骂") || strings.Contains(label, "攻击") || strings.Contains(label, "骚扰") || strings.Contains(lower, "harassment"):
		return "harassment"
	case strings.Contains(label, "威胁"):
		return "harassment/threatening"
	case strings.Contains(label, "仇恨") || strings.Contains(label, "歧视") || strings.Contains(lower, "hate"):
		return "hate"
	case strings.Contains(label, "自杀") || strings.Contains(label, "自残") || strings.Contains(label, "自伤"):
		return "self-harm/intent"
	case strings.Contains(label, "暴恐") || strings.Contains(label, "血腥"):
		return "violence/graphic"
	case strings.Contains(label, "暴力") || strings.Contains(label, "武器") || strings.Contains(label, "枪") || strings.Contains(label, "爆炸"):
		return "violence"
	case strings.Contains(label, "违法") || strings.Contains(label, "违禁") || strings.Contains(label, "诈骗") || strings.Contains(label, "黑产") || strings.Contains(label, "涉政") || strings.Contains(label, "敏感"):
		return "illicit"
	default:
		return "illicit"
	}
}
