<template>
  <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800">
    <!-- Collapsed summary header (clickable) -->
    <div
      class="flex cursor-pointer select-none items-center gap-2"
      @click="collapsed = !collapsed"
    >
      <Icon
        :name="collapsed ? 'chevronRight' : 'chevronDown'"
        size="sm"
        :stroke-width="2"
        class="flex-shrink-0 text-gray-400 transition-transform duration-200"
      />

      <!-- Summary: model tags + billing badge -->
      <div v-if="collapsed" class="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
        <!-- Compact model tags (show first 3) -->
        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1">
          <span
            v-for="(m, i) in entry.models.slice(0, 3)"
            :key="i"
            class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-xs"
            :class="getPlatformTagClass(props.platform || '')"
          >
            {{ m }}
          </span>
          <span
            v-if="entry.models.length > 3"
            class="whitespace-nowrap text-xs text-gray-400"
          >
            +{{ entry.models.length - 3 }}
          </span>
          <span
            v-if="entry.models.length === 0"
            class="text-xs italic text-gray-400"
          >
            {{ t('admin.channels.form.noModels') }}
          </span>
        </div>

        <!-- Billing mode badge -->
        <span
          class="flex-shrink-0 rounded-full bg-primary-100 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
        >
          {{ billingModeLabel }}
        </span>
      </div>

      <!-- Expanded: show the label "Pricing Entry" or similar -->
      <div v-else class="flex-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        {{ t('admin.channels.form.pricingEntry') }}
      </div>

      <!-- Remove button (always visible, stop propagation) -->
      <button
        type="button"
        @click.stop="emit('remove')"
        class="flex-shrink-0 rounded p-1 text-gray-400 hover:text-red-500"
      >
        <Icon name="trash" size="sm" />
      </button>
    </div>

    <!-- Expandable content with transition -->
    <div
      class="collapsible-content"
      :class="{ 'collapsible-content--collapsed': collapsed }"
    >
      <div class="collapsible-inner">
        <!-- Header: Models + Billing Mode -->
        <div class="mt-3 flex items-start gap-2">
          <div class="flex-1">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.models') }} <span class="text-red-500">*</span>
            </label>
            <ModelTagInput
              :models="entry.models"
              :platform="props.platform"
              @update:models="onModelsUpdate($event)"
              :placeholder="t('admin.channels.form.modelsPlaceholder')"
              class="mt-1"
            />
          </div>
          <div class="w-40">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.billingMode') }}
            </label>
            <Select
              :modelValue="entry.billing_mode"
              @update:modelValue="emit('update', {
                ...entry,
                billing_mode: $event as BillingMode,
                intervals: [],
                time_pricing: { ...entry.time_pricing, periods: [] },
              })"
              :options="billingModeOptions"
              class="mt-1"
            />
          </div>
        </div>

        <!-- 参考价查价状态：manual / unsupported / 失败必须可见，不能静默吞掉 -->
        <div
          v-if="displayStatus.state !== 'idle' && displayStatus.state !== 'priced'"
          class="mt-2 flex flex-wrap items-center gap-2 rounded border border-dashed px-2 py-1.5 text-xs"
          :class="displayStatus.state === 'loading'
            ? 'border-gray-300 text-gray-500 dark:border-dark-500 dark:text-gray-400'
            : 'border-amber-300 text-amber-700 dark:border-amber-700 dark:text-amber-300'"
          data-testid="pricing-lookup-status"
        >
          <span>{{ lookupMessage }}</span>
          <button
            type="button"
            class="text-primary-600 underline hover:text-primary-700"
            data-testid="pricing-lookup-retry"
            @click="completeEmptyFields()"
          >
            {{ t('admin.channels.form.pricingLookupRetry') }}
          </button>
        </div>

        <!-- 参考价来源：priced 时标明价从哪来；proxy_reference 必须明示
             「非供应商公开价」，避免运营者把代理价当成官方价背书。 -->
        <div
          v-if="lookupSourceMessage"
          class="mt-1 text-xs text-gray-500 dark:text-gray-400"
          data-testid="pricing-lookup-source"
        >
          {{ lookupSourceMessage }}
        </div>

        <!-- Token mode -->
        <div v-if="entry.billing_mode === 'token'">
          <!-- Default prices (fallback when no interval matches) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPrices') }}
            <span class="ml-1 font-normal text-gray-400">$/MTok</span>
          </label>
          <div class="pricing-default-grid mt-1 grid gap-2">
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.inputPrice') }}</label>
              <input :value="entry.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.outputPrice') }}</label>
              <input :value="entry.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheWrite5mPrice') }}</label>
              <input :value="entry.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheWrite1hPrice') }}</label>
              <input :value="entry.cache_write_1h_price" @input="emitField('cache_write_1h_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheReadPrice') }}</label>
              <input :value="entry.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageInputPrice') }}</label>
              <input :value="entry.image_input_price" @input="emitField('image_input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageTokenPrice') }}</label>
              <input :value="entry.image_output_price" @input="emitField('image_output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
          </div>

          <div v-if="enableTierMultipliers" class="mt-3 grid max-w-md grid-cols-1 gap-2 sm:grid-cols-2">
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.fastMultiplier') }}</label>
              <input :value="entry.fast_multiplier" @input="emitField('fast_multiplier', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0.000001" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.multiplierPlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.flexMultiplier') }}</label>
              <input :value="entry.flex_multiplier" @input="emitField('flex_multiplier', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0.000001" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.multiplierPlaceholder')" />
            </div>
          </div>

          <!-- Channel token intervals; the group long-context toggle controls whether tiers apply. -->
          <div v-if="!hideTokenIntervals" class="mt-3">
            <div class="flex items-center justify-between">
              <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.channels.form.intervals') }}
                <span class="ml-1 font-normal text-gray-400">(min, max]</span>
              </label>
              <button type="button" @click="addInterval" class="text-xs text-primary-600 hover:text-primary-700">
                + {{ t('admin.channels.form.addInterval') }}
              </button>
            </div>
            <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
              <IntervalRow
                v-for="(iv, idx) in entry.intervals"
                :key="idx"
                :interval="iv"
                :mode="entry.billing_mode"
                :enable-multipliers="enableTierMultipliers"
                @update="updateInterval(idx, $event)"
                @remove="removeInterval(idx)"
              />
            </div>
          </div>

          <TimePricingSection
            v-if="enableTimePricing"
            :model-value="entry.time_pricing"
            @update:model-value="emit('update', { ...entry, time_pricing: $event })"
          />
        </div>

        <!-- Per-request mode -->
        <div v-else-if="entry.billing_mode === 'per_request'">
          <!-- Default per-request price -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPerRequestPrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.requestTiers') }}
            </label>
            <button type="button" @click="addInterval" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
          <div v-else class="mt-2 rounded border border-dashed border-gray-300 p-3 text-center text-xs text-gray-400 dark:border-dark-500">
            {{ t('admin.channels.form.noTiersYet') }}
          </div>
        </div>

        <!-- Image/video mode -->
        <div v-else-if="entry.billing_mode === 'image' || entry.billing_mode === 'video'">
          <!-- Default image price (per-request, same as per_request mode) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ entry.billing_mode === 'video' ? t('admin.channels.form.defaultVideoPrice') : t('admin.channels.form.defaultImagePrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Image tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ entry.billing_mode === 'video' ? t('admin.channels.form.videoTiers') : t('admin.channels.form.imageTiers') }}
            </label>
            <button type="button" @click="addMediaTier" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
        </div>

        <div class="mt-3 border-t border-gray-200 pt-3 dark:border-dark-600" data-testid="reasoning-effort-multipliers">
          <div class="flex items-center justify-between gap-2">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.reasoningEffortMultipliers') }}
            </label>
            <button
              v-if="Object.keys(entry.reasoning_effort_multipliers || {}).length"
              type="button"
              class="text-xs text-gray-500 hover:text-red-500"
              @click="emit('update', { ...entry, reasoning_effort_multipliers: null })"
            >
              {{ t('admin.channels.form.clearReasoningEffortMultipliers') }}
            </button>
          </div>
          <p class="mt-1 text-xs text-gray-400">{{ t('admin.channels.form.reasoningEffortMultipliersHint') }}</p>
          <div class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
            <label v-for="effort in REASONING_EFFORT_LEVELS" :key="effort" class="text-xs text-gray-500 dark:text-gray-400">
              {{ effort }}
              <input
                :value="entry.reasoning_effort_multipliers?.[effort]"
                :aria-label="t('admin.channels.form.reasoningEffortMultiplierLabel', { effort })"
                :aria-invalid="!isValidPositiveMultiplier(entry.reasoning_effort_multipliers?.[effort])"
                :data-reasoning-effort="effort"
                @input="updateReasoningEffortMultiplier(effort, ($event.target as HTMLInputElement).value)"
                type="number"
                step="any"
                min="0"
                class="input mt-0.5 text-sm"
                :placeholder="t('admin.channels.form.reasoningEffortMultiplierDefault')"
              />
            </label>
          </div>
          <p v-if="reasoningEffortMultiplierError" role="alert" class="mt-1 text-xs text-red-500">
            {{ reasoningEffortMultiplierError }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import IntervalRow from './IntervalRow.vue'
import ModelTagInput from './ModelTagInput.vue'
import TimePricingSection from './TimePricingSection.vue'
import type { PricingFormEntry, IntervalFormEntry } from './types'
import {
  getPlatformTagClass,
  isValidPositiveMultiplier,
  referenceToPricingRule,
  validateReasoningEffortMultipliers,
} from './types'
import { REASONING_EFFORT_LEVELS, type ReasoningEffortLevel } from '@/constants/channel'
import type { BillingMode, ModelPricingReference, ModelPricingSource } from '@/api/admin/channels'
import channelsAPI from '@/api/admin/channels'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  entry: PricingFormEntry
  platform?: string
  hideTokenIntervals?: boolean
  enableTimePricing?: boolean
  enableTierMultipliers?: boolean
}>(), {
  hideTokenIntervals: false,
  enableTimePricing: false,
  enableTierMultipliers: false,
})

