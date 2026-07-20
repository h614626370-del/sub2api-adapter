package adapter

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr           string         `json:"listen_addr"`
	DatabasePath         string         `json:"database_path"`
	AuthTokens           []string       `json:"auth_tokens"`
	AdminToken           string         `json:"admin_token,omitempty"`
	ForceAllow           bool           `json:"force_allow"`
	MaxBodyBytes         int64          `json:"max_body_bytes"`
	DirectModelAudit     bool           `json:"direct_model_audit"`
	MissSampleRate       float64        `json:"miss_sample_rate"`
	AuditOnKeywordHit    bool           `json:"audit_on_keyword_hit"`
	MinTextChars         int            `json:"min_text_chars"`
	MaxTextChars         int            `json:"max_text_chars"`
	ImageAuditMode       string         `json:"image_audit_mode"`
	ImageSampleRate      float64        `json:"image_sample_rate"`
	MaxImages            int            `json:"max_images_per_request"`
	AllowDataURLImage    bool           `json:"allow_data_url_image"`
	DecisionCache        CacheConfig    `json:"decision_cache"`
	HashSalt             string         `json:"hash_salt"`
	ResultScoreCategory  string         `json:"result_score_category"`
	ResultBlockThreshold float64        `json:"result_block_threshold"`
	LogRawInput          bool           `json:"log_raw_input"`
	KeywordSets          []KeywordSet   `json:"keyword_sets"`
	LabelMappings        []LabelMapping `json:"provider_label_mapping"`
	Provider             ProviderConfig `json:"provider"`
	ImageProviderEnabled bool           `json:"image_provider_enabled"`
	ImageProvider        ProviderConfig `json:"image_provider"`
	EventRetention       int            `json:"event_retention"`
	EventRetentionDays   int            `json:"event_retention_days"`
	EstimatedPromptUSD   float64        `json:"estimated_prompt_price_usd_per_1m"`
	EstimatedOutputUSD   float64        `json:"estimated_completion_price_usd_per_1m"`
	EstimatedCachedUSD   float64        `json:"estimated_cached_price_usd_per_1m"`
}

type CacheConfig struct {
	Enabled         bool `json:"enabled"`
	AllowTTLSeconds int  `json:"allow_ttl_seconds"`
	BlockTTLSeconds int  `json:"block_ttl_seconds"`
	allowTTL        time.Duration
	blockTTL        time.Duration
}

type ProviderConfig struct {
	Type            string            `json:"type"`
	Endpoint        string            `json:"endpoint"`
	APIKey          string            `json:"api_key"`
	SecretID        string            `json:"secret_id"`
	SecretKey       string            `json:"secret_key"`
	Region          string            `json:"region"`
	BizType         string            `json:"biz_type"`
	Model           string            `json:"model"`
	SystemPrompt    string            `json:"system_prompt"`
	ActivePromptID  string            `json:"active_prompt_template_id"`
	PromptTemplates []PromptTemplate  `json:"prompt_templates"`
	EnableFewShot   bool              `json:"enable_few_shot"`
	WrapUserInput   bool              `json:"wrap_user_input"`
	Temperature     float64           `json:"temperature"`
	TopP            float64           `json:"top_p"`
	MaxTokens       int               `json:"max_tokens"`
	EnableSearch    bool              `json:"enable_search"`
	EnableThinking  bool              `json:"enable_thinking"`
	ThinkingBudget  int               `json:"thinking_budget"`
	HighResolution  bool              `json:"enable_high_resolution_images"`
	TimeoutMS       int               `json:"timeout_ms"`
	Disabled        bool              `json:"disabled"`
	Headers         map[string]string `json:"headers"`
}

type PromptTemplate struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt"`
}

type KeywordSet struct {
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	RiskDomain string   `json:"risk_domain"`
	MatchType  string   `json:"match_type"`
	Normalized bool     `json:"normalized"`
	Keywords   []string `json:"keywords"`
}

type LabelMapping struct {
	ProviderLabel  string `json:"provider_label"`
	TargetCategory string `json:"target_category"`
}

