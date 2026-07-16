# sub2api Adapter 项目工作约定

本文档是本项目后续开发、排障、发布和部署的事实来源。修改架构、默认值、镜像仓库或部署方式后，应同步更新本文档。

## 1. 项目定位

本项目是 sub2api 风控中心与上游审核模型之间的 OpenAI-compatible moderation Adapter。

- 对 sub2api 暴露 `POST /v1/moderations`。
- 对上游调用 OpenAI 兼容的 `/chat/completions`。
- 通过本地关键词预筛、未命中抽样、文本模型、独立图片模型、决策缓存和 fail-open 形成综合判断。
- 管理员通过 Vue 管理后台完成密钥、模型、提示词、策略、测试、事件和系统维护。
- SQLite 保存运行配置、事件、决策缓存、配置审计和系统提示词历史。

关联项目：

- sub2api 源码：`D:\ideaWork\sub2api\sub2api`
- sub2api 使用指南：`D:\ideaWork\sub2api_guide`
- 当前项目：`D:\ideaWork\sub2api_adapter`
- GitHub 源码仓库：`https://github.com/h614626370-del/sub2api-adapter`（公开仓库，敏感运行配置不得提交）。

## 2. 技术栈与目录

- 后端：Go，入口为 `cmd/moderation-adapter`。
- 前端：Vue 3 + TypeScript + Vite，主页面为 `src/App.vue`，样式为 `src/styles.css`。
- 图标：`lucide-vue-next`。
- 数据库：SQLite，驱动为 `modernc.org/sqlite`。
- 容器：多阶段 Dockerfile，Node 构建前端，`golang:1.26.5-alpine` 构建静态服务，Alpine 运行。
- 管理后台静态产物：`internal/adapter/web/dist`，由 `npm run build` 生成并嵌入 Go 二进制。

主要文件：

- `internal/adapter/app.go`：路由、moderation 主流程和事件写入。
- `internal/adapter/config.go`：配置结构、默认值、环境变量覆盖和校验。
- `internal/adapter/store.go`：SQLite migration 与持久化。
- `internal/adapter/provider_chat.go`：OpenAI 兼容文本/视觉模型调用。
- `internal/adapter/admin.go`：管理后台 API。
- `internal/adapter/system_stats.go`：系统监控数据。
- `internal/adapter/system_update.go`：在线更新触发 API。
- `deploy/docker-compose.yml`：正式 Docker Hub 部署和 Watchtower 在线更新。
- `scripts/install.sh`：可独立下载运行的 Linux 一键部署脚本，自行生成标准 Docker Compose 和 `.env`。
- `scripts/publish-docker.ps1`：发布版本标签和 `latest`。
- `scripts/smoke.ps1`：上线前链路烟测。

## 3. 当前核心业务口径

### 3.1 最终分数

- 上游模型直接返回最终 `confidence`。
- Adapter 将最终分数原样写入一个指定的 `category_scores` 字段，默认是 `results[0].category_scores.illicit`。
- 其它分类字段保持 `0`，不再做多分类本地纠偏。
- Adapter 不保留“疑似”这一档功能。
- 是否阻断由 sub2api 读取该字段并按自己的阈值判断；管理后台的链路测试只模拟并展示是否达到阈值。

### 3.2 文本送审规则

- `direct_model_audit=false`：先做关键词初筛；命中后调用文本模型，未命中按抽样率调用。
- 未命中关键词抽样率默认 `0.3`，即 30%。
- `direct_model_audit=true`：达到最小文本长度的请求直接调用文本模型，不依赖关键词。
- 联网搜索默认关闭。
- 上游异常默认 fail-open，生产环境必须观察错误率和 P95。

关键词匹配类型：

- `contains`：对归一化文本做不区分大小写的子串匹配，并兼容常见空格、标点差异；中文优先使用。
- `regex`：对归一化文本执行不区分大小写的 Go 正则。
- `word_boundary`：要求关键词前后不是字母、数字或下划线；适合英文完整词，不适合连续中文。

