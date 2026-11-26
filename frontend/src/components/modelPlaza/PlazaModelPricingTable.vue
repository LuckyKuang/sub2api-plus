<template>
  <div class="record-table-scroll table-container plaza-pricing-table overflow-x-auto" :style="accentStyle">
    <table class="record-table w-full min-w-[1000px] table-auto border-collapse text-sm tabular-nums">
      <colgroup>
        <col class="w-[25%]" />
        <col class="w-[11%]" />
        <col class="w-[9%]" />
        <col class="w-[14%]" />
        <col class="w-[11%]" />
        <col class="w-[8%]" />
        <col class="w-[14%]" />
        <col class="w-[8%]" />
      </colgroup>
      <thead>
        <tr
          class="text-xs font-semibold uppercase tracking-wider surface-muted"
        >
          <th
            rowspan="2"
            data-header-edge-bottom
            class="border-r border-[var(--surface-edge)] py-2.5 pl-5 pr-4 text-left align-middle"
          >
            {{ t('modelPlaza.table.model') }}
          </th>
          <th colspan="3" class="pt-2 text-center">
            <div class="pz-title pb-2 font-semibold">
              {{ t('modelPlaza.table.paidPrice') }}
              <span class="pz-unit ml-1 normal-case font-normal">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th
            colspan="3"
            class="border-l border-[var(--surface-edge)] pt-2 text-center"
          >
            <div class="pb-2 surface-muted">
              {{ t('modelPlaza.table.officialPrice') }}
              <span class="ml-1 normal-case font-normal surface-muted">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th
            rowspan="2"
            data-header-edge-bottom
            class="border-l border-[var(--surface-edge)] py-2.5 pl-3 pr-5 text-right align-middle"
          >
            {{ t('modelPlaza.table.rate') }}
          </th>
        </tr>
        <tr
          class="border-b border-[var(--surface-edge)] text-left text-[11px] font-medium uppercase leading-4 tracking-wide surface-muted"
        >
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.input') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
          <th class="border-l border-[var(--surface-edge)] px-3 py-2 font-medium">
            {{ t('modelPlaza.table.input') }}
          </th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
        </tr>
      </thead>
      <tbody v-for="group in rowGroups" :key="group.key" class="record-group">
        <tr
          v-for="({ model: m, period, key }, rowIndex) in group.rows"
          :key="key"
          class="transition-colors"
        >
          <!-- 模型名 + 非 token 计费模式徽章;分时时段行额外标注时段 -->
          <td :data-record-edges="recordEdges(rowIndex, group.rows.length, 'left')" class="py-2.5 pl-5 pr-4 align-middle">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="inline-flex min-w-0 items-center gap-1">
                <button
                  type="button"
                  class="min-w-0 break-all text-left font-medium text-gray-900 hover:underline focus-visible:underline dark:text-white"
                  @click="emit('open', m)"
                >{{ m.name }}</button>
                <button
                  type="button"
                  class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                  :title="t('modelPlaza.table.copyModelId', { model: m.name })"
                  :aria-label="t('modelPlaza.table.copyModelId', { model: m.name })"
                  @click="copyToClipboard(m.name)"
                >
                  <Icon name="copy" size="sm" aria-hidden="true" />
                </button>
              </span>
              <!-- 时段徽章紧跟模型名,其余徽章排在后面,空间不足时先换行的是它们 -->
              <span
                v-if="period"
                class="inline-flex items-center whitespace-nowrap rounded-md surface-inset px-1 py-0.5 font-mono text-[10px] font-medium surface-muted"
                :title="timePricingRowHint(m)"
              >
                <span v-if="m.time_pricing?.weekdays_only" class="mr-1 font-sans">{{
                  t('modelPlaza.table.timePricingWeekdays')
                }}</span>
                {{ formatTimeWindow(period) }}
              </span>
              <span
                v-if="platform && m.platform !== platform"
                :class="[
                  'inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium',
                  platformBadgeLightClass(m.platform)
                ]"
              >
                {{ platformLabel(m.platform) }}
              </span>
              <span
                v-if="billingMode(m) !== BILLING_MODE_TOKEN"
                class="rounded-md surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted"
              >
                {{ billingModeLabel(m) }}
              </span>
              <span
                v-if="m.long_context_basis === 'marginal'"
                class="rounded-md surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted"
                :title="t('modelPlaza.table.tierHintMarginal')"
              >
                {{ t('modelPlaza.table.marginalBadge') }}
              </span>
              <span
                v-for="([effort, multiplier]) in reasoningEffortMultipliers(m)"
                :key="effort"
                class="rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                :title="t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier })"
                :data-reasoning-effort="effort"
              >
                {{ t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier }) }}
              </span>
            </div>
          </td>

          <!-- token 计费:输入 / 输出 / 缓存(写/读),有阶梯时每档一行;档位标签只放输入列,其余列按行对齐 -->
          <template v-if="billingMode(m) === BILLING_MODE_TOKEN">
            <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" class="px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="whitespace-nowrap text-xs leading-5"
                >
                  <span class="mr-1 font-sans font-normal surface-muted" :title="tierHint(m)">{{ tierLabel(iv) }}</span>
                  {{ paidPerMillion(iv.input_price, period) }}
                </div>
              </template>
              <template v-else>{{ paidPerMillion(m.pricing?.input_price, period) }}</template>
            </td>
            <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" class="px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="whitespace-nowrap text-xs leading-5"
                  :title="tierHint(m)"
                >
                  {{ paidPerMillion(iv.output_price, period) }}
                </div>
              </template>
              <template v-else>{{ paidPerMillion(m.pricing?.output_price, period) }}</template>
            </td>
            <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" class="px-3 py-2.5 align-middle">
              <template v-if="hasTierCachePricing(tokenIntervals(m))">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="whitespace-nowrap font-mono text-xs leading-5 text-gray-800 dark:text-gray-200"
                  :title="tierHint(m)"
                >
                  <template v-if="iv.cache_write_price != null || iv.cache_write_1h_price != null || iv.cache_write_12h_price != null || iv.cache_read_price != null">
                    <span class="font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheWriteShort') }}</span>
                    {{ paidPerMillion(iv.cache_write_price, period) }}
                    <template v-if="iv.cache_write_1h_price != null"
                      ><span class="font-sans font-normal surface-muted"> (1h </span>{{ paidPerMillion(iv.cache_write_1h_price, period)
                      }}<span class="font-sans font-normal surface-muted">)</span></template
                    ><template v-if="iv.cache_write_12h_price != null"
                      ><span class="font-sans font-normal surface-muted"> (12h </span>{{ paidPerMillion(iv.cache_write_12h_price, period)
                      }}<span class="font-sans font-normal surface-muted">)</span></template
                    >
                    <span class="ml-1 font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheReadShort') }}</span>
                    {{ paidPerMillion(iv.cache_read_price, period) }}
                  </template>
                  <span v-else class="surface-muted">-</span>
                </div>
              </template>
              <div
                v-else-if="hasCachePrices(m.pricing)"
                class="space-y-0.5 font-mono text-xs text-gray-800 dark:text-gray-200"
              >
                <div>
                  <span class="mr-1 font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheWrite') }}</span>
                  {{ paidPerMillion(m.pricing?.cache_write_price, period)
                  }}<template v-if="m.pricing?.cache_write_1h_price != null"
                    ><span class="font-sans font-normal surface-muted"> (1h </span>{{ paidPerMillion(m.pricing.cache_write_1h_price, period)
                    }}<span class="font-sans font-normal surface-muted">)</span></template
                  ><template v-if="m.pricing?.cache_write_12h_price != null"
                    ><span class="font-sans font-normal surface-muted"> (12h </span>{{ paidPerMillion(m.pricing.cache_write_12h_price, period)
                    }}<span class="font-sans font-normal surface-muted">)</span></template
                  >
                </div>
                <div>
                  <span class="mr-1 font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheRead') }}</span>
                  {{ paidPerMillion(m.pricing?.cache_read_price, period) }}
                </div>
              </div>
              <span v-else class="surface-muted">-</span>
            </td>
          </template>

          <!-- 按次 / 按图片计费:实付区整体合并,阶梯芯片或单一按次价 -->
          <template v-else>
            <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" colspan="3" class="px-3 py-2.5 align-middle">
              <div
                v-if="requestIntervals(m).length"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  v-for="(iv, idx) in requestIntervals(m)"
                  :key="idx"
                  class="inline-flex items-center gap-1 rounded-md surface-inset px-2 py-0.5 font-mono text-xs text-gray-800 dark:text-gray-200"
                >
                  <span class="font-sans surface-muted">{{ tierLabel(iv) }}</span>
                  {{ paidRequestPrice(m, iv.per_request_price)
                  }}<span class="font-sans surface-muted">{{ t(priceUnit(m)) }}</span>
                </span>
              </div>
              <template v-else-if="m.pricing?.per_request_price != null">
                <span class="font-mono font-semibold text-gray-900 dark:text-gray-50">
                  {{ paidRequestPrice(m, m.pricing.per_request_price) }}
                </span>
                <span class="ml-1 text-xs surface-muted">{{ t(priceUnit(m)) }}</span>
              </template>
              <span v-else class="surface-muted">-</span>
            </td>
          </template>

          <!-- 官方价格(参考价,不乘倍率;官方有阶梯时每档一行) -->
          <td
            :data-record-edges="recordEdges(rowIndex, group.rows.length)"
            class="px-3 py-2.5 align-middle font-mono text-xs surface-muted"
          >
            <template v-if="officialIntervals(m).length">
              <div
                v-for="(iv, idx) in officialIntervals(m)"
                :key="idx"
                class="whitespace-nowrap leading-5"
              >
                <span class="mr-1 font-sans surface-muted" :title="t('modelPlaza.table.tierHint')">{{ tierLabel(iv) }}</span>
                {{ tokenPrice(iv.input_price) }}
              </div>
            </template>
            <template v-else>{{ tokenPrice(m.official_pricing?.input_price) }}</template>
          </td>
          <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" class="px-3 py-2.5 align-middle font-mono text-xs surface-muted">
            <template v-if="officialIntervals(m).length">
              <div
                v-for="(iv, idx) in officialIntervals(m)"
                :key="idx"
                class="whitespace-nowrap leading-5"
                :title="t('modelPlaza.table.tierHint')"
              >
                {{ tokenPrice(iv.output_price) }}
              </div>
            </template>
            <template v-else>{{ tokenPrice(m.official_pricing?.output_price) }}</template>
          </td>
          <td :data-record-edges="recordEdges(rowIndex, group.rows.length)" class="px-3 py-2.5 align-middle">
            <template v-if="hasTierCachePricing(officialIntervals(m))">
              <div
                v-for="(iv, idx) in officialIntervals(m)"
                :key="idx"
                class="whitespace-nowrap font-mono text-xs leading-5 surface-muted"
                :title="t('modelPlaza.table.tierHint')"
              >
                <template v-if="iv.cache_write_price != null || iv.cache_write_1h_price != null || iv.cache_write_12h_price != null || iv.cache_read_price != null">
                  <span class="font-sans surface-muted">{{ t('modelPlaza.table.cacheWriteShort') }}</span>
                  {{ tokenPrice(iv.cache_write_price) }}
                  <template v-if="iv.cache_write_1h_price != null"
                    ><span class="font-sans surface-muted"> (1h </span>{{ tokenPrice(iv.cache_write_1h_price)
                    }}<span class="font-sans surface-muted">)</span></template
                  ><template v-if="iv.cache_write_12h_price != null"
                    ><span class="font-sans surface-muted"> (12h </span>{{ tokenPrice(iv.cache_write_12h_price)
                    }}<span class="font-sans surface-muted">)</span></template
                  >
                  <span class="ml-1 font-sans surface-muted">{{ t('modelPlaza.table.cacheReadShort') }}</span>
                  {{ tokenPrice(iv.cache_read_price) }}
                </template>
                <span v-else class="surface-muted">-</span>
              </div>
            </template>
            <div
              v-else-if="m.official_pricing && hasCachePrices(m.official_pricing)"
              class="space-y-0.5 font-mono text-xs surface-muted"
            >
              <div>
                <span class="mr-1 font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheWrite') }}</span>
                {{ tokenPrice(m.official_pricing.cache_write_price)
                }}<template v-if="m.official_pricing.cache_write_1h_price != null"
                  ><span class="font-sans surface-muted"> (1h </span>{{ tokenPrice(m.official_pricing.cache_write_1h_price)
                  }}<span class="font-sans surface-muted">)</span></template
                ><template v-if="m.official_pricing.cache_write_12h_price != null"
                  ><span class="font-sans surface-muted"> (12h </span>{{ tokenPrice(m.official_pricing.cache_write_12h_price)
                  }}<span class="font-sans surface-muted">)</span></template
                >
              </div>
              <div>
                <span class="mr-1 font-sans font-normal surface-muted">{{ t('modelPlaza.table.cacheRead') }}</span>
                {{ tokenPrice(m.official_pricing.cache_read_price) }}
              </div>
            </div>
            <span v-else class="surface-muted">-</span>
          </td>

          <!-- 折扣倍率(分时时段行展示 生效倍率×时段倍率;生图独立倍率行展示独立倍率;专属倍率划线展示原倍率) -->
          <td
            :data-record-edges="recordEdges(rowIndex, group.rows.length, 'right')"
            class="py-2.5 pl-3 pr-5 text-right align-middle font-mono text-xs"
          >
            <span
              v-if="period"
              class="font-bold text-primary-600 dark:text-primary-400"
              :title="t('modelPlaza.table.timePricingRateHint', { rate: effectiveRate, multiplier: period.multiplier })"
              >{{ periodRate(period) }}x</span
            >
            <span
              v-else-if="usesIndependentMediaRate(m, props)"
              class="font-bold surface-muted"
              >{{ modelRate(m, props) }}x</span
            >
            <template v-else-if="hasCustomRate">
              <span class="mr-1 line-through surface-muted">{{ rateMultiplier }}x</span>
              <span class="font-bold text-primary-600 dark:text-primary-400">{{ effectiveRate }}x</span>
            </template>
            <span v-else class="font-bold surface-muted">{{ effectiveRate }}x</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformAccentColor, platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO } from '@/constants/channel'
