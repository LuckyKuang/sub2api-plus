import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { BillingMode, ModelPricingReference, ModelPricingSource } from '@/api/admin/channels'
import channelsAPI from '@/api/admin/channels'
import { Input, Select } from '@/components/ui'
import {
  REASONING_EFFORT_LEVELS,
  getPlatformTagClass,
  isValidPositiveMultiplier,
  referenceToPricingRule,
  validateReasoningEffortMultipliers,
  type IntervalFormEntry,
  type PricingFormEntry
} from './channelTypes'
import { IntervalRow, ModelTagInput, TimePricingSection } from './ChannelPricing'

export type LookupStatus =
  | { state: 'idle' }
  | { state: 'loading' }
  | { state: 'priced'; source: ModelPricingSource; matchedModel: string }
  | { state: 'manual_required'; reasonCode: string }
  | { state: 'unsupported_unit'; reasonCode: string }
  | { state: 'error' }

/**
 * 非 priced 的参考价状态收窄：调用方已排除 priced，这里把联合类型收敛成
 * 具体字面量，未知状态按 error 处理（宁可多重试一次，不静默显示成功）。
 */
function nonPricedLookupStatus(reference: ModelPricingReference): LookupStatus {
  switch (reference.status) {
    case 'manual_required':
      return { state: 'manual_required', reasonCode: reference.reason_code }
    case 'unsupported_unit':
      return { state: 'unsupported_unit', reasonCode: reference.reason_code }
    default:
      return { state: 'error' }
  }
}

/** 用户是否已经填过任何价格。显式 0 视为「已填」，自动填充与补齐都不得覆盖。 */
function hasAnyPricingValue(entry: PricingFormEntry): boolean {
  const fields: Array<keyof PricingFormEntry> = [
    'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
    'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price',
    'fast_multiplier', 'flex_multiplier',
  ]
  return fields.some(field => entry[field] !== null && entry[field] !== undefined && entry[field] !== '')
    || !!entry.reasoning_effort_multipliers
    || (entry.intervals || []).length > 0
}

const PRICE_FIELDS = [
  'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
  'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price',
  'fast_multiplier', 'flex_multiplier',
] as const

function emptyInterval(sortOrder: number, tierLabel = ''): IntervalFormEntry {
  return {
    min_tokens: 0, max_tokens: null, tier_label: tierLabel,
    input_price: null, output_price: null, cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null, per_request_price: null,
    input_multiplier: null, output_multiplier: null,
    cache_write_multiplier: null, cache_read_multiplier: null,
    sort_order: sortOrder
  }
}

