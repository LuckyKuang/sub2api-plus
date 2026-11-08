import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'
import { platformAccentColor, platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO } from '@/utils/billingMode'
import type { PlazaModel, PlazaTimePricingPeriod } from '@/api/modelPlaza'
import type { UserPricingInterval } from '@/api/channels'

const REASONING_EFFORT_LEVELS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const

type BillingMode = string

function reasoningEffortMultipliers(model: PlazaModel): [string, number][] {
  const multipliers = model.pricing?.reasoning_effort_multipliers
  return REASONING_EFFORT_LEVELS.flatMap((effort) => {
    const multiplier = multipliers?.[effort]
    return typeof multiplier === 'number' && Number.isFinite(multiplier) && multiplier > 0
      ? [[effort, multiplier] as [string, number]]
      : []
  })
}

export interface PlazaModelPricingTableProps {
  models: PlazaModel[]
  /** 分组平台;实付分区底色随平台着色,未知平台回退品牌青。 */
  platform?: string
  /** 分组默认倍率。 */
  rateMultiplier: number
  /** 用户专属倍率;与默认不同,实付价按此计算并划线展示原倍率。 */
  userRateMultiplier?: number | null
  /** 生图独立倍率:true 时图片计费模型的实付倍率取 imageRateMultiplier,不取分组/专属倍率。 */
  imageRateIndependent?: boolean
  imageRateMultiplier?: number | null
  /** 视频独立倍率开启时，覆盖分组/用户专属倍率，与后端视频计费保持一致。 */
  videoRateIndependent?: boolean
  videoRateMultiplier?: number | null
  /**
   * 高峰窗口描述(含倍率与服务器时区标注),空串/缺省 = 分组未启用高峰。
   * 表格所有价格均为不含高峰因子的口径,该窗口仅用于分时时段行的 tooltip 披露:
   * 与高峰重叠的部分实付还会再乘高峰倍率。
   */
  peakWindow?: string
  peakRateMultiplier?: number | null
}

/** 表格行:每个模型一行标准价;配置了分时倍率的模型再按时段各加一行。 */
interface PlazaRow {
  model: PlazaModel
  period: PlazaTimePricingPeriod | null
  key: string
}

const PER_MILLION = 1_000_000

/** 价格统一保底 2 位小数,更长的有效小数原样保留。 */
const MIN_DECIMALS = 2

function recordEdges(index: number, count: number, side?: 'left' | 'right'): string {
  const top = index === 0
  const bottom = index === count - 1
  return [
    top && 't',
    bottom && 'b',
    side === 'left' && 'l',
    side === 'right' && 'r',
    top && side === 'left' && 'tl',
    top && side === 'right' && 'tr',
    bottom && side === 'left' && 'bl',
    bottom && side === 'right' && 'br'
  ]
    .filter(Boolean)
    .join(' ')
}

