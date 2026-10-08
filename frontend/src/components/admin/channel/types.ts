import type {
  BillingMode,
  ChannelTimePricing,
  ModelPricingReference,
  PricingInterval,
} from '@/api/admin/channels'
import { REASONING_EFFORT_LEVELS } from '@/constants/channel'

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

export interface IntervalFormEntry {
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_write_1h_price?: number | string | null
  cache_read_price: number | string | null
  input_multiplier: number | string | null
  output_multiplier: number | string | null
  cache_write_multiplier: number | string | null
  cache_read_multiplier: number | string | null
  per_request_price: number | string | null
  sort_order: number
}

export interface PricingFormEntry {
  models: string[]
  billing_mode: BillingMode
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_write_1h_price?: number | string | null
  cache_read_price: number | string | null
  fast_multiplier?: number | string | null
  flex_multiplier?: number | string | null
  reasoning_effort_multipliers?: Record<string, number | string> | null
  image_input_price: number | string | null
  image_output_price: number | string | null
  per_request_price: number | string | null
  intervals: IntervalFormEntry[]
  time_pricing: TimePricingFormEntry
  /**
   * 表单专用元数据：本规则创建时使用的参考价（同步或手动查询结果）。
   * 仅用于展示来源与 manual/unsupported 原因，提交时由 formToAPI 逐字段
   * 重建，不会泄露到请求体；从 API 读回的既有规则没有该字段。
   */
  reference?: ModelPricingReference
}

export interface TimePricingPeriodFormEntry {
  start_time: string
  end_time: string
  multiplier: number | string
}

export interface TimePricingFormEntry {
  timezone: string
  weekdays_only: boolean
  periods: TimePricingPeriodFormEntry[]
}

export const DEFAULT_TIME_PRICING_TIMEZONE = 'Asia/Shanghai'

const CLOCK_TIME = /^(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d$/
const LEGACY_CLOCK_TIME = /^(?:[01]\d|2[0-3]):[0-5]\d$/
const TWO_DECIMAL_MULTIPLIER = /^\d+(?:\.\d{1,2})?$/

export function isValidTimePricingMultiplier(value: number | string): boolean {
  const multiplier = String(value)
  const numericValue = Number(multiplier)
  return TWO_DECIMAL_MULTIPLIER.test(multiplier) &&
    Number.isFinite(numericValue) && numericValue > 0
}

export const COMMON_TIMEZONES = [
  'UTC', 'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Singapore', 'Asia/Kolkata',
  'Australia/Sydney', 'Europe/London', 'Europe/Paris', 'Europe/Berlin',
  'America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles',
  'America/Toronto', 'America/Sao_Paulo', 'Pacific/Auckland', 'Pacific/Honolulu',
]

export function createDefaultTimePricingForm(): TimePricingFormEntry {
  return { timezone: DEFAULT_TIME_PRICING_TIMEZONE, weekdays_only: false, periods: [] }
}

export function apiTimePricingToForm(value: ChannelTimePricing | null | undefined): TimePricingFormEntry {
  if (!value) return createDefaultTimePricingForm()
  return {
    timezone: value.timezone || DEFAULT_TIME_PRICING_TIMEZONE,
    weekdays_only: value.weekdays_only === true,
    periods: (value.periods || []).map(period => ({
      start_time: LEGACY_CLOCK_TIME.test(period.start_time) ? `${period.start_time}:00` : period.start_time,
      end_time: LEGACY_CLOCK_TIME.test(period.end_time) ? `${period.end_time}:00` : period.end_time,
      multiplier: Number(period.multiplier).toFixed(2),
    })),
  }
}

export function formTimePricingToAPI(value: TimePricingFormEntry | null | undefined): ChannelTimePricing | null {
  if (!value?.periods?.length) return null
  const timezone = typeof value.timezone === 'string' ? value.timezone.trim() : ''
  return {
    timezone,
    weekdays_only: value.weekdays_only === true,
    periods: value.periods.map(period => ({
      start_time: period.start_time,
      end_time: period.end_time,
      multiplier: Number(period.multiplier),
    })),
  }
}

function timeToSeconds(time: string, isEnd: boolean): number {
  if (isEnd && time === '00:00:00') return 24 * 60 * 60
  const [hours, minutes, seconds] = time.split(':').map(Number)
  return hours * 60 * 60 + minutes * 60 + seconds
}

function timePricingValidationMessage(t: TranslateFn, key: string): string {
  return t(`admin.channels.timePricingValidation.${key}`)
}

export function validateTimePricing(value: TimePricingFormEntry, t: TranslateFn): string | null {
  if (!value?.periods?.length) return null

  if (typeof value.timezone !== 'string' || value.timezone.trim() === '') {
    return timePricingValidationMessage(t, 'timezone')
  }
  const timezone = value.timezone.trim()

  try {
    new Intl.DateTimeFormat('en-US', { timeZone: timezone })
  } catch {
    return timePricingValidationMessage(t, 'timezone')
  }

  const periods = [] as { start: number, end: number }[]
  for (const period of value.periods) {
    if (!CLOCK_TIME.test(period.start_time) || !CLOCK_TIME.test(period.end_time)) {
      return timePricingValidationMessage(t, 'format')
    }
    if (period.start_time === period.end_time) {
      return timePricingValidationMessage(t, 'range')
    }

    const start = timeToSeconds(period.start_time, false)
    const end = timeToSeconds(period.end_time, true)
    if (start >= end) return timePricingValidationMessage(t, 'range')

    if (!isValidTimePricingMultiplier(period.multiplier)) {
      return timePricingValidationMessage(t, 'multiplier')
    }
    periods.push({ start, end })
  }

  const sorted = [...periods].sort((a, b) => a.start - b.start)
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i].start < sorted[i - 1].end) {
      return timePricingValidationMessage(t, 'overlap')
    }
  }
  return null
}

