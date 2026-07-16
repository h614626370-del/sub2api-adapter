package adapter

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tms/v20201229"
)

type tencentTMSProvider struct {
	cfg       ProviderConfig
	client    *tms.Client
	configErr error
}

func newTencentTMSProvider(cfg ProviderConfig) (provider, error) {
	cfg.SecretID = strings.TrimSpace(cfg.SecretID)
	cfg.SecretKey = strings.TrimSpace(cfg.SecretKey)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.BizType = strings.TrimSpace(cfg.BizType)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.SecretID == "" || cfg.SecretKey == "" {
		return &tencentTMSProvider{cfg: cfg}, nil
	}
	if err := validateTencentBizType(cfg.BizType); err != nil {
		return &tencentTMSProvider{cfg: cfg, configErr: err}, nil
	}
	if err := validateTencentCredentials(cfg); err != nil {
		return &tencentTMSProvider{cfg: cfg, configErr: err}, nil
	}
	region := cfg.Region
	if region == "" {
		region = "ap-guangzhou"
	}
	cpf := profile.NewClientProfile()
	if cfg.Endpoint != "" {
		cpf.HttpProfile.Endpoint = cfg.Endpoint
	}
	if cfg.TimeoutMS > 0 {
		cpf.HttpProfile.ReqTimeout = max(1, cfg.TimeoutMS/1000)
	}
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	client, err := tms.NewClient(credential, region, cpf)
	if err != nil {
		return nil, err
	}
	return &tencentTMSProvider{cfg: cfg, client: client}, nil
}

func (p *tencentTMSProvider) Name() string { return "tencent_tms" }

func (p *tencentTMSProvider) Audit(ctx context.Context, in providerRequest) (providerResult, error) {
	if p.configErr != nil {
		return providerResult{}, p.configErr
	}
	if p.client == nil {
		return providerResult{}, errors.New("腾讯云 SecretId 或 SecretKey 未配置，请先在后台“密钥”页面填写")
	}
	if !in.AuditText || strings.TrimSpace(in.Text) == "" {
		return providerResult{Action: "pass", RawSummary: "腾讯云内容安全跳过：没有可审核文本"}, nil
	}
	start := timeNow()
	req := tms.NewTextModerationRequest()
	content := base64.StdEncoding.EncodeToString([]byte(in.Text))
	req.Content = &content
	dataID := trimRunes(in.RequestID, 64)
	req.DataId = &dataID
	sourceLanguage := "zh"
	req.SourceLanguage = &sourceLanguage
	serviceType := "TEXT"
	req.Type = &serviceType
	if p.cfg.BizType != "" {
		bizType := p.cfg.BizType
		req.BizType = &bizType
	}
	resp, err := p.client.TextModerationWithContext(ctx, req)
	if err != nil {
		return providerResult{}, translateTencentError(err)
	}
	if resp == nil || resp.Response == nil {
		return providerResult{}, errors.New("腾讯云内容安全返回空响应")
	}
	action := canonicalProviderAction(derefString(resp.Response.Suggestion))
	resultScore := clamp01(float64(derefInt(resp.Response.Score)) / 100)
	labels := []providerLabel{}
	if label := derefString(resp.Response.Label); label != "" && label != "Normal" {
		labels = append(labels, providerLabel{Label: label, Category: mapTencentLabel(label), Score: resultScore})
	}
	if sub := derefString(resp.Response.SubLabel); sub != "" {
		labels = append(labels, providerLabel{Label: sub, Category: mapTencentLabel(sub), Score: resultScore})
	}
	if action == "" {
		action = "pass"
	}
	return providerResult{
		Action:     action,
		Score:      resultScore,
		Labels:     labels,
		RawSummary: "suggestion=" + derefString(resp.Response.Suggestion) + ", label=" + derefString(resp.Response.Label) + ", request_id=" + derefString(resp.Response.RequestId),
		LatencyMS:  time.Since(start).Milliseconds(),
	}, nil
}