function trimZero(n: number): string {
  return String(Math.round(n * 100) / 100)
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${trimZero(n / 1_000_000)}M`
  if (n >= 1_000) return `${trimZero(n / 1_000)}K`
  return String(n)
}

export function PlazaModelPricingTable({
  models,
  platform,
  rateMultiplier,
  userRateMultiplier = null,
  imageRateIndependent,
  imageRateMultiplier,
  videoRateIndependent,
  videoRateMultiplier,
  peakWindow = '',
  peakRateMultiplier
}: PlazaModelPricingTableProps) {
  const { t } = useTranslation()

  /** 实付分区只从平台拿一个主色,浅底/标题/下划线全部由 CSS 用 color-mix 派生。 */
  const accentStyle = { '--plaza-accent': platformAccentColor(platform ?? '') } as React.CSSProperties

  const effectiveRate = userRateMultiplier ?? rateMultiplier
  const hasCustomRate = userRateMultiplier != null && userRateMultiplier !== rateMultiplier

  function billingMode(model: PlazaModel): BillingMode {
    return (model.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
  }

  function billingModeLabel(model: PlazaModel): string {
    return billingMode(model) === BILLING_MODE_IMAGE
      ? t('modelPlaza.table.perImage')
      : t('modelPlaza.table.perRequest')
  }

  /**
   * 展示顺序:
   * 1. token 计费的排在前,按图/按次计费的沉到末尾——它们的官方 token 价与实付的按张/按次价不同量纲,混排无意义;
   * 2. 组内按官方输出价从高到低,无官方价的排最后;
   * 3. 同价按名称降序(新版本号在前,如 gpt-5.6 先于 gpt-5.5)。
   */
  const sortedModels = useMemo(() => {
    return [...models].sort((a, b) => {
      const ta = billingMode(a) === BILLING_MODE_TOKEN
      const tb = billingMode(b) === BILLING_MODE_TOKEN
      if (ta !== tb) return ta ? -1 : 1
      const pa = a.official_pricing?.output_price ?? null
      const pb = b.official_pricing?.output_price ?? null
      if (pa != null && pb != null && pa !== pb) return pb - pa
      if (pa != null && pb == null) return -1
      if (pa == null && pb != null) return 1
      return b.name.localeCompare(a.name)
    })
  }, [models])

  const rowGroups = useMemo(
    () =>
      sortedModels.map((model) => {
        const base: PlazaRow = { model, period: null, key: `${model.platform}:${model.name}` }
        const periodRows: PlazaRow[] = (model.time_pricing?.periods ?? []).map((period, index) => ({
          model,
          period,
          key: `${model.platform}:${model.name}:${index}`
        }))
        return { key: base.key, rows: [base, ...periodRows] }
      }),
    [sortedModels]
  )

  /** 时段行的生效倍率 = 生效倍率 × 时段倍率(去掉浮点噪声)。 */
  function periodRate(period: PlazaTimePricingPeriod): number {
    return Math.round(effectiveRate * period.multiplier * 1000) / 1000
  }

  /** 实付价 = 渠道单价 × 生效倍率(时段行再乘时段倍率),按 $/1M token 展示。 */
  function paidPerMillion(
    value: number | null | undefined,
    period: PlazaTimePricingPeriod | null = null
  ): string {
    if (value == null) return '-'
    const rate = period ? periodRate(period) : effectiveRate
    return formatScaled(value * rate, PER_MILLION, MIN_DECIMALS)
  }

  /** 图片和视频分别使用对应的独立倍率开关。 */
  function usesIndependentMediaRate(model: PlazaModel): boolean {
    return (
      (billingMode(model) === BILLING_MODE_IMAGE && imageRateIndependent === true) ||
      (billingMode(model) === BILLING_MODE_VIDEO && videoRateIndependent === true)
    )
  }

  /** 非 token 行的生效倍率，图片和视频可分别覆盖通用倍率。 */
  function requestRate(model: PlazaModel): number {
    if (billingMode(model) === BILLING_MODE_VIDEO && videoRateIndependent === true) {
      return Math.max(0, videoRateMultiplier ?? 1)
    }
    return billingMode(model) === BILLING_MODE_IMAGE && imageRateIndependent === true
      ? (imageRateMultiplier ?? 1)
      : effectiveRate
  }

  /** 非 token 单价乘该行生效倍率，不换算为每百万 token。 */
  function paidRequestPrice(model: PlazaModel, value: number | null | undefined): string {
    if (value == null) return '-'
    return formatScaled(value * requestRate(model), 1, MIN_DECIMALS)
  }

  /** 官方参考价不乘倍率。 */
  function official(value: number | null | undefined): string {
    if (value == null) return '-'
    return formatScaled(value, PER_MILLION, MIN_DECIMALS)
  }

  /** 非 token 计费的单位后缀:按图片 → “/ 张”,按次 → “/ 次”。 */
  function perUnitSuffix(model: PlazaModel): string {
    return billingMode(model) === BILLING_MODE_IMAGE
      ? t('modelPlaza.table.perUnitImage')
      : t('modelPlaza.table.perUnitRequest')
  }

  function hasCachePricing(model: PlazaModel): boolean {
    return (
      model.pricing?.cache_write_price != null ||
      model.pricing?.cache_write_1h_price != null ||
      model.pricing?.cache_read_price != null
    )
  }

  function hasOfficialCache(officialPricing: NonNullable<PlazaModel['official_pricing']>): boolean {
    return (
      officialPricing.cache_write_price != null ||
      officialPricing.cache_read_price != null ||
      officialPricing.cache_write_1h_price != null
    )
  }

  /**
   * 时段行 tooltip:仅工作日生效的配置换用带周末回落说明的文案;
   * 分组启用高峰倍率时追加披露——本行价格不含高峰因子,与高峰窗口重叠的部分实付再乘高峰倍率。
   */
  function timePricingRowHint(model: PlazaModel): string {
    const key = model.time_pricing?.weekdays_only
      ? 'modelPlaza.table.timePricingRowHintWeekdays'
      : 'modelPlaza.table.timePricingRowHint'
    let hint = t(key, { timezone: model.time_pricing?.timezone })
    if (peakWindow) {
      hint += t('modelPlaza.table.timePricingRowHintPeak', {
        window: peakWindow,
        multiplier: peakRateMultiplier ?? 1
      })
    }
    return hint
  }

  /** “00:30–08:30”;整分钟的 HH:mm:ss 省略秒。 */
  function formatTimeWindow(period: PlazaTimePricingPeriod): string {
    const clock = (value: string) => value.replace(/^(\d{2}:\d{2}):00$/, '$1')
    return `${clock(period.start_time)}–${clock(period.end_time)}`
  }

  /** 上下文档位按下限升序展示(后端已升序,此处兜底)。 */
  function sortByContext(intervals: UserPricingInterval[]): UserPricingInterval[] {
    return [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)
  }

  /** token 模式的阶梯定价(内联进输入/输出/缓存列)。 */
  function tokenIntervals(model: PlazaModel): UserPricingInterval[] {
    return sortByContext(model.pricing?.intervals ?? []).map((interval) =>
      resolveIntervalPrices(interval, model.pricing!)
    )
  }

  /** 官方阶梯(后端按目录规则合成,不受分组开关影响)。 */
  function officialIntervals(model: PlazaModel): UserPricingInterval[] {
    return sortByContext(model.official_pricing?.intervals ?? [])
  }

  /** 任一档带缓存价才按档渲染缓存列;否则沿用平价的写入/读取两行。 */
  function hasTierCachePricing(intervals: UserPricingInterval[]): boolean {
    return intervals.some(
      (interval) =>
        interval.cache_write_price != null ||
        interval.cache_write_1h_price != null ||
        interval.cache_read_price != null ||
        interval.cache_write_multiplier != null ||
        interval.cache_read_multiplier != null
    )
  }

  /** 档位说明:整单按档计价,或(平台旧规则)仅超出部分按档计价。 */
  function tierHint(model: PlazaModel): string {
    return model.long_context_basis === 'marginal'
      ? t('modelPlaza.table.tierHintMarginal')
      : t('modelPlaza.table.tierHint')
  }

  /** 按次/按图模式的阶梯定价(仅保留配了按次价的档位)。 */
  function requestIntervals(model: PlazaModel): UserPricingInterval[] {
    return (model.pricing?.intervals ?? []).filter((interval) => interval.per_request_price != null)
  }

  /**
   * 档位标签:优先后端/管理员给出的 tier_label,否则按区间生成统一形态——
   * 有上限为「≤上限」,末档为「>下限」;档位升序排列,相邻的 ≤100K / ≤200K 即表示 (100K,200K]。
   */
  function tierLabel(interval: UserPricingInterval): string {
    if (interval.tier_label) return interval.tier_label
    const { min_tokens: min, max_tokens: max } = interval
    return max == null ? `>${formatTokenCount(min)}` : `≤${formatTokenCount(max)}`
  }

  return (
    <div className="record-table-scroll table-container plaza-pricing-table overflow-x-auto" style={accentStyle}>
      <table className="record-table w-full min-w-[1000px] table-auto border-collapse text-sm tabular-nums">
        <colgroup>
          <col className="w-[25%]" />
          <col className="w-[11%]" />
          <col className="w-[9%]" />
          <col className="w-[14%]" />
          <col className="w-[11%]" />
          <col className="w-[8%]" />
          <col className="w-[14%]" />
          <col className="w-[8%]" />
        </colgroup>
        <thead>
          <tr className="text-xs font-semibold uppercase tracking-wider surface-muted">
            <th
              rowSpan={2}
              data-header-edge-bottom
              className="border-r border-gray-100 py-2.5 pl-5 pr-4 text-left align-middle dark:border-dark-700/60"
            >
              {t('modelPlaza.table.model')}
            </th>
            <th colSpan={3} className="pt-2 text-center">
              <div className="pz-title pb-2 font-semibold">
                {t('modelPlaza.table.paidPrice')}
                <span className="pz-unit ml-1 normal-case font-normal">
                  {t('modelPlaza.table.unitPerMillion')}
                </span>
              </div>
            </th>
            <th
              colSpan={3}
              className="border-l border-gray-100 pt-2 text-center dark:border-dark-700/60"
            >
              <div className="pb-2 surface-muted">
                {t('modelPlaza.table.officialPrice')}
                <span className="ml-1 normal-case font-normal surface-muted">
                  {t('modelPlaza.table.unitPerMillion')}
                </span>
              </div>
            </th>
            <th
              rowSpan={2}
              data-header-edge-bottom
              className="border-l border-gray-100 py-2.5 pl-3 pr-5 text-right align-middle dark:border-dark-700/60"
            >
              {t('modelPlaza.table.rate')}
            </th>
          </tr>
          <tr className="border-b border-gray-200 text-left text-[11px] font-medium uppercase leading-4 tracking-wide dark:border-dark-700 surface-muted">
            <th className="px-3 py-2 font-medium">{t('modelPlaza.table.input')}</th>
            <th className="px-3 py-2 font-medium">{t('modelPlaza.table.output')}</th>
            <th className="px-3 py-2 font-medium">{t('modelPlaza.table.cache')}</th>
            <th className="border-l border-gray-100 px-3 py-2 font-medium dark:border-dark-700/60">
              {t('modelPlaza.table.input')}
            </th>
            <th className="px-3 py-2 font-medium">{t('modelPlaza.table.output')}</th>
            <th className="px-3 py-2 font-medium">{t('modelPlaza.table.cache')}</th>
          </tr>
        </thead>

        {rowGroups.map((group) => (
          <tbody key={group.key} className="record-group">
            {group.rows.map(({ model: m, period, key }, rowIndex) => (
              <tr key={key} className="transition-colors">
                {/* 模型名 + 非 token 计费模式徽章;分时时段行额外标注时段 */}
                <td
                  data-record-edges={recordEdges(rowIndex, group.rows.length, 'left')}
                  className="py-2.5 pl-5 pr-4 align-middle"
                >
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="font-medium text-gray-900 dark:text-white">{m.name}</span>
                    {/* 时段徽章紧跟模型名,其余徽章排在后面,空间不足时先换行的是它们 */}
                    {period && (
                      <span
                        className="inline-flex items-center whitespace-nowrap rounded-md surface-inset px-1 py-0.5 font-mono text-[10px] font-medium surface-muted"
                        title={timePricingRowHint(m)}
                      >
                        {m.time_pricing?.weekdays_only && (
                          <span className="mr-1 font-sans">{t('modelPlaza.table.timePricingWeekdays')}</span>
                        )}
                        {formatTimeWindow(period)}
                      </span>
                    )}
                    {platform && m.platform !== platform && (
                      <span
                        className={[
                          'inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium',
                          platformBadgeLightClass(m.platform)
                        ].join(' ')}
                      >
                        {platformLabel(m.platform)}
                      </span>
                    )}
                    {billingMode(m) !== BILLING_MODE_TOKEN && (
                      <span className="rounded-md surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
                        {billingModeLabel(m)}
                      </span>
                    )}
                    {m.long_context_basis === 'marginal' && (
                      <span
                        className="rounded-md surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted"
                        title={t('modelPlaza.table.tierHintMarginal')}
                      >
                        {t('modelPlaza.table.marginalBadge')}
                      </span>
                    )}
                    {reasoningEffortMultipliers(m).map(([effort, multiplier]) => (
                      <span
                        key={effort}
                        className="rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                        title={t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier })}
                        data-reasoning-effort={effort}
                      >
                        {t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier })}
                      </span>
                    ))}
                  </div>
                </td>

                {/* token 计费:输入 / 输出 / 缓存(写/读),有阶梯时每档一行;档位标签只放输入列,其余列按行对齐 */}
                {billingMode(m) === BILLING_MODE_TOKEN ? (
                  <>
                    <td
                      data-record-edges={recordEdges(rowIndex, group.rows.length)}
                      className="px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50"
                    >
                      {tokenIntervals(m).length ? (
                        tokenIntervals(m).map((interval, index) => (
                          <div key={index} className="whitespace-nowrap text-xs leading-5">
                            <span className="mr-1 font-sans font-normal surface-muted" title={tierHint(m)}>
                              {tierLabel(interval)}
                            </span>
                            {paidPerMillion(interval.input_price, period)}
                          </div>
                        ))
                      ) : (
                        paidPerMillion(m.pricing?.input_price, period)
                      )}
                    </td>
                    <td
                      data-record-edges={recordEdges(rowIndex, group.rows.length)}
                      className="px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50"
                    >
                      {tokenIntervals(m).length ? (
                        tokenIntervals(m).map((interval, index) => (
                          <div key={index} className="whitespace-nowrap text-xs leading-5" title={tierHint(m)}>
                            {paidPerMillion(interval.output_price, period)}
                          </div>
                        ))
                      ) : (
                        paidPerMillion(m.pricing?.output_price, period)
                      )}
                    </td>
                    <td
                      data-record-edges={recordEdges(rowIndex, group.rows.length)}
                      className="px-3 py-2.5 align-middle"
                    >
                      {hasTierCachePricing(tokenIntervals(m)) ? (
                        tokenIntervals(m).map((interval, index) => (
                          <div
                            key={index}
                            className="whitespace-nowrap font-mono text-xs leading-5 text-gray-800 dark:text-gray-200"
                            title={tierHint(m)}
                          >
                            {interval.cache_write_price != null ||
                            interval.cache_write_1h_price != null ||
                            interval.cache_read_price != null ? (
                              <>
                                <span className="font-sans font-normal surface-muted">
                                  {t('modelPlaza.table.cacheWriteShort')}
                                </span>
                                {paidPerMillion(interval.cache_write_price, period)}
                                {interval.cache_write_1h_price != null && (
                                  <>
                                    <span className="font-sans font-normal surface-muted"> (1h </span>
                                    {paidPerMillion(interval.cache_write_1h_price, period)}
                                    <span className="font-sans font-normal surface-muted">)</span>
                                  </>
                                )}
                                <span className="ml-1 font-sans font-normal surface-muted">
                                  {t('modelPlaza.table.cacheReadShort')}
                                </span>
                                {paidPerMillion(interval.cache_read_price, period)}
                              </>
                            ) : (
                              <span className="surface-muted">-</span>
                            )}
                          </div>
                        ))
                      ) : hasCachePricing(m) ? (
                        <div className="space-y-0.5 font-mono text-xs text-gray-800 dark:text-gray-200">
                          <div>
                            <span className="mr-1 font-sans font-normal surface-muted">
                              {t('modelPlaza.table.cacheWrite')}
                            </span>
                            {paidPerMillion(m.pricing?.cache_write_price, period)}
                            {m.pricing?.cache_write_1h_price != null && (
                              <>
                                <span className="font-sans font-normal surface-muted"> (1h </span>
                                {paidPerMillion(m.pricing.cache_write_1h_price, period)}
                                <span className="font-sans font-normal surface-muted">)</span>
                              </>
                            )}
                          </div>
                          <div>
                            <span className="mr-1 font-sans font-normal surface-muted">
                              {t('modelPlaza.table.cacheRead')}
                            </span>
                            {paidPerMillion(m.pricing?.cache_read_price, period)}
                          </div>
                        </div>
                      ) : (
                        <span className="surface-muted">-</span>
                      )}
                    </td>
                  </>
                ) : (
                  /* 按次 / 按图片计费:实付区整体合并,阶梯芯片或单一按次价 */
                  <td
                    data-record-edges={recordEdges(rowIndex, group.rows.length)}
                    colSpan={3}
                    className="px-3 py-2.5 align-middle"
                  >
                    {requestIntervals(m).length ? (
                      <div className="flex flex-wrap items-center gap-1.5">
                        {requestIntervals(m).map((interval, index) => (
                          <span
                            key={index}
                            className="inline-flex items-center gap-1 rounded-md surface-inset px-2 py-0.5 font-mono text-xs text-gray-800 dark:text-gray-200"
                          >
                            <span className="font-sans surface-muted">{tierLabel(interval)}</span>
                            {paidRequestPrice(m, interval.per_request_price)}
                            <span className="font-sans surface-muted">{perUnitSuffix(m)}</span>
                          </span>
                        ))}
                      </div>
                    ) : m.pricing?.per_request_price != null ? (
                      <>
                        <span className="font-mono font-semibold text-gray-900 dark:text-gray-50">
                          {paidRequestPrice(m, m.pricing.per_request_price)}
                        </span>
                        <span className="ml-1 text-xs surface-muted">{perUnitSuffix(m)}</span>
                      </>
                    ) : (
                      <span className="surface-muted">-</span>
                    )}
                  </td>
                )}

                {/* 官方价格(参考价,不乘倍率;官方有阶梯时每档一行) */}
                <td
                  data-record-edges={recordEdges(rowIndex, group.rows.length)}
                  className="px-3 py-2.5 align-middle font-mono text-xs surface-muted"
                >
                  {officialIntervals(m).length ? (
                    officialIntervals(m).map((interval, index) => (
                      <div key={index} className="whitespace-nowrap leading-5">
                        <span className="mr-1 font-sans surface-muted" title={t('modelPlaza.table.tierHint')}>
                          {tierLabel(interval)}
                        </span>
                        {official(interval.input_price)}
                      </div>
                    ))
                  ) : (
                    official(m.official_pricing?.input_price)
                  )}
                </td>
                <td
                  data-record-edges={recordEdges(rowIndex, group.rows.length)}
                  className="px-3 py-2.5 align-middle font-mono text-xs surface-muted"
                >
                  {officialIntervals(m).length ? (
                    officialIntervals(m).map((interval, index) => (
                      <div
                        key={index}
                        className="whitespace-nowrap leading-5"
                        title={t('modelPlaza.table.tierHint')}
                      >
                        {official(interval.output_price)}
                      </div>
                    ))
                  ) : (
                    official(m.official_pricing?.output_price)
                  )}
                </td>
                <td
                  data-record-edges={recordEdges(rowIndex, group.rows.length)}
                  className="px-3 py-2.5 align-middle"
                >
                  {hasTierCachePricing(officialIntervals(m)) ? (
                    officialIntervals(m).map((interval, index) => (
                      <div
                        key={index}
                        className="whitespace-nowrap font-mono text-xs leading-5 surface-muted"
                        title={t('modelPlaza.table.tierHint')}
                      >
                        {interval.cache_write_price != null ||
                        interval.cache_write_1h_price != null ||
                        interval.cache_read_price != null ? (
                          <>
                            <span className="font-sans surface-muted">
                              {t('modelPlaza.table.cacheWriteShort')}
                            </span>
                            {official(interval.cache_write_price)}
                            {interval.cache_write_1h_price != null && (
                              <>
                                <span className="font-sans surface-muted"> (1h </span>
                                {official(interval.cache_write_1h_price)}
                                <span className="font-sans surface-muted">)</span>
                              </>
                            )}
                            <span className="ml-1 font-sans surface-muted">
                              {t('modelPlaza.table.cacheReadShort')}
                            </span>
                            {official(interval.cache_read_price)}
                          </>
                        ) : (
                          <span className="surface-muted">-</span>
                        )}
                      </div>
                    ))
                  ) : m.official_pricing && hasOfficialCache(m.official_pricing) ? (
                    <div className="space-y-0.5 font-mono text-xs surface-muted">
                      <div>
                        <span className="mr-1 font-sans font-normal surface-muted">
                          {t('modelPlaza.table.cacheWrite')}
                        </span>
                        {official(m.official_pricing.cache_write_price)}
                        {m.official_pricing.cache_write_1h_price != null && (
                          <>
                            <span className="font-sans surface-muted"> (1h </span>
                            {official(m.official_pricing.cache_write_1h_price)}
                            <span className="font-sans surface-muted">)</span>
                          </>
                        )}
                      </div>
                      <div>
                        <span className="mr-1 font-sans font-normal surface-muted">
                          {t('modelPlaza.table.cacheRead')}
                        </span>
                        {official(m.official_pricing.cache_read_price)}
                      </div>
                    </div>
                  ) : (
                    <span className="surface-muted">-</span>
                  )}
                </td>

                {/* 折扣倍率(分时时段行展示 生效倍率×时段倍率;生图独立倍率行展示独立倍率;专属倍率划线展示原倍率) */}
                <td
                  data-record-edges={recordEdges(rowIndex, group.rows.length, 'right')}
                  className="py-2.5 pl-3 pr-5 text-right align-middle font-mono text-xs"
                >
                  {period ? (
                    <span
                      className="font-bold text-primary-600 dark:text-primary-400"
                      title={t('modelPlaza.table.timePricingRateHint', {
                        rate: effectiveRate,
                        multiplier: period.multiplier
                      })}
                    >
                      {periodRate(period)}x
                    </span>
                  ) : usesIndependentMediaRate(m) ? (
                    <span className="font-bold surface-muted">{requestRate(m)}x</span>
                  ) : hasCustomRate ? (
                    <>
                      <span className="mr-1 line-through surface-muted">{rateMultiplier}x</span>
                      <span className="font-bold text-primary-600 dark:text-primary-400">{effectiveRate}x</span>
                    </>
                  ) : (
                    <span className="font-bold surface-muted">{effectiveRate}x</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        ))}
      </table>
    </div>
  )
}

export default PlazaModelPricingTable
