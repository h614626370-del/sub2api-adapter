package adapter

import (
	"regexp"
	"strings"
	"unicode"
)

type keywordEngine struct {
	sets []compiledKeywordSet
}

type compiledKeywordSet struct {
	Name       string
	Enabled    bool
	RiskDomain string
	MatchType  string
	Normalized bool
	Keywords   []compiledKeyword
}

type compiledKeyword struct {
	Raw     string
	Needle  string
	Compact string
	Regex   *regexp.Regexp
}

func newKeywordEngine(sets []KeywordSet) (*keywordEngine, error) {
	engine := &keywordEngine{}
	for _, set := range sets {
		cs := compiledKeywordSet{
			Name:       strings.TrimSpace(set.Name),
			Enabled:    set.Enabled,
			RiskDomain: strings.TrimSpace(set.RiskDomain),
			MatchType:  strings.ToLower(strings.TrimSpace(set.MatchType)),
			Normalized: set.Normalized,
		}
		if cs.Name == "" {
			cs.Name = cs.RiskDomain
		}
		if cs.MatchType == "" {
			cs.MatchType = "contains"
		}
		for _, kw := range set.Keywords {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			ck := compiledKeyword{Raw: kw, Needle: normalizeText(kw), Compact: compactForMatch(kw)}
			if cs.MatchType == "regex" {
				re, err := regexp.Compile("(?i)" + kw)
				if err != nil {
					return nil, err
				}
				ck.Regex = re
			}
			cs.Keywords = append(cs.Keywords, ck)
		}
		engine.sets = append(engine.sets, cs)
	}
	return engine, nil
}

func (e *keywordEngine) Match(text string) []keywordHit {
	if e == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	normalized := normalizeText(text)
	compact := compactForMatch(text)
	hits := make([]keywordHit, 0)
	seen := map[string]struct{}{}
	for _, set := range e.sets {
		if !set.Enabled {
			continue
		}
		for _, kw := range set.Keywords {
			if keywordMatches(set, kw, normalized, compact) {
				key := set.Name + "\x00" + kw.Raw
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				hits = append(hits, keywordHit{SetName: set.Name, RiskDomain: set.RiskDomain, Keyword: kw.Raw})
			}
		}
	}
	return hits
}

func keywordMatches(set compiledKeywordSet, kw compiledKeyword, normalized string, compact string) bool {
	switch set.MatchType {
	case "regex":
		return kw.Regex != nil && kw.Regex.MatchString(normalized)
	case "word_boundary":
		return wordBoundaryContains(normalized, kw.Needle)
	default:
		return strings.Contains(normalized, kw.Needle) || (kw.Compact != "" && strings.Contains(compact, kw.Compact))
	}
}

func wordBoundaryContains(haystack string, needle string) bool {
	if needle == "" {
		return false
	}
	idx := strings.Index(haystack, needle)
	for idx >= 0 {
		beforeOK := idx == 0 || !isWordRune([]rune(haystack[:idx])[len([]rune(haystack[:idx]))-1])
		afterIdx := idx + len(needle)
		afterOK := afterIdx >= len(haystack) || !isWordRune([]rune(haystack[afterIdx:])[0])
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(haystack[idx+len(needle):], needle)
		if next < 0 {
			break
		}
		idx += len(needle) + next
	}
	return false
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
