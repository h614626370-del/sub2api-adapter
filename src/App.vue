<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  AlertTriangle, Archive, Beaker, CheckCircle2, ChevronLeft, ChevronRight, Copy,
  Database, Download, FileClock, Gauge, HardDrive, History, Image, KeyRound, Layers,
  LogOut, RefreshCw, RotateCcw, Save, Search, Settings, Shield, SlidersHorizontal, Tags, Trash2, Upload
} from 'lucide-vue-next'
import Field from './components/Field.vue'

type ProviderConfig = {
  type: string; endpoint: string; api_key: string; secret_id: string; secret_key: string; region: string; biz_type: string;
  model: string; system_prompt: string; active_prompt_template_id: string; prompt_templates: PromptTemplate[];
  enable_few_shot: boolean; wrap_user_input: boolean; temperature: number; top_p: number;
  max_tokens: number; enable_search: boolean; enable_thinking: boolean; thinking_budget: number;
  enable_high_resolution_images?: boolean; timeout_ms: number; disabled: boolean; headers?: Record<string, string>
}
type CacheConfig = { enabled: boolean; allow_ttl_seconds: number; block_ttl_seconds: number }
type KeywordSet = { name: string; enabled: boolean; risk_domain: string; match_type: string; normalized: boolean; keywords: string[] }
type KeywordStat = { set_name: string; risk_domain: string; enabled: boolean; hit_count: number; audited_count: number; blocked_count: number; updated_at?: string }
type LabelMapping = { provider_label: string; target_category: string }
type PromptTemplate = { id: string; name: string; description: string; system_prompt: string }
type ProviderTestResult = { ok: boolean; latency_ms?: number; error?: string; result?: { action?: string; raw_summary?: string; latency_ms?: number } }
type PromptVersion = { id: number; description: string; system_prompt: string; actor: string; source_ip: string; created_at: string }
type SystemStats = {
  collected_at: string; version: Record<string, string>; uptime_seconds: number; process_rss_bytes: number;
  heap_alloc_bytes: number; heap_sys_bytes: number; runtime_sys_bytes: number; goroutines: number;
  database_bytes: number; database_wal_bytes: number; database_shm_bytes: number; data_directory: string;
  data_bytes: number; data_files: number; filesystem_total_bytes: number; filesystem_free_bytes: number;
  event_rows: number; decision_cache_rows: number; requests_total: number; blocks_total: number;
  fail_open_total: number; provider_p95_ms: number;
}
type UpdateStatus = { configured: boolean; image: string; channel: string; version: Record<string, string> }
type AdminTestResult = {
  request_id?: string; normalized_text?: string; keyword_hits?: any[]; sampled?: boolean; external_audited?: boolean;
  cache_hit?: boolean; provider?: string; provider_raw_summary?: string; category_scores?: Record<string, number>; final_response?: any;
  result_score_category?: string; result_score?: number; sub2api_block_threshold?: number;
  would_block_sub2api?: boolean; keyword_prefilter_enabled?: boolean; event?: any; adapter_request?: any; normalized_input?: any; provider_request?: any;
  upstream_request?: any; upstream_response?: any; upstream_note?: string; cache_note?: string;
}
type Config = {
  listen_addr: string; database_path: string; auth_tokens?: string[]; force_allow: boolean;
  max_body_bytes: number; direct_model_audit: boolean; miss_sample_rate: number; audit_on_keyword_hit: boolean; min_text_chars: number; max_text_chars: number;
  image_audit_mode: string; image_sample_rate: number; max_images_per_request: number; allow_data_url_image: boolean;
  decision_cache: CacheConfig; hash_salt: string; result_score_category: string; result_block_threshold: number;
  log_raw_input: boolean; keyword_sets: KeywordSet[]; provider_label_mapping: LabelMapping[]; provider: ProviderConfig;
  image_provider_enabled: boolean; image_provider: ProviderConfig;
  event_retention: number; event_retention_days: number; estimated_prompt_price_usd_per_1m: number;
  estimated_completion_price_usd_per_1m: number; estimated_cached_price_usd_per_1m: number;
}
type Status = { provider: string; force_allow: boolean; provider_disabled: boolean; image_provider_enabled?: boolean; image_provider_key_status?: string; cache: Record<string, number>; events?: { total: number; oldest?: string; newest?: string; retention_days: number; max_rows: number }; metrics: Record<string, number>; keyword_sets: number; keyword_stats?: KeywordStat[]; started_at: string; adapter_version: Record<string, string>; auth_token_configured: boolean; admin_login_mode?: string; hash_salt_configured: boolean; provider_key_status: string; production_warnings: string[]; database_path: string }

const headers = computed<Record<string, string>>(() => ({}))
const loggedIn = ref(false)
const loginUsername = ref('admin')
const loginPassword = ref('')
const loginError = ref('')
const loginBusy = ref(false)
const active = ref('overview')
const status = ref<Status | null>(null)
const config = ref<Config | null>(null)
const recommendedKeywordSets = ref<KeywordSet[]>([])
const events = ref<any[]>([])
const audits = ref<any[]>([])
const promptVersions = ref<PromptVersion[]>([])
const systemStats = ref<SystemStats | null>(null)
const updateStatus = ref<UpdateStatus | null>(null)
const systemNotice = ref('')
const systemBusy = ref(false)
const rawOutput = ref('')
const testResult = ref<AdminTestResult | null>(null)
const notice = ref('')
const testNotice = ref('')
const eventNotice = ref('')
const providerTestResult = ref<ProviderTestResult | null>(null)
const providerTesting = ref(false)
const imageProviderTestResult = ref<ProviderTestResult | null>(null)
const imageProviderTesting = ref(false)
const filterAction = ref('')
const eventPage = ref(1)
const eventPageSize = ref(20)
const eventTotal = ref(0)
const eventTotalPages = ref(1)
const testText = ref('我的 app 被人逆向了，我应该怎么加固？')
const testImage = ref('')
const busy = ref(false)
const adapterTokenInput = ref('')
const hashSaltInput = ref('')
const providerApiKeyInput = ref('')
const imageProviderApiKeyInput = ref('')
const providerEndpointPreset = ref('us-east-1-shared')
const providerWorkspaceId = ref('')
const imageEndpointPreset = ref('us-east-1-shared')
const imageWorkspaceId = ref('')
const providerModelPreset = ref('qwen3.6-flash-us')
const imageModelPreset = ref('qwen3-vl-flash-us')
const savedConfigSnapshot = ref('')