const emit = defineEmits<{
  update: [entry: PricingFormEntry]
  remove: []
  /** 多模型粘贴时，需要拆成独立规则的模型（父级追加新条目）。 */
  split: [models: string[]]
}>()

// Collapse state: entries with existing models default to collapsed
const collapsed = ref(props.entry.models.length > 0)

const billingModeOptions = computed(() => [
  { value: 'token', label: t('admin.channels.billingMode.token') },
  { value: 'per_request', label: t('admin.channels.billingMode.perRequest') },
  { value: 'image', label: t('admin.channels.billingMode.image') },
  { value: 'video', label: t('admin.channels.billingMode.video') }
])

const billingModeLabel = computed(() => {
  const opt = billingModeOptions.value.find(o => o.value === props.entry.billing_mode)
  return opt ? opt.label : props.entry.billing_mode
})

const reasoningEffortMultiplierError = computed(() =>
  validateReasoningEffortMultipliers(props.entry.reasoning_effort_multipliers, t)
)

function updateReasoningEffortMultiplier(effort: ReasoningEffortLevel, value: string) {
  const multipliers = { ...props.entry.reasoning_effort_multipliers }
  if (value === '') delete multipliers[effort]
  else multipliers[effort] = value
  emit('update', {
    ...props.entry,
    reasoning_effort_multipliers: Object.keys(multipliers).length ? multipliers : null,
  })
}

