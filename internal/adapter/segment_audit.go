package adapter

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var inlineTranscriptBoundary = regexp.MustCompile(`(?i)\s+(\[\d+\]\s+(?:user|assistant|tool|system|developer):)`)
var transcriptUserMarker = regexp.MustCompile(`(?i)\[\d+\]\s+user:`)

type auditSegment struct {
	Index int
	Text  string
}

type segmentAuditItem struct {
	Index    int     `json:"index"`
	Chars    int     `json:"chars"`
	CacheHit bool    `json:"cache_hit"`
	Score    float64 `json:"score,omitempty"`
	Failed   bool    `json:"failed"`
}

type segmentedAuditOutcome struct {
	Result          providerResult
	Complete        bool
	Failures        []providerFailure
	Items           []segmentAuditItem
	ProviderCalls   int
	CacheHits       int
	ContextReviewed bool
	LatencyMS       int64
}

func shouldUseSegmentAudit(cfg Config, input extractedInput, auditText bool, auditImage bool) bool {
	return cfg.SegmentAudit.Enabled && auditText && !auditImage && len([]rune(input.StructuredText)) > cfg.SegmentAudit.ThresholdChars
}

func splitAuditSegments(text string, cfg SegmentAuditConfig) []auditSegment {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	units := structuredUnits(text, cfg.TargetChars)
	chunks := packSegmentUnits(units, cfg.TargetChars)
	if len(chunks) > cfg.MaxSegments {
		head := append([]string(nil), chunks[:cfg.MaxSegments-1]...)
		head = append(head, strings.Join(chunks[cfg.MaxSegments-1:], "\n\n"))
		chunks = head
	}
	segments := make([]auditSegment, 0, len(chunks))
	for i, chunk := range chunks {
		segmentText := strings.TrimSpace(chunk)
		if i > 0 && cfg.OverlapChars > 0 {
			overlap := tailRunes(chunks[i-1], cfg.OverlapChars)
			if overlap != "" {
				segmentText = "[上段末尾重叠]\n" + overlap + "\n[本段]\n" + segmentText
			}
		}
		segments = append(segments, auditSegment{Index: i + 1, Text: segmentText})
	}
	return segments
}

func structuredUnits(text string, target int) []string {
	paragraphs := splitParagraphs(text)
	var units []string
	for _, paragraph := range paragraphs {
		for _, chunk := range splitLongUnit(paragraph, target) {
			if strings.TrimSpace(chunk) != "" {
				units = append(units, strings.TrimSpace(chunk))
			}
		}
	}
	return units
}

func splitParagraphs(text string) []string {
	text = inlineTranscriptBoundary.ReplaceAllString(text, "\n\n$1")
	lines := strings.Split(text, "\n")
	var out []string
	var current []string
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.Join(current, "\n"))
		current = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if len(current) > 0 && looksLikeMessageBoundary(trimmed) {
			flush()
		}
		current = append(current, trimmed)
	}
	flush()
	return out
}