### 3.3 图片审核

- 独立图片模型默认开启。
- 关闭独立图片模型后完全不做视觉审核，只审核文本；不能让纯文本模型读取 `image_url`。
- 支持请求中的图片 URL 和允许范围内的 data URL/Base64 图片。
- `image_audit_mode=all`：所有带图请求都审核。
- `image_audit_mode=sampled`：对所有带图请求独立抽样，是否命中关键词不影响图片抽样。
- `image_audit_mode=triggered`：文本直接送审、关键词命中或文本抽样命中时审核图片；其余带图请求再按 `image_sample_rate` 补充抽样。
- `image_audit_mode=off`：不审核图片。
- 图片高清模式只影响视觉模型请求；适合小字、截图和二维码，但会增加成本与延迟。

### 3.4 提示词与事件

- 页面只维护一套当前系统提示词和一段说明，不提供多模板切换。
- 每次保存提示词修改前，旧内容自动进入 `prompt_versions`。
- 恢复历史版本时，恢复前的当前内容也必须先归档。
- 被阻断的真实 moderation 请求保存归一化文本明文，后台事件页可展开查看。
- 放行事件不保存完整明文，只保留指纹或摘要。
- 事件页使用服务端分页。

## 4. 重要默认值

- 管理后台使用用户名密码登录，不使用后台 token。独立安装脚本默认使用用户名 `admin` 并生成随机强密码，保存在权限为 `600` 的部署 `.env` 中。
- Docker 部署时 Adapter 容器内部固定监听 `0.0.0.0:18080`；宿主机管理端口默认只发布到 `127.0.0.1:18080`，由 `ADAPTER_HOST_BIND` 控制。
- 数据库：`/app/data/adapter.db`。
- 综合结果字段：`illicit`。
- 未命中抽样率：`0.3`。
- 独立图片模型：默认开启。
- 图片策略：默认 `triggered`。
- 图片补充抽样率：默认 `0.05`。
- 文本模型：`qwen-flash`。
- 图片模型：`qwen3-vl-flash`。
- 联网搜索：文本和图片模型均默认关闭。
- 决策缓存：默认开启。
- 内存决策缓存最多 10000 条；SQLite 决策缓存同样按 10000 条清理，后台维护循环每分钟清理过期缓存和超量事件。
- 事件最多保留 1000 条，默认保留 30 天。

密钥和盐值不要写进本文档、代码、Compose 或提交记录。运行密钥优先在后台配置；环境变量只用于自动化覆盖。

## 5. HTTP 接口

公开运行接口：

- `POST /v1/moderations`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /admin`

重要管理接口需要先用固定用户名密码登录：

- `POST /admin/api/login`
- `GET|PUT /admin/api/config`
- `POST /admin/api/secrets/sub2api-token`
- `GET /admin/api/prompt/versions`
- `POST /admin/api/prompt/restore`
- `GET /admin/api/events`
- `GET /admin/api/system/stats`
- `GET|POST /admin/api/system/update`

## 6. 本地开发与验证

Windows 命令优先使用 PowerShell 7，即 `pwsh`。

首次安装和启动：

```powershell
npm install
npm run build
go run ./cmd/moderation-adapter
```

本地管理地址：`http://127.0.0.1:18080/admin`。

每次修改至少执行：

```powershell
npm run build
go test ./...
```

上线前烟测：

```powershell
pwsh -ExecutionPolicy Bypass -File .\scripts\smoke.ps1 `
  -BaseUrl http://127.0.0.1:18080 `
  -Token "<sub2api调用密钥>" `
  -AdminPassword "<管理员密码>" `
  -ClearCache `
  -Assert
```

前端变更还要检查：

- 桌面宽屏和 `390x844` 手机视口。
- 页面无横向整体溢出，表格允许在自身容器内横向滚动。
- 高级参数网格、提示词历史、事件分页和系统监控无重叠。
- 浏览器控制台无 error。

## 7. Docker Hub 发布