func DefaultConfig() Config {
	return Config{
		ListenAddr:           "127.0.0.1:18080",
		DatabasePath:         "data/adapter.db",
		AuthTokens:           []string{"change-me-before-production"},
		AdminToken:           "",
		HashSalt:             generatedHashSalt(),
		ResultScoreCategory:  "illicit",
		ResultBlockThreshold: 0.95,
		MaxBodyBytes:         16 << 20,
		DirectModelAudit:     false,
		MissSampleRate:       0.3,
		AuditOnKeywordHit:    true,
		MinTextChars:         1,
		MaxTextChars:         12000,
		ImageAuditMode:       "all",
		ImageSampleRate:      0.05,
		MaxImages:            2,
		AllowDataURLImage:    true,
		EventRetention:       1000,
		EventRetentionDays:   30,
		EstimatedPromptUSD:   0.25,
		EstimatedOutputUSD:   1.5,
		EstimatedCachedUSD:   0,
		DecisionCache: CacheConfig{
			Enabled:         true,
			AllowTTLSeconds: 3600,
			BlockTTLSeconds: 86400,
		},
		Provider: ProviderConfig{
			Type:            "chat_json",
			Endpoint:        "https://dashscope-us.aliyuncs.com/compatible-mode/v1",
			Model:           "qwen3.6-flash-us",
			SystemPrompt:    defaultAuditSystemPrompt(),
			ActivePromptID:  "default-cyber",
			PromptTemplates: defaultPromptTemplates(defaultAuditSystemPrompt()),
			EnableFewShot:   true,
			WrapUserInput:   true,
			Temperature:     0,
			TopP:            1,
			MaxTokens:       128,
			ThinkingBudget:  1,
			TimeoutMS:       2000,
		},
		ImageProviderEnabled: true,
		ImageProvider: ProviderConfig{
			Type:           "chat_json",
			Endpoint:       "https://dashscope-us.aliyuncs.com/compatible-mode/v1",
			Model:          "qwen3-vl-flash-us",
			SystemPrompt:   defaultAuditSystemPrompt(),
			EnableFewShot:  false,
			WrapUserInput:  true,
			Temperature:    0,
			TopP:           1,
			MaxTokens:      128,
			ThinkingBudget: 1,
			TimeoutMS:      3000,
		},
		KeywordSets:   defaultKeywordSets(),
		LabelMappings: defaultLabelMappings(),
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	applyEnvOverrides(&cfg)
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:18080"
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 16 << 20
	}
	if cfg.MaxBodyBytes > 32<<20 {
		cfg.MaxBodyBytes = 32 << 20
	}
	cfg.MissSampleRate = clamp01(cfg.MissSampleRate)
	cfg.ImageSampleRate = clamp01(cfg.ImageSampleRate)
	if cfg.MaxTextChars <= 0 || cfg.MaxTextChars > 12000 {
		cfg.MaxTextChars = 12000
	}
	if cfg.MinTextChars < 0 {
		cfg.MinTextChars = 0
	}
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = 2
	}
	if cfg.MaxImages > 8 {
		cfg.MaxImages = 8
	}
	cfg.ResultScoreCategory = normalizeCategory(strings.TrimSpace(cfg.ResultScoreCategory))
	if cfg.ResultScoreCategory == "" {
		cfg.ResultScoreCategory = "illicit"
	}
	if cfg.ResultBlockThreshold <= 0 {
		cfg.ResultBlockThreshold = 0.95
	}
	if cfg.ResultBlockThreshold > 1 {
		cfg.ResultBlockThreshold = 1
	}
	if cfg.EventRetention <= 0 {
		cfg.EventRetention = 1000
	}
	if cfg.EventRetention > 10000 {
		cfg.EventRetention = 10000
	}
	if cfg.EventRetentionDays <= 0 {
		cfg.EventRetentionDays = 30
	}
	if cfg.EventRetentionDays > 3650 {
		cfg.EventRetentionDays = 3650
	}
	if cfg.Provider.Type == "" {
		cfg.Provider.Type = "chat_json"
	}
	if strings.TrimSpace(cfg.DatabasePath) == "" {
		cfg.DatabasePath = "data/adapter.db"
	}
	if cfg.Provider.TimeoutMS <= 0 {
		cfg.Provider.TimeoutMS = 2000
	}
	if cfg.Provider.TimeoutMS > 30000 {
		cfg.Provider.TimeoutMS = 30000
	}
	if cfg.Provider.Model == "" {
		cfg.Provider.Model = "qwen3.6-flash-us"
	}
	if cfg.Provider.SystemPrompt == "" {
		cfg.Provider.SystemPrompt = defaultAuditSystemPrompt()
	}
	cfg.Provider = normalizePromptTemplates(cfg.Provider)
	cfg.ImageProvider = normalizeImageProviderConfig(cfg.ImageProvider, cfg.Provider)
	if cfg.EstimatedPromptUSD < 0 {
		cfg.EstimatedPromptUSD = 0
	}
	if cfg.EstimatedOutputUSD < 0 {
		cfg.EstimatedOutputUSD = 0
	}
	if cfg.EstimatedCachedUSD < 0 {
		cfg.EstimatedCachedUSD = 0
	}
	if cfg.Provider.TopP <= 0 || cfg.Provider.TopP > 1 {
		cfg.Provider.TopP = 1
	}
	if cfg.Provider.Temperature < 0 || cfg.Provider.Temperature > 2 {
		cfg.Provider.Temperature = 0
	}
	if cfg.Provider.MaxTokens <= 0 {
		cfg.Provider.MaxTokens = 128
	}
	if cfg.Provider.MaxTokens > 4096 {
		cfg.Provider.MaxTokens = 4096
	}
	if cfg.Provider.ThinkingBudget <= 0 {
		cfg.Provider.ThinkingBudget = 1
	}
	cfg.ImageAuditMode = strings.ToLower(strings.TrimSpace(cfg.ImageAuditMode))
	switch cfg.ImageAuditMode {
	case "off", "triggered", "sampled", "all":
	default:
		cfg.ImageAuditMode = "all"
	}
	if cfg.DecisionCache.AllowTTLSeconds <= 0 {
		cfg.DecisionCache.AllowTTLSeconds = 3600
	}
	if cfg.DecisionCache.AllowTTLSeconds > 86400 {
		cfg.DecisionCache.AllowTTLSeconds = 86400
	}
	if cfg.DecisionCache.BlockTTLSeconds <= 0 {
		cfg.DecisionCache.BlockTTLSeconds = 86400
	}
	if cfg.DecisionCache.BlockTTLSeconds > 7776000 {
		cfg.DecisionCache.BlockTTLSeconds = 7776000
	}
	cfg.DecisionCache.allowTTL = time.Duration(cfg.DecisionCache.AllowTTLSeconds) * time.Second
	cfg.DecisionCache.blockTTL = time.Duration(cfg.DecisionCache.BlockTTLSeconds) * time.Second
	if len(cfg.AuthTokens) == 0 {
		return cfg, errors.New("at least one auth token is required")
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("ADAPTER_LISTEN_ADDR")); v != "" {
		cfg.ListenAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("ADAPTER_DB")); v != "" {
		cfg.DatabasePath = v
	}
	if v := strings.TrimSpace(os.Getenv("ADAPTER_AUTH_TOKEN")); v != "" {
		cfg.AuthTokens = splitCSV(v)
	}
	if v := strings.TrimSpace(os.Getenv("ADAPTER_HASH_SALT")); v != "" {
		cfg.HashSalt = v
	}
	if v := strings.TrimSpace(os.Getenv("PROVIDER_TYPE")); v != "" {
		cfg.Provider.Type = v
	}
	if v := strings.TrimSpace(os.Getenv("PROVIDER_ENDPOINT")); v != "" {
		cfg.Provider.Endpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("PROVIDER_API_KEY")); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("TENCENT_SECRET_ID")); v != "" {
		cfg.Provider.SecretID = v
	}
	if v := strings.TrimSpace(os.Getenv("TENCENT_SECRET_KEY")); v != "" {
		cfg.Provider.SecretKey = v
	}
	if v := strings.TrimSpace(os.Getenv("TENCENT_REGION")); v != "" {
		cfg.Provider.Region = v
	}
	if v := strings.TrimSpace(os.Getenv("TENCENT_BIZ_TYPE")); v != "" {
		cfg.Provider.BizType = v
	}
	if v := strings.TrimSpace(os.Getenv("UPSTREAM_PROVIDER")); v != "" {
		cfg.Provider.Type = v
	}
	if v := strings.TrimSpace(os.Getenv("UPSTREAM_BASE_URL")); v != "" {
		cfg.Provider.Endpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("UPSTREAM_API_KEY")); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("UPSTREAM_MODEL")); v != "" {
		cfg.Provider.Model = v
	}
	if v, ok := boolEnv("ENABLE_FEW_SHOT"); ok {
		cfg.Provider.EnableFewShot = v
	}
	if v, ok := boolEnv("ENABLE_WRAP_USER_INPUT"); ok {
		cfg.Provider.WrapUserInput = v
	}
	if v, ok := boolEnv("ENABLE_SEARCH"); ok {
		cfg.Provider.EnableSearch = v
	}
	if v, ok := boolEnv("ENABLE_THINKING"); ok {
		cfg.Provider.EnableThinking = v
	}
	if v, ok := intEnv("THINKING_BUDGET"); ok {
		cfg.Provider.ThinkingBudget = v
	}
	if v, ok := floatEnv("TEMPERATURE"); ok {
		cfg.Provider.Temperature = v
	}
	if v, ok := floatEnv("TOP_P"); ok {
		cfg.Provider.TopP = v
	}
	if v, ok := boolEnv("FORCE_ALLOW"); ok {
		cfg.ForceAllow = v
	}
	if v, ok := boolEnv("MODEL_DISABLED"); ok {
		cfg.Provider.Disabled = v
	}
	if v, ok := boolEnv("PROVIDER_DISABLED"); ok {
		cfg.Provider.Disabled = v
	}
	if v, ok := floatEnv("MISS_SAMPLE_RATE"); ok {
		cfg.MissSampleRate = v
	}
	if v, ok := floatEnv("RESULT_BLOCK_THRESHOLD"); ok {
		cfg.ResultBlockThreshold = v
	}
	if v, ok := boolEnv("DIRECT_MODEL_AUDIT"); ok {
		cfg.DirectModelAudit = v
	}
	if v, ok := boolEnv("IMAGE_PROVIDER_ENABLED"); ok {
		cfg.ImageProviderEnabled = v
	}
	if v := strings.TrimSpace(os.Getenv("IMAGE_PROVIDER_API_KEY")); v != "" {
		cfg.ImageProvider.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("IMAGE_PROVIDER_MODEL")); v != "" {
		cfg.ImageProvider.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("IMAGE_PROVIDER_ENDPOINT")); v != "" {
		cfg.ImageProvider.Endpoint = v
	}
	if v, ok := intEnv("PROVIDER_TIMEOUT_MS"); ok {
		cfg.Provider.TimeoutMS = v
	}
	if v, ok := intEnv("UPSTREAM_TIMEOUT_MS"); ok {
		cfg.Provider.TimeoutMS = v
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func boolEnv(key string) (bool, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func intEnv(key string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	return v, err == nil
}

func floatEnv(key string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	return v, err == nil
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func generatedHashSalt() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "auto-generated:" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "auto-generated:" + base64.RawURLEncoding.EncodeToString(b[:])
}

func defaultKeywordSets() []KeywordSet {
	return []KeywordSet{
		{Name: "中文网络攻击与破解", Enabled: true, RiskDomain: "cyber", MatchType: "contains", Normalized: true, Keywords: []string{"逆向", "反编译", "破解", "绕过", "越权", "提权", "未授权访问", "sql注入", "命令执行", "漏洞利用", "反弹shell", "木马", "后门", "勒索软件", "免杀", "钓鱼", "爆破", "撞库", "盗号", "凭据填充"}},
		{Name: "英文网络攻击术语", Enabled: true, RiskDomain: "cyber", MatchType: "word_boundary", Normalized: true, Keywords: []string{"sqli", "xss", "rce", "ssrf", "webshell", "reverse shell", "malware", "ransomware", "keygen", "crack", "phishing", "credential stuffing"}},
		{Name: "账号与批量滥用", Enabled: true, RiskDomain: "abuse", MatchType: "contains", Normalized: true, Keywords: []string{"窃取token", "盗用token", "token池", "窃取cookie", "偷cookie", "盗用cookie", "窃取密钥", "爆破密码", "批量注册", "养号", "刷单", "刷量", "控评", "群发钓鱼", "诈骗群发", "验证码平台", "接码平台", "短信轰炸"}},
		{Name: "露骨色情与成人深伪", Enabled: true, RiskDomain: "sexual", MatchType: "contains", Normalized: true, Keywords: []string{"裸照", "色情", "涉黄", "露骨色情", "性器官", "生殖器", "阴茎", "阴道", "裸体", "裸露", "露点", "性行为", "成人视频", "成人内容", "未成年色情", "儿童色情", "儿童性剥削", "不雅照", "色情图片", "成人深伪", "不雅换脸", "色情换脸"}},
		{Name: "英文露骨色情与成人深伪", Enabled: true, RiskDomain: "sexual", MatchType: "word_boundary", Normalized: true, Keywords: []string{"explicit sex", "pornography", "porn", "nude photo", "nude image", "nude video", "csam", "child sexual abuse material", "revenge porn", "non-consensual intimate image", "sexual deepfake"}},
		{Name: "真人隐私与现实暴力", Enabled: true, RiskDomain: "violence", MatchType: "contains", Normalized: true, Keywords: []string{"人肉搜索", "开盒", "社工库", "泄露住址", "查他住址", "查她住址", "暴力威胁", "杀人", "杀了他", "杀了她", "砍死", "捅死", "弄死", "炸死", "买凶", "投毒", "绑架", "上门砍", "上门报复", "现实报复", "枪支", "爆炸物", "自制武器", "制作炸弹"}},
		{Name: "英文人身风险术语", Enabled: true, RiskDomain: "violence", MatchType: "word_boundary", Normalized: true, Keywords: []string{"dox", "doxxing", "kill", "murder", "kidnapping", "poisoning"}},
	}
}

func defaultLabelMappings() []LabelMapping {
	return []LabelMapping{
		{ProviderLabel: "辱骂", TargetCategory: "harassment"},
		{ProviderLabel: "威胁", TargetCategory: "harassment/threatening"},
		{ProviderLabel: "仇恨", TargetCategory: "hate"},
		{ProviderLabel: "歧视", TargetCategory: "hate"},
		{ProviderLabel: "违法", TargetCategory: "illicit"},
		{ProviderLabel: "违禁", TargetCategory: "illicit"},
		{ProviderLabel: "诈骗", TargetCategory: "illicit"},
		{ProviderLabel: "黑产", TargetCategory: "illicit"},
		{ProviderLabel: "涉政", TargetCategory: "illicit"},
		{ProviderLabel: "暴力犯罪", TargetCategory: "illicit/violent"},
		{ProviderLabel: "武器", TargetCategory: "violence"},
		{ProviderLabel: "自杀", TargetCategory: "self-harm/intent"},
		{ProviderLabel: "自残", TargetCategory: "self-harm/intent"},
		{ProviderLabel: "色情", TargetCategory: "sexual"},
		{ProviderLabel: "未成年色情", TargetCategory: "sexual/minors"},
		{ProviderLabel: "血腥", TargetCategory: "violence/graphic"},
	}
}

func defaultPromptTemplates(systemPrompt string) []PromptTemplate {
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = defaultAuditSystemPrompt()
	}
	return []PromptTemplate{
		{
			ID:           "default-cyber",
			Name:         "默认综合内容审核",
			Description:  "自有资产操作放行；明确攻击他人、露骨色情、成人深伪、dox 和真实暴力威胁给高分。",
			SystemPrompt: systemPrompt,
		},
		{
			ID:          "conservative-allow",
			Name:        "保守放行模板",
			Description: "适合刚接入时降低误杀：只有明确攻击他人、绕过授权或露骨色情等职责内风险才给高分。",
			SystemPrompt: defaultAuditSystemPrompt() + `

保守放行补充：
- 只有明确攻击他人、绕过授权、窃取凭据、批量滥用时给高分。
- 语义短、上下文不足、归属不明时给中低分。
- 自有资产防御、授权测试、排障和加固需求优先给低分。`,
		},
		{
			ID:          "strict-preblock",
			Name:        "前置阻断模板",
			Description: "适合阻断链路稳定后使用：对明确绕过授权、爆破、窃密和攻击他人给更高分。",
			SystemPrompt: defaultAuditSystemPrompt() + `

前置阻断补充：
- 明确要求攻击他人系统、逆向破解他人软件、绕过授权校验、爆破密码或窃取凭据时给高分。
- 没有声明授权或自有资产，但涉及绕过、破解、爆破、提权、木马、免杀时，只有明确用于攻击他人或绕过授权才给高分。
- 不要因为出现“学习”“研究”就自动给低分，要结合对象归属和行为后果。`,
		},
	}
}

func normalizePromptTemplates(cfg ProviderConfig) ProviderConfig {
	if strings.TrimSpace(cfg.SystemPrompt) == "" {
		cfg.SystemPrompt = defaultAuditSystemPrompt()
	}
	if len(cfg.PromptTemplates) == 0 {
		cfg.PromptTemplates = defaultPromptTemplates(cfg.SystemPrompt)
	}
	seen := map[string]int{}
	for i := range cfg.PromptTemplates {
		tpl := &cfg.PromptTemplates[i]
		tpl.ID = strings.TrimSpace(tpl.ID)
		if tpl.ID == "" {
			tpl.ID = fmt.Sprintf("template-%d", i+1)
		}
		if count := seen[tpl.ID]; count > 0 {
			tpl.ID = fmt.Sprintf("%s-%d", tpl.ID, count+1)
		}
		seen[tpl.ID]++
		tpl.Name = strings.TrimSpace(tpl.Name)
		if tpl.Name == "" {
			tpl.Name = fmt.Sprintf("审核模板 %d", i+1)
		}
		tpl.Description = strings.TrimSpace(tpl.Description)
		if strings.TrimSpace(tpl.SystemPrompt) == "" {
			tpl.SystemPrompt = cfg.SystemPrompt
		}
	}
	if cfg.ActivePromptID == "" || !promptTemplateExists(cfg.PromptTemplates, cfg.ActivePromptID) {
		cfg.ActivePromptID = cfg.PromptTemplates[0].ID
	}
	cfg.SystemPrompt = activeSystemPrompt(cfg)
	return cfg
}

func normalizeImageProviderConfig(image ProviderConfig, text ProviderConfig) ProviderConfig {
	if strings.TrimSpace(image.Type) == "" {
		image.Type = "chat_json"
	}
	if strings.TrimSpace(image.Endpoint) == "" {
		image.Endpoint = strings.TrimSpace(text.Endpoint)
	}
	if strings.TrimSpace(image.Endpoint) == "" {
		image.Endpoint = "https://dashscope-us.aliyuncs.com/compatible-mode/v1"
	}
	if strings.TrimSpace(image.Model) == "" {
		image.Model = "qwen3-vl-flash-us"
	}
	if strings.TrimSpace(image.SystemPrompt) == "" {
		image.SystemPrompt = activeSystemPrompt(text)
	}
	if image.TopP <= 0 || image.TopP > 1 {
		image.TopP = 1
	}
	if image.Temperature < 0 || image.Temperature > 2 {
		image.Temperature = 0
	}
	if image.MaxTokens <= 0 {
		image.MaxTokens = 128
	}
	if image.MaxTokens > 4096 {
		image.MaxTokens = 4096
	}
	if image.ThinkingBudget <= 0 {
		image.ThinkingBudget = 1
	}
	if image.TimeoutMS <= 0 {
		image.TimeoutMS = 3000
	}
	if image.TimeoutMS > 30000 {
		image.TimeoutMS = 30000
	}
	image = normalizePromptTemplates(image)
	return image
}

func effectiveImageProviderConfig(cfg Config) ProviderConfig {
	image := cfg.ImageProvider
	image.SystemPrompt = activeSystemPrompt(cfg.Provider)
	image.ActivePromptID = ""
	image.PromptTemplates = nil
	if strings.TrimSpace(image.APIKey) == "" {
		image.APIKey = cfg.Provider.APIKey
	}
	if strings.TrimSpace(image.SecretID) == "" {
		image.SecretID = cfg.Provider.SecretID
	}
	if strings.TrimSpace(image.SecretKey) == "" {
		image.SecretKey = cfg.Provider.SecretKey
	}
	return image
}

func promptTemplateExists(templates []PromptTemplate, id string) bool {
	for _, tpl := range templates {
		if tpl.ID == id {
			return true
		}
	}
	return false
}

func activeSystemPrompt(cfg ProviderConfig) string {
	for _, tpl := range cfg.PromptTemplates {
		if tpl.ID == cfg.ActivePromptID && strings.TrimSpace(tpl.SystemPrompt) != "" {
			return tpl.SystemPrompt
		}
	}
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		return cfg.SystemPrompt
	}
	return defaultAuditSystemPrompt()
}
