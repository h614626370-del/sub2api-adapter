package adapter

import "time"

var moderationCategories = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

type moderationRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []moderationResult `json:"results"`
}

type moderationResult struct {
	Flagged        bool               `json:"flagged"`
	Categories     map[string]bool    `json:"categories"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

type extractedInput struct {
	Text           string
	StructuredText string
	Images         []string
}

type providerFailure struct {
	Stage        string `json:"stage"`
	SegmentIndex int    `json:"segment_index,omitempty"`
	Kind         string `json:"kind"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	UpstreamCode string `json:"upstream_code,omitempty"`
	UpstreamID   string `json:"upstream_request_id,omitempty"`
	Retryable    bool   `json:"retryable"`
	Message      string `json:"message"`
}

type keywordHit struct {
	SetName    string `json:"set_name"`
	RiskDomain string `json:"risk_domain"`
	Keyword    string `json:"keyword"`
}

type decision struct {
	Action          string             `json:"action"`
	Flagged         bool               `json:"flagged"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	Provider        string             `json:"provider"`
	RawSummary      string             `json:"raw_summary,omitempty"`
}

type event struct {
	Time               time.Time          `json:"time"`
	RequestID          string             `json:"request_id"`
	InputHash          string             `json:"input_hash"`
	Action             string             `json:"action"`
	KeywordHit         bool               `json:"keyword_hit"`
	KeywordHits        []keywordHit       `json:"keyword_hits,omitempty"`
	Sampled            bool               `json:"sampled"`
	CacheHit           bool               `json:"cache_hit"`
	ExternalAudited    bool               `json:"external_audited"`
	Provider           string             `json:"provider"`
	HighestCategory    string             `json:"highest_category"`
	HighestScore       float64            `json:"highest_score"`
	CategoryScores     map[string]float64 `json:"category_scores,omitempty"`
	LocalLatencyMS     int64              `json:"local_latency_ms"`
	ProviderLatencyMS  int64              `json:"provider_latency_ms"`
	ErrorSummary       string             `json:"error_summary,omitempty"`
	InputExcerpt       string             `json:"input_excerpt,omitempty"`
	BlockedInput       string             `json:"blocked_input,omitempty"`
	ImageCount         int                `json:"image_count"`
	EstimatedCostCNY   float64            `json:"estimated_cost_cny"`
	EstimatedCostUSD   float64            `json:"estimated_cost_usd"`
	ProviderRawSummary string             `json:"provider_raw_summary,omitempty"`
	ProviderFailures   []providerFailure  `json:"provider_failures,omitempty"`
	ProviderCalls      int                `json:"provider_calls"`
	SegmentCount       int                `json:"segment_count"`
	SegmentCacheHits   int                `json:"segment_cache_hits"`
	ContextReviewed    bool               `json:"context_reviewed"`
}

type evaluationTrace struct {
	ProviderRequest  *providerRequest `json:"provider_request,omitempty"`
	UpstreamRequest  any              `json:"upstream_request,omitempty"`
	UpstreamResponse any              `json:"upstream_response,omitempty"`
	CacheNote        string           `json:"cache_note,omitempty"`
	SegmentSummary   any              `json:"segment_summary,omitempty"`
}