固定 Docker Hub 仓库：

```text
614626370/sub2api-adapter
```

版本格式必须使用简洁语义版本，例如 `0.0.4`、`0.0.5`，不要使用日期作为镜像版本号。

标准发布命令：

```powershell
pwsh -File .\scripts\publish-docker.ps1 -Version 0.0.7
```

发布脚本应同时推送：

- `614626370/sub2api-adapter:<version>`
- `614626370/sub2api-adapter:latest`

当前正式发布（2026-07-17）：

- 版本：`0.0.7`
- Git 提交：`199e779e8129`
- Docker Hub 摘要：`sha256:cc8441ed94cdc330033f76ea02ef3a4f71b7a380b0fa9df206e8d9cd73eab58e`
- `0.0.7` 与 `latest` 指向同一摘要。
- GitHub Release：`https://github.com/h614626370-del/sub2api-adapter/releases/tag/v0.0.7`
- 发布前已通过 `go test`、`go vet`、Linux race、staticcheck、govulncheck、前端构建、npm audit、ShellCheck、仓库秘密扫描和最终镜像 Trivy 扫描；Adapter 运行层与 Go 二进制当次高危/严重漏洞均为 0。

当前 Windows 主机没有直接可用的 `docker` 命令时，使用 Ubuntu WSL 中的 Docker。WSL 已登录正确的 Docker Hub 账号，且已有 `614626370/kkflow-guide-api` 发布记录。虚拟机中的 Docker 登录不是该发布账号，不要从虚拟机执行 push，也不要读取或复制 Docker 凭据。

若 WSL 构建因拉取基础镜像网络失败，可将虚拟机已经构建好的当前镜像临时 `docker save | gzip`，传到本机临时目录后在 WSL `docker load` 并 push。成功后必须删除两端临时 tar。

## 8. 正式 Docker Compose 部署

正式部署使用 `deploy/docker-compose.yml`，不使用旧的 load-and-run 脚本，也不使用旧的本地 build Compose。

当前线上状态（用户于 2026-07-17 确认）：

- 用户已在正式服务器执行最新版 `scripts/install.sh`，脚本成功启动服务并输出随机管理员账号密码。
- 本线程没有直接登录正式服务器，线上容器版本、网络名和健康状态仍以服务器实际检查结果为准，不能写成已独立验证。
- 账号密码、`ADAPTER_UPDATE_TOKEN`、模型 Key、服务器地址和部署目录不得写入公开 `AGENTS.md`；管理员密码只保存在服务器部署目录权限为 `600` 的 `.env` 中。
- 线上 sub2api Compose 使用 `container_name: sub2api`、自定义 bridge 业务网络，以及本地目录形式的 sub2api/PostgreSQL/Redis 数据挂载。

推荐的一键部署方式（服务器需已安装 Docker Engine 和 Compose）：

```bash
mkdir -p sub2api-adapter && cd sub2api-adapter
curl -fsSL https://raw.githubusercontent.com/h614626370-del/sub2api-adapter/main/scripts/install.sh -o install-sub2api-adapter.sh
bash install-sub2api-adapter.sh
```

该脚本完全独立，服务器不需要下载源码或上传项目目录。应先启动名为 `sub2api` 的容器；脚本会自动检测其业务网络，也可通过 `--network` 显式指定。它会自行生成 `.env` 和 `docker-compose.yml`，默认安装到执行时的当前目录、把管理端口发布到宿主机 `127.0.0.1:18080`、自动生成并持久化 `ADAPTER_UPDATE_TOKEN`，然后执行 `docker compose pull` 和 `docker compose up -d`。应先进入专用目录再执行；重复执行会保留原更新令牌、管理员密码、业务网络名称和数据卷。

业务网络识别优先级：