export function formatTimezoneOffset(timezone: string, at = new Date()): string {
  try {
    const part = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      timeZoneName: 'shortOffset',
    }).formatToParts(at).find(item => item.type === 'timeZoneName')?.value
    if (!part || part === 'GMT') return 'UTC+00:00'
    const match = /^GMT([+-])(\d{1,2})(?::(\d{2}))?$/.exec(part)
    if (!match) return ''
    return `UTC${match[1]}${match[2].padStart(2, '0')}:${match[3] || '00'}`
  } catch {
    return ''
  }
}

// 价格转换：后端存 per-token，前端显示 per-MTok ($/1M tokens)
const MTOK = 1_000_000

export function toNullableNumber(val: number | string | null | undefined): number | null {
  if (val === null || val === undefined || val === '') return null
  const num = Number(val)
  return isNaN(num) ? null : num
}

export function isValidPositiveMultiplier(val: number | string | null | undefined): boolean {
  if (val === null || val === undefined || val === '') return true
  const multiplier = Number(val)
  return Number.isFinite(multiplier) && multiplier > 0
}

export function formReasoningEffortMultipliersToAPI(
  value: PricingFormEntry['reasoning_effort_multipliers'],
): Record<string, number> | null {
  const entries = Object.entries(value || {})
    .filter(([, multiplier]) => multiplier !== '')
    .map(([effort, multiplier]) => [effort, Number(multiplier)])
  return entries.length ? Object.fromEntries(entries) : null
}

export function validateReasoningEffortMultipliers(
  value: PricingFormEntry['reasoning_effort_multipliers'],
  t: TranslateFn,
): string | null {
  for (const [effort, multiplier] of Object.entries(value || {})) {
    if (!REASONING_EFFORT_LEVELS.some(level => level === effort)) {
      return t('admin.channels.form.reasoningEffortLevelInvalid', { effort })
    }
    if (multiplier !== '' && !isValidPositiveMultiplier(multiplier)) {
      return t('admin.channels.form.reasoningEffortMultiplierPositive', { effort })
    }
  }
  return null
}

/** 前端显示值($/MTok) → 后端存储值(per-token) */
export function mTokToPerToken(val: number | string | null | undefined): number | null {
  const num = toNullableNumber(val)
  return num === null ? null : parseFloat((num / MTOK).toPrecision(10))
}

/** 后端存储值(per-token) → 前端显示值($/MTok) */
export function perTokenToMTok(val: number | null | undefined): number | null {
  if (val === null || val === undefined) return null
  // toPrecision(10) 消除 IEEE 754 浮点乘法精度误差，如 5e-8 * 1e6 = 0.04999...96 → 0.05
  return parseFloat((val * MTOK).toPrecision(10))
}

