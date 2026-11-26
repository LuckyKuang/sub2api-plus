<template>
  <BaseDialog :show="!!model" :title="model?.name || t('availableChannels.catalog.details')" width="wide" close-on-click-outside @close="$emit('close')">
    <div v-if="model" :key="model.key" class="catalog-details-body min-w-0">
      <div class="catalog-dialog-topbar">
        <div class="catalog-dialog-meta">
          <span class="catalog-dialog-platform-pill" :class="platformTextClass(model.platform)">
            <PlatformIcon :platform="model.platform" size="sm" />
            <span>{{ model.platform }}</span>
          </span>
          <span class="catalog-dialog-quotes-count">
            {{ t('availableChannels.catalog.groupQuotes', { count: model.quotes.length }) }}
          </span>
        </div>

        <button
          type="button"
          class="catalog-copy-model btn btn-secondary shrink-0 text-xs"
          :class="{ 'is-copied': copied }"
          :aria-label="t('availableChannels.catalog.copyModel', { model: model.name })"
          @click="onCopyModel(model.name)"
        >
          <Icon :name="copied ? 'check' : 'copy'" size="sm" />
          <span>{{ t('common.copy') }} ID</span>
        </button>
      </div>

      <p class="catalog-dialog-note">{{ t('availableChannels.catalog.billingGroupNote') }}</p>

      <p v-if="rateUnavailable" class="text-sm text-amber-700 dark:text-amber-300">
        {{ t('availableChannels.catalog.ratesUnavailable') }}
      </p>

      <div v-if="model.quotes.length > 0" class="catalog-table-hint-row">
        <p class="catalog-table-hint">
          <strong class="catalog-table-hint-title">{{ t('availableChannels.catalog.quoteComparison') }}</strong>
          <span aria-hidden="true"> · </span>
          <span>{{ t('availableChannels.catalog.clickRowToExpand') }}</span>
        </p>

        <button
          type="button"
          class="catalog-toggle-all-btn focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :aria-pressed="showAllDisclosures"
          @click="toggleAllDisclosures"
        >
          {{ showAllDisclosures
            ? t('availableChannels.catalog.showSelectedOnly')
            : t('availableChannels.catalog.showAllDisclosures', { count: model.quotes.length }) }}
        </button>
      </div>

      <div
        class="record-table-scroll record-table-dialog-scroll max-w-full overflow-x-auto rounded-lg border border-[var(--surface-edge)]"
        tabindex="0"
        :aria-label="t('availableChannels.catalog.quoteComparison')"
      >
        <table class="record-table catalog-comparison w-full text-left text-xs surface-muted">
          <thead class="surface-inset surface-muted">
            <tr>
              <th class="px-3 py-2">{{ t('modelPlaza.filters.groupLabel') }}</th>
              <th class="px-3 py-2">{{ t('availableChannels.catalog.effectiveRateLabel') }}</th>
              <th class="px-3 py-2 text-right">{{ t('availableChannels.catalog.baseInputOrUnit') }}</th>
              <th class="px-3 py-2 text-right">{{ t('availableChannels.pricing.outputPrice') }}</th>
              <th class="px-3 py-2">{{ t('availableChannels.catalog.billingUnit') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[var(--surface-edge)]">
            <tr
              v-for="quote in model.quotes"
              :key="quote.offer.offer_key"
              class="catalog-comparison-row"
              :class="{ 'is-selected': openQuotes.has(quoteKey(quote)) }"
              @click="onComparisonRowClick(quote)"
            >
              <th class="min-w-28 break-words px-3 py-2 font-medium">
                <button
                  type="button"
                  class="catalog-row-toggle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
                  :aria-expanded="openQuotes.has(quoteKey(quote))"
                  @click.stop="onComparisonRowClick(quote)"
                >
                  <svg
                    class="catalog-row-chevron"
                    :class="{ 'is-open': openQuotes.has(quoteKey(quote)) }"
                    viewBox="0 0 12 12"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.8"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    aria-hidden="true"
                  >
                    <path d="M4.25 2.5L7.75 6l-3.5 3.5" />
                  </svg>
                  <span class="min-w-0 break-words text-left">
                    {{ quote.group.name }}<span
                      v-if="quote.group.is_exclusive"
                      class="whitespace-nowrap text-purple-700 dark:text-purple-300"
                    >{{ ' ' + t('availableChannels.exclusive') }}</span>
                  </span>
                </button>
              </th>
              <td class="min-w-28 px-3 py-2"><CatalogRateBadge :group="quote.group" :offer="quote.offer" /></td>
              <td class="whitespace-nowrap px-3 py-2 text-right font-mono tabular-nums">
                {{ catalogMoney(quotePrice(quote, false), 1, quote.offer.billing_unit === 'token') }}
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-right font-mono tabular-nums">
                {{ quote.offer.billing_unit === 'token' ? catalogMoney(quotePrice(quote, true), 1, true) : '—' }}
              </td>
              <td class="whitespace-nowrap px-3 py-2">{{ t(`availableChannels.catalog.units.${quote.offer.billing_unit}`) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <details
        v-for="quote in model.quotes"
        :key="quoteKey(quote)"
        :ref="el => setDisclosureRef(quoteKey(quote), el)"
        class="catalog-quote-disclosure min-w-0"
        :open="openQuotes.has(quoteKey(quote))"
        :hidden="!showAllDisclosures && !openQuotes.has(quoteKey(quote))"
      >
        <summary
          class="catalog-quote-summary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="{ 'is-open': openQuotes.has(quoteKey(quote)) }"
          @click.prevent="toggleQuote(quoteKey(quote))"
        >
          <span class="font-semibold">{{ quote.group.name }}</span>
          <span v-if="openQuotes.has(quoteKey(quote))" class="catalog-summary-action is-open">
            {{ t('availableChannels.catalog.collapseQuoteDetails') }}
            <span aria-hidden="true">▲</span>
          </span>
          <span v-else class="catalog-summary-action">
            {{ t('availableChannels.catalog.quoteDetails') }}
            <span aria-hidden="true">▼</span>
          </span>
        </summary>
        <CatalogQuoteDetails
          v-if="openQuotes.has(quoteKey(quote))"
          class="mt-3"
          :group="quote.group"
          :offer="quote.offer"
          :rate-unavailable="rateUnavailable"
        />
      </details>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { type ComponentPublicInstance, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformTextClass } from '@/utils/platformColors'
import CatalogQuoteDetails from './CatalogQuoteDetails.vue'
import CatalogRateBadge from './CatalogRateBadge.vue'
import { catalogMoney, quotePrice, type CatalogModel } from './catalog'

const props = defineProps<{ model: CatalogModel | null; rateUnavailable: boolean }>()
defineEmits<{ close: [] }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const openQuotes = ref(new Set<string>())
const showAllDisclosures = ref(false)
const copied = ref(false)
const disclosureRefs = new Map<string, HTMLElement>()
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const quoteKey = (quote: CatalogModel['quotes'][number]) => `${quote.group.id}:${quote.offer.offer_key}`

function setDisclosureRef(key: string, el: Element | ComponentPublicInstance | null) {
  if (el instanceof HTMLElement) disclosureRefs.set(key, el)
  else disclosureRefs.delete(key)
}

function prefersReducedMotion() {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

async function onCopyModel(name: string) {
  const modelKey = props.model?.key
  const ok = await copyToClipboard(name)
  if (!ok || props.model?.key !== modelKey) return
  copied.value = true
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copied.value = false
  }, 1400)
}

function toggleAllDisclosures() {
  if (showAllDisclosures.value) {
    // Leaving show-all keeps at most one expanded comparison. Every other
    // disclosure is collapsed and its controls leave the dialog's focus loop.
    const first = openQuotes.value.values().next().value
    openQuotes.value = first ? new Set([first]) : new Set()
  }
  showAllDisclosures.value = !showAllDisclosures.value
}

function toggleQuote(key: string) {
  const next = new Set(openQuotes.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  openQuotes.value = next
}

async function onComparisonRowClick(quote: CatalogModel['quotes'][number]) {
  const key = quoteKey(quote)
  if (!showAllDisclosures.value) {
    if (openQuotes.value.has(key)) {
      openQuotes.value = new Set()
      return
    }
    openQuotes.value = new Set([key])
    await nextTick()
    const el = disclosureRefs.get(key)
    el?.scrollIntoView?.({ behavior: prefersReducedMotion() ? 'auto' : 'smooth', block: 'nearest' })
    return
  }
  toggleQuote(key)
}

// BaseDialog owns the shared focus trap, Escape and focus restoration. Hidden
// quotes and tiers have no mounted controls for its keyboard loop to inspect.
watch(() => props.model?.key, () => {
  openQuotes.value = new Set()
  showAllDisclosures.value = false
  copied.value = false
  disclosureRefs.clear()
})

onUnmounted(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
</script>

<style scoped>
.catalog-details-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.catalog-dialog-topbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--surface-edge) 55%, transparent);
}

.catalog-dialog-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.catalog-dialog-platform-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border-radius: var(--surface-radius-tag);
  background: var(--record-card-fill-strong);
  border: 1px solid var(--surface-edge);
  font-size: 12px;
  font-weight: 600;
}

.catalog-dialog-quotes-count {
  font-size: 12.5px;
  color: var(--surface-muted);
  font-variant-numeric: tabular-nums;
}

.catalog-copy-model.is-copied {
  border-color: rgb(16 185 129 / 45%);
  background: rgb(16 185 129 / 12%);
  color: #10b981;
}

.catalog-dialog-note {
  margin: 0;
  padding: 9px 12px;
  border-radius: var(--surface-radius-item);
  background: var(--record-card-fill);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 65%, transparent);
  border-left: 3px solid rgb(var(--console-accent-500, 20 184 166) / 70%);
  font-size: 12px;
  line-height: 1.55;
  color: var(--surface-muted);
}