func looksLikeMessageBoundary(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range []string{"user:", "user：", "assistant:", "assistant：", "tool:", "tool：", "system:", "system：", "developer:", "developer：", "用户:", "用户：", "助手:", "助手：", "工具:", "工具：", "<user", "<assistant", "<tool", "<system", "<developer", "[user", "[assistant", "[tool", "[system"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func splitLongUnit(text string, target int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= target {
		return []string{string(runes)}
	}
	var out []string
	for len(runes) > target {
		cut := naturalBreak(runes, target)
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if tail := strings.TrimSpace(string(runes)); tail != "" {
		out = append(out, tail)
	}
	return out
}

func naturalBreak(runes []rune, target int) int {
	if len(runes) <= target {
		return len(runes)
	}
	min := target * 2 / 3
	for i := target; i >= min; i-- {
		switch runes[i-1] {
		case '\n', '。', '！', '？', '.', '!', '?', ';', '；':
			return i
		}
	}
	for i := target; i >= min; i-- {
		if unicode.IsSpace(runes[i-1]) {
			return i
		}
	}
	return target
}

func packSegmentUnits(units []string, target int) []string {
	var chunks []string
	var current string
	for _, unit := range units {
		if current == "" {
			current = unit
			continue
		}
		if len([]rune(current))+2+len([]rune(unit)) <= target {
			current += "\n\n" + unit
			continue
		}
		chunks = append(chunks, current)
		current = unit
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

func tailRunes(text string, count int) string {
	runes := []rune(strings.TrimSpace(text))
	if count <= 0 || len(runes) == 0 {
		return ""
	}
	if len(runes) <= count {
		return string(runes)
	}
	return string(runes[len(runes)-count:])
}

func segmentCacheHash(cfg Config, text string) string {
	return stableTextHash(cfg.HashSalt + "\nsegment-policy:" + policyFingerprint(cfg) + "\n" + normalizeText(text))
}

func decisionAsProviderResult(d decision) providerResult {
	action := "pass"
	if d.Action == "block" {
		action = "block"
	}
	return providerResult{Action: action, Score: d.HighestScore, RawSummary: d.RawSummary}
}

func (a *App) cachedSegment(ctx context.Context, cfg Config, segment auditSegment) (providerResult, bool) {
	if !cfg.DecisionCache.Enabled {
		return providerResult{}, false
	}
	key := segmentCacheHash(cfg, segment.Text)
	if cached, ok, err := a.store.GetDecision(ctx, key); err == nil && ok {
		return decisionAsProviderResult(cached), true
	} else if err != nil {
		a.metrics.Inc("moderation_segment_cache_errors_total", nil)
	}
	if cached, ok := a.cache.Get(key); ok {
		return decisionAsProviderResult(cached), true
	}
	return providerResult{}, false
}

func (a *App) saveSegmentDecision(ctx context.Context, cfg Config, providerName string, segment auditSegment, result providerResult) {
	if !cfg.DecisionCache.Enabled {
		return
	}
	d := decisionFromProvider(result, cfg, providerName)
	key := segmentCacheHash(cfg, segment.Text)
	ttl := a.ttlFor(d.Action)
	a.cache.Set(key, d, ttl)
	if err := a.store.SaveDecision(ctx, key, d, ttl); err != nil {
		a.metrics.Inc("moderation_segment_cache_errors_total", nil)
	}
}

func (a *App) invokeProvider(ctx context.Context, p provider, providerName string, request providerRequest, stage string, segmentIndex int) (providerResult, *providerFailure, int64) {
	a.metrics.Inc("moderation_provider_calls_total", map[string]string{"provider": providerName})
	start := timeNow()
	result, err := p.Audit(ctx, request)
	latency := time.Since(start).Milliseconds()
	a.metrics.Observe("moderation_provider_latency_ms", map[string]string{"provider": providerName}, float64(latency))
	if err == nil {
		return result, nil, latency
	}
	failure := providerFailureFromError(err, stage, segmentIndex)
	a.metrics.Inc("moderation_provider_errors_total", map[string]string{"provider": providerName})
	a.metrics.Inc("moderation_provider_failure_total", map[string]string{"provider": providerName, "kind": failure.Kind})
	return providerResult{}, &failure, latency
}

func (a *App) auditSegments(ctx context.Context, p provider, providerName string, base providerRequest, input extractedInput, cfg Config) segmentedAuditOutcome {
	started := timeNow()
	segments := splitAuditSegments(input.StructuredText, cfg.SegmentAudit)
	out := segmentedAuditOutcome{Items: make([]segmentAuditItem, len(segments))}
	results := make([]providerResult, len(segments))
	pending := make([]int, 0, len(segments))
	for i, segment := range segments {
		out.Items[i] = segmentAuditItem{Index: segment.Index, Chars: len([]rune(segment.Text))}
		if cached, ok := a.cachedSegment(ctx, cfg, segment); ok {
			results[i] = cached
			out.Items[i].CacheHit = true
			out.Items[i].Score = cached.Score
			out.CacheHits++
			continue
		}
		pending = append(pending, i)
	}

	type callResult struct {
		index   int
		result  providerResult
		failure *providerFailure
	}
	jobs := make(chan int)
	completed := make(chan callResult, len(pending))
	workers := cfg.SegmentAudit.Concurrency
	if workers > len(pending) {
		workers = len(pending)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				req := base
				req.Text = segments[i].Text
				req.NormalizedInput = normalizeText(segments[i].Text)
				req.AuditMode = "segment"
				req.SegmentIndex = segments[i].Index
				req.SegmentCount = len(segments)
				result, failure, _ := a.invokeProvider(ctx, p, providerName, req, "segment", segments[i].Index)
				completed <- callResult{index: i, result: result, failure: failure}
			}
		}()
	}
	go func() {
		for _, i := range pending {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(completed)
	}()
	for item := range completed {
		out.ProviderCalls++
		if item.failure != nil {
			out.Failures = append(out.Failures, *item.failure)
			out.Items[item.index].Failed = true
			continue
		}
		results[item.index] = item.result
		out.Items[item.index].Score = item.result.Score
		a.saveSegmentDecision(ctx, cfg, providerName, segments[item.index], item.result)
	}

	reviewIndices := make(map[int]struct{})
	for i, result := range results {
		if out.Items[i].Failed || result.Score >= cfg.SegmentAudit.ReviewScore || canonicalProviderAction(result.Action) == "block" {
			reviewIndices[i] = struct{}{}
		}
	}
	if len(reviewIndices) > 0 {
		out.ContextReviewed = true
		reviewText := buildReviewContext(segments, reviewIndices, cfg.SegmentAudit.ReviewMaxChars)
		req := base
		req.Text = reviewText
		req.NormalizedInput = normalizeText(reviewText)
		req.AuditMode = "context_review"
		req.SegmentIndex = 0
		req.SegmentCount = len(segments)
		result, failure, _ := a.invokeProvider(ctx, p, providerName, req, "context_review", 0)
		out.ProviderCalls++
		if failure != nil {
			out.Failures = append(out.Failures, *failure)
			out.LatencyMS = time.Since(started).Milliseconds()
			return out
		}
		for _, item := range results {
			result.PromptTokens += item.PromptTokens
			result.CompletionTokens += item.CompletionTokens
			result.CachedTokens += item.CachedTokens
		}
		out.Result = result
		out.Complete = true
		out.LatencyMS = time.Since(started).Milliseconds()
		return out
	}

	out.Result = aggregateSafeSegmentResults(results)
	out.Complete = len(out.Failures) == 0
	out.LatencyMS = time.Since(started).Milliseconds()
	return out
}

func aggregateSafeSegmentResults(results []providerResult) providerResult {
	result := providerResult{Action: "pass", RawSummary: "分段审核完成：未发现需要上下文复核的高风险片段"}
	for _, item := range results {
		result.PromptTokens += item.PromptTokens
		result.CompletionTokens += item.CompletionTokens
		result.CachedTokens += item.CachedTokens
		if item.Score > result.Score {
			result.Score = item.Score
		}
	}
	return result
}

func buildReviewContext(segments []auditSegment, selected map[int]struct{}, maxChars int) string {
	include := make(map[int]struct{})
	for i := range selected {
		for _, candidate := range []int{i - 1, i, i + 1} {
			if candidate >= 0 && candidate < len(segments) {
				include[candidate] = struct{}{}
			}
		}
	}
	if len(segments) > 0 {
		include[len(segments)-1] = struct{}{}
	}
	latestUser := latestUserSegment(segments)
	if latestUser >= 0 {
		include[latestUser] = struct{}{}
	}
	indices := make([]int, 0, len(include))
	for i := range include {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	if len(segments) > 0 {
		latest := len(segments) - 1
		ordered := make([]int, 0, len(indices))
		if latestUser >= 0 {
			ordered = append(ordered, latestUser)
		}
		if latest != latestUser {
			ordered = append(ordered, latest)
		}
		for _, i := range indices {
			if i != latest && i != latestUser {
				ordered = append(ordered, i)
			}
		}
		indices = ordered
	}
	var b strings.Builder
	for _, i := range indices {
		label := "上下文片段"
		if i == latestUser {
			label = "最后识别到的用户消息片段"
		} else if i == len(segments)-1 {
			label = "内容末段"
		}
		part := fmt.Sprintf("[%s %d/%d]\n%s\n\n", label, i+1, len(segments), segments[i].Text)
		remaining := maxChars - len([]rune(b.String()))
		if remaining <= 0 {
			break
		}
		if len([]rune(part)) > remaining {
			part = trimRunes(part, remaining)
		}
		b.WriteString(part)
	}
	return strings.TrimSpace(b.String())
}

func latestUserSegment(segments []auditSegment) int {
	for i := len(segments) - 1; i >= 0; i-- {
		if transcriptUserMarker.MatchString(segments[i].Text) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(segments[i].Text)), "user:") || strings.HasPrefix(strings.TrimSpace(segments[i].Text), "用户:") || strings.HasPrefix(strings.TrimSpace(segments[i].Text), "用户：") {
			return i
		}
	}
	return -1
}
