package adapter

import "strings"

func allowDecision(provider string) decision {
	return decision{
		Action:         "allow",
		Flagged:        false,
		CategoryScores: zeroScores(),
		Provider:       provider,
	}
}

func decisionFromProvider(result providerResult, cfg Config, providerName string) decision {
	action := canonicalProviderAction(result.Action)
	if action == "" {
		action = "pass"
	}
	scores := zeroScores()
	resultCategory := normalizeCategory(cfg.ResultScoreCategory)
	if resultCategory == "" {
		resultCategory = "illicit"
	}
	score := clamp01(result.Score)
	if score == 0 && action == "block" {
		score = 1
	}
	scores[resultCategory] = score
	flagged := score >= sub2APIBlockThreshold(resultCategory)
	if flagged {
		action = "block"
	} else {
		action = "allow"
	}
	highestCategory, highestScore := highestScore(scores)
	return decision{
		Action:          action,
		Flagged:         flagged,
		CategoryScores:  scores,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		Provider:        providerName,
		RawSummary:      result.RawSummary,
	}
}

func mapConfiguredProviderLabel(cfg Config, label string) string {
	for _, mapping := range cfg.LabelMappings {
		if mapping.ProviderLabel != "" && strings.Contains(label, mapping.ProviderLabel) {
			if category := normalizeCategory(mapping.TargetCategory); category != "" {
				return category
			}
		}
	}
	return mapProviderLabel(label)
}

func toModerationResponse(id string, model string, d decision) moderationResponse {
	scores := completeScores(d.CategoryScores)
	categories := make(map[string]bool, len(scores))
	for _, category := range moderationCategories {
		categories[category] = scores[category] >= sub2APIBlockThreshold(category)
	}
	flagged := wouldBlockSub2API(scores)
	return moderationResponse{
		ID:    id,
		Model: model,
		Results: []moderationResult{{
			Flagged:        flagged,
			Categories:     categories,
			CategoryScores: scores,
		}},
	}
}

func sub2APIBlockThreshold(category string) float64 {
	switch normalizeCategory(category) {
	case "harassment":
		return 0.98
	case "harassment/threatening", "sexual", "sexual/minors", "violence", "violence/graphic":
		return 0.90
	case "hate", "hate/threatening", "self-harm", "self-harm/intent", "self-harm/instructions":
		return 0.65
	case "illicit", "illicit/violent":
		return 0.95
	default:
		return 1
	}
}

func wouldBlockSub2API(scores map[string]float64) bool {
	for _, category := range moderationCategories {
		if scores[category] >= sub2APIBlockThreshold(category) {
			return true
		}
	}
	return false
}

func zeroScores() map[string]float64 {
	out := make(map[string]float64, len(moderationCategories))
	for _, category := range moderationCategories {
		out[category] = 0
	}
	return out
}

func completeScores(in map[string]float64) map[string]float64 {
	out := zeroScores()
	for category, score := range in {
		if normalized := normalizeCategory(category); normalized != "" {
			out[normalized] = score
		}
	}
	return out
}

func normalizeCategory(category string) string {
	for _, known := range moderationCategories {
		if category == known {
			return known
		}
	}
	return ""
}

func highestScore(scores map[string]float64) (string, float64) {
	highestCategory := ""
	highestScore := 0.0
	for _, category := range moderationCategories {
		score := scores[category]
		if highestCategory == "" || score > highestScore {
			highestCategory = category
			highestScore = score
		}
	}
	if highestScore <= 0 {
		return "none", 0
	}
	return highestCategory, highestScore
}