func validateTencentCredentials(cfg ProviderConfig) error {
	secretID := strings.TrimSpace(cfg.SecretID)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if secretID == "" || secretKey == "" {
		return errors.New("腾讯云 SecretId 或 SecretKey 未配置，请先在后台“密钥”页面填写")
	}
	if secretID == secretKey {
		return errors.New("腾讯云 SecretId 和 SecretKey 不能相同；请分别从腾讯云“访问密钥”页面复制 SecretId 与 SecretKey")
	}
	if strings.ContainsAny(secretID, "/ \t\r\n") || strings.Contains(secretID, "=") || strings.Contains(secretID, "&") {
		return errors.New("腾讯云 SecretId 格式不对：请只填写 SecretId 本身，不要填 SecretKey、Authorization 头、其它系统令牌或整段复制内容")
	}
	if !strings.HasPrefix(secretID, "AKID") {
		return errors.New("腾讯云 SecretId 格式不对：SecretId 通常以 AKID 开头；请到腾讯云“访问密钥”页面复制 SecretId")
	}
	if strings.ContainsAny(secretKey, " \t\r\n") || strings.Contains(secretKey, "&Secret") || strings.Contains(secretKey, "SecretId=") {
		return errors.New("腾讯云 SecretKey 格式不对：请只填写 SecretKey 本身，不要粘贴整段密钥说明或其它系统令牌")
	}
	if len([]rune(secretID)) < 20 || len([]rune(secretKey)) < 20 {
		return errors.New("腾讯云访问密钥长度看起来不对，请重新从腾讯云“访问密钥”页面复制 SecretId 和 SecretKey")
	}
	return nil
}

func validateTencentBizType(bizType string) error {
	if bizType == "" {
		return nil
	}
	if len([]rune(bizType)) > 64 {
		return errors.New("腾讯判断方案模板 BizType 太长：请只填写控制台里的 BizType 编号，不要粘贴整段说明")
	}
	for _, r := range bizType {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return errors.New("腾讯判断方案模板 BizType 格式不对：只能包含英文、数字、下划线或短横线；留空表示使用腾讯默认策略")
	}
	return nil
}

func translateTencentError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "AuthFailure.InvalidAuthorization") || strings.Contains(msg, "Credential scope size not valid"):
		return errors.New("腾讯云鉴权头格式错误：SecretId 很可能填错了。SecretId 应该是 AKID 开头的腾讯云 SecretId，不要填写 SecretKey、其它系统令牌或整段 Authorization 内容")
	case strings.Contains(msg, "AuthFailure.SecretIdNotFound"):
		return errors.New("腾讯云找不到这个 SecretId：请确认 SecretId 是否复制完整，并且属于当前腾讯云账号")
	case strings.Contains(msg, "AuthFailure.SignatureFailure") || strings.Contains(msg, "AuthFailure.SignatureExpire"):
		return errors.New("腾讯云签名校验失败：请确认 SecretKey 是否复制正确，并检查本机时间是否准确")
	case strings.Contains(msg, "AuthFailure.UnauthorizedOperation"):
		return errors.New("腾讯云账号没有文本内容安全接口权限：请确认已开通内容安全服务，并给密钥所属子账号授权")
	case strings.Contains(msg, "UnsupportedRegion"):
		return errors.New("腾讯云区域不可用：请在旧版腾讯配置中切换到已开通内容安全的区域")
	default:
		return err
	}
}

func mapTencentLabel(label string) string {
	lower := strings.ToLower(label)
	switch {
	case lower == "porn" || strings.Contains(label, "色情"):
		return "sexual"
	case lower == "abuse" || strings.Contains(label, "谩骂"):
		return "harassment"
	case lower == "ad" || strings.Contains(label, "广告"):
		return "illicit"
	default:
		return mapProviderLabel(label)
	}
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