const navGroups = [
  { title: '日常查看', items: [['overview', '运行概览', Gauge]] },
  { title: '模型与认证', items: [['secrets', '密钥与认证', Shield], ['provider', '文本模型', KeyRound], ['imageProvider', '图片模型', Image], ['test', '链路测试', Beaker]] },
  { title: '风控策略', items: [['general', '总开关', Shield], ['sampling', '模型调用规则', SlidersHorizontal], ['keywords', '初筛关键词', Tags], ['mapping', '返回规则', Layers], ['images', '图片审核', Image], ['cache', '决策缓存', Database]] },
  { title: '记录与系统', items: [['events', '事件记录', Search], ['audits', '配置审计', FileClock], ['monitor', '系统监控', HardDrive], ['system', '系统维护', Settings]] }
] as const
const pageIntro: Record<string, { title: string; summary: string; impact: string }> = {
  overview: { title: '运行概览', summary: '看 Adapter 现在是否稳定：请求量、送审量、阻断量、故障放行、图片请求和缓存都在这里汇总。', impact: '这里只读，不会改变策略。先看这里判断系统是否能上线测试。' },
  provider: { title: '文本模型', summary: '选择文本审核模型、接入地域和系统提示词，并测试当前草稿能否稳定返回审核分数。', impact: '模型、地域或提示词变更会影响审核口径、延迟和成本。API Key 统一在“密钥与认证”中管理。' },
  imageProvider: { title: '图片模型', summary: '独立配置图片审核使用的阿里视觉模型。关闭时系统只审核文本，不会让文本模型读取图片 URL。', impact: '开启图片模型会增加请求延迟和成本；图片 Key 默认复用文本模型 Key，也可以在“密钥与认证”中单独配置。' },
  secrets: { title: '密钥与认证', summary: '集中管理 sub2api 调用认证、日志指纹盐值，以及文本和图片模型使用的供应商凭据。', impact: '新值只在输入时出现，保存后仅显示掩码；输入框留空表示保持现有密钥不变。' },
  test: { title: '链路测试', summary: '输入一段文本，观察它经过“归一化、缓存、文本审核模型打分、JSON 解析、最终返回”的完整过程。', impact: '这是排查页面，适合看最终分数、sub2api 阈值和是否会阻断；测试会写入事件记录。' },
  general: { title: '总开关', summary: '管理全局安全开关、请求大小、日志摘要和事件保留数量。', impact: '“紧急全量放行”风险最高，开启后即使命中风险也会放行。' },
  sampling: { title: '模型调用规则', summary: '决定是否先用本地关键词预筛，再把内容送到上游模型打分。', impact: '默认启用关键词预筛：命中关键词后调用模型，未命中按 0.3 抽样；关闭后达到文本长度的请求直接调用模型。' },
  keywords: { title: '初筛关键词', summary: '本地先扫一遍文本，命中这些词后通常会送文本审核模型打分。', impact: '关键词只决定是否送审；最终分数来自上游 confidence，并写入指定 category_scores 字段。' },
  mapping: { title: 'sub2api 返回规则', summary: '把上游返回的最终 confidence 写进一个指定的 category_scores 字段，其它分类字段保持放行分数。', impact: 'sub2api 按自己的阈值读取这个指定分数字段；链路测试页会显示该分数是否达到拦截阈值。' },
  images: { title: '图片审核', summary: '控制图片 URL 或 data URL 是否参与审核，以及图片请求的抽样和数量限制。', impact: '图片审核会增加调用成本；关闭后只处理文本。' },
  cache: { title: '决策缓存', summary: '相同内容短时间内复用上一次审核结果，减少延迟和上游模型调用次数。', impact: '缓存能省成本，但 prompt 或策略变更后可以清缓存，避免旧结果继续生效。' },
  events: { title: '事件记录', summary: '集中查看阻断、故障放行和模型禁用放行，正常放行只保留汇总指标。', impact: '这里可以手动清理事件；系统运行中会按留存天数和最多保留条数定时删除旧记录。' },
  audits: { title: '配置审计', summary: '查看谁在什么时候改了配置，以及改动摘要。', impact: '用于上线后追踪策略变更，敏感密钥不会显示明文。' },
  monitor: { title: '系统监控', summary: '查看进程内存、SQLite 数据、数据卷占用、磁盘余量、事件数量和关键运行指标。', impact: '监控数据只读；Docker stdout 日志由宿主机管理，不计入 Adapter 数据卷。' },
  system: { title: '系统维护', summary: '查看版本、执行在线更新、导入导出配置，或恢复推荐默认值。', impact: '在线更新会重启 Adapter；导入和恢复默认值会覆盖当前策略，操作前要确认。' }
}
const activeIntro = computed(() => pageIntro[active.value] || pageIntro.overview)
const activeGroupTitle = computed(() => navGroups.find(group => group.items.some(([id]) => id === active.value))?.title || '控制台')
const hasUnsavedChanges = computed(() => {
  const configChanged = !!config.value && JSON.stringify(config.value) !== savedConfigSnapshot.value
  const secretChanged = [adapterTokenInput, hashSaltInput, providerApiKeyInput, imageProviderApiKeyInput].some(item => item.value.trim())
  return configChanged || secretChanged
})
const imageKeySourceText = computed(() => {
  if (imageProviderApiKeyInput.value.trim()) return '独立图片 Key，待保存'
  if (config.value?.image_provider?.api_key?.trim()) return '独立图片 Key'
  return '复用文本模型 Key'
})
const actionOptions = [
  ['', '全部动作'],
  ['allow', '放行'],
  ['block', '阻断'],
  ['fail_open', '故障放行'],
  ['provider_disabled', '模型禁用放行'],
  ['force_allow', '全量放行']
] as const
const imageAuditOptions = [
  ['off', '关闭图片审核'],
  ['triggered', '文本命中风险时审核图片'],
  ['sampled', '按抽样率审核图片'],
  ['all', '所有图片都审核']
] as const
const customModelPreset = '__custom__'
type AliModelOption = { value: string; label: string; group: string; tier: string; description: string }
const modelOptionGroups = ['美国区推荐', '全球调度', '兼容旧模型'] as const
const aliTextModelOptions: AliModelOption[] = [
  { value: 'qwen3.6-flash-us', label: 'Qwen3.6 Flash US（推荐）', group: '美国区推荐', tier: '洛杉矶生产首选', description: '美国境内推理；新一代 Flash，在多语言理解、延迟和成本之间更均衡。' },
  { value: 'qwen3.7-plus-us', label: 'Qwen3.7 Plus US', group: '美国区推荐', tier: '准确率优先', description: '美国境内推理；复杂语义和少数语言更稳，但延迟与费用高于 Flash。' },
  { value: 'qwen-flash-us', label: 'Qwen Flash US', group: '美国区推荐', tier: '最低成本', description: '美国境内推理；速度快、价格低，模型较旧，少数语言误判风险相对更高。' },
  { value: 'qwen-plus-us', label: 'Qwen Plus US', group: '美国区推荐', tier: '稳定兼容', description: '美国境内推理；旧一代 Plus，适合已有验证数据的兼容场景。' },
  { value: 'qwen3.7-max-us', label: 'Qwen3.7 Max US', group: '美国区推荐', tier: '最高能力', description: '美国境内推理；能力最强，但审核 JSON 场景通常不值得承担额外延迟和费用。' },
  { value: 'qwen3.6-flash', label: 'Qwen3.6 Flash（Global）', group: '全球调度', tier: '全球 Flash', description: '由全球资源池调度；不保证推理留在美国，延迟和数据路径可能波动。' },
  { value: 'qwen3.7-plus', label: 'Qwen3.7 Plus（Global）', group: '全球调度', tier: '全球 Plus', description: '能力较强但由全球资源池调度；美国生产环境优先选择带 -us 的版本。' },
  { value: 'qwen-flash', label: 'Qwen Flash（Global）', group: '兼容旧模型', tier: '旧版 Flash', description: '保留给现有配置；新部署建议改用 qwen3.6-flash-us。' },
  { value: 'qwen-plus', label: 'Qwen Plus（Global）', group: '兼容旧模型', tier: '旧版 Plus', description: '保留给现有配置；美国部署建议改用 qwen3.7-plus-us。' },
  { value: 'qwen-max', label: 'Qwen Max（Global）', group: '兼容旧模型', tier: '旧版 Max', description: '成本和延迟较高，仅用于已有兼容需求。' },
  { value: 'qwen-turbo', label: 'Qwen Turbo（旧版）', group: '兼容旧模型', tier: '历史兼容', description: '历史模型，不建议用于新的审核部署。' }
]
const aliImageModelOptions: AliModelOption[] = [
  { value: 'qwen3-vl-flash-us', label: 'Qwen3-VL Flash US（推荐）', group: '美国区推荐', tier: '洛杉矶生产首选', description: '美国境内视觉推理；适合常规色情、暴力、深伪和截图内容审核。' },
  { value: 'qwen3-vl-flash', label: 'Qwen3-VL Flash（Global）', group: '全球调度', tier: '全球低成本', description: '由全球资源池调度；价格低，但美国服务器的延迟和数据路径不固定。' },
  { value: 'qwen3-vl-plus', label: 'Qwen3-VL Plus（Global）', group: '全球调度', tier: '视觉准确率优先', description: '复杂图片理解更强，但当前没有推荐的美国专用别名，延迟与费用更高。' },
  { value: 'qwen-vl-plus', label: 'Qwen-VL Plus（旧版）', group: '兼容旧模型', tier: '历史兼容', description: '保留给现有配置；新部署使用 Qwen3-VL。' },
  { value: 'qwen-vl-max', label: 'Qwen-VL Max（旧版）', group: '兼容旧模型', tier: '历史兼容', description: '保留给现有配置；新部署不建议选择。' }
]
const providerModelInfo = computed(() => aliTextModelOptions.find(option => option.value === providerModelPreset.value))
const imageModelInfo = computed(() => aliImageModelOptions.find(option => option.value === imageModelPreset.value))
function modelOptionsInGroup(options: AliModelOption[], group: string) {
  return options.filter(option => option.group === group)
}
type AliEndpointOption = { id: string; label: string; region: string; workspace: boolean; endpoint?: string }
const aliEndpointOptions: AliEndpointOption[] = [
  { id: 'cn-beijing-shared', label: '华北 2（北京）共享域名', region: 'cn-beijing', workspace: false, endpoint: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
  { id: 'cn-beijing-workspace', label: '华北 2（北京）Workspace 专属域名', region: 'cn-beijing', workspace: true },
  { id: 'ap-southeast-1-shared', label: '新加坡共享域名', region: 'ap-southeast-1', workspace: false, endpoint: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1' },
  { id: 'ap-southeast-1-workspace', label: '新加坡 Workspace 专属域名', region: 'ap-southeast-1', workspace: true },
  { id: 'cn-hongkong-shared', label: '中国香港共享域名', region: 'cn-hongkong', workspace: false, endpoint: 'https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1' },
  { id: 'cn-hongkong-workspace', label: '中国香港 Workspace 专属域名', region: 'cn-hongkong', workspace: true },
  { id: 'us-east-1-shared', label: '美国（弗吉尼亚，洛杉矶服务器推荐）共享域名', region: 'us-east-1', workspace: false, endpoint: 'https://dashscope-us.aliyuncs.com/compatible-mode/v1' },
  { id: 'eu-central-1-workspace', label: '德国（法兰克福）Workspace 专属域名', region: 'eu-central-1', workspace: true },
  { id: 'ap-northeast-1-workspace', label: '日本（东京）Workspace 专属域名', region: 'ap-northeast-1', workspace: true },
  { id: 'custom', label: '自定义 Base URL', region: '', workspace: false }
]
const riskDomainOptions = [
  ['cyber', '网络攻击'],
  ['credential', '账号凭证'],
  ['abuse', '账号/批量滥用'],
  ['sexual', '露骨色情'],
  ['violence', '人身风险'],
  ['self_harm', '自伤（兼容旧配置）']
] as const
const matchTypeOptions = [
  ['contains', '中文/短语包含（推荐）'],
  ['word_boundary', '英文完整词'],
  ['regex', '高级正则']
] as const
const categoryLabels: Record<string, string> = {
  none: '无风险分类',
  harassment: '骚扰',
  'harassment/threatening': '威胁性骚扰',
  hate: '仇恨',
  'hate/threatening': '威胁性仇恨',
  illicit: '违法/违规',
  'illicit/violent': '暴力违法',
  'self-harm': '自伤',
  'self-harm/intent': '自伤意图',
  'self-harm/instructions': '自伤指导',
  sexual: '色情/性内容',
  'sexual/minors': '未成年人性内容',
  violence: '暴力',
  'violence/graphic': '血腥暴力'
}
const scoreCategoryOptions = Object.entries(categoryLabels).filter(([key]) => key !== 'none')

const m = computed(() => status.value?.metrics || {})
const keywordStats = computed(() => status.value?.keyword_stats || [])
const cards = computed(() => [
  ['总请求', num(m.value.moderation_requests_total), '所有进入 Adapter 的 moderation 请求'],
  ['本地放行', num(m.value.moderation_local_allow_total), '未命中关键词且未抽样'],
  ['上游送审', num(sumPrefix('moderation_provider_calls_total')), '实际调用文本或图片审核模型'],
  ['阻断', num(sumPrefix('moderation_flagged_total')), '综合分数字段达到阻断口径'],
  ['故障放行', num(m.value.moderation_fail_open_total), '上游模型异常后放行'],
  ['图片请求', num(m.value.moderation_image_requests_total), '包含图片 URL 或 data URL 的请求'],
  ['图片送审', num(m.value.moderation_image_audit_total), '实际调用独立图片审核模型']
])
const effectiveRate = computed(() => {
  if (config.value?.direct_model_audit) return 1
  const hit = ratio(m.value.moderation_keyword_request_total || 0, m.value.moderation_requests_total || 0)
  const sample = config.value?.miss_sample_rate || 0
  return hit + (1 - hit) * sample
})
const keywordPrefilterEnabled = computed({
  get: () => !config.value?.direct_model_audit,
  set: (enabled: boolean) => {
    if (config.value) config.value.direct_model_audit = !enabled
  }
})
const activePromptTemplate = computed(() => {
  if (!config.value) return null
  const templates = ensurePromptTemplates(config.value.provider)
  return templates.find(t => t.id === config.value?.provider.active_prompt_template_id) || templates[0] || null
})

function sumPrefix(prefix: string) {
  return Object.entries(m.value).filter(([k]) => k.startsWith(prefix)).reduce((s, [, v]) => s + Number(v || 0), 0)
}
function ratio(a: number, b: number) { return b ? a / b : 0 }
function pct(v: number) { return `${(v * 100).toFixed(2)}%` }
function num(v: unknown) { return Number(v || 0).toLocaleString('zh-CN') }
function keywordBlockRate(item: KeywordStat) { return item.audited_count ? item.blocked_count / item.audited_count : 0 }
function riskDomainLabel(value: string) { return riskDomainOptions.find(([key]) => key === value)?.[1] || value || '-' }
function boolText(v: unknown) { return v ? '是' : '否' }
function dateText(v?: string) { return v && !v.startsWith('0001-') ? new Date(v).toLocaleString() : '-' }
function imageAuditLabel(value?: string) { return imageAuditOptions.find(([id]) => id === value)?.[1] || value || '未设置' }
function humanBytes(value: unknown) {
  let bytes = Number(value || 0)
  if (bytes < 1024) return `${bytes.toFixed(0)} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let unit = -1
  while (bytes >= 1024 && unit < units.length - 1) { bytes /= 1024; unit++ }
  return `${bytes.toFixed(bytes >= 100 ? 0 : bytes >= 10 ? 1 : 2)} ${units[unit]}`
}
function durationText(seconds: unknown) {
  const total = Math.max(0, Math.floor(Number(seconds || 0)))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  return days ? `${days}天 ${hours}小时` : hours ? `${hours}小时 ${minutes}分钟` : `${minutes}分钟`
}
function matchTypeHint(value: string) {
  if (value === 'regex') return '仅供熟悉 Go 正则的高级配置使用，例如 login\\s+failed；普通关键词不要选这个。'
  if (value === 'word_boundary') return '适合 sqli、rce、keygen 等英文术语；只有前后不是字母、数字或下划线时才命中。'
  return '适合中文和固定短语；出现在文本任意位置就触发模型送审，但不会直接判定违规。'
}
function keywordPlaceholder(value: string) {
  if (value === 'regex') return '每行一个 Go 正则表达式'
  if (value === 'word_boundary') return '每行一个英文术语或英文短语'
  return '每行一个中文关键词或固定短语'
}
function imageSampleRateHint(mode: string) {
  if (mode === 'sampled') return '对所有带图请求独立抽样，不区分是否命中关键词；0.05 表示随机审核 5%。'
  if (mode === 'triggered') return '命中关键词、文本抽样命中或直接文本送审时会审核图片；其余带图请求再按此比例补充抽样。'
  return mode === 'all' ? '所有带图请求都会审核，不使用抽样率。' : '图片审核关闭时不使用抽样率。'
}
function finalActionText(result: AdminTestResult | null) {
  if (!result) return '-'
  return result.would_block_sub2api ? '会阻断' : '会放行'
}
function finalActionHint(result: AdminTestResult | null) {
  if (!result) return ''
  const field = resultScoreField(result)
  const score = resultScoreValue(result).toFixed(2)
  const threshold = sub2apiThreshold(result).toFixed(2)
  if (result.would_block_sub2api) return `上游分数 ${score} 已写入 ${field}，达到 sub2api 阈值 ${threshold}。`
  return `上游分数 ${score} 已写入 ${field}，低于 sub2api 阈值 ${threshold}。`
}
function resultScoreCategory(result?: AdminTestResult | null) {
  return result?.result_score_category || config.value?.result_score_category || 'illicit'
}
function resultScoreField(result?: AdminTestResult | null) {
  return `results[0].category_scores.${resultScoreCategory(result)}`
}
function resultScoreValue(result?: AdminTestResult | null) {
  const category = resultScoreCategory(result)
  return Number(result?.result_score ?? result?.category_scores?.[category] ?? 0)
}
function sub2apiThreshold(result?: AdminTestResult | null) {
  return Number(result?.sub2api_block_threshold ?? config.value?.result_block_threshold ?? 0.95)
}
function resultScoreParam(result: AdminTestResult | null) {
  if (!result) return `${resultScoreField(result)}=-`
  return `${resultScoreField(result)}=${resultScoreValue(result).toFixed(2)}`
}
function resultScoreHint(result: AdminTestResult | null) {
  if (!result) return ''
  return `上游 confidence 原样写入，当前阈值 ${sub2apiThreshold(result).toFixed(2)}，其它分类字段保持 0。`
}
function flaggedReadable(result: AdminTestResult | null) {
  if (!result) return '-'
  return result.would_block_sub2api ? 'true（达到阈值）' : 'false（低于阈值）'
}
function resultScoreReadable(result: AdminTestResult | null) {
  if (!result) return '-'
  return `${resultScoreField(result)} = ${resultScoreValue(result).toFixed(2)}`
}
function topCategory(scores?: Record<string, number>) {
  const items = Object.entries(scores || {}).sort((a, b) => Number(b[1]) - Number(a[1]))
  const [name, score] = items[0] || ['-', 0]
  const value = Number(score || 0)
  return value > 0 ? { name, score: value } : { name: 'none', score: 0 }
}
function scoredCategories(scores?: Record<string, number>) {
  return Object.entries(scores || {})
    .sort((a, b) => Number(b[1]) - Number(a[1]))
    .filter(([, score]) => Number(score || 0) > 0)
    .slice(0, 8)
    .map(([name, score]) => ({ name, score: Number(score || 0) }))
}
function topCategoryHint(scores?: Record<string, number>) {
  const top = topCategory(scores)
  if (top.score <= 0) return '没有超过阈值的最终风险分数'
  return `分数 ${top.score.toFixed(2)}`
}
function categoryScoresText(scores?: Record<string, number>) {
  const items = scoredCategories(scores)
  if (!items.length) return '全部为 0，没有最终风险分类'
  return items.map(c => `${explainCategory(c.name)}: ${c.score.toFixed(2)}`).join('，')
}
function explainAction(action?: string) {
  switch (action) {
    case 'allow': return '放行'
    case 'block': return '阻断'
    case 'fail_open': return '故障放行'
    case 'provider_disabled': return '模型禁用放行'
    case 'force_allow': return '全量放行'
    default: return action || '-'
  }
}
function explainActionDetail(action?: string) {
  switch (action) {
    case 'allow': return '本地或模型判断可以通过'
    case 'block': return '当前策略判断应拦截'
    case 'fail_open': return '上游模型异常时按故障放行策略通过'
    case 'provider_disabled': return '上游模型被禁用，未调用模型'
    case 'force_allow': return '总开关开启后全部通过'
    default: return action || '-'
  }
}
function explainCategory(category?: string) {
  if (!category || category === '-') return '-'
  return categoryLabels[category] || category
}
function explainProvider(provider?: string) {
  switch (provider) {
    case 'image_chat_json': return '图片审核模型（OpenAI 兼容）'
    case 'image_qwen': return 'Qwen 图片审核模型'
    case 'image_openai_compatible': return '图片审核模型（OpenAI 兼容）'
    case 'image_http_json': return '图片审核模型（HTTP JSON）'
    case 'chat_json': return '文本审核模型（OpenAI 兼容）'
    case 'qwen': return 'Qwen 文本审核模型'
    case 'openai_compatible': return '文本审核模型（OpenAI 兼容）'
    case 'tencent_tms': return '旧版腾讯云内容安全'
    case 'mock': return '模拟上游（历史自测记录）'
    case 'http_json': return 'HTTP JSON 上游（历史配置）'
    case '':
    case undefined:
      return '本地处理，未调用模型'
    default:
      return provider
  }
}
function explainScore(category?: string, score?: unknown) {
  const value = Number(score || 0)
  if (value <= 0) return '无风险分类'
  const text = explainCategory(category)
  return text === '-' ? '-' : `${text} ${value.toFixed(2)}`
}
function formatJSON(value: any) {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}
function aliEndpointOption(id: string) {
  return aliEndpointOptions.find(option => option.id === id) || aliEndpointOptions[0]
}
function endpointWorkspaceRequired(id: string) {
  return aliEndpointOption(id).workspace
}
function endpointHint(id: string) {
  const option = aliEndpointOption(id)
  if (option.id === 'custom') return '手动填写完整 Base URL。'
  if (option.workspace) return `会生成 https://{WorkspaceId}.${option.region}.maas.aliyuncs.com/compatible-mode/v1`
  return option.id === 'us-east-1-shared' ? '美国弗吉尼亚入口，不需要 WorkspaceId；选择带 -us 的模型可将推理限制在美国。' : '共享域名，不需要 WorkspaceId。'
}
function buildAliEndpoint(preset: string, workspaceID: string) {
  const option = aliEndpointOption(preset)
  if (option.id === 'custom') return null
  if (!option.workspace) return option.endpoint || ''
  const workspace = workspaceID.trim()
  if (!workspace) return ''
  return `https://${workspace}.${option.region}.maas.aliyuncs.com/compatible-mode/v1`
}
function detectAliEndpoint(endpoint: string) {
  const clean = endpoint.trim().replace(/\/+$/, '')
  const workspaceMatch = clean.match(/^https:\/\/([^.\/]+)\.(cn-beijing|ap-southeast-1|cn-hongkong|eu-central-1|ap-northeast-1)\.maas\.aliyuncs\.com\/compatible-mode\/v1$/)
  if (workspaceMatch) {
    return { preset: `${workspaceMatch[2]}-workspace`, workspace: workspaceMatch[1] }
  }
  const matched = aliEndpointOptions.find(option => option.endpoint && clean === option.endpoint.replace(/\/+$/, ''))
  if (matched) return { preset: matched.id, workspace: '' }
  return { preset: 'custom', workspace: '' }
}
function detectModelPreset(model: string, options: AliModelOption[]) {
  return options.some(option => option.value === model) ? model : customModelPreset
}
function syncEndpointControlsFromConfig() {
  if (!config.value) return
  const provider = detectAliEndpoint(config.value.provider.endpoint || '')
  providerEndpointPreset.value = provider.preset
  providerWorkspaceId.value = provider.workspace
  const image = detectAliEndpoint(config.value.image_provider?.endpoint || '')
  imageEndpointPreset.value = image.preset
  imageWorkspaceId.value = image.workspace
  providerModelPreset.value = detectModelPreset(config.value.provider.model || '', aliTextModelOptions)
  imageModelPreset.value = detectModelPreset(config.value.image_provider.model || '', aliImageModelOptions)
}
function applyProviderEndpointPreset() {
  if (!config.value) return
  const endpoint = buildAliEndpoint(providerEndpointPreset.value, providerWorkspaceId.value)
  if (endpoint !== null) config.value.provider.endpoint = endpoint
}
function applyImageEndpointPreset() {
  if (!config.value) return
  const endpoint = buildAliEndpoint(imageEndpointPreset.value, imageWorkspaceId.value)
  if (endpoint !== null) config.value.image_provider.endpoint = endpoint
}
function applyProviderModelPreset() {
  if (!config.value) return
  if (providerModelPreset.value === customModelPreset) {
    if (aliTextModelOptions.some(option => option.value === config.value?.provider.model)) config.value.provider.model = ''
    return
  }
  config.value.provider.model = providerModelPreset.value
}
function applyImageModelPreset() {
  if (!config.value) return
  if (imageModelPreset.value === customModelPreset) {
    if (aliImageModelOptions.some(option => option.value === config.value?.image_provider.model)) config.value.image_provider.model = ''
    return
  }
  config.value.image_provider.model = imageModelPreset.value
}
function endpointInputReadonly(preset: string) {
  return preset !== 'custom'
}
async function api(path: string, init: RequestInit = {}) {
  const res = await fetch(path, { ...init, credentials: 'same-origin', headers: { ...headers.value, ...(init.headers || {}) } })
  if (res.status === 401) loggedIn.value = false
  if (!res.ok && res.status !== 409) throw new Error(await res.text())
  return res.json()
}
async function refresh(options: { reloadConfig?: boolean } = {}) {
  const reloadConfig = options.reloadConfig !== false
  const [nextStatus, configResult, eventResult, auditResult] = await Promise.all([
    api('/admin/api/status'),
    reloadConfig || !config.value ? api('/admin/api/config') : Promise.resolve(null),
    api(`/admin/api/events?page=${eventPage.value}&page_size=${eventPageSize.value}${filterAction.value ? `&action=${filterAction.value}` : ''}`),
    api('/admin/api/audits?limit=50')
  ])
  status.value = nextStatus
  loggedIn.value = true
  if (configResult) {
    config.value = configResult.config
    recommendedKeywordSets.value = Array.isArray(configResult.recommended_keyword_sets)
      ? JSON.parse(JSON.stringify(configResult.recommended_keyword_sets))
      : []
    if (config.value) ensurePromptTemplates(config.value.provider)
    if (config.value) ensureImageProviderDefaults(config.value)
    if (config.value) syncEndpointControlsFromConfig()
    savedConfigSnapshot.value = config.value ? JSON.stringify(config.value) : ''
  }
  events.value = eventResult.items || []
  eventPage.value = Number(eventResult.page || 1)
  eventPageSize.value = Number(eventResult.page_size || eventPageSize.value)
  eventTotal.value = Number(eventResult.total || 0)
  eventTotalPages.value = Math.max(1, Number(eventResult.total_pages || 1))
  audits.value = auditResult.items || []
  if (active.value === 'provider') await refreshPromptVersions()
  if (active.value === 'monitor' || active.value === 'system') await refreshSystemData()
}
async function refreshPromptVersions() {
  const result = await api('/admin/api/prompt/versions?limit=50')
  promptVersions.value = result.items || []
}
async function refreshSystemData() {
  const [stats, updater] = await Promise.all([
    api('/admin/api/system/stats'),
    api('/admin/api/system/update')
  ])
  systemStats.value = stats
  updateStatus.value = updater
}
async function openPage(id: string) {
  active.value = id
  if (id === 'provider') await refreshPromptVersions()
  if (id === 'monitor' || id === 'system') await refreshSystemData()
}
async function handleMobilePageChange() {
  await openPage(active.value)
}
async function manualRefresh() {
  if (hasUnsavedChanges.value && !confirm('当前有未保存的配置，刷新会丢弃这些改动。确认继续？')) return
  await refresh()
  notice.value = '页面数据已刷新'
}
async function login() {
  loginBusy.value = true
  loginError.value = ''
  try {
    await api('/admin/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: loginUsername.value.trim(), password: loginPassword.value })
    })
    loggedIn.value = true
    loginPassword.value = ''
    await refresh()
  } catch (err) {
    loginError.value = String(err).replace(/^Error:\s*/, '').trim() || '登录失败'
  } finally {
    loginBusy.value = false
  }
}
async function logout() {
  await api('/admin/api/logout', { method: 'POST' }).catch(() => null)
  loggedIn.value = false
  config.value = null
  recommendedKeywordSets.value = []
  status.value = null
  events.value = []
  audits.value = []
}
async function save(confirmRisk = false) {
  if (!config.value) return
  busy.value = true
  try {
    applySecretDrafts()
    syncActivePromptTemplate()
    const res = await api('/admin/api/config', {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ config: config.value, confirm_risk: confirmRisk, actor: 'admin-ui' })
    })
    clearSecretDrafts()
    config.value = res.config
    notice.value = '配置已保存并写入审计日志'
    await refresh()
  } catch (err) {
    notice.value = String(err)
  } finally { busy.value = false }
}
async function saveRisk() {
  if (!hasUnsavedChanges.value) return
  if (confirm('确认保存配置变更？高风险开关会立即影响后续请求。')) await save(true)
}
async function runTest() {
  testNotice.value = ''
  const images = testImage.value.trim() ? [testImage.value.trim()] : []
  const result = await api('/admin/api/test', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: testText.value, images }) })
  testResult.value = result
  rawOutput.value = JSON.stringify(result, null, 2)
  await refresh({ reloadConfig: false })
}
async function providerTest() {
  if (!config.value) return
  providerTesting.value = true
  providerTestResult.value = null
  try {
    providerTestResult.value = await api('/admin/api/provider/test', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ config: providerTestDraft() })
    })
  } catch (err) {
    providerTestResult.value = { ok: false, error: String(err) }
  } finally {
    providerTesting.value = false
  }
}
async function imageProviderTest() {
  if (!config.value) return
  imageProviderTesting.value = true
  imageProviderTestResult.value = null
  try {
    imageProviderTestResult.value = await api('/admin/api/image-provider/test', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ config: imageProviderTestDraft() })
    })
  } catch (err) {
    imageProviderTestResult.value = { ok: false, error: String(err) }
  } finally {
    imageProviderTesting.value = false
  }
}
async function clearCache(action = '') {
  const result = await api('/admin/api/cache/clear', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action }) })
  notice.value = `已清理 ${num(result.deleted)} 条决策缓存`
  rawOutput.value = JSON.stringify(result, null, 2)
  await refresh({ reloadConfig: false })
}
async function clearTestCache(rerun = false) {
  const result = await api('/admin/api/cache/clear', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: '' }) })
  testNotice.value = `已清理 ${num(result.deleted)} 条决策缓存${rerun ? '，并重新执行链路测试' : ''}`
  rawOutput.value = JSON.stringify(result, null, 2)
  await refresh({ reloadConfig: false })
  if (rerun) {
    await runTest()
    testNotice.value = `已清理 ${num(result.deleted)} 条决策缓存，并重新执行链路测试`
  }
}
async function pruneEvents() {
  const result = await api('/admin/api/events/prune', { method: 'POST' })
  eventNotice.value = `已清理过期日志 ${num(result.expired_deleted)} 条，超过数量上限的旧日志 ${num(result.overflow_deleted)} 条`
  await refresh({ reloadConfig: false })
}
async function clearEvents() {
  if (!confirm('确认清空全部事件记录？这个操作不会删除配置审计，但事件日志清空后无法在页面恢复。')) return
  const result = await api('/admin/api/events/clear', { method: 'POST' })
  eventNotice.value = `已清空事件记录 ${num(result.deleted)} 条`
  await refresh({ reloadConfig: false })
}
async function clearKeywordStats() {
  if (!confirm('确认清空全部关键词效果统计？这个操作不会修改关键词配置、事件记录或其它运行数据。')) return
  const result = await api('/admin/api/keyword-stats/clear', { method: 'POST' })
  if (status.value) status.value.keyword_stats = result.keyword_stats || []
  notice.value = `已清空 ${num(result.deleted)} 个关键词分组的历史统计`
}
async function changeEventFilter() {
  eventPage.value = 1
  await refresh({ reloadConfig: false })
}
async function changeEventPageSize() {
  eventPage.value = 1
  await refresh({ reloadConfig: false })
}
async function goToEventPage(page: number) {
  const next = Math.min(Math.max(1, page), eventTotalPages.value)
  if (next === eventPage.value) return
  eventPage.value = next
  await refresh({ reloadConfig: false })
}
async function copySub2APIToken() {
  try {
    let token = adapterTokenInput.value.trim()
    if (!token) token = (await api('/admin/api/secrets/sub2api-token', { method: 'POST' })).token || ''
    if (!token) throw new Error('sub2api 调用密钥尚未配置')
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(token)
    } else {
      const input = document.createElement('textarea')
      input.value = token
      input.style.position = 'fixed'
      input.style.opacity = '0'
      document.body.appendChild(input)
      input.select()
      document.execCommand('copy')
      input.remove()
    }
    notice.value = 'sub2api 调用密钥已复制'
  } catch (err) {
    notice.value = String(err).replace(/^Error:\s*/, '')
  }
}
async function restorePromptVersion(item: PromptVersion) {
  if (!confirm(`确认恢复 ${new Date(item.created_at).toLocaleString()} 的系统提示词？当前内容会自动保存为一个历史版本。`)) return
  const result = await api('/admin/api/prompt/restore', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: item.id })
  })
  config.value = result.config
  if (config.value) {
    ensurePromptTemplates(config.value.provider)
    syncEndpointControlsFromConfig()
    savedConfigSnapshot.value = JSON.stringify(config.value)
  }
  notice.value = '系统提示词已恢复，恢复前的内容已记入历史版本'
  await refreshPromptVersions()
}
async function triggerSystemUpdate() {
  if (!updateStatus.value?.configured) {
    systemNotice.value = '在线更新尚未配置，请先在部署环境中启用独立更新器。'
    return
  }
  if (!confirm(`确认从 ${updateStatus.value.image}:${updateStatus.value.channel} 拉取并更新？Adapter 可能短暂重启。`)) return
  systemBusy.value = true
  systemNotice.value = ''
  try {
    const result = await api('/admin/api/system/update', { method: 'POST' })
    systemNotice.value = result.message || '更新任务已提交'
  } catch (err) {
    systemNotice.value = String(err).replace(/^Error:\s*/, '')
  } finally {
    systemBusy.value = false
  }
}
function applySecretDrafts() {
  if (!config.value) return
  config.value.provider.type = 'chat_json'
  ensureImageProviderDefaults(config.value)
  applyProviderEndpointPreset()
  applyImageEndpointPreset()
  ensurePromptTemplates(config.value.provider)
  const adapterToken = adapterTokenInput.value.trim()
  const hashSalt = hashSaltInput.value.trim()
  const providerKey = providerApiKeyInput.value.trim()
  const imageProviderKey = imageProviderApiKeyInput.value.trim()
  if (adapterToken) config.value.auth_tokens = [adapterToken]
  if (hashSalt) config.value.hash_salt = hashSalt
  if (providerKey) config.value.provider.api_key = providerKey
  if (imageProviderKey) config.value.image_provider.api_key = imageProviderKey
}
function providerTestDraft() {
  if (!config.value) return null
  syncActivePromptTemplate()
  applyProviderEndpointPreset()
  const draft = JSON.parse(JSON.stringify(config.value)) as Config
  draft.provider.type = 'chat_json'
  const providerKey = providerApiKeyInput.value.trim()
  if (providerKey) draft.provider.api_key = providerKey
  return draft
}
function imageProviderTestDraft() {
  if (!config.value) return null
  applyImageEndpointPreset()
  const draft = JSON.parse(JSON.stringify(config.value)) as Config
  ensureImageProviderDefaults(draft)
  draft.image_provider.type = 'chat_json'
  const providerKey = providerApiKeyInput.value.trim()
  const imageProviderKey = imageProviderApiKeyInput.value.trim()
  if (providerKey) draft.provider.api_key = providerKey
  if (imageProviderKey) draft.image_provider.api_key = imageProviderKey
  return draft
}
function ensureImageProviderDefaults(draft: Config) {
  if (!draft.image_provider) {
    draft.image_provider = JSON.parse(JSON.stringify(draft.provider)) as ProviderConfig
  }
  draft.image_provider.type = 'chat_json'
  if (!draft.image_provider.endpoint) draft.image_provider.endpoint = draft.provider.endpoint || 'https://dashscope-us.aliyuncs.com/compatible-mode/v1'
  if (!draft.image_provider.model) draft.image_provider.model = 'qwen3-vl-flash-us'
  if (!draft.image_provider.timeout_ms) draft.image_provider.timeout_ms = 3000
  if (!draft.image_provider.max_tokens) draft.image_provider.max_tokens = 128
  if (!draft.image_provider.top_p) draft.image_provider.top_p = 1
  if (draft.image_provider.temperature === undefined || draft.image_provider.temperature === null) draft.image_provider.temperature = 0
  draft.image_provider.enable_few_shot = false
  draft.image_provider.wrap_user_input = true
  if (draft.image_provider.enable_high_resolution_images === undefined || draft.image_provider.enable_high_resolution_images === null) {
    draft.image_provider.enable_high_resolution_images = false
  }
  if (!draft.image_provider.thinking_budget) draft.image_provider.thinking_budget = 1
}
function ensurePromptTemplates(provider: ProviderConfig) {
  if (!provider.prompt_templates) provider.prompt_templates = []
  if (provider.prompt_templates.length === 0) {
    provider.prompt_templates.push({
      id: 'default-cyber',
      name: '默认综合内容审核',
      description: '自有资产操作放行；明确攻击他人、露骨色情和人身伤害风险给高分。',
      system_prompt: provider.system_prompt || ''
    })
  }
  if (!provider.active_prompt_template_id || !provider.prompt_templates.some(t => t.id === provider.active_prompt_template_id)) {
    provider.active_prompt_template_id = provider.prompt_templates[0].id
  }
  return provider.prompt_templates
}
function syncActivePromptTemplate() {
  if (!config.value) return
  const tpl = activePromptTemplate.value
  if (tpl) config.value.provider.system_prompt = tpl.system_prompt
}
function clearSecretDrafts() {
  adapterTokenInput.value = ''
  hashSaltInput.value = ''
  providerApiKeyInput.value = ''
  imageProviderApiKeyInput.value = ''
}
function addKeywordSet() { config.value?.keyword_sets.push({ name: '新分组', enabled: true, risk_domain: 'cyber', match_type: 'contains', normalized: true, keywords: [] }) }
function keywordText(set: KeywordSet) { return set.keywords.join('\n') }
function setKeywordText(set: KeywordSet, value: string) { set.keywords = [...new Set(value.split(/\r?\n/).map(s => s.trim()).filter(Boolean))] }
function loadRecommendedKeywordSets() {
  if (!config.value || recommendedKeywordSets.value.length === 0) {
    notice.value = '暂时无法读取推荐关键词，请刷新页面后重试'
    return
  }
  if (!confirm('载入推荐关键词会替换当前页面中的全部关键词分组，但不会修改模型、密钥、提示词或其它策略。确认继续？')) return
  config.value.keyword_sets = JSON.parse(JSON.stringify(recommendedKeywordSets.value))
  notice.value = '已载入推荐关键词；点击右上角保存后生效'
}
function exportConfig() {
  if (!config.value) return
  const blob = new Blob([JSON.stringify(config.value, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a'); a.href = url; a.download = 'sub2api-adapter-config.json'; a.click(); URL.revokeObjectURL(url)
}
async function importConfig(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0]; if (!file) return
  const imported = JSON.parse(await file.text())
  if (!confirm('导入配置会覆盖当前页面配置，确认继续？')) return
  const res = await api('/admin/api/config/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ config: imported, confirm_risk: true, actor: 'admin-ui-import' }) })
  config.value = res.config; await refresh()
}
async function resetDefaults() {
  if (!confirm('恢复推荐值会覆盖当前策略配置，确认继续？')) return
  config.value = (await api('/admin/api/config/reset', { method: 'POST' })).config
  await refresh()
}
onMounted(() => {
  refresh().catch(() => {
    loggedIn.value = false
  })
})
</script>

<template>
  <div v-if="!loggedIn" class="login-shell">
    <form class="login-panel" @submit.prevent="login">
      <div class="brand login-brand"><Shield :size="20" /><strong>Risk Adapter</strong></div>
      <h1>后台登录</h1>
      <p>使用部署时生成的用户名密码进入管理后台。</p>
      <Field label="用户名">
        <input v-model="loginUsername" autocomplete="username" />
      </Field>
      <Field label="密码">
        <input v-model="loginPassword" type="password" autocomplete="current-password" />
      </Field>
      <p class="field-hint">安装脚本会显示初始密码，并保存在部署目录的 .env 中。</p>
      <p v-if="loginError" class="login-error">{{ loginError }}</p>
      <button class="primary" :disabled="loginBusy" type="submit">{{ loginBusy ? '登录中' : '登录' }}</button>
    </form>
  </div>
  <div v-else class="shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark"><Shield :size="19" /></span>
        <span><strong>sub2api Adapter</strong><small>风控控制台</small></span>
      </div>
      <nav class="sidebar-nav" aria-label="管理功能">
        <div v-for="group in navGroups" :key="group.title" class="nav-group">
          <div class="nav-group-title">{{ group.title }}</div>
          <button v-for="[id, label, Icon] in group.items" :key="id" type="button" class="nav" :class="{ active: active === id }" :aria-current="active === id ? 'page' : undefined" @click="openPage(id)">
            <component :is="Icon" :size="17" /><span>{{ label }}</span>
          </button>
        </div>
      </nav>
      <select v-model="active" class="mobile-nav" aria-label="管理页面" @change="handleMobilePageChange">
        <optgroup v-for="group in navGroups" :key="group.title" :label="group.title">
          <option v-for="[id, label] in group.items" :key="id" :value="id">{{ label }}</option>
        </optgroup>
      </select>
      <div class="sidebar-status">
        <span class="status-dot" :class="status?.force_allow ? 'danger' : 'ok'"></span>
        <span><strong>{{ status?.force_allow ? '紧急放行中' : '策略服务正常' }}</strong><small>{{ explainProvider(status?.provider) }}</small></span>
      </div>
    </aside>

    <main class="content">
      <header class="topbar">
        <div class="topbar-context">
          <strong>sub2api 风控审计</strong>
          <span>/</span>
          <span>{{ activeIntro.title }}</span>
        </div>
        <div class="actions">
          <span class="pill" :class="status?.force_allow ? 'danger' : 'ok'">{{ status?.force_allow ? '全量放行' : '正常策略' }}</span>
          <span v-if="hasUnsavedChanges" class="unsaved-indicator">有未保存更改</span>
          <button class="icon-button" type="button" title="刷新页面数据" aria-label="刷新页面数据" @click="manualRefresh"><RefreshCw :size="16" /></button>
          <button class="primary" type="button" :disabled="busy || !hasUnsavedChanges" @click="saveRisk"><Save :size="16" />{{ busy ? '保存中' : (hasUnsavedChanges ? '保存更改' : '已保存') }}</button>
          <button class="icon-button" type="button" title="退出登录" aria-label="退出登录" @click="logout"><LogOut :size="16" /></button>
        </div>
      </header>

      <p v-if="notice" class="notice">{{ notice }}</p>
      <div v-if="status?.production_warnings?.length" class="warning-list">
        <AlertTriangle :size="17" />
        <div>
          <strong>上线前警告</strong>
          <p v-for="warning in status.production_warnings" :key="warning">{{ warning }}</p>
        </div>
      </div>
      <section class="page-heading">
        <div>
          <span class="page-scope">{{ activeGroupTitle }}</span>
          <h1>{{ activeIntro.title }}</h1>
          <p>{{ activeIntro.summary }}</p>
        </div>
        <div class="impact-note"><AlertTriangle :size="17" /><div><strong>配置影响</strong><p>{{ activeIntro.impact }}</p></div></div>
      </section>

      <section v-if="active === 'overview'" class="section">
        <div class="metric-grid">
          <article v-for="[label, value, hint] in cards" :key="label" class="metric"><b>{{ label }}</b><span>{{ value }}</span><small>{{ hint }}</small></article>
        </div>
        <div class="panel-row">
          <div class="panel"><h2>模型调用率估算</h2><strong>{{ pct(effectiveRate) }}</strong><p>关键词命中率 + 未命中抽样率组合后的上游模型调用比例。</p></div>
          <div class="panel"><h2>缓存</h2><strong>{{ status?.cache?.total || 0 }}</strong><p>放行 {{ status?.cache?.allow || 0 }} · 阻断 {{ status?.cache?.block || 0 }}</p></div>
          <div class="panel"><h2>密钥状态</h2><strong>{{ status?.provider_key_status || '未配置' }}</strong><p>密钥可在后台配置，但页面不会展示明文。</p></div>
        </div>
        <div class="overview-guide">
          <h2>功能区说明</h2>
          <div class="explain-grid">
            <div class="explain-item"><strong>模型与认证</strong><p>集中配置调用密钥、文本模型和图片模型，再通过链路测试确认最终返回。</p></div>
            <div class="explain-item"><strong>风控策略</strong><p>决定哪些内容会调用模型、命中哪些关键词、返回给 sub2api 什么结果，以及缓存怎么控制。</p></div>
            <div class="explain-item"><strong>记录与系统</strong><p>查看每条请求、每次配置变更、版本和导入导出。出问题时先看事件记录和配置审计。</p></div>
          </div>
        </div>
      </section>

      <template v-if="config">
        <section v-if="active === 'general'" class="section form-grid">
          <div class="wide subsection-heading"><div><h2>全局处理</h2><p>控制所有 moderation 请求的最高优先级行为和请求大小限制。</p></div></div>
          <Field class="danger-setting" label="紧急全量放行">
            <input type="checkbox" v-model="config.force_allow" />
            <small class="field-hint">仅用于上游异常或紧急排障。开启后所有审核结果都会放行。</small>
          </Field>
          <Field label="请求体上限 bytes"><input type="number" v-model.number="config.max_body_bytes" /></Field>
          <div class="wide subsection-heading"><div><h2>事件留存</h2><p>只持久化阻断、故障放行和模型禁用放行；正常放行仅进入运行指标。</p></div></div>
          <Field label="记录脱敏输入摘要"><input type="checkbox" v-model="config.log_raw_input" /></Field>
          <Field label="事件最多保留条数"><input type="number" min="1" v-model.number="config.event_retention" /></Field>
          <Field label="事件留存天数"><input type="number" min="1" max="3650" v-model.number="config.event_retention_days" /></Field>
          <div class="wide subsection-heading"><div><h2>运行路径</h2><p>由部署环境决定，只读展示，不能在页面中修改。</p></div></div>
          <Field label="Adapter 监听地址"><input v-model="config.listen_addr" disabled /></Field>
          <Field label="SQLite 路径"><input v-model="config.database_path" disabled /></Field>
        </section>

        <section v-if="active === 'provider'" class="section form-grid">
          <div class="wide connection-summary">
            <div><span>供应商</span><strong>阿里云百炼</strong></div>
            <div><span>当前模型</span><strong>{{ config.provider.model || '未选择' }}</strong></div>
            <div><span>API Key</span><strong>{{ status?.provider_key_status || '未配置' }}</strong></div>
            <button type="button" @click="active = 'secrets'"><KeyRound :size="16" />管理密钥</button>
          </div>
          <Field label="阿里接入地域">
            <select v-model="providerEndpointPreset" @change="applyProviderEndpointPreset">
              <option v-for="option in aliEndpointOptions" :key="option.id" :value="option.id">{{ option.label }}</option>
            </select>
            <small class="field-hint">{{ endpointHint(providerEndpointPreset) }}</small>
          </Field>
          <Field v-if="endpointWorkspaceRequired(providerEndpointPreset)" label="WorkspaceId">
            <input v-model.trim="providerWorkspaceId" autocomplete="off" spellcheck="false" placeholder="例如 llm-xxxxxx" @input="applyProviderEndpointPreset" />
            <small class="field-hint">只填 WorkspaceId，系统会自动拼出当前地域的完整 Base URL。</small>
          </Field>
          <Field label="Base URL">
            <input v-model="config.provider.endpoint" :readonly="endpointInputReadonly(providerEndpointPreset)" autocomplete="off" spellcheck="false" placeholder="https://dashscope-us.aliyuncs.com/compatible-mode/v1" />
          </Field>
          <Field label="文本审核模型">
            <select v-model="providerModelPreset" @change="applyProviderModelPreset">
              <optgroup v-for="group in modelOptionGroups" :key="group" :label="group">
                <option v-for="option in modelOptionsInGroup(aliTextModelOptions, group)" :key="option.value" :value="option.value">{{ option.label }}</option>
              </optgroup>
              <option :value="customModelPreset">自定义模型名</option>
            </select>
            <span v-if="providerModelInfo" class="model-guidance"><strong>{{ providerModelInfo.tier }}</strong><span>{{ providerModelInfo.description }}</span></span>
            <small v-else class="field-hint">模型可用范围取决于所选地域；自定义模型请确认部署范围和 API 权限。</small>
          </Field>
          <Field v-if="providerModelPreset === customModelPreset" label="自定义模型名">
            <input v-model.trim="config.provider.model" autocomplete="off" spellcheck="false" placeholder="输入阿里模型 ID" />
          </Field>
          <Field class="danger-setting" label="禁用上游文本模型">
            <input type="checkbox" v-model="config.provider.disabled" />
            <small class="field-hint">开启后文本请求不再调用上游模型，并按故障放行策略处理。</small>
          </Field>
          <details class="settings-disclosure wide">
            <summary><span><strong>高级模型参数</strong><small>采样参数、输出限制和提示词增强</small></span></summary>
            <div class="parameter-grid">
              <Field label="temperature"><input type="number" step="0.01" min="0" max="2" v-model.number="config.provider.temperature" /></Field>
              <Field label="top_p"><input type="number" step="0.01" min="0" max="1" v-model.number="config.provider.top_p" /></Field>
              <Field label="最大输出 tokens"><input type="number" min="64" v-model.number="config.provider.max_tokens" /></Field>
              <Field label="超时 ms"><input type="number" v-model.number="config.provider.timeout_ms" /></Field>
              <Field label="启用 few-shot 示例"><input type="checkbox" v-model="config.provider.enable_few_shot" /><small class="field-hint">建议开启；内置示例区分自有资产、攻击他人、露骨色情和医学解剖。</small></Field>
              <Field label="包裹 user_input"><input type="checkbox" v-model="config.provider.wrap_user_input" /></Field>
              <Field label="启用联网搜索"><input type="checkbox" v-model="config.provider.enable_search" /></Field>
              <Field label="启用显式思考"><input type="checkbox" v-model="config.provider.enable_thinking" /></Field>
              <Field label="思考预算"><input type="number" min="1" v-model.number="config.provider.thinking_budget" :disabled="!config.provider.enable_thinking" /></Field>
            </div>
          </details>
          <div class="wide prompt-editor" v-if="activePromptTemplate">
            <div class="prompt-editor-head">
              <div><h2>系统提示词</h2><p>系统只使用这一套提示词。每次保存修改前的内容都会自动进入历史版本。</p></div>
              <button type="button" title="刷新提示词历史" @click="refreshPromptVersions"><RefreshCw :size="16" />刷新历史</button>
            </div>
            <Field label="说明"><input v-model="activePromptTemplate.description" spellcheck="false" placeholder="说明这套审核口径的用途" /></Field>
            <Field class="wide" label="系统提示词">
              <textarea v-model="activePromptTemplate.system_prompt" spellcheck="false"></textarea>
            </Field>
            <p>保存后立即用于上游文本审核模型的 system 消息。历史版本不会参与当前请求。</p>
            <details class="prompt-history">
              <summary><span><History :size="16" />历史版本</span><strong>{{ promptVersions.length }}</strong></summary>
              <div v-if="promptVersions.length" class="history-list">
                <div v-for="item in promptVersions" :key="item.id" class="history-row">
                  <div><strong>{{ dateText(item.created_at) }}</strong><small>{{ item.description || '无说明' }} · {{ item.actor || 'admin' }}</small></div>
                  <p>{{ item.system_prompt.slice(0, 140) || '空提示词' }}{{ item.system_prompt.length > 140 ? '…' : '' }}</p>
                  <button type="button" @click="restorePromptVersion(item)"><RotateCcw :size="16" />恢复</button>
                </div>
              </div>
              <div v-else class="empty-state compact-empty">还没有历史版本。首次修改并保存后，修改前的内容会出现在这里。</div>
            </details>
          </div>
          <div class="wide inline-actions">
            <button :disabled="providerTesting" @click="providerTest">
              <CheckCircle2 :size="16" />{{ providerTesting ? '测试中' : '连通性测试' }}
            </button>
          </div>
          <div v-if="providerTestResult" class="wide test-result" :class="providerTestResult.ok ? 'success' : 'error'">
            <div class="result-head">
              <CheckCircle2 v-if="providerTestResult.ok" :size="17" />
              <AlertTriangle v-else :size="17" />
              <strong>{{ providerTestResult.ok ? '连通性测试成功' : '连通性测试失败' }}</strong>
              <span v-if="providerTestResult.latency_ms !== undefined">{{ providerTestResult.latency_ms }} ms</span>
            </div>
            <p v-if="providerTestResult.ok">
              上游模型已返回审核结果：{{ providerTestResult.result?.action || 'pass' }}。
              {{ providerTestResult.result?.raw_summary || '接口调用正常。' }}
            </p>
            <p v-else>{{ providerTestResult.error || '接口调用失败，请检查 API Key、Base URL、模型名和网络。' }}</p>
          </div>
        </section>

        <section v-if="active === 'imageProvider'" class="section form-grid">
          <Field label="启用独立图片模型">
            <input type="checkbox" v-model="config.image_provider_enabled" />
            <small class="field-hint">关闭时不做视觉审核，也不会把图片 URL 发给文本模型；开启后，命中图片审核策略的请求才使用下方模型。</small>
          </Field>
          <div class="wide callout">
            <AlertTriangle :size="17" />
            <span>{{ config.image_provider_enabled ? '图片模型已开启。实际是否调用由“图片审核”策略决定。' : '图片模型当前关闭，下方配置已停用，系统只审核文本。' }}</span>
            <button v-if="config.image_provider_enabled" type="button" class="callout-action" @click="active = 'images'">查看图片审核策略</button>
          </div>
          <div class="wide connection-summary compact">
            <div><span>供应商</span><strong>阿里云百炼</strong></div>
            <div><span>当前模型</span><strong>{{ config.image_provider.model || '未选择' }}</strong></div>
            <div><span>密钥来源</span><strong>{{ imageKeySourceText }}</strong></div>
            <button type="button" @click="active = 'secrets'"><KeyRound :size="16" />管理密钥</button>
          </div>
          <fieldset class="dependent-settings wide" :disabled="!config.image_provider_enabled">
            <legend>图片模型连接配置</legend>
            <div class="form-grid">
              <Field label="阿里接入地域">
                <select v-model="imageEndpointPreset" @change="applyImageEndpointPreset">
                  <option v-for="option in aliEndpointOptions" :key="option.id" :value="option.id">{{ option.label }}</option>
                </select>
                <small class="field-hint">{{ endpointHint(imageEndpointPreset) }}</small>
              </Field>
              <Field v-if="endpointWorkspaceRequired(imageEndpointPreset)" label="WorkspaceId">
                <input v-model.trim="imageWorkspaceId" autocomplete="off" spellcheck="false" placeholder="例如 llm-xxxxxx" @input="applyImageEndpointPreset" />
                <small class="field-hint">只填 WorkspaceId，系统会自动拼出当前地域的完整 Base URL。</small>
              </Field>
              <Field label="Base URL"><input v-model="config.image_provider.endpoint" :readonly="endpointInputReadonly(imageEndpointPreset)" autocomplete="off" spellcheck="false" placeholder="https://dashscope-us.aliyuncs.com/compatible-mode/v1" /></Field>
              <Field label="图片审核模型">
                <select v-model="imageModelPreset" @change="applyImageModelPreset">
                  <optgroup v-for="group in modelOptionGroups" :key="group" :label="group">
                    <option v-for="option in modelOptionsInGroup(aliImageModelOptions, group)" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </optgroup>
                  <option :value="customModelPreset">自定义模型名</option>
                </select>
                <span v-if="imageModelInfo" class="model-guidance"><strong>{{ imageModelInfo.tier }}</strong><span>{{ imageModelInfo.description }}</span></span>
                <small v-else class="field-hint">自定义视觉模型必须支持 OpenAI-compatible image_url 输入和 JSON 输出。</small>
              </Field>
              <Field v-if="imageModelPreset === customModelPreset" label="自定义模型名"><input v-model.trim="config.image_provider.model" autocomplete="off" spellcheck="false" placeholder="输入阿里视觉模型 ID" /></Field>
              <details class="settings-disclosure wide">
                <summary><span><strong>高级图片模型参数</strong><small>高清模式、采样参数和输出限制</small></span></summary>
                <div class="parameter-grid">
                  <Field label="temperature"><input type="number" step="0.01" min="0" max="2" v-model.number="config.image_provider.temperature" /></Field>
                  <Field label="top_p"><input type="number" step="0.01" min="0" max="1" v-model.number="config.image_provider.top_p" /></Field>
                  <Field label="最大输出 tokens"><input type="number" min="64" v-model.number="config.image_provider.max_tokens" /></Field>
                  <Field label="超时 ms"><input type="number" v-model.number="config.image_provider.timeout_ms" /></Field>
                  <Field label="启用高清图片审核"><input type="checkbox" v-model="config.image_provider.enable_high_resolution_images" /><small class="field-hint">适合小字、截图和二维码；会增加成本和延迟。</small></Field>
                  <Field label="启用联网搜索"><input type="checkbox" v-model="config.image_provider.enable_search" /></Field>
                  <Field label="启用显式思考"><input type="checkbox" v-model="config.image_provider.enable_thinking" /></Field>
                  <Field label="思考预算"><input type="number" min="1" v-model.number="config.image_provider.thinking_budget" :disabled="!config.image_provider.enable_thinking" /></Field>
                </div>
              </details>
              <div class="wide inline-actions"><button :disabled="imageProviderTesting" @click="imageProviderTest"><CheckCircle2 :size="16" />{{ imageProviderTesting ? '测试中' : '测试图片模型连接' }}</button></div>
            </div>
          </fieldset>
          <div v-if="imageProviderTestResult" class="wide test-result" :class="imageProviderTestResult.ok ? 'success' : 'error'">
            <div class="result-head">
              <CheckCircle2 v-if="imageProviderTestResult.ok" :size="17" />
              <AlertTriangle v-else :size="17" />
              <strong>{{ imageProviderTestResult.ok ? '图片模型连通性测试成功' : '图片模型连通性测试失败' }}</strong>
              <span v-if="imageProviderTestResult.latency_ms !== undefined">{{ imageProviderTestResult.latency_ms }} ms</span>
            </div>
            <p v-if="imageProviderTestResult.ok">
              图片模型已返回审核结果：{{ imageProviderTestResult.result?.action || 'pass' }}。
              {{ imageProviderTestResult.result?.raw_summary || '接口调用正常。' }}
            </p>
            <p v-else>{{ imageProviderTestResult.error || '接口调用失败，请检查 API Key、Base URL、模型名，以及该模型是否支持 image_url。' }}</p>
          </div>
        </section>

        <section v-if="active === 'secrets'" class="section form-grid">
          <div class="wide secret-status-strip">
            <div><span>sub2api 调用认证</span><strong>{{ status?.auth_token_configured ? '已配置' : '未配置' }}</strong></div>
            <div><span>文本模型 Key</span><strong>{{ status?.provider_key_status || '未配置' }}</strong></div>
            <div><span>图片模型 Key</span><strong>{{ imageKeySourceText }}</strong></div>
            <div><span>风险哈希盐</span><strong>{{ status?.hash_salt_configured ? '已配置' : '未配置' }}</strong></div>
          </div>
          <div class="wide credential-group">
            <div class="subsection-heading">
              <div><h2>Adapter 调用认证</h2><p>sub2api 请求 moderation 接口时使用的 Bearer Token，以及事件指纹使用的稳定盐值。</p></div>
            </div>
            <div class="form-grid compact">
              <div class="field">
                <span>sub2api 调用密钥</span>
                <span class="input-with-action">
                  <input v-model="adapterTokenInput" name="sub2api-adapter-bearer-token" type="password" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="config.auth_tokens?.[0] || '输入新的 Bearer Token'" />
                  <button type="button" class="icon-button" title="复制 sub2api 调用密钥" aria-label="复制 sub2api 调用密钥" @click="copySub2APIToken"><Copy :size="16" /></button>
                </span>
                <small class="field-hint">修改后需要同步更新 sub2api 风控中心中的 Adapter 调用密钥。</small>
              </div>
              <Field label="风险哈希盐">
                <input v-model="hashSaltInput" name="sub2api-adapter-hash-salt" type="password" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="config.hash_salt || '输入稳定随机盐值'" />
                <small class="field-hint">用于生成不可逆内容指纹；修改后，相同内容会产生新的事件指纹和缓存键。</small>
              </Field>
            </div>
          </div>
          <div class="wide credential-group">
            <div class="subsection-heading">
              <div><h2>阿里云百炼凭据</h2><p>文本和图片模型可以使用同一个 Key；只有图片模型使用不同账号时才需要填写独立 Key。</p></div>
            </div>
            <div class="form-grid compact">
              <Field label="文本模型 API Key">
                <input v-model="providerApiKeyInput" name="sub2api-adapter-upstream-api-key" type="password" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="config.provider.api_key || '输入阿里云百炼 API Key'" />
                <small class="field-hint">文本审核必须配置。图片独立 Key 留空时也会复用这个 Key。</small>
              </Field>
              <Field label="图片模型独立 API Key（可选）">
                <input v-model="imageProviderApiKeyInput" name="sub2api-adapter-image-provider-api-key" type="password" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="config.image_provider.api_key || '留空则复用文本模型 API Key'" />
                <small class="field-hint">当前：{{ imageKeySourceText }}。新输入会覆盖图片模型原有独立 Key。</small>
              </Field>
            </div>
          </div>
        </section>

        <section v-if="active === 'sampling'" class="section form-grid">
          <Field label="启用关键词预筛">
            <input type="checkbox" v-model="keywordPrefilterEnabled" />
            <small class="field-hint">开启后先跑本地关键词；关闭后不看关键词和抽样率，达到文本长度就直接调用上游模型。</small>
          </Field>
          <Field label="未命中抽样率">
            <input type="number" step="0.001" min="0" max="1" v-model.number="config.miss_sample_rate" :disabled="!keywordPrefilterEnabled" />
            <small class="field-hint">只在关键词预筛开启时生效；0.3 表示未命中关键词的请求抽 30% 送模型。</small>
          </Field>
          <Field label="命中关键词后调用模型">
            <input type="checkbox" v-model="config.audit_on_keyword_hit" :disabled="!keywordPrefilterEnabled" />
            <small class="field-hint">只在关键词预筛开启时生效。</small>
          </Field>
          <Field label="最小文本长度"><input type="number" v-model.number="config.min_text_chars" /></Field>
          <Field label="最大文本长度"><input type="number" v-model.number="config.max_text_chars" /></Field>
          <div class="wide callout">
            <AlertTriangle :size="17" />
            预计模型调用率 {{ pct(effectiveRate) }}。{{ keywordPrefilterEnabled ? '当前先用关键词预筛，未命中内容按抽样率送上游打分。' : '当前跳过关键词预筛，达到文本长度的内容直接送上游打分。' }}最终分数来自上游 confidence。
          </div>
        </section>

        <section v-if="active === 'images'" class="section form-grid">
          <div class="wide connection-summary">
            <div><span>图片模型</span><strong>{{ config.image_provider_enabled ? '已启用' : '未启用' }}</strong></div>
            <div><span>当前模型</span><strong>{{ config.image_provider.model || '未选择' }}</strong></div>
            <div><span>当前策略</span><strong>{{ imageAuditLabel(config.image_audit_mode) }}</strong></div>
            <button type="button" @click="active = 'imageProvider'"><Image :size="16" />配置图片模型</button>
          </div>
          <Field label="图片审核策略">
            <select v-model="config.image_audit_mode" :disabled="!config.image_provider_enabled">
              <option v-for="[value, label] in imageAuditOptions" :key="value" :value="value">{{ label }}</option>
            </select>
            <small v-if="!config.image_provider_enabled" class="field-hint">请先到“图片模型”开启独立图片模型。</small>
            <small v-else class="field-hint">{{ imageSampleRateHint(config.image_audit_mode) }}</small>
          </Field>
          <Field label="图片补充抽样率">
            <input type="number" step="0.001" min="0" max="1" v-model.number="config.image_sample_rate" :disabled="!config.image_provider_enabled || !['triggered', 'sampled'].includes(config.image_audit_mode)" />
            <small class="field-hint">{{ imageSampleRateHint(config.image_audit_mode) }}</small>
          </Field>
          <Field label="最大图片数"><input type="number" v-model.number="config.max_images_per_request" :disabled="!config.image_provider_enabled || config.image_audit_mode === 'off'" /></Field>
          <Field label="允许 data URL"><input type="checkbox" v-model="config.allow_data_url_image" :disabled="!config.image_provider_enabled || config.image_audit_mode === 'off'" /></Field>
        </section>

        <section v-if="active === 'cache'" class="section form-grid">
          <Field label="启用决策缓存"><input type="checkbox" v-model="config.decision_cache.enabled" /></Field>
          <Field label="放行结果 TTL 秒"><input type="number" v-model.number="config.decision_cache.allow_ttl_seconds" :disabled="!config.decision_cache.enabled" /></Field>
          <Field label="阻断结果 TTL 秒"><input type="number" v-model.number="config.decision_cache.block_ttl_seconds" :disabled="!config.decision_cache.enabled" /></Field>
          <div class="wide inline-actions"><button @click="clearCache('allow')">清放行缓存</button><button @click="clearCache('block')">清阻断缓存</button><button @click="clearCache('')">清全部缓存</button></div>
        </section>

        <section v-if="active === 'mapping'" class="section">
          <div class="explain-grid">
            <div class="explain-item"><strong>sub2api 最终取哪个值</strong><p>取最终响应里的一个指定分数字段：results[0].category_scores.{{ config.result_score_category || 'illicit' }}。</p></div>
            <div class="explain-item"><strong>为什么只写一个字段</strong><p>上游已经完成综合打分，sub2api 不需要理解内部分类；其它分类分数统一保持 0，避免误读。</p></div>
            <div class="explain-item"><strong>上游模型要返回什么</strong><p>当前文本审核模型只需要返回 confidence 和 reason；Adapter 会把 confidence 原样写入指定字段。</p></div>
          </div>

          <div class="return-rule-grid">
            <div class="return-rule-card">
              <b>综合分数字段</b>
              <span class="mono">results[0].category_scores.{{ config.result_score_category || 'illicit' }}</span>
              <small>sub2api 的阈值规则只需要看这个字段。</small>
            </div>
            <div class="return-rule-card">
              <b>其它分类字段</b>
              <span>全部 0</span>
              <small>不再按色情、暴力、网络攻击等内部分类分别写分。</small>
            </div>
            <div class="return-rule-card">
              <b>sub2api 阻断阈值</b>
              <span>{{ Number(config.result_block_threshold || 0.95).toFixed(2) }}</span>
              <small>用于同步 sub2api 当前配置；不会反向修改 sub2api。</small>
            </div>
          </div>

          <div class="form-grid compact threshold-panel">
            <Field label="综合结果写入字段">
              <select v-model="config.result_score_category">
                <option v-for="[value, label] in scoreCategoryOptions" :key="value" :value="value">{{ label }} / {{ value }}</option>
              </select>
              <small class="field-hint">建议保持 illicit；Adapter 会把上游 confidence 直接写到这个字段。</small>
            </Field>
            <Field label="sub2api 阻断阈值">
              <input type="number" step="0.01" min="0.01" max="1" v-model.number="config.result_block_threshold" />
              <small class="field-hint">必须与 sub2api 对该字段配置的阈值保持一致；用于链路测试、flagged 返回值和运行概览阻断统计。</small>
            </Field>
          </div>

          <details class="advanced-block">
            <summary>查看当前固定返回结构</summary>
            <pre>{
  "results": [
    {
      "flagged": false,
      "categories": {
        "{{ config.result_score_category || 'illicit' }}": false,
        "sexual": false,
        "violence": false
      },
      "category_scores": {
        "{{ config.result_score_category || 'illicit' }}": 0.87,
        "sexual": 0,
        "violence": 0
      }
    }
  ]
}</pre>
          </details>
        </section>

        <section v-if="active === 'keywords'" class="section">
          <div class="callout keyword-callout">
            <Tags :size="18" />
            <span><strong>关键词只决定是否送审</strong>命中后仍由模型结合上下文打分，不会因为一个关键词直接阻断。</span>
          </div>
          <div class="explain-grid">
            <div class="explain-item"><strong>中文或固定短语</strong><p>选择“中文/短语包含（推荐）”。适合逆向、未授权访问、未成年色情等中文语义词。</p></div>
            <div class="explain-item"><strong>英文安全术语</strong><p>选择“英文完整词”。适合 sqli、rce、keygen，避免在较长英文单词内部误命中。</p></div>
            <div class="explain-item"><strong>复杂格式</strong><p>只有熟悉 Go 正则并确实需要模式匹配时才选择“高级正则”。</p></div>
          </div>
          <div class="subsection-heading keyword-stats-heading">
            <div><h2>关键词效果统计</h2><p>每个请求在同一分组只计一次；阻断率按“阻断 ÷ 已送审”计算。命中缓存和关闭关键词预筛时不会重复计算。</p></div>
            <button type="button" class="ghost" @click="clearKeywordStats"><Trash2 :size="16" />清空统计</button>
          </div>
          <div class="keyword-stats-scroll">
            <table class="keyword-stats-table">
              <thead><tr><th>关键词分组</th><th>风险领域</th><th>命中</th><th>已送审</th><th>阻断</th><th>阻断率</th><th>最近命中</th></tr></thead>
              <tbody>
                <tr v-for="item in keywordStats" :key="item.set_name">
                  <td><strong>{{ item.set_name }}</strong><small class="table-subtext">{{ item.enabled ? '当前启用' : '当前停用' }}</small></td>
                  <td>{{ riskDomainLabel(item.risk_domain) }}</td>
                  <td>{{ num(item.hit_count) }}</td>
                  <td>{{ num(item.audited_count) }}</td>
                  <td>{{ num(item.blocked_count) }}</td>
                  <td>{{ pct(keywordBlockRate(item)) }}</td>
                  <td>{{ item.updated_at ? dateText(item.updated_at) : '-' }}</td>
                </tr>
                <tr v-if="!keywordStats.length"><td colspan="7" class="table-empty">暂无关键词分组统计。</td></tr>
              </tbody>
            </table>
          </div>
          <div class="table-actions">
            <button type="button" @click="loadRecommendedKeywordSets"><RotateCcw :size="16" />载入推荐关键词</button>
            <button type="button" class="ghost" @click="addKeywordSet"><Tags :size="16" />新增分组</button>
          </div>
          <div class="keyword-grid">
            <article v-for="(set, i) in config.keyword_sets" :key="i" class="keyword-set">
              <div class="keyword-head"><input v-model="set.name" /><label><input type="checkbox" v-model="set.enabled" />启用</label></div>
              <div class="mini-grid">
                <label class="mini-field"><span>风险领域</span><select v-model="set.risk_domain">
                  <option v-for="[value, label] in riskDomainOptions" :key="value" :value="value">{{ label }}</option>
                </select></label>
                <label class="mini-field"><span>匹配方式</span><select v-model="set.match_type">
                  <option v-for="[value, label] in matchTypeOptions" :key="value" :value="value">{{ label }}</option>
                </select></label>
              </div>
              <small class="match-type-hint">{{ matchTypeHint(set.match_type) }}</small>
              <label class="keyword-editor"><span>关键词（{{ set.keywords.length }} 个，每行一个）</span><textarea :value="keywordText(set)" :placeholder="keywordPlaceholder(set.match_type)" @input="setKeywordText(set, ($event.target as HTMLTextAreaElement).value)"></textarea></label>
              <button type="button" class="ghost" @click="config.keyword_sets.splice(i, 1)"><Trash2 :size="15" />删除分组</button>
            </article>
          </div>
        </section>

        <section v-if="active === 'test'" class="section">
          <div class="test-layout">
            <div class="test-inputs">
              <Field label="输入文本"><textarea v-model="testText"></textarea></Field>
              <Field label="图片 URL / data URL"><input v-model="testImage" /></Field>
              <div class="inline-actions">
                <button @click="runTest"><Beaker :size="16" />运行链路测试</button>
                <button class="ghost" @click="clearTestCache(false)"><Trash2 :size="16" />清理缓存</button>
              </div>
            </div>
            <div class="explain-item sticky-help">
              <strong>这页怎么看</strong>
              <p>先看“最终动作”：会放行还是会阻断。再看 sub2api 取值字段的分数；其它分类字段应该保持 0。</p>
            </div>
          </div>
          <p v-if="testNotice" class="notice test-notice">{{ testNotice }}</p>

          <div v-if="testResult" class="test-report">
            <div class="result-banner" :class="testResult.would_block_sub2api ? 'error' : 'success'">
              <strong>{{ finalActionText(testResult) }}</strong>
              <span>{{ finalActionHint(testResult) }}</span>
              <div class="result-banner-actions">
                <button class="ghost" @click="clearTestCache(false)"><Trash2 :size="16" />清理缓存</button>
                <button v-if="testResult.cache_hit" @click="clearTestCache(true)"><RefreshCw :size="16" />清缓存并重测</button>
              </div>
            </div>
            <div class="summary-grid">
              <div><b>sub2api 取值字段</b><span class="mono param-value">{{ resultScoreParam(testResult) }}</span><small>{{ resultScoreHint(testResult) }}</small></div>
              <div><b>拦截阈值</b><span>{{ sub2apiThreshold(testResult).toFixed(2) }}</span><small>链路测试按这个值判断是否会被 sub2api pre_block 拦截</small></div>
              <div><b>是否命中关键词</b><span>{{ testResult.cache_hit ? '未重算' : (testResult.keyword_prefilter_enabled === false ? '未启用' : boolText((testResult.keyword_hits || []).length)) }}</span><small>{{ testResult.cache_hit ? '本次命中缓存，没有重新执行关键词初筛' : (testResult.keyword_prefilter_enabled === false ? '已关闭关键词预筛，本次直接进入模型调用规则' : ((testResult.keyword_hits || []).length ? '命中词：' + testResult.keyword_hits?.map((h:any)=>h.keyword).join('、') : '没有命中本地初筛词')) }}</small></div>
              <div><b>是否命中缓存</b><span>{{ boolText(testResult.cache_hit) }}</span><small>{{ testResult.cache_hit ? '复用之前的审核结果，没有重新跑完整链路' : '本次重新计算判断过程' }}</small></div>
              <div><b>是否调用模型</b><span>{{ boolText(testResult.external_audited) }}</span><small>{{ testResult.external_audited ? '已经调用上游审核模型' : '未调用模型，按本地规则或缓存处理' }}</small></div>
              <div><b>是否抽样调用</b><span>{{ testResult.keyword_prefilter_enabled === false ? '不适用' : boolText(testResult.sampled) }}</span><small>{{ testResult.keyword_prefilter_enabled === false ? '关键词预筛关闭时不使用未命中抽样率' : '没命中关键词时，按抽样率随机调用模型；默认 0.3 表示抽 30%' }}</small></div>
              <div><b>其它分类分数</b><span>全部放行</span><small>内部分类只进摘要和日志，不再分散写入 sub2api 字段</small></div>
            </div>

            <div class="readable-block">
              <h3>关键返回参数说明</h3>
              <table>
                <tbody>
                  <tr><th>normalized_text</th><td>系统实际用于判断的文本，已做归一化和长度限制。</td><td>{{ testResult.normalized_text || '-' }}</td></tr>
                  <tr><th>keyword_hits</th><td>本地初筛命中的词。通常用于触发上游模型打分。</td><td>{{ testResult.keyword_prefilter_enabled === false ? '关键词预筛未启用' : ((testResult.keyword_hits || []).length ? testResult.keyword_hits?.map((h:any)=>h.keyword).join('、') : '无') }}</td></tr>
                  <tr><th>cache_hit</th><td>是否复用缓存结果。命中缓存时不会重新跑关键词和模型调用。</td><td>{{ boolText(testResult.cache_hit) }}</td></tr>
                  <tr><th>external_audited</th><td>是否调用了上游文本或图片审核模型。</td><td>{{ boolText(testResult.external_audited) }}</td></tr>
                  <tr><th>provider_raw_summary</th><td>上游模型返回摘要，方便确认 JSON 分类结果。</td><td>{{ testResult.provider_raw_summary || '-' }}</td></tr>
                  <tr><th>{{ resultScoreField(testResult) }}</th><td>上游 confidence 原样写入的最终分数字段。</td><td>{{ resultScoreReadable(testResult) }}</td></tr>
                  <tr><th>sub2api threshold</th><td>链路测试按这个阈值判断是否会被 pre_block 拦截。</td><td>{{ sub2apiThreshold(testResult).toFixed(2) }}</td></tr>
                  <tr><th>final_response.results[0].flagged</th><td>兼容 OpenAI moderation 的布尔字段；这里按 sub2api 阈值计算。</td><td>{{ flaggedReadable(testResult) }}</td></tr>
                  <tr><th>category_scores</th><td>最终给 sub2api/OpenAI moderation 的完整分类分数；只有指定字段承载上游 confidence。</td><td>{{ categoryScoresText(testResult.category_scores) }}</td></tr>
                  <tr><th>event.action</th><td>Adapter 内部动作：放行、阻断、故障放行、模型禁用放行等。</td><td>{{ explainAction(testResult.event?.action) }}：{{ explainActionDetail(testResult.event?.action) }}</td></tr>
                </tbody>
              </table>
            </div>

            <div class="readable-block">
              <h3>请求参数和返回参数</h3>
              <div class="param-grid">
                <details class="param-details" open>
                  <summary>Adapter 接收的请求参数</summary>
                  <pre>{{ formatJSON(testResult.adapter_request) }}</pre>
                </details>
                <details class="param-details" open>
                  <summary>归一化后的判断输入</summary>
                  <pre>{{ formatJSON(testResult.normalized_input) }}</pre>
                </details>
                <details class="param-details" :open="!!testResult.upstream_request">
                  <summary>上游模型请求参数</summary>
                  <pre>{{ testResult.upstream_request ? formatJSON(testResult.upstream_request) : (testResult.upstream_note || '本次没有请求上游模型') }}</pre>
                </details>
                <details class="param-details" :open="!!testResult.upstream_response">
                  <summary>上游模型返回参数</summary>
                  <pre>{{ testResult.upstream_response ? formatJSON(testResult.upstream_response) : (testResult.upstream_note || '本次没有上游返回') }}</pre>
                </details>
                <details class="param-details">
                  <summary>最终返回给 sub2api 的参数</summary>
                  <pre>{{ formatJSON(testResult.final_response) }}</pre>
                </details>
              </div>
            </div>

            <details class="raw-json">
              <summary>查看原始 JSON</summary>
              <pre>{{ rawOutput }}</pre>
            </details>
          </div>
          <div v-else class="empty-state">还没有测试结果。输入一段文本后点击“运行链路测试”，这里会显示完整判断过程。</div>
        </section>

        <section v-if="active === 'events'" class="section">
          <div class="explain-grid">
            <div class="explain-item"><strong>动作是什么意思</strong><p>“放行”表示当前请求通过；“阻断”表示应拦截；“故障放行”表示上游模型异常时按策略先放过；“模型禁用放行”表示你关闭了模型调用。</p></div>
            <div class="explain-item"><strong>什么时候看这里</strong><p>排查阻断原因、上游模型故障或模型被禁用时，先按动作筛选，再看关键词、模型摘要、最高分类和错误。</p></div>
            <div class="explain-item"><strong>阻断内容</strong><p>被阻断的请求会保存并显示归一化后的文本明文；放行请求仍只保留指纹或脱敏摘要。</p></div>
            <div class="explain-item"><strong>事件怎么清理</strong><p>系统运行中每分钟检查一次。超过 {{ config.event_retention_days }} 天或超过 {{ config.event_retention }} 条的旧事件会被删除。</p></div>
          </div>
          <p v-if="eventNotice" class="notice">{{ eventNotice }}</p>
          <div class="panel-row compact-row">
            <div class="panel"><h2>已保留事件</h2><strong>{{ num(status?.events?.total) }}</strong><p>最多保留 {{ num(config.event_retention) }} 条 · 留存 {{ num(config.event_retention_days) }} 天</p></div>
            <div class="panel"><h2>最早记录</h2><p>{{ dateText(status?.events?.oldest) }}</p></div>
            <div class="panel"><h2>最新记录</h2><p>{{ dateText(status?.events?.newest) }}</p></div>
          </div>
          <div class="table-actions">
            <select v-model="filterAction" aria-label="动作筛选" @change="changeEventFilter">
              <option v-for="[value, label] in actionOptions" :key="value" :value="value">{{ label }}</option>
            </select>
            <select v-model.number="eventPageSize" aria-label="每页条数" @change="changeEventPageSize">
              <option :value="10">每页 10 条</option><option :value="20">每页 20 条</option><option :value="50">每页 50 条</option>
            </select>
            <button @click="refresh({ reloadConfig: false })"><RefreshCw :size="16" />刷新</button>
            <button @click="pruneEvents"><RefreshCw :size="16" />清理过期日志</button>
            <button class="danger-button" @click="clearEvents"><Trash2 :size="16" />清空全部日志</button>
          </div>
          <table class="events-table"><thead><tr><th>时间</th><th>动作</th><th>请求内容</th><th>内容指纹</th><th>关键词</th><th>上游模型</th><th>最高分类</th><th>本地/模型耗时</th><th>错误</th></tr></thead><tbody>
            <tr v-for="e in events" :key="e.request_id">
              <td>{{ new Date(e.time).toLocaleString() }}</td><td><span class="pill" :class="e.action">{{ explainAction(e.action) }}</span></td>
              <td class="event-input-cell"><details v-if="e.blocked_input"><summary>查看阻断明文</summary><p>{{ e.blocked_input }}</p></details><span v-else>{{ e.input_excerpt || '-' }}</span></td>
              <td class="mono">{{ e.input_hash.slice(0, 12) }}</td><td>{{ e.keyword_hits?.map((h:any)=>h.keyword).join('、') || '-' }}</td><td>{{ explainProvider(e.provider) }}</td><td>{{ explainScore(e.highest_category, e.highest_score) }}</td><td>{{ e.local_latency_ms }}/{{ e.provider_latency_ms }}ms</td><td>{{ e.error_summary || '-' }}</td>
            </tr>
            <tr v-if="!events.length"><td colspan="9" class="table-empty">当前筛选条件下没有事件记录。</td></tr>
          </tbody></table>
          <div class="pagination-bar">
            <span>共 {{ num(eventTotal) }} 条 · 第 {{ eventPage }} / {{ eventTotalPages }} 页</span>
            <div class="pagination-actions"><button class="icon-button" type="button" title="上一页" :disabled="eventPage <= 1" @click="goToEventPage(eventPage - 1)"><ChevronLeft :size="17" /></button><button class="icon-button" type="button" title="下一页" :disabled="eventPage >= eventTotalPages" @click="goToEventPage(eventPage + 1)"><ChevronRight :size="17" /></button></div>
          </div>
        </section>

        <section v-if="active === 'monitor'" class="section monitor-page">
          <div class="table-actions monitor-actions"><button type="button" @click="refreshSystemData"><RefreshCw :size="16" />刷新监控</button><span>采集时间 {{ dateText(systemStats?.collected_at) }}</span></div>
          <div v-if="systemStats" class="metric-grid monitor-grid">
            <article class="metric"><b>进程内存 RSS</b><span>{{ humanBytes(systemStats.process_rss_bytes || systemStats.runtime_sys_bytes) }}</span><small>Adapter 进程当前常驻内存</small></article>
            <article class="metric"><b>Go 堆内存</b><span>{{ humanBytes(systemStats.heap_alloc_bytes) }}</span><small>已分配堆内存 · {{ num(systemStats.goroutines) }} 个 goroutine</small></article>
            <article class="metric"><b>数据卷占用</b><span>{{ humanBytes(systemStats.data_bytes) }}</span><small>{{ num(systemStats.data_files) }} 个文件</small></article>
            <article class="metric"><b>SQLite 数据</b><span>{{ humanBytes(systemStats.database_bytes + systemStats.database_wal_bytes + systemStats.database_shm_bytes) }}</span><small>数据库、WAL 和共享内存文件</small></article>
            <article class="metric"><b>磁盘可用</b><span>{{ humanBytes(systemStats.filesystem_free_bytes) }}</span><small>所在文件系统总计 {{ humanBytes(systemStats.filesystem_total_bytes) }}</small></article>
            <article class="metric"><b>运行时间</b><span>{{ durationText(systemStats.uptime_seconds) }}</span><small>{{ systemStats.version.version }} · {{ systemStats.version.commit }}</small></article>
          </div>
          <div v-if="systemStats" class="monitor-bands">
            <section>
              <div class="subsection-heading"><div><h2>存储明细</h2><p>{{ systemStats.data_directory }}</p></div></div>
              <div class="storage-list">
                <div><span>主数据库</span><strong>{{ humanBytes(systemStats.database_bytes) }}</strong></div>
                <div><span>SQLite WAL</span><strong>{{ humanBytes(systemStats.database_wal_bytes) }}</strong></div>
                <div><span>SQLite SHM</span><strong>{{ humanBytes(systemStats.database_shm_bytes) }}</strong></div>
                <div><span>数据卷合计</span><strong>{{ humanBytes(systemStats.data_bytes) }}</strong></div>
              </div>
            </section>
            <section>
              <div class="subsection-heading"><div><h2>运行指标</h2><p>进程重启后计数重新开始，数据库行数不会重置。</p></div></div>
              <div class="storage-list">
                <div><span>事件记录</span><strong>{{ num(systemStats.event_rows) }}</strong></div>
                <div><span>决策缓存</span><strong>{{ num(systemStats.decision_cache_rows) }}</strong></div>
                <div><span>总请求</span><strong>{{ num(systemStats.requests_total) }}</strong></div>
                <div><span>阻断 / 故障放行</span><strong>{{ num(systemStats.blocks_total) }} / {{ num(systemStats.fail_open_total) }}</strong></div>
                <div><span>上游 P95 延迟</span><strong>{{ Number(systemStats.provider_p95_ms || 0).toFixed(0) }} ms</strong></div>
              </div>
            </section>
          </div>
          <div v-else class="empty-state">监控数据加载中。</div>
        </section>

        <section v-if="active === 'system'" class="section system-page">
          <div class="panel-row">
            <div class="panel"><h2>版本</h2><p>{{ status?.adapter_version?.version }} · {{ status?.adapter_version?.commit }}</p></div>
            <div class="panel"><h2>启动时间</h2><p>{{ status?.started_at ? new Date(status.started_at).toLocaleString() : '-' }}</p></div>
            <div class="panel"><h2>密钥</h2><p>Adapter {{ status?.auth_token_configured ? '已配置' : '未配置' }} · Hash salt {{ status?.hash_salt_configured ? '已配置' : '未配置' }}</p></div>
          </div>
          <p v-if="systemNotice" class="notice">{{ systemNotice }}</p>
          <div class="system-update-panel">
            <div><h2>在线更新</h2><p v-if="updateStatus?.configured">镜像 {{ updateStatus.image }} · 通道 {{ updateStatus.channel }}</p><p v-else>尚未连接独立更新器。配置完成前，管理页面不会获得 Docker 宿主机权限。</p></div>
            <span class="pill" :class="updateStatus?.configured ? 'ok' : ''">{{ updateStatus?.configured ? '已配置' : '未配置' }}</span>
            <button type="button" :disabled="systemBusy || !updateStatus?.configured" @click="triggerSystemUpdate"><Download :size="16" />{{ systemBusy ? '提交中' : '拉取并更新' }}</button>
          </div>
          <div class="subsection-heading maintenance-heading"><div><h2>配置维护</h2><p>导入和恢复推荐值会改变运行策略；导出文件中的密钥仍为掩码。</p></div></div>
          <div class="inline-actions"><button @click="exportConfig"><Download :size="16" />导出配置</button><label class="button"><Upload :size="16" />导入配置<input hidden type="file" accept="application/json" @change="importConfig" /></label><button class="danger-button" @click="resetDefaults"><Archive :size="16" />恢复推荐值</button></div>
        </section>

        <section v-if="active === 'audits'" class="section">
          <table><thead><tr><th>时间</th><th>操作人</th><th>来源</th><th>摘要</th></tr></thead><tbody>
            <tr v-for="a in audits" :key="a.id"><td>{{ new Date(a.created_at).toLocaleString() }}</td><td>{{ a.actor }}</td><td>{{ a.source_ip }}</td><td>{{ a.summary }}</td></tr>
          </tbody></table>
        </section>
      </template>
    </main>
  </div>
</template>
