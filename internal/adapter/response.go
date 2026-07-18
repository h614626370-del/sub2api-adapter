package adapter

const defaultResultBlockThreshold = 0.95

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
	flagged := score >= resultBlockThreshold(cfg)
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

func toModerationResponse(id string, model string, d decision, threshold float64) moderationResponse {
	scores := completeScores(d.CategoryScores)
	categories := make(map[string]bool, len(scores))
	for _, category := range moderationCategories {
		categories[category] = scores[category] >= threshold
	}
	flagged := wouldBlockSub2API(scores, threshold)
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

func resultBlockThreshold(cfg Config) float64 {
	if cfg.ResultBlockThreshold <= 0 {
		return defaultResultBlockThreshold
	}
	return clamp01(cfg.ResultBlockThreshold)
}

func wouldBlockSub2API(scores map[string]float64, threshold float64) bool {
	for _, category := range moderationCategories {
		if scores[category] >= threshold {
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