.catalog-table-hint-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.catalog-table-hint {
  margin: 0;
  font-size: 12px;
  color: var(--surface-muted);
}

.catalog-table-hint-title {
  font-weight: 600;
  color: var(--surface-text);
}

.catalog-toggle-all-btn {
  padding: 4px 10px;
  border-radius: var(--surface-radius-tag);
  background: var(--record-card-fill);
  border: 1px solid var(--surface-edge);
  font-size: 11.5px;
  font-weight: 500;
  color: var(--surface-muted);
  cursor: pointer;
  white-space: nowrap;
  transition: background-color 140ms ease, border-color 140ms ease, color 140ms ease;
}

.catalog-toggle-all-btn:hover {
  background: var(--surface-hover);
  border-color: var(--console-control-hover-edge);
  color: var(--surface-text);
}

.catalog-toggle-all-btn[aria-pressed='true'] {
  background: rgb(var(--console-accent-500, 20 184 166) / 14%);
  border-color: rgb(var(--console-accent-500, 20 184 166) / 50%);
  color: var(--console-accent-readable, var(--surface-text));
}

table.catalog-comparison thead th:nth-child(3),
table.catalog-comparison thead th:nth-child(4),
table.catalog-comparison tbody td:nth-child(3),
table.catalog-comparison tbody td:nth-child(4) {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

table.catalog-comparison tbody tr.catalog-comparison-row {
  cursor: pointer;
}

table.catalog-comparison tbody tr.is-selected > :is(th, td) {
  --record-row-tint: rgb(var(--console-accent-500, 20 184 166) / 14%);
  --record-row-edge: rgb(var(--console-accent-500, 20 184 166) / 60%);
}

.catalog-row-toggle {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: pointer;
  text-align: left;
  border-radius: var(--surface-radius-xs);
}

.catalog-row-chevron {
  width: 12px;
  height: 12px;
  flex: none;
  color: var(--surface-muted);
  transition: transform 150ms ease, color 150ms ease;
}

.catalog-row-chevron.is-open {
  transform: rotate(90deg);
  color: var(--console-accent-readable, var(--surface-text));
}

.catalog-quote-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 14px;
  border-radius: var(--surface-radius-item);
  background: var(--record-card-fill-strong);
  border: 1px solid var(--surface-edge);
  font-size: 13px;
  color: var(--surface-text);
  cursor: pointer;
  list-style: none;
  transition: background-color 140ms ease, border-color 140ms ease;
}

.catalog-quote-summary::-webkit-details-marker {
  display: none;
}

.catalog-quote-summary:hover,
.catalog-quote-summary.is-open {
  border-color: rgb(var(--console-accent-500, 20 184 166) / 45%);
}

.catalog-summary-action {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: none;
  font-size: 11.5px;
  color: var(--surface-muted);
}

.catalog-summary-action.is-open {
  color: var(--console-accent-readable, var(--surface-text));
  font-weight: 500;
}

.catalog-details-body :deep(.catalog-quote) {
  border-radius: var(--surface-radius-card);
  background: var(--record-card-fill);
  border-color: var(--surface-edge);
}

@media (prefers-reduced-motion: reduce) {
  .catalog-toggle-all-btn,
  .catalog-row-chevron,
  .catalog-quote-summary {
    transition: none;
  }
}
</style>
