package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var dataURLPrefix = regexp.MustCompile(`(?i)^data:[^,]{1,128},`)

func extractModerationInput(input any, maxTextChars int, maxImages int, allowDataURL bool) extractedInput {
	var parts []string
	var images []string
	collectInput(input, &parts, &images, allowDataURL)
	text := normalizeText(strings.Join(parts, "\n"))
	if maxTextChars > 0 {
		text = trimRunes(text, maxTextChars)
	}
	images = dedupeImages(images, maxImages)
	return extractedInput{Text: text, Images: images}
}

func collectInput(v any, parts *[]string, images *[]string, allowDataURL bool) {
	switch x := v.(type) {
	case nil:
		return
	case string:
		addText(parts, x)
	case []any:
		for _, item := range x {
			collectInput(item, parts, images, allowDataURL)
		}
	case map[string]any:
		typ := strings.ToLower(strings.TrimSpace(asString(x["type"])))
		if text := asString(x["text"]); text != "" && (typ == "" || strings.Contains(typ, "text") || typ == "message") {
			addText(parts, text)
		}
		if content, ok := x["content"]; ok && typ != "image_url" && typ != "input_image" && typ != "image" {
			collectInput(content, parts, images, allowDataURL)
		}
		collectImageField(x, images, allowDataURL)
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(x, &decoded) == nil {
			collectInput(decoded, parts, images, allowDataURL)
		}
	}
}

func collectImageField(m map[string]any, images *[]string, allowDataURL bool) {
	addImage(images, asString(m["url"]), allowDataURL)
	addImage(images, asString(m["image_url"]), allowDataURL)
	if ref, ok := m["image_url"].(map[string]any); ok {
		addImage(images, asString(ref["url"]), allowDataURL)
	}
	if source, ok := m["source"].(map[string]any); ok {
		if data := asString(source["data"]); data != "" {
			mime := asString(source["media_type"])
			if mime == "" {
				mime = asString(source["mediaType"])
			}
			if strings.HasPrefix(data, "data:") {
				addImage(images, data, allowDataURL)
			} else if mime != "" {
				addImage(images, "data:"+mime+";base64,"+data, allowDataURL)
			}
		}
	}
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return ""
	}
}

func addText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "<system-reminder>") {
		return
	}
	*parts = append(*parts, text)
}

func addImage(images *[]string, image string, allowDataURL bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:") && !allowDataURL {
		return
	}
	if strings.HasPrefix(image, "data:") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
}