/** 一张参考价卡对应的表单条目，连同原始状态一起返回。 */
export interface PricedModelRule {
  /** 只含一个模型：多个模型共用一条规则就等于给它们标同一套价。 */
  entry: PricingFormEntry
  /** 触发本次填充的参考价，用于展示 manual/unsupported 原因和来源。 */
  reference: ModelPricingReference
}

/** per-token → $/MTok；null 保持 null（缺字段不是免费）。 */
function toMTokOrNull(value: number | null | undefined): number | null {
  return value === null || value === undefined ? null : perTokenToMTok(value)
}

function emptyPricingFormEntry(model: string): PricingFormEntry {
  return {
    models: [model],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: createDefaultTimePricingForm(),
  }
}

/**
 * 把一份参考价转换成一条只含该模型的计价规则。
 *
 * 单模型查询、批量同步和批量粘贴共用这一份换算口径，避免三处各写一规则后
 * 出现「同步有价、手动添加没价」的割裂。
 *
 * - status=priced：套用完整价卡（token/缓存/倍率/长上下文区间/图片/按次）。
 * - status!=priced：保留空表单，让运营者看到待填状态和原因，绝不用 0 冒充免费。
 */
export function referenceToPricingRule(reference: ModelPricingReference): PricedModelRule {
  const entry = emptyPricingFormEntry(reference.model)
  const card = reference.pricing

  if (reference.status !== 'priced' || !card) {
    // manual/unsupported：保留空表单，但把原因挂在条目上，让规则卡片能把
    // 「需要手填/单位不支持」显示出来，而不是静默摆一条空规则。
    return { entry: { ...entry, reference }, reference }
  }

  entry.billing_mode = card.billing_mode || 'token'
  entry.input_price = toMTokOrNull(card.input_price)
  entry.output_price = toMTokOrNull(card.output_price)
  entry.cache_write_price = toMTokOrNull(card.cache_write_price)
  entry.cache_write_1h_price = toMTokOrNull(card.cache_write_1h_price)
  entry.cache_read_price = toMTokOrNull(card.cache_read_price)
  entry.image_input_price = toMTokOrNull(card.image_input_price)
  entry.image_output_price = toMTokOrNull(card.image_output_price)
  entry.fast_multiplier = card.fast_multiplier ?? null
  entry.flex_multiplier = card.flex_multiplier ?? null
  entry.reasoning_effort_multipliers = card.reasoning_effort_multipliers
    ? { ...card.reasoning_effort_multipliers }
    : null
  // per_request_price 本身就是绝对美元价，不做 per-token 换算。
  entry.per_request_price = card.per_request_price ?? null
  entry.intervals = apiIntervalsToForm(card.intervals || [])
  return { entry: { ...entry, reference }, reference }
}

/**
 * 把同步/查询得到的参考价列表转换成计价规则：**每个模型一条独立规则**。
 *
 * 早期实现把所有模型塞进同一条空价规则，或者按价签名把多个模型合并到一条规则，
 * 两者都会让一部分模型拿到另一部分模型的价格。这里一个模型一条，即使两个模型
 * 恰好同价也不合并。
 */
export function buildSyncedPricingEntries(
  references: ModelPricingReference[],
): PricedModelRule[] {
  return references.map(reference => referenceToPricingRule(reference))
}

export function apiIntervalsToForm(intervals: PricingInterval[]): IntervalFormEntry[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label || '',
    input_price: perTokenToMTok(iv.input_price),
    output_price: perTokenToMTok(iv.output_price),
    cache_write_price: perTokenToMTok(iv.cache_write_price),
    cache_write_1h_price: perTokenToMTok(iv.cache_write_1h_price),
    cache_read_price: perTokenToMTok(iv.cache_read_price),
    input_multiplier: iv.input_multiplier,
    output_multiplier: iv.output_multiplier,
    cache_write_multiplier: iv.cache_write_multiplier,
    cache_read_multiplier: iv.cache_read_multiplier,
    per_request_price: iv.per_request_price,
    sort_order: iv.sort_order
  }))
}