export function PricingEntryCard({
  entry,
  platform,
  hideTokenIntervals = false,
  enableTimePricing = false,
  enableTierMultipliers = false,
  autoFillPricing = true,
  splitNewModels = true,
  onUpdate,
  onRemove,
  onSplit
}: {
  entry: PricingFormEntry
  platform?: string
  hideTokenIntervals?: boolean
  enableTimePricing?: boolean
  enableTierMultipliers?: boolean
  autoFillPricing?: boolean
  splitNewModels?: boolean
  onUpdate: (entry: PricingFormEntry) => void
  onRemove: () => void
  onSplit?: (models: string[]) => void
}) {
  const { t } = useTranslation()
  // Collapse state: entries with existing models default to collapsed
  const [collapsed, setCollapsed] = useState(entry.models.length > 0)
  // 每条规则的查价请求序号：晚到的旧响应必须被丢弃，不能覆盖用户后续输入。
  const lookupRequestId = useRef(0)
  const [lookupStatus, setLookupStatus] = useState<LookupStatus>({ state: 'idle' })

  const billingModeOptions = [
    { value: 'token', label: t('admin.channels.billingMode.token') },
    { value: 'per_request', label: t('admin.channels.billingMode.perRequest') },
    { value: 'image', label: t('admin.channels.billingMode.image') },
    { value: 'video', label: t('admin.channels.billingMode.video') }
  ]

  const billingModeLabel =
    billingModeOptions.find((o) => o.value === entry.billing_mode)?.label ?? entry.billing_mode

  const reasoningEffortMultiplierError = validateReasoningEffortMultipliers(entry.reasoning_effort_multipliers, t)

  const updateReasoningEffortMultiplier = (effort: string, value: string) => {
    const multipliers = { ...entry.reasoning_effort_multipliers }
    if (value === '') delete multipliers[effort]
    else multipliers[effort] = value
    onUpdate({
      ...entry,
      reasoning_effort_multipliers: Object.keys(multipliers).length ? multipliers : null,
    })
  }

  const emitField = (field: keyof PricingFormEntry, value: string) => {
    onUpdate({ ...entry, [field]: value === '' ? null : value })
  }

  const addInterval = () => {
    const intervals = [...(entry.intervals || [])]
    intervals.push(emptyInterval(intervals.length))
    onUpdate({ ...entry, intervals })
  }

  const addMediaTier = () => {
    const intervals = [...(entry.intervals || [])]
    const labels = entry.billing_mode === 'video'
      ? ['480p', '720p', '1080p']
      : ['1K', '2K', '4K', 'HD']
    intervals.push(emptyInterval(intervals.length, labels[intervals.length] || ''))
    onUpdate({ ...entry, intervals })
  }

  const updateInterval = (idx: number, updated: IntervalFormEntry) => {
    const intervals = [...(entry.intervals || [])]
    intervals[idx] = updated
    onUpdate({ ...entry, intervals })
  }

  const removeInterval = (idx: number) => {
    const intervals = [...(entry.intervals || [])]
    intervals.splice(idx, 1)
    onUpdate({ ...entry, intervals })
  }

  const onModelsUpdate = async (newModels: string[]) => {
    const oldModels = entry.models
    const addedModels = newModels.filter((m) => !oldModels.includes(m))

    // 目标模型列表：有新增时保留第一个新增（其余按既有策略拆分为独立规则）；
    // 仅删除时直接采用新列表（删除不再被丢弃，×/退格可移除误加的模型）。
    let nextModels: string[]
    if (addedModels.length > 0 && splitNewModels) {
      nextModels = [...oldModels, addedModels[0]]
      const splitModels = addedModels.slice(1)
      if (splitModels.length > 0) onSplit?.(splitModels)
    } else if (newModels.length !== oldModels.length || addedModels.length > 0) {
      nextModels = newModels
    } else {
      return
    }

    // 主模型切换判定：只有"原本已有主模型（oldModels 非空）且被移除/替换"才算切换。
    // 只有切换才需要把旧主模型自动填充的价格/区间/倍率一并清空——否则残留（尤其
    // fast/flex 倍率）会被 hasAnyPricingValue 当成"已填"，挡住新模型的重新查价。
    // 向"空规则"添加首个模型不算切换：那里的倍率是用户手填的配置，必须保留，auto-fill
    // 不得覆盖（见既有 keeps custom effort multipliers 契约测试）。
    const oldPrimary = (oldModels[0] ?? '').trim()
    const newPrimary = (nextModels[0] ?? '').trim()
    const primaryChanged = newPrimary !== oldPrimary
    // Group cards opt out of reference auto-fill: their explicitly shared prices
    // remain valid for the remaining models when the first tag is removed.
    const clearingStaleReference = autoFillPricing && primaryChanged && oldModels.length > 0
    const nextEntry: PricingFormEntry = { ...entry, models: nextModels }
    if (clearingStaleReference) {
      const target = nextEntry as unknown as Record<string, unknown>
      for (const f of PRICE_FIELDS) {
        target[f] = null
      }
      target.intervals = []
      target.reasoning_effort_multipliers = null
    }
    onUpdate(nextEntry)

    // 仅在“新增模型”时触发参考价自动填充。
    const model = addedModels[0]?.trim()
    if (!model || !platform || !autoFillPricing) return
    // Appending to an existing rule must preserve explicitly empty prices.
    if (oldModels.length > 0 && !primaryChanged) return

    // 已有用户填写的价格/倍率时不覆盖（界面另有显式「补齐空字段」）。换模型时价格已
    // 在上面清空，这里自然放行、为新主模型重新查价；未换模型则维持既有保护。
    if (hasAnyPricingValue(nextEntry)) {
      setLookupStatus({ state: 'idle' })
      return
    }

    const requestId = ++lookupRequestId.current
    setLookupStatus({ state: 'loading' })
    try {
      const reference = await channelsAPI.getModelDefaultPricing(platform, model)
      if (requestId !== lookupRequestId.current) return

      if (reference.status !== 'priced' || !reference.pricing) {
        setLookupStatus(nonPricedLookupStatus(reference))
        return
      }
      setLookupStatus({
        state: 'priced',
        source: reference.source,
        matchedModel: reference.matched_model,
      })
      const { entry: filled } = referenceToPricingRule(reference)
      onUpdate({ ...filled, models: nextModels })
    } catch {
      if (requestId !== lookupRequestId.current) return
      setLookupStatus({ state: 'error' })
    }
  }

  // 显式「补齐空字段」：只填回仍然为空的字段，已经填过（含 0）的不动。
  const completeEmptyFields = async () => {
    const model = entry.models[0]?.trim()
    if (!model || !platform) return
    const requestId = ++lookupRequestId.current
    setLookupStatus({ state: 'loading' })
    try {
      const reference = await channelsAPI.getModelDefaultPricing(platform, model)
      if (requestId !== lookupRequestId.current) return
      if (reference.status !== 'priced' || !reference.pricing) {
        setLookupStatus(nonPricedLookupStatus(reference))
        return
      }
      setLookupStatus({
        state: 'priced',
        source: reference.source,
        matchedModel: reference.matched_model,
      })
      const { entry: filled } = referenceToPricingRule(reference)
      const current = entry
      const merged = { ...current } as PricingFormEntry
      const target = merged as unknown as Record<string, unknown>
      for (const field of PRICE_FIELDS) {
        if (target[field] === null || target[field] === undefined || target[field] === '') {
          target[field] = (filled as unknown as Record<string, unknown>)[field]
        }
      }
      if (!current.intervals?.length && filled.intervals.length) merged.intervals = filled.intervals
      if (!current.reasoning_effort_multipliers && filled.reasoning_effort_multipliers) {
        merged.reasoning_effort_multipliers = filled.reasoning_effort_multipliers
      }
      onUpdate(merged)
    } catch {
      if (requestId !== lookupRequestId.current) return
      setLookupStatus({ state: 'error' })
    }
  }

  const lookupMessage = (() => {
    switch (lookupStatus.state) {
      case 'loading': return t('admin.channels.form.pricingLookupLoading')
      case 'priced': return ''
      case 'manual_required': return t('admin.channels.form.pricingLookupManualRequired')
      case 'unsupported_unit': return t('admin.channels.form.pricingLookupUnsupportedUnit')
      case 'error': return t('admin.channels.form.pricingLookupError')
      default: return ''
    }
  })()

  // 展示用状态：本次会话的查价结果优先；没有进行中的查价时，回落到条目自带的
  // 参考价元数据——同步创建出来的 manual/unsupported 规则也必须把原因显示出来，
  // 不能静默摆一条空规则。运营者已经填过价（含显式 0）时提示让位。
  const displayStatus: LookupStatus = (() => {
    if (lookupStatus.state !== 'idle') return lookupStatus
    const reference = entry.reference
    if (!reference) return { state: 'idle' }
    if (reference.status === 'priced') {
      return { state: 'priced', source: reference.source, matchedModel: reference.matched_model }
    }
    if (hasAnyPricingValue(entry)) return { state: 'idle' }
    return nonPricedLookupStatus(reference)
  })()

  // 参考价来源说明：design 要求 UI 显示来源，proxy_reference 必须明示
  // 「非供应商公开价」。matched 与请求型号不同时一并展示，避免运营者
  // 以为价就是自己输入的那个型号的价。
  const lookupSourceMessage = (() => {
    const status = displayStatus
    if (status.state !== 'priced') return ''
    const sourceKey = {
      release_catalog: 'pricingLookupSourceCatalog',
      builtin_fallback: 'pricingLookupSourceFallback',
      proxy_reference: 'pricingLookupSourceProxy',
      none: 'pricingLookupSourceNone',
    }[status.source] ?? 'pricingLookupSourceNone'
    const source = t(`admin.channels.form.${sourceKey}`)
    const requested = entry.models[0]
    return status.matchedModel && status.matchedModel !== requested
      ? `${source}（${status.matchedModel}）`
      : source
  })()

  return (
    <div className="rounded-lg border border-gray-200 surface-inset p-3 dark:border-dark-600">
      <div className="flex cursor-pointer select-none items-center gap-2" onClick={() => setCollapsed((prev) => !prev)}>
        {collapsed ? (
          <ChevronRight className="flex-shrink-0 h-4 w-4 text-gray-400 transition-transform duration-200" strokeWidth={2} />
        ) : (
          <ChevronDown className="flex-shrink-0 h-4 w-4 text-gray-400 transition-transform duration-200" strokeWidth={2} />
        )}

        {collapsed ? (
          <div className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
              {entry.models.slice(0, 3).map((m, i) => (
                <span key={`${m}-${i}`} className={`inline-flex shrink-0 rounded px-1.5 py-0.5 text-xs ${getPlatformTagClass(platform || '')}`}>
                  {m}
                </span>
              ))}
              {entry.models.length > 3 && <span className="whitespace-nowrap text-xs surface-muted">+{entry.models.length - 3}</span>}
              {entry.models.length === 0 && <span className="text-xs italic surface-muted">{t('admin.channels.form.noModels')}</span>}
            </div>
            <span className="flex-shrink-0 rounded-full bg-primary-100/[0.15] px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {billingModeLabel}
            </span>
          </div>
        ) : (
          <div className="flex-1 text-xs font-medium surface-muted">{t('admin.channels.form.pricingEntry')}</div>
        )}

        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            onRemove()
          }}
          className="flex-shrink-0 rounded p-1 hover:text-red-500 surface-control surface-muted"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      </div>

      <div className={cn('collapsible-content', collapsed && 'collapsible-content--collapsed')}>
        <div className="collapsible-inner">
          <div className="mt-3 flex items-start gap-2">
            <div className="flex-1">
              <label className="text-xs font-medium surface-muted">
                {t('admin.channels.form.models')} <span className="text-red-500">*</span>
              </label>
              <div className="mt-1">
                <ModelTagInput
                  models={entry.models}
                  platform={platform}
                  onModelsChange={(models) => void onModelsUpdate(models)}
                  placeholder={t('admin.channels.form.modelsPlaceholder')}
                />
              </div>
            </div>
            <div className="w-40">
              <label className="text-xs font-medium surface-muted">{t('admin.channels.form.billingMode')}</label>
              <div className="mt-1">
                <Select
                  value={entry.billing_mode}
                  options={billingModeOptions}
                  searchable={false}
                  onValueChange={(value) =>
                    onUpdate({
                      ...entry,
                      billing_mode: value as BillingMode,
                      intervals: [],
                      time_pricing: { ...entry.time_pricing, periods: [] },
                    })
                  }
                />
              </div>
            </div>
          </div>

          {/* 参考价查价状态：manual / unsupported / 失败必须可见，不能静默吞掉 */}
          {displayStatus.state !== 'idle' && displayStatus.state !== 'priced' && (
            <div
              className={`mt-2 flex flex-wrap items-center gap-2 rounded border border-dashed px-2 py-1.5 text-xs ${
                displayStatus.state === 'loading'
                  ? 'border-gray-300 text-gray-500 dark:border-dark-500 dark:text-gray-400'
                  : 'border-amber-300 text-amber-700 dark:border-amber-700 dark:text-amber-300'
              }`}
              data-testid="pricing-lookup-status"
            >
              <span>{lookupMessage}</span>
              <button
                type="button"
                className="text-primary-600 underline hover:text-primary-700 surface-control"
                data-testid="pricing-lookup-retry"
                onClick={() => void completeEmptyFields()}
              >
                {t('admin.channels.form.pricingLookupRetry')}
              </button>
            </div>
          )}

          {/* 参考价来源：priced 时标明价从哪来 */}
          {lookupSourceMessage && (
            <div className="mt-1 text-xs surface-muted" data-testid="pricing-lookup-source">
              {lookupSourceMessage}
            </div>
          )}

          {entry.billing_mode === 'token' && (
            <div>
              <label className="mt-3 block text-xs font-medium surface-muted">
                {t('admin.channels.form.defaultPrices')}
                <span className="ml-1 font-normal surface-muted">$/MTok</span>
              </label>
              <div className="pricing-default-grid mt-1 grid gap-2">
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.inputPrice')}</label>
                  <Input value={entry.input_price} onValueChange={(value) => emitField('input_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.outputPrice')}</label>
                  <Input value={entry.output_price} onValueChange={(value) => emitField('output_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.cacheWrite5mPrice')}</label>
                  <Input value={entry.cache_write_price} onValueChange={(value) => emitField('cache_write_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.cacheWrite1hPrice')}</label>
                  <Input value={entry.cache_write_1h_price} onValueChange={(value) => emitField('cache_write_1h_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.cacheReadPrice')}</label>
                  <Input value={entry.cache_read_price} onValueChange={(value) => emitField('cache_read_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.imageInputPrice')}</label>
                  <Input value={entry.image_input_price} onValueChange={(value) => emitField('image_input_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
                <div>
                  <label className="text-xs surface-muted">{t('admin.channels.form.imageTokenPrice')}</label>
                  <Input value={entry.image_output_price} onValueChange={(value) => emitField('image_output_price', value)} type="number" step="any" min="0" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
                </div>
              </div>

              {enableTierMultipliers && (
                <div className="mt-3 grid max-w-md grid-cols-1 gap-2 sm:grid-cols-2">
                  <div>
                    <label className="text-xs surface-muted">{t('admin.channels.form.fastMultiplier')}</label>
                    <Input value={entry.fast_multiplier} onValueChange={(value) => emitField('fast_multiplier', value)} type="number" step="any" min="0.000001" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.multiplierPlaceholder')} />
                  </div>
                  <div>
                    <label className="text-xs surface-muted">{t('admin.channels.form.flexMultiplier')}</label>
                    <Input value={entry.flex_multiplier} onValueChange={(value) => emitField('flex_multiplier', value)} type="number" step="any" min="0.000001" className="mt-0.5 text-sm" placeholder={t('admin.channels.form.multiplierPlaceholder')} />
                  </div>
                </div>
              )}

              {/* Channel token intervals; the group long-context toggle controls whether tiers apply. */}
              {!hideTokenIntervals && (
                <div className="mt-3">
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium surface-muted">
                      {t('admin.channels.form.intervals')}
                      <span className="ml-1 font-normal surface-muted">(min, max]</span>
                    </label>
                    <button type="button" onClick={addInterval} className="text-xs text-primary-600 hover:text-primary-700 surface-control">
                      + {t('admin.channels.form.addInterval')}
                    </button>
                  </div>
                  {entry.intervals && entry.intervals.length > 0 && (
                    <div className="mt-2 space-y-2">
                      {entry.intervals.map((iv, idx) => (
                        <IntervalRow
                          key={`interval-${idx}`}
                          interval={iv}
                          mode={entry.billing_mode}
                          enableMultipliers={enableTierMultipliers}
                          onUpdate={(updated) => updateInterval(idx, updated)}
                          onRemove={() => removeInterval(idx)}
                        />
                      ))}
                    </div>
                  )}
                </div>
              )}

              {enableTimePricing && (
                <div className="mt-3">
                  <TimePricingSection
                    value={entry.time_pricing}
                    onChange={(time_pricing) => onUpdate({ ...entry, time_pricing })}
                  />
                </div>
              )}
            </div>
          )}

          {entry.billing_mode === 'per_request' && (
            <div>
              <label className="mt-3 block text-xs font-medium surface-muted">
                {t('admin.channels.form.defaultPerRequestPrice')}
                <span className="ml-1 font-normal surface-muted">$</span>
              </label>
              <div className="mt-1 w-48">
                <Input value={entry.per_request_price} onValueChange={(value) => emitField('per_request_price', value)} type="number" step="any" min="0" className="text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
              </div>

              <div className="mt-3 flex items-center justify-between">
                <label className="text-xs font-medium surface-muted">{t('admin.channels.form.requestTiers')}</label>
                <button type="button" onClick={addInterval} className="text-xs text-primary-600 hover:text-primary-700 surface-control">
                  + {t('admin.channels.form.addTier')}
                </button>
              </div>
              {entry.intervals && entry.intervals.length > 0 ? (
                <div className="mt-2 space-y-2">
                  {entry.intervals.map((iv, idx) => (
                    <IntervalRow
                      key={`interval-${idx}`}
                      interval={iv}
                      mode={entry.billing_mode}
                      onUpdate={(updated) => updateInterval(idx, updated)}
                      onRemove={() => removeInterval(idx)}
                    />
                  ))}
                </div>
              ) : (
                <div className="mt-2 rounded border border-dashed border-gray-300 p-3 text-center text-xs dark:border-dark-500 surface-muted">
                  {t('admin.channels.form.noTiersYet')}
                </div>
              )}
            </div>
          )}

          {(entry.billing_mode === 'image' || entry.billing_mode === 'video') && (
            <div>
              <label className="mt-3 block text-xs font-medium surface-muted">
                {entry.billing_mode === 'video' ? t('admin.channels.form.defaultVideoPrice') : t('admin.channels.form.defaultImagePrice')}
                <span className="ml-1 font-normal surface-muted">$</span>
              </label>
              <div className="mt-1 w-48">
                <Input value={entry.per_request_price} onValueChange={(value) => emitField('per_request_price', value)} type="number" step="any" min="0" className="text-sm" placeholder={t('admin.channels.form.pricePlaceholder')} />
              </div>

              <div className="mt-3 flex items-center justify-between">
                <label className="text-xs font-medium surface-muted">
                  {entry.billing_mode === 'video' ? t('admin.channels.form.videoTiers') : t('admin.channels.form.imageTiers')}
                </label>
                <button type="button" onClick={addMediaTier} className="text-xs text-primary-600 hover:text-primary-700 surface-control">
                  + {t('admin.channels.form.addTier')}
                </button>
              </div>
              {entry.intervals && entry.intervals.length > 0 && (
                <div className="mt-2 space-y-2">
                  {entry.intervals.map((iv, idx) => (
                    <IntervalRow
                      key={`interval-${idx}`}
                      interval={iv}
                      mode={entry.billing_mode}
                      onUpdate={(updated) => updateInterval(idx, updated)}
                      onRemove={() => removeInterval(idx)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}

          <div className="mt-3 border-t border-gray-200 pt-3 dark:border-dark-600" data-testid="reasoning-effort-multipliers">
            <div className="flex items-center justify-between gap-2">
              <label className="text-xs font-medium surface-muted">{t('admin.channels.form.reasoningEffortMultipliers')}</label>
              {Object.keys(entry.reasoning_effort_multipliers || {}).length > 0 && (
                <button
                  type="button"
                  className="text-xs hover:text-red-500 surface-control surface-muted"
                  onClick={() => onUpdate({ ...entry, reasoning_effort_multipliers: null })}
                >
                  {t('admin.channels.form.clearReasoningEffortMultipliers')}
                </button>
              )}
            </div>
            <p className="mt-1 text-xs surface-muted">{t('admin.channels.form.reasoningEffortMultipliersHint')}</p>
            <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
              {REASONING_EFFORT_LEVELS.map((effort) => (
                <label key={effort} className="text-xs surface-muted">
                  {effort}
                  <Input
                    value={entry.reasoning_effort_multipliers?.[effort] ?? ''}
                    aria-label={t('admin.channels.form.reasoningEffortMultiplierLabel', { effort })}
                    aria-invalid={!isValidPositiveMultiplier(entry.reasoning_effort_multipliers?.[effort]) ? 'true' : undefined}
                    data-reasoning-effort={effort}
                    onValueChange={(value) => updateReasoningEffortMultiplier(effort, value)}
                    type="number"
                    step="any"
                    min="0"
                    className="mt-0.5 text-sm"
                    placeholder={t('admin.channels.form.reasoningEffortMultiplierDefault')}
                  />
                </label>
              ))}
            </div>
            {reasoningEffortMultiplierError && (
              <p role="alert" className="mt-1 text-xs text-red-500">
                {reasoningEffortMultiplierError}
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default PricingEntryCard
