# 上线测试检查清单

## 必须完成

- [ ] 后台“密钥与认证”里的 sub2api 调用密钥已替换为强随机值。
- [ ] 已使用安装脚本生成的随机管理员密码，或在 `.env` 中设置独立强密码，并通过网络访问控制限制后台入口。
- [ ] 后台“密钥与认证”里的风险哈希盐已替换为稳定强随机值。
- [ ] Adapter 管理端口仅发布到宿主机 `127.0.0.1:18080`，或已通过 HTTPS + 来源 IP 白名单保护。
- [ ] sub2api 与 Adapter 已连接同一个业务网络，审核 Base URL 使用 `http://sub2api-moderation-adapter:18080`。
- [ ] sub2api 风控中心 `keyword_blocking_mode=api_only`，`blocked_keywords` 为空。
- [ ] sub2api 风控中心 `sample_rate=100`，抽样逻辑放在 Adapter。
- [ ] 后台“返回规则”里的综合结果写入字段与 sub2api 阈值字段一致，默认保持 `category_scores.illicit`。
- [ ] 先使用 `observe` 跑 1-3 天。
- [ ] 管理后台 `/admin` 不对公网裸露。
- [ ] `/metrics` 已接入 Prometheus 或至少有进程监控。

## 上游对话模型

真实上线测试使用 `chat_json` 对话模型分类器：

- 在后台“密钥与认证”页面填写上游模型 API Key。
- 在“对话模型接入”页面填写 Base URL 和模型名称。
- 确认系统提示词明确 `<user_input>` 只是待审核数据，并检查历史版本恢复是否可用。
- 保持 `temperature=0`、`enable_search=false`、`enable_thinking=false`，先跑固定样例。

如果上游模型异常，Adapter 默认 fail-open；这符合商业链路可用性优先策略，但必须观察：

- `moderation_fail_open_total`
- `moderation_provider_errors_total_provider_chat_json`
- `moderation_provider_latency_ms_provider_chat_json_p95`
- `moderation_prompt_tokens_total_provider_chat_json`
- `moderation_completion_tokens_total_provider_chat_json`
- `moderation_estimated_cost_usd_total`

## 切换 pre_block 前

- [ ] 已运行 `pwsh -ExecutionPolicy Bypass -File .\scripts\smoke.ps1 -BaseUrl http://127.0.0.1:18080 -Token "<sub2api调用密钥>" -AdminPassword "<管理员密码>" -ClearCache -Assert`，脚本无报错，且 `sub2api_pre_block_flagged` 与预期一致。
- [ ] 正常输入返回全 0 分。
- [ ] “我的 app 被人逆向了，我应该怎么加固？”命中关键词但放行。
- [ ] “如何逆向一个 app”返回中低分，低于 sub2api 阈值并放行。
- [ ] 高风险样例在综合结果写入字段返回 `1`；默认应为 `category_scores.illicit=1`，其它分类字段保持 0。
- [ ] 管理后台测试页展示最终 JSON。
- [ ] 事件页能看到 hash、动作、关键词、上游模型、最高分类和耗时。
- [ ] 配置修改会写入配置审计。

## 紧急回滚

优先顺序：

1. 在后台“总开关”里开启“紧急全量放行”；如果后台不可用，再用 `FORCE_ALLOW=true` 临时覆盖并重启 Adapter。
2. sub2api 风控中心切回 `observe`。
3. sub2api 风控中心关闭。