func dedupeImages(images []string, max int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(images))
	for _, image := range images {
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		out = append(out, image)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func normalizeText(text string) string {
	text = strings.TrimSpace(text)
	var b strings.Builder
	space := false
	for _, r := range text {
		r = foldWidth(r)
		r = unicode.ToLower(r)
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func compactForMatch(text string) string {
	var b strings.Builder
	for _, r := range normalizeText(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || isCJK(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func foldWidth(r rune) rune {
	if r == 0x3000 {
		return ' '
	}
	if r >= 0xFF01 && r <= 0xFF5E {
		return r - 0xFEE0
	}
	return r
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}

func trimRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func riskHash(salt string, input extractedInput) string {
	h := sha256.New()
	_, _ = h.Write([]byte(salt))
	_, _ = h.Write([]byte("\ntext:"))
	_, _ = h.Write([]byte(input.Text))
	for _, image := range input.Images {
		imgHash := sha256.Sum256([]byte(image))
		_, _ = h.Write([]byte("\nimage:"))
		_, _ = h.Write([]byte(hex.EncodeToString(imgHash[:])))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func decisionCacheHash(cfg Config, input extractedInput) string {
	return riskHash(cfg.HashSalt+"\npolicy:"+policyFingerprint(cfg), input)
}

func policyFingerprint(cfg Config) string {
	providerHeadersHash := stableJSONHash(cfg.Provider.Headers)
	imageProviderHeadersHash := stableJSONHash(cfg.ImageProvider.Headers)
	imageProviderCfg := effectiveImageProviderConfig(cfg)
	policy := struct {
		AlgorithmVersion        string         `json:"algorithm_version"`
		ProviderType            string         `json:"provider_type"`
		ProviderEndpoint        string         `json:"provider_endpoint"`
		ProviderModel           string         `json:"provider_model"`
		ProviderDisabled        bool           `json:"provider_disabled"`
		ProviderCredential      string         `json:"provider_credential_hash"`
		ActivePromptID          string         `json:"active_prompt_id"`
		ActivePromptHash        string         `json:"active_prompt_hash"`
		EnableFewShot           bool           `json:"enable_few_shot"`
		WrapUserInput           bool           `json:"wrap_user_input"`
		EnableSearch            bool           `json:"enable_search"`
		EnableThinking          bool           `json:"enable_thinking"`
		ThinkingBudget          int            `json:"thinking_budget"`
		Temperature             float64        `json:"temperature"`
		TopP                    float64        `json:"top_p"`
		MaxTokens               int            `json:"max_tokens"`
		ProviderHeaders         string         `json:"provider_headers_hash"`
		ImageProviderEnabled    bool           `json:"image_provider_enabled"`
		ImageProviderType       string         `json:"image_provider_type"`
		ImageProviderEndpoint   string         `json:"image_provider_endpoint"`
		ImageProviderModel      string         `json:"image_provider_model"`
		ImageHighResolution     bool           `json:"image_high_resolution"`
		ImageProviderCredential string         `json:"image_provider_credential_hash"`
		ImageProviderPromptHash string         `json:"image_provider_prompt_hash"`
		ImageProviderHeaders    string         `json:"image_provider_headers_hash"`
		DirectModelAudit        bool           `json:"direct_model_audit"`
		MissSampleRate          float64        `json:"miss_sample_rate"`
		AuditKeywordHit         bool           `json:"audit_on_keyword_hit"`
		MinTextChars            int            `json:"min_text_chars"`
		MaxTextChars            int            `json:"max_text_chars"`
		ImageAuditMode          string         `json:"image_audit_mode"`
		ImageSampleRate         float64        `json:"image_sample_rate"`
		MaxImages               int            `json:"max_images_per_request"`
		AllowDataURLImage       bool           `json:"allow_data_url_image"`
		ResultScoreCategory     string         `json:"result_score_category"`
		KeywordSets             []KeywordSet   `json:"keyword_sets"`
		LabelMappings           []LabelMapping `json:"provider_label_mapping"`
	}{
		AlgorithmVersion:        "decision-policy-v4",
		ProviderType:            strings.ToLower(strings.TrimSpace(cfg.Provider.Type)),
		ProviderEndpoint:        strings.TrimSpace(cfg.Provider.Endpoint),
		ProviderModel:           strings.TrimSpace(cfg.Provider.Model),
		ProviderDisabled:        cfg.Provider.Disabled,
		ProviderCredential:      stableTextHash(strings.Join([]string{cfg.Provider.APIKey, cfg.Provider.SecretID, cfg.Provider.SecretKey}, "\n")),
		ActivePromptID:          strings.TrimSpace(cfg.Provider.ActivePromptID),
		ActivePromptHash:        stableTextHash(activeSystemPrompt(cfg.Provider)),
		EnableFewShot:           cfg.Provider.EnableFewShot,
		WrapUserInput:           cfg.Provider.WrapUserInput,
		EnableSearch:            cfg.Provider.EnableSearch,
		EnableThinking:          cfg.Provider.EnableThinking,
		ThinkingBudget:          cfg.Provider.ThinkingBudget,
		Temperature:             cfg.Provider.Temperature,
		TopP:                    cfg.Provider.TopP,
		MaxTokens:               cfg.Provider.MaxTokens,
		ProviderHeaders:         providerHeadersHash,
		ImageProviderEnabled:    cfg.ImageProviderEnabled,
		ImageProviderType:       strings.ToLower(strings.TrimSpace(imageProviderCfg.Type)),
		ImageProviderEndpoint:   strings.TrimSpace(imageProviderCfg.Endpoint),
		ImageProviderModel:      strings.TrimSpace(imageProviderCfg.Model),
		ImageHighResolution:     imageProviderCfg.HighResolution,
		ImageProviderCredential: stableTextHash(strings.Join([]string{imageProviderCfg.APIKey, imageProviderCfg.SecretID, imageProviderCfg.SecretKey}, "\n")),
		ImageProviderPromptHash: stableTextHash(activeSystemPrompt(imageProviderCfg)),
		ImageProviderHeaders:    imageProviderHeadersHash,
		DirectModelAudit:        cfg.DirectModelAudit,
		MissSampleRate:          cfg.MissSampleRate,
		AuditKeywordHit:         cfg.AuditOnKeywordHit,
		MinTextChars:            cfg.MinTextChars,
		MaxTextChars:            cfg.MaxTextChars,
		ImageAuditMode:          cfg.ImageAuditMode,
		ImageSampleRate:         cfg.ImageSampleRate,
		MaxImages:               cfg.MaxImages,
		AllowDataURLImage:       cfg.AllowDataURLImage,
		ResultScoreCategory:     cfg.ResultScoreCategory,
		KeywordSets:             cfg.KeywordSets,
		LabelMappings:           cfg.LabelMappings,
	}
	return stableJSONHash(policy)
}

func stableTextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func stableJSONHash(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return stableTextHash(fmt.Sprintf("%v", v))
	}
	return stableTextHash(string(raw))
}

func redactImage(image string) string {
	if strings.HasPrefix(image, "data:") {
		return dataURLPrefix.ReplaceAllString(image, "data:...;base64,")[:min(len(dataURLPrefix.ReplaceAllString(image, "data:...;base64,")), 64)] + "..."
	}
	if parsed, err := url.Parse(image); err == nil {
		parsed.RawQuery = ""
		return parsed.String()
	}
	return trimRunes(image, 120)
}

func inputExcerpt(text string, enabled bool) string {
	if !enabled {
		return ""
	}
	return trimRunes(redactSecrets(text), 240)
}

func redactSecrets(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-]{12,}`),
		regexp.MustCompile(`(?i)(sk-[a-z0-9_\-]{8})[a-z0-9_\-]{8,}`),
		regexp.MustCompile(`\b1[3-9]\d{9}\b`),
		regexp.MustCompile(`\b\d{15,18}[0-9xX]\b`),
	}
	out := text
	for _, re := range patterns {
		out = re.ReplaceAllString(out, "$1***")
	}
	return out
}

func newRequestID() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", nowUnixNano())))
	return "modr-adapter-" + hex.EncodeToString(sum[:])[:16]
}

func nowUnixNano() int64 {
	return timeNow().UnixNano()
}