function emitField(field: keyof PricingFormEntry, value: string) {
  emit('update', { ...props.entry, [field]: value === '' ? null : value })
}

function addInterval() {
  const intervals = [...(props.entry.intervals || [])]
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null, per_request_price: null,
    input_multiplier: null, output_multiplier: null,
    cache_write_multiplier: null, cache_read_multiplier: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function addMediaTier() {
  const intervals = [...(props.entry.intervals || [])]
  const labels = props.entry.billing_mode === 'video'
    ? ['480p', '720p', '1080p']
    : ['1K', '2K', '4K', 'HD']
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: labels[intervals.length] || '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null, per_request_price: null,
    input_multiplier: null, output_multiplier: null,
    cache_write_multiplier: null, cache_read_multiplier: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function updateInterval(idx: number, updated: IntervalFormEntry) {
  const intervals = [...(props.entry.intervals || [])]
  intervals[idx] = updated
  emit('update', { ...props.entry, intervals })
}

function removeInterval(idx: number) {
  const intervals = [...(props.entry.intervals || [])]
  intervals.splice(idx, 1)
  emit('update', { ...props.entry, intervals })
}

// ── 参考价自动填充 ──────────────────────────────────────────

/** 每条规则的查价请求序号：晚到的旧响应必须被丢弃，不能覆盖用户后续输入。 */
const lookupRequestId = ref(0)

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

const lookupStatus = ref<LookupStatus>({ state: 'idle' })

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

async function onModelsUpdate(newModels: string[]) {
  const oldModels = props.entry.models
  const addedModels = newModels.filter(m => !oldModels.includes(m))

  // 目标模型列表：有新增时保留第一个新增（其余按既有策略拆分为独立规则）；
  // 仅删除时直接采用新列表（删除不再被丢弃，×/退格可移除误加的模型）。
  let nextModels: string[]
  if (addedModels.length > 0) {
    nextModels = [...oldModels, addedModels[0]]
    const splitModels = addedModels.slice(1)
    if (splitModels.length > 0) emit('split', splitModels)
  } else if (newModels.length !== oldModels.length) {
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
  const clearingStaleReference = primaryChanged && oldModels.length > 0
  let nextEntry: PricingFormEntry = { ...props.entry, models: nextModels }
  if (clearingStaleReference) {
    const target = nextEntry as unknown as Record<string, unknown>
    for (const f of [
      'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
      'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price',
      'fast_multiplier', 'flex_multiplier',
    ]) {
      target[f] = null
    }
    target.intervals = []
    target.reasoning_effort_multipliers = null
  }
  emit('update', nextEntry)

  // 仅在“新增模型”时触发参考价自动填充。
  const model = addedModels[0]?.trim()
  if (!model || !props.platform) return

  // 已有用户填写的价格/倍率时不覆盖（界面另有显式「补齐空字段」）。换模型时价格已
  // 在上面清空，这里自然放行、为新主模型重新查价；未换模型则维持既有保护。
  if (hasAnyPricingValue(nextEntry)) {
    lookupStatus.value = { state: 'idle' }
    return
  }

  const requestId = ++lookupRequestId.value
  lookupStatus.value = { state: 'loading' }
  try {
    const reference = await channelsAPI.getModelDefaultPricing(props.platform, model)
    if (requestId !== lookupRequestId.value) return

    if (reference.status !== 'priced' || !reference.pricing) {
      lookupStatus.value = nonPricedLookupStatus(reference)
      return
    }
    lookupStatus.value = {
      state: 'priced',
      source: reference.source,
      matchedModel: reference.matched_model,
    }
    const { entry } = referenceToPricingRule(reference)
    emit('update', { ...entry, models: nextModels })
  } catch {
    if (requestId !== lookupRequestId.value) return
    lookupStatus.value = { state: 'error' }
  }
}

/** 显式「补齐空字段」：只填回仍然为空的字段，已经填过（含 0）的不动。 */
async function completeEmptyFields() {
  const model = props.entry.models[0]?.trim()
  if (!model || !props.platform) return
  const requestId = ++lookupRequestId.value
  lookupStatus.value = { state: 'loading' }
  try {
    const reference = await channelsAPI.getModelDefaultPricing(props.platform, model)
    if (requestId !== lookupRequestId.value) return
    if (reference.status !== 'priced' || !reference.pricing) {
      lookupStatus.value = nonPricedLookupStatus(reference)
      return
    }
    lookupStatus.value = {
      state: 'priced',
      source: reference.source,
      matchedModel: reference.matched_model,
    }
    const { entry } = referenceToPricingRule(reference)
    const current = props.entry
    const filled = { ...current } as PricingFormEntry
    const target = filled as unknown as Record<string, unknown>
    for (const field of ['input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
      'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price',
      'fast_multiplier', 'flex_multiplier']) {
      if (target[field] === null || target[field] === undefined || target[field] === '') {
        target[field] = (entry as unknown as Record<string, unknown>)[field]
      }
    }
    if (!current.intervals?.length && entry.intervals.length) filled.intervals = entry.intervals
    if (!current.reasoning_effort_multipliers && entry.reasoning_effort_multipliers) {
      filled.reasoning_effort_multipliers = entry.reasoning_effort_multipliers
    }
    emit('update', filled)
  } catch {
    if (requestId !== lookupRequestId.value) return
    lookupStatus.value = { state: 'error' }
  }
}

const lookupMessage = computed(() => {
  switch (displayStatus.value.state) {
    case 'loading': return t('admin.channels.form.pricingLookupLoading')
    case 'priced': return ''
    case 'manual_required': return t('admin.channels.form.pricingLookupManualRequired')
    case 'unsupported_unit': return t('admin.channels.form.pricingLookupUnsupportedUnit')
    case 'error': return t('admin.channels.form.pricingLookupError')
    default: return ''
  }
})

// 展示用状态：本次会话的查价结果优先；没有进行中的查价时，回落到条目自带的
// 参考价元数据——同步创建出来的 manual/unsupported 规则也必须把原因显示出来，
// 不能静默摆一条空规则。运营者已经填过价（含显式 0）时提示让位。
const displayStatus = computed<LookupStatus>(() => {
  if (lookupStatus.value.state !== 'idle') return lookupStatus.value
  const reference = props.entry.reference
  if (!reference) return { state: 'idle' }
  if (reference.status === 'priced') {
    return { state: 'priced', source: reference.source, matchedModel: reference.matched_model }
  }
  if (hasAnyPricingValue(props.entry)) return { state: 'idle' }
  return nonPricedLookupStatus(reference)
})

// 参考价来源说明：design 要求 UI 显示来源，proxy_reference 必须明示
// 「非供应商公开价」。matched 与请求型号不同时一并展示，避免运营者
// 以为价就是自己输入的那个型号的价。
const lookupSourceMessage = computed(() => {
  const status = displayStatus.value
  if (status.state !== 'priced') return ''
  const sourceKey = {
    release_catalog: 'pricingLookupSourceCatalog',
    builtin_fallback: 'pricingLookupSourceFallback',
    proxy_reference: 'pricingLookupSourceProxy',
    none: 'pricingLookupSourceNone',
  }[status.source] ?? 'pricingLookupSourceNone'
  const source = t(`admin.channels.form.${sourceKey}`)
  const requested = props.entry.models[0]
  return status.matchedModel && status.matchedModel !== requested
    ? `${source}（${status.matchedModel}）`
    : source
})
</script>

<style scoped>
.pricing-default-grid {
  grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
}

.collapsible-content {
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.25s ease;
}

.collapsible-content--collapsed {
  grid-template-rows: 0fr;
}

.collapsible-inner {
  overflow: hidden;
}
</style>