export function formIntervalsToAPI(intervals: IntervalFormEntry[]): PricingInterval[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label,
    input_price: mTokToPerToken(iv.input_price),
    output_price: mTokToPerToken(iv.output_price),
    cache_write_price: mTokToPerToken(iv.cache_write_price),
    cache_write_1h_price: mTokToPerToken(iv.cache_write_1h_price),
    cache_read_price: mTokToPerToken(iv.cache_read_price),
    input_multiplier: toNullableNumber(iv.input_multiplier),
    output_multiplier: toNullableNumber(iv.output_multiplier),
    cache_write_multiplier: toNullableNumber(iv.cache_write_multiplier),
    cache_read_multiplier: toNullableNumber(iv.cache_read_multiplier),
    per_request_price: toNullableNumber(iv.per_request_price),
    sort_order: iv.sort_order
  }))
}

// ── 模型模式冲突检测 ──────────────────────────────────────

interface ModelPattern {
  pattern: string
  prefix: string  // lowercase, 通配符去掉尾部 *
  wildcard: boolean
}

function toModelPattern(model: string): ModelPattern {
  const lower = model.toLowerCase()
  const wildcard = lower.endsWith('*')
  return {
    pattern: model,
    prefix: wildcard ? lower.slice(0, -1) : lower,
    wildcard,
  }
}

function patternsConflict(a: ModelPattern, b: ModelPattern): boolean {
  if (!a.wildcard && !b.wildcard) return a.prefix === b.prefix
  if (a.wildcard && !b.wildcard) return b.prefix.startsWith(a.prefix)
  if (!a.wildcard && b.wildcard) return a.prefix.startsWith(b.prefix)
  // 双通配符：任一前缀是另一前缀的前缀即冲突
  return a.prefix.startsWith(b.prefix) || b.prefix.startsWith(a.prefix)
}

/** 检测模型模式列表中的冲突，返回冲突的两个模式名；无冲突返回 null */
export function findModelConflict(models: string[]): [string, string] | null {
  const patterns = models.map(toModelPattern)
  for (let i = 0; i < patterns.length; i++) {
    for (let j = i + 1; j < patterns.length; j++) {
      if (patternsConflict(patterns[i], patterns[j])) {
        return [patterns[i].pattern, patterns[j].pattern]
      }
    }
  }
  return null
}

/**
 * 从候选模型里剔除已经被本平台条目覆盖的型号，返回需要新建的型号。
 *
 * 用大小写不敏感的精确匹配 + 已有通配符规则，和「新建条目时的冲突校验」同一套
 * 语义：否则 `claude-*` 规则已经覆盖 Sonnet 时，同步又会建一条重复规则。
 * 幂等：同步两次不会产生新规则。
 */
export function filterAlreadyCoveredModels(
  candidates: string[],
  existingRules: Array<{ models: string[] }>,
): string[] {
  const existingPatterns = existingRules
    .flatMap(rule => rule.models || [])
    .map(toModelPattern)
  const covered = new Set<string>()
  for (const pattern of existingPatterns) {
    if (pattern.wildcard) {
      for (const candidate of candidates) {
        if (candidate.toLowerCase().startsWith(pattern.prefix)) covered.add(candidate)
      }
      continue
    }
    covered.add(pattern.prefix)
  }
  return candidates.filter(model => !covered.has(model.toLowerCase()))
}

// ── 区间校验 ──────────────────────────────────────────────

/** 校验区间列表的合法性，返回错误消息；通过则返回 null
 *
 * mode 决定区间语义：
 * - token：区间是上下文 token 数分段 (min, max]，不能重叠，无上限段必须放最后
 * - per_request / image：区间是按 tier_label 分层（1K/2K/4K 等），后端按 label
 *   匹配，不依赖 min/max，因此跳过重叠 / last-unlimited 校验
 */
export function validateIntervals(
  intervals: IntervalFormEntry[],
  mode: BillingMode,
  t: TranslateFn,
): string | null {
  if (!intervals || intervals.length === 0) return null

  // 按 min_tokens 排序（不修改原数组）
  const sorted = [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)

  for (let i = 0; i < sorted.length; i++) {
    const err = validateSingleInterval(sorted[i], i, t)
    if (err) return err
  }

  // per_request / image 模式按 tier_label 匹配，不做 token 区间重叠校验
  if (mode !== 'token') return null
  return checkIntervalOverlap(sorted, t)
}

function intervalValidationMessage(
  t: TranslateFn,
  key: string,
  params: Record<string, unknown>,
): string {
  return t(`admin.channels.intervalValidation.${key}`, params)
}

function intervalPriceLabel(t: TranslateFn, key: string): string {
  return t(`admin.channels.intervalValidation.price.${key}`)
}