1. 优先使用命令行 `--network NAME`。
2. 未显式指定时复用部署 `.env` 中的 `SUB2API_DOCKER_NETWORK`。
3. 仍为空时通过 `docker inspect sub2api` 读取容器当前连接的网络。
4. 多个网络中若只有一个名称包含 `sub2api-network`，选择该网络；如果总共只有一个网络，直接选择它。
5. 无法唯一判断时必须报错退出并要求 `--network`，不得猜测网络。
6. 最终使用 `docker network inspect` 验证网络真实存在，再写入 `.env`。

常用参数：

```bash
bash install-sub2api-adapter.sh --dir /opt/sub2api-adapter
bash install-sub2api-adapter.sh --network deploy_sub2api-network
bash install-sub2api-adapter.sh --bind 0.0.0.0:18080
bash install-sub2api-adapter.sh --proxy http://192.168.1.2:7897
```

`--proxy` 只设置 Watchtower 在线更新器的代理，Adapter 模型请求保持直连。若首次 `docker compose pull` 也依赖代理，必须提前配置 Docker daemon 代理。

把管理端口发布到 `0.0.0.0` 前必须配置防火墙、来源 IP 限制或 HTTPS 反向代理，不能把 `/admin` 裸露到公网。

首次部署：

```bash
cp deploy/adapter.env.example .env
# 至少设置 SUB2API_DOCKER_NETWORK、一个 32 字节以上的随机 ADAPTER_UPDATE_TOKEN 和强管理员密码
docker compose -f deploy/docker-compose.yml up -d
```

Compose 包含两个服务：

- `moderation-adapter`：`614626370/sub2api-adapter:latest`，同时加入外部 sub2api 业务网络和项目内部控制网络；宿主机管理端口默认仅发布到 `127.0.0.1:18080`。
- `adapter-updater`：使用公共镜像 `nickfedor/watchtower:nightly@sha256:011cbd0246d247f8827a2624dd6202d8b0d1a3d8b9c9fc7937b427e37aa5f2c9`，按摘要固定到已验证构建；只加入内部控制网络，不发布宿主机端口。变更摘要前必须重新做镜像漏洞扫描和更新 API 回归。
- 2026-07-16 的 Trivy 结果：旧 `containrrr/watchtower:1.7.1` 有 37 个高危和 5 个严重漏洞，`nickfedor/watchtower:1.19.0` 仍有 1 个高危；当前固定摘要为 0 个高危/严重。不得为了缩短面板显示而改成未固定的 `nightly` 标签，也不得换回旧镜像。

数据与权限边界：

- Adapter 挂载 `adapter-data:/app/data`，SQLite 数据必须一直复用该卷。
- sub2api 通过共享业务网络和容器 DNS `http://sub2api-moderation-adapter:18080` 调用 Adapter，不需要 `extra_hosts` 或宿主机网关地址。
- Docker Compose 强制使用容器内的 `/app/data/adapter.db`；systemd 直跑时使用宿主机 `/opt/sub2api-adapter/data/adapter.db`，两者不要混用。
- Adapter 只读挂载 `${ADAPTER_CONFIG_DIR:-./configs}:/app/configs`。
- Adapter 根文件系统只读，仅 `/app/data` 和 32 MiB 的 `/tmp` 可写；默认限制 512 MiB 内存和 256 个 PID，并移除全部 Linux capabilities。
- 只有 Watchtower 挂载 `/var/run/docker.sock`。
- Adapter 不得直接挂载 Docker Socket。
- Watchtower 根文件系统只读，仅使用 16 MiB `/tmp`，移除全部 Linux capabilities，禁止提权，并限制为 256 MiB 内存和 128 个 PID。
- Watchtower 只更新带 `com.centurylinklabs.watchtower.enable=true` 标签的容器。
- Watchtower 不做周期轮询，只响应后台“系统维护”的更新请求。
- sub2api、PostgreSQL 和 Redis 当前 Compose 均不带 Watchtower 更新标签，正常更新选择逻辑不会重建这些容器。

在线更新链路：

