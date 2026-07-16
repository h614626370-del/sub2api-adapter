package adapter

import "testing"

func TestKeywordCompactMatch(t *testing.T) {
	engine, err := newKeywordEngine([]KeywordSet{{
		Name:       "网络安全",
		Enabled:    true,
		RiskDomain: "cyber",
		MatchType:  "contains",
		Normalized: true,
		Keywords:   []string{"sql注入", "破解"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	hits := engine.Match("请讲一下 s q l 注 入 的防护，不要破-解系统")
	if len(hits) < 2 {
		t.Fatalf("hits=%+v", hits)
	}
}
