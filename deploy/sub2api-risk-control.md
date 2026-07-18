# sub2api 风控中心配置

Adapter 本机部署时建议：

| 配置项 | 值 |
| --- | --- |
| 启用风控 | 开启 |
| 模式 | 先 `observe`，稳定后 `pre_block` |
| Base URL | `http://127.0.0.1:18080` |
| Model | `llm-audit-adapter-v1`，或任意占位模型名 |
| API Key | 后台“密钥与认证”页面里的 sub2api 调用密钥 |
| Timeout | `3500ms`（必须大于 Adapter 的模型超时） |
| Retry Count | `0` 或 `1` |
| Sample Rate | `100` |
| Keyword Mode | `api_only` |
| Blocked Keywords | 留空 |
| Pre Hash Check | 开启 |
| 自动封号 | 灰度期关闭 |

验证阻断时，Adapter 返回 `category_scores.illicit = 1`，sub2api 在 `pre_block` 模式下应阻断。