1. 发布新版本标签和新的 `latest`。
2. 管理员在“系统维护”点击“拉取并更新”。
3. Adapter 使用内部令牌经控制网络调用 `http://adapter-updater:8080/v1/update`。
4. Watchtower 拉取 `latest` 并按需重建 Adapter。
5. SQLite 数据卷不变。

在线更新影响边界：

- 正常更新只停止并重建 `sub2api-moderation-adapter`；不会执行 sub2api 项目的 `docker compose down`，不会删除外部业务网络，也不会挂载或修改 sub2api/PostgreSQL/Redis 数据目录。
- Adapter 重建期间会有数秒审核不可用窗口；sub2api 主服务继续运行，但这段时间的审核请求可能超时或按 sub2api 策略 fail-open。
- `WATCHTOWER_CLEANUP=true` 只清理被成功更新的 Adapter 旧镜像，不删除 sub2api 镜像、容器或数据卷。版本标签仍保留在 Docker Hub，可重新拉取回滚。
- 用户已明确决定不增加 Watchtower 的容器名称参数白名单，当前依赖 `WATCHTOWER_LABEL_ENABLE=true` 与 Adapter 专属标签限制正常更新目标。
- `/var/run/docker.sock` 是宿主机高权限接口；只读根文件系统、`cap_drop` 和 `no-new-privileges` 不能从根本上限制通过 Docker API 的权限。固定镜像摘要、内部控制网络、随机令牌和标签过滤降低风险，但不能形成对恶意更新器的强隔离，后续不得弱化这些边界。
- 2026-07-16/17 已用临时 Docker 网络验证：sub2api 容器可通过 `http://sub2api-moderation-adapter:18080/healthz` 访问 Adapter；更新器无宿主机端口；后台在线更新通过 `adapter-control` 网络成功触发。

## 9. 本地测试环境

- 测试虚拟机的 IP、SSH 用户、代理、部署目录和运行状态只记录在被 Git 忽略的 `AGENTS.local.md`，不得提交到公开仓库。
- Adapter 容器不得设置宿主机开发代理；上游模型请求保持直连。
- Docker daemon、GitHub 或 Watchtower 是否使用代理，由具体部署环境单独配置，不写死在公开源码中。
- 本地验证仍需覆盖 Adapter、Watchtower、sub2api、PostgreSQL、Redis、在线更新、事件数据和数据卷持久化。

## 10. 部署与清理纪律

- 不创建备份目录、备份 Compose、zip 或长期保存的 tar 包，除非用户明确要求。
- 临时源码包或镜像 tar 只用于传输；部署验证成功后同时删除本机和虚拟机副本。
- 新版本验证成功后删除旧 Adapter 镜像标签和 dangling layers；不要删除正在运行的 sub2api、PostgreSQL、Redis 镜像或数据卷。
- 更新 Adapter 时只重建 Adapter/Watchtower，不要重建或清空 sub2api 的数据库和 Redis。
- 任何部署都先验证 `/healthz`、`/readyz`、容器状态、版本、事件数量和数据卷，再清理旧文件。
- 不覆盖用户已有的 `configs/config.json`，不重置 SQLite，不删除 `adapter-data`。
- 不在输出中展示 sub2api 调用密钥、上游 API Key、更新令牌、Docker 凭据或 SSH 密码。

## 11. 代码修改约定

- 先阅读现有实现并沿用本项目模式，避免无关重构。
- 搜索优先使用 `rg`，Windows Shell 优先使用 `pwsh`。
- 手工修改文件使用 `apply_patch`。
- 前端按钮优先使用现有 Lucide 图标和现有控件样式。
- 管理后台是生产运维工具，保持信息密集、克制、可扫描；不要做营销式页面或装饰性卡片堆叠。
- 新增配置必须同步考虑：默认值、JSON、环境变量、SQLite 持久化、后台表单、配置导入导出、测试和部署文档。
- 新增敏感数据时默认掩码；只有用户明确要求的受控场景才能显示明文。
- 后端共享行为或管理接口变更要补集成测试。
- 发布前必须通过 `go test ./...` 和 `npm run build`。
