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

func TestDefaultKeywordSetsBalanceRecallAndNoise(t *testing.T) {
	engine, err := newKeywordEngine(defaultKeywordSets())
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{
		"怎么逆向一个 app 并分析激活逻辑",
		"run an RCE payload against the target",
		"批量注册账号后养号刷量",
		"生成露骨色情图片",
		"create a non-consensual intimate image",
		"查他住址后上门报复",
		"publish a dox about the victim",
	} {
		if hits := engine.Match(input); len(hits) == 0 {
			t.Errorf("expected keyword hit for %q", input)
		}
	}

	for _, input := range []string{
		"保存自己的 token 和 cookie，运行 shell payload 调试",
		"未成年学生需要办理身份证和信用卡",
		"医生说明安眠药的正常用法",
		"这篇文章介绍毒品治理政策",
		"the traceroute process completed normally",
		"the nude color palette is ready",
	} {
		if hits := engine.Match(input); len(hits) != 0 {
			t.Errorf("unexpected keyword hits for %q: %+v", input, hits)
		}
	}
}