import type { PlazaModel, PlazaTimePricingPeriod } from '@/api/modelPlaza'
import {
  billingMode, formatTimeWindow, hasCachePrices, hasTierCachePricing, modelRate, officialIntervals, periodRate as applyPeriodRate,
  reasoningEffortMultipliers, requestIntervals, requestPrice, tierLabel, tokenIntervals,
  timePricing, tokenPrice, usesIndependentMediaRate, priceUnit
} from './pricing'

const props = defineProps<{
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
}>()

const emit = defineEmits<{ open: [model: PlazaModel] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()

/** 实付分区只从平台拿一个主色,浅底/标题/下划线全部由 scoped CSS 用 color-mix 派生。 */
const accentStyle = computed(() => ({ '--plaza-accent': platformAccentColor(props.platform ?? '') }))

/**
 * 展示顺序:
 * 1. token 计费的排在前,按图/按次计费的沉到末尾——它们的官方 token 价与实付的按张/按次价不同量纲,混排无意义;
 * 2. 组内按官方输出价从高到低,无官方价的排最后;
 * 3. 同价按名称降序(新版本号在前,如 gpt-5.6 先于 gpt-5.5)。
 */
const sortedModels = computed(() => {
  return [...props.models].sort((a, b) => {
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
})

const effectiveRate = computed(() => props.userRateMultiplier ?? props.rateMultiplier)
const hasCustomRate = computed(
  () => props.userRateMultiplier != null && props.userRateMultiplier !== props.rateMultiplier
)

function billingModeLabel(m: PlazaModel): string {
  switch (billingMode(m)) {
    case BILLING_MODE_IMAGE: return t('modelPlaza.table.perImage')
    case BILLING_MODE_VIDEO: return t('modelPlaza.table.perSecond')
    default: return t('modelPlaza.table.perRequest')
  }
}

/** 表格行:每个模型一行标准价;配置了分时倍率的模型再按时段各加一行。 */
interface PlazaRow {
  model: PlazaModel
  period: PlazaTimePricingPeriod | null
  key: string
}

const rowGroups = computed(() =>
  sortedModels.value.map((m) => {
    const base: PlazaRow = { model: m, period: null, key: `${m.platform}:${m.name}` }
    const periodRows = timePeriods(m).map<PlazaRow>((p, idx) => ({
      model: m,
      period: p,
      key: `${m.platform}:${m.name}:${idx}`
    }))
    return { key: base.key, rows: [base, ...periodRows] }
  })
)

function recordEdges(index: number, count: number, side?: 'left' | 'right'): string {
  const top = index === 0
  const bottom = index === count - 1
  return [
    top && 't', bottom && 'b',
    side === 'left' && 'l', side === 'right' && 'r',
    top && side === 'left' && 'tl', top && side === 'right' && 'tr',
    bottom && side === 'left' && 'bl', bottom && side === 'right' && 'br'
  ].filter(Boolean).join(' ')
}

/** 时段行的生效倍率 = 生效倍率 × 时段倍率(去掉浮点噪声)。 */
function periodRate(period: PlazaTimePricingPeriod): number {
  return applyPeriodRate(effectiveRate.value, period)
}

/** 实付价 = 渠道单价 × 生效倍率(时段行再乘时段倍率),按 $/1M token 展示。 */
function paidPerMillion(value: number | null | undefined, period: PlazaTimePricingPeriod | null = null): string {
  return tokenPrice(value, period ? periodRate(period) : effectiveRate.value)
}

function paidRequestPrice(m: PlazaModel, value: number | null | undefined): string {
  return requestPrice(value, modelRate(m, props))
}

/** 分时倍率时段(后端只给出倍率 ≠ 1 的时段,已升序)。 */
function timePeriods(m: PlazaModel): PlazaTimePricingPeriod[] {
  return timePricing(m)?.periods ?? []
}

/**
 * 时段行 tooltip:仅工作日生效的配置换用带周末回落说明的文案;
 * 分组启用高峰倍率时追加披露——本行价格不含高峰因子,与高峰窗口重叠的部分实付再乘高峰倍率。
 */
function timePricingRowHint(m: PlazaModel): string {
  const key = m.time_pricing?.weekdays_only
    ? 'modelPlaza.table.timePricingRowHintWeekdays'
    : 'modelPlaza.table.timePricingRowHint'
  let hint = t(key, { timezone: m.time_pricing?.timezone })
  if (props.peakWindow) {
    hint += t('modelPlaza.table.timePricingRowHintPeak', {
      window: props.peakWindow,
      multiplier: props.peakRateMultiplier ?? 1
    })
  }
  return hint
}

/** 档位说明:整单按档计价,或(平台旧规则)仅超出部分按档计价。 */
function tierHint(m: PlazaModel): string {
  return m.long_context_basis === 'marginal'
    ? t('modelPlaza.table.tierHintMarginal')
    : t('modelPlaza.table.tierHint')
}


</script>

<style scoped>
/* 实付分区文字色从平台主色派生；底色由外层卡片统一承载。 */
.plaza-pricing-table {
  --pz-title: color-mix(in srgb, var(--plaza-accent) 88%, black);
}

.dark .plaza-pricing-table {
  --pz-title: color-mix(in srgb, var(--plaza-accent) 70%, white);
}

.pz-title {
  /* color-mix 不可用的老浏览器回退为平台原色 */
  color: var(--plaza-accent);
  color: var(--pz-title);
}

.pz-unit {
  color: color-mix(in srgb, var(--pz-title) 62%, transparent);
}
</style>