function validateSingleInterval(iv: IntervalFormEntry, idx: number, t: TranslateFn): string | null {
  const index = idx + 1
  if (iv.min_tokens < 0) {
    return intervalValidationMessage(
      t,
      'negativeMin',
      { index, value: iv.min_tokens },
    )
  }
  if (iv.max_tokens != null) {
    if (iv.max_tokens <= 0) {
      return intervalValidationMessage(
        t,
        'maxPositive',
        { index, value: iv.max_tokens },
      )
    }
    if (iv.max_tokens <= iv.min_tokens) {
      return intervalValidationMessage(
        t,
        'maxGreaterThanMin',
        { index, max: iv.max_tokens, min: iv.min_tokens },
      )
    }
  }
  return validateIntervalPrices(iv, idx, t)
}

function validateIntervalPrices(iv: IntervalFormEntry, idx: number, t: TranslateFn): string | null {
  const index = idx + 1
  const prices: [string, number | string | null][] = [
    ['inputPrice', iv.input_price],
    ['outputPrice', iv.output_price],
    ['cacheWritePrice', iv.cache_write_price],
    ['cacheWrite1hPrice', iv.cache_write_1h_price ?? null],
    ['cacheReadPrice', iv.cache_read_price],
    ['perRequestPrice', iv.per_request_price],
  ]
  for (const [key, val] of prices) {
    if (val != null && val !== '' && Number(val) < 0) {
      const field = intervalPriceLabel(t, key)
      return intervalValidationMessage(
        t,
        'negativePrice',
        { index, field },
      )
    }
  }
  const multipliers: [string, number | string | null][] = [
    ['inputMultiplier', iv.input_multiplier],
    ['outputMultiplier', iv.output_multiplier],
    ['cacheWriteMultiplier', iv.cache_write_multiplier],
    ['cacheReadMultiplier', iv.cache_read_multiplier],
  ]
  for (const [key, val] of multipliers) {
    if (!isValidPositiveMultiplier(val)) {
      return intervalValidationMessage(t, 'multiplierPositive', {
        index,
        field: intervalPriceLabel(t, key),
      })
    }
  }
  return null
}

function checkIntervalOverlap(sorted: IntervalFormEntry[], t: TranslateFn): string | null {
  for (let i = 0; i < sorted.length; i++) {
    // 无上限区间必须是最后一个
    if (sorted[i].max_tokens == null && i < sorted.length - 1) {
      return intervalValidationMessage(
        t,
        'unboundedLast',
        { index: i + 1 },
      )
    }
    if (i === 0) continue
    const prev = sorted[i - 1]
    // (min, max] 语义：前一个区间上界 > 当前区间下界则重叠
    if (prev.max_tokens == null || prev.max_tokens > sorted[i].min_tokens) {
      const prevMax = prev.max_tokens == null ? '∞' : String(prev.max_tokens)
      return intervalValidationMessage(
        t,
        'overlap',
        { previousIndex: i, currentIndex: i + 1, previousMax: prevMax, currentMin: sorted[i].min_tokens },
      )
    }
  }
  return null
}

/** 平台对应的模型 tag 样式（背景+文字） */
export function getPlatformTagClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    case 'openai': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'gemini': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
    case 'antigravity': return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    case 'grok': return 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
    case 'kimi': return 'bg-pink-100 text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
    case 'zhipu': return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
    case 'deepseek': return 'bg-teal-100 text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
    case 'stepfun': return 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
    default: return 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400'
  }
}

/** 平台对应的模型文字色（仅 text-*，用于 input/text 场景）— 与 getPlatformTagClass 同色系 */
export function getPlatformTextClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'text-orange-700 dark:text-orange-400'
    case 'openai': return 'text-emerald-700 dark:text-emerald-400'
    case 'gemini': return 'text-blue-700 dark:text-blue-400'
    case 'antigravity': return 'text-purple-700 dark:text-purple-400'
    case 'grok': return 'text-slate-700 dark:text-slate-300'
    case 'kimi': return 'text-pink-700 dark:text-pink-400'
    case 'zhipu': return 'text-indigo-700 dark:text-indigo-400'
    case 'deepseek': return 'text-teal-700 dark:text-teal-400'
    case 'stepfun': return 'text-cyan-700 dark:text-cyan-400'
    default: return ''
  }
}
