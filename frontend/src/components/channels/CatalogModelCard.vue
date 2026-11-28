<template>
  <article class="catalog-model-card">
    <div class="catalog-card-head">
      <span class="catalog-platform-badge" :class="platformTextClass(model.platform)" aria-hidden="true">
        <PlatformIcon :platform="model.platform" size="sm" />
      </span>

      <div class="catalog-card-head-body">
        <div class="catalog-card-title-row">
          <h3 class="catalog-card-name" :title="model.name">{{ model.name }}</h3>
          <button
            type="button"
            class="catalog-copy-model focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
            :class="{ 'is-copied': copied }"
            :aria-label="t('availableChannels.catalog.copyModel', { model: model.name })"
            @click="onCopy"
          >
            <Icon :name="copied ? 'check' : 'copy'" size="xs" />
          </button>
        </div>

        <div class="catalog-card-sub">
          <span class="catalog-platform-label" :class="platformTextClass(model.platform)">{{ model.platform }}</span>
          <template v-if="model.prices.length">
            <span class="catalog-sub-dot" aria-hidden="true">·</span>
            <span>
              {{ model.prices.length > 1
                ? t('availableChannels.catalog.multipleUnits')
                : t(`availableChannels.catalog.units.${model.prices[0]!.unit}`) }}
            </span>
          </template>
          <span
            v-if="model.quotes.some(q => rateSource(q.group, q.offer) === 'personal')"
            class="catalog-tag-chip text-emerald-700 dark:text-emerald-300"
          >
            {{ t('availableChannels.catalog.personalRate') }}
          </span>
          <span
            v-if="model.tiered !== 'none'"
            class="catalog-tag-chip text-blue-700 dark:text-blue-300"
          >
            {{ t(model.tiered === 'all' ? 'availableChannels.pricing.intervals' : 'availableChannels.catalog.someTiered') }}
          </span>
          <span
            v-if="model.dynamic !== 'none'"
            class="catalog-tag-chip text-amber-700 dark:text-amber-300"
          >
            {{ t(model.dynamic === 'all' ? 'availableChannels.catalog.dynamicBilling' : 'availableChannels.catalog.someDynamic') }}
          </span>
        </div>
      </div>
    </div>

    <div class="catalog-card-ledger">
      <div
        v-for="(price, index) in model.prices"
        :key="price.unit"
        class="catalog-ledger-unit"
        :class="{ 'has-divider': index > 0 }"
      >
        <p v-if="model.prices.length > 1" class="catalog-ledger-unit-label">
          {{ t(`availableChannels.catalog.units.${price.unit}`) }}
        </p>

        <template v-if="price.input || price.output">
          <div class="catalog-ledger-row">
            <span class="catalog-ledger-label">
              {{ t(price.unit === 'token' ? 'availableChannels.pricing.inputPrice' : 'availableChannels.catalog.unitPrice') }}
            </span>
            <span class="catalog-ledger-val font-mono tabular-nums" :class="{ 'is-muted': !price.input }">
              {{ catalogRange(price.input, price.unit) }}
            </span>
          </div>

          <div v-if="price.unit === 'token'" class="catalog-ledger-row catalog-ledger-row--divided">
            <span class="catalog-ledger-label">
              {{ t('availableChannels.pricing.outputPrice') }}
            </span>
            <span class="catalog-ledger-val font-mono tabular-nums" :class="{ 'is-muted': !price.output }">
              {{ catalogRange(price.output, price.unit) }}
            </span>
          </div>
        </template>

        <p v-if="price.incomplete" class="catalog-ledger-note">
          {{ t(price.input || price.output ? 'availableChannels.catalog.partialUnknown' : 'availableChannels.catalog.reasons.pricing_unavailable') }}
        </p>
      </div>
    </div>

    <p v-if="rateUnavailable" class="catalog-card-rate-note">
      {{ t('availableChannels.catalog.referenceRange') }}
    </p>

    <div class="catalog-card-footer">
      <span class="catalog-card-quotes">
        {{ t('availableChannels.catalog.quoteCount', { count: model.quotes.length }) }}<span
          v-if="model.quotes.some(q => q.group.is_exclusive)"
          class="whitespace-nowrap font-medium text-purple-700 dark:text-purple-300"
          :title="t('availableChannels.catalog.exclusiveQuote')"
        >{{ ' ' + t('availableChannels.exclusive') }}</span>
      </span>

      <button
        type="button"
        class="catalog-card-details-btn focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
        :aria-label="t('availableChannels.catalog.detailsFor', { model: model.name })"
        @click="$emit('details', model)"
      >
        <span>{{ t('availableChannels.catalog.details') }}</span>
        <span class="catalog-detail-arrow" aria-hidden="true">→</span>
      </button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformTextClass } from '@/utils/platformColors'
import { catalogRange, rateSource, type CatalogModel } from './catalog'

const props = defineProps<{ model: CatalogModel; rateUnavailable: boolean }>()
defineEmits<{ details: [model: CatalogModel] }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

async function onCopy() {
  const name = props.model.name
  const ok = await copyToClipboard(name)
  if (!ok || props.model.name !== name) return
  copied.value = true
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copied.value = false
  }, 1400)
}

onUnmounted(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
</script>

<style scoped>
.catalog-model-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  min-width: 0;
  padding: 12px;
  border-radius: var(--surface-radius-card);
  background-color: var(--surface-fill);
  background-image: linear-gradient(135deg, var(--surface-glint), transparent 65%);
  border: 1px solid var(--surface-edge);
  box-shadow: var(--surface-shadow);
  transition: border-color 150ms ease, box-shadow 150ms ease;
}

.catalog-model-card:hover {
  border-color: rgb(var(--console-accent-500, 20 184 166) / 60%);
  box-shadow: 0 6px 20px -8px rgb(0 0 0 / 28%), inset 0 1px 0 var(--surface-glint);
}

.catalog-card-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
}

.catalog-platform-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: var(--surface-radius-control);
  background: color-mix(in srgb, currentColor 12%, transparent);
  border: 1px solid color-mix(in srgb, currentColor 24%, transparent);
}

.catalog-card-head-body {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  flex: 1;
}

.catalog-card-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 6px;
  min-width: 0;
}

.catalog-card-name {
  margin: 0;
  min-width: 0;
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.3;
  color: var(--surface-text);
  overflow-wrap: anywhere;
  word-break: break-word;
}

.catalog-copy-model {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  flex: none;
  border-radius: var(--surface-radius-tag);
  border: 1px solid transparent;
  background: transparent;
  color: var(--surface-muted);
  cursor: pointer;
  transition: background-color 140ms ease, border-color 140ms ease, color 140ms ease;
}

.catalog-copy-model:hover {
  background: var(--record-card-fill-strong);
  border-color: var(--surface-edge);
  color: var(--surface-text);
}

.catalog-copy-model.is-copied {
  background: rgb(16 185 129 / 12%);
  border-color: rgb(16 185 129 / 45%);
  color: #10b981;
}

.catalog-card-sub {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--surface-muted);
  min-width: 0;
}

.catalog-platform-label {
  font-weight: 500;
}

.catalog-sub-dot {
  opacity: 0.55;
}

.catalog-tag-chip {
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  border-radius: var(--surface-radius-xs);
  font-size: 10.5px;
  font-weight: 500;
  line-height: 1.4;
  background: color-mix(in srgb, currentColor 12%, transparent);
  white-space: nowrap;
}

.catalog-card-ledger {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  border-radius: var(--surface-radius-item);
  background: var(--record-card-fill);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 70%, transparent);
}

.catalog-ledger-unit {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.catalog-ledger-unit.has-divider {
  padding-top: 6px;
  border-top: 1px dashed color-mix(in srgb, var(--surface-edge) 60%, transparent);
}

.catalog-ledger-unit-label {
  margin: 0;
  font-size: 11px;
  color: var(--surface-muted);
}

.catalog-ledger-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}

.catalog-ledger-row--divided {
  padding-top: 4px;
  border-top: 1px solid color-mix(in srgb, var(--surface-edge) 45%, transparent);
}

.catalog-ledger-label {
  flex: none;
  font-size: 11.5px;
  color: var(--surface-muted);
}

.catalog-ledger-val {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--surface-text);
  white-space: nowrap;
  text-align: right;
}

.catalog-ledger-val.is-muted {
  color: var(--surface-muted);
  font-weight: 500;
}

.catalog-ledger-note {
  margin: 0;
  font-size: 11px;
  color: var(--surface-muted);
}

.catalog-card-rate-note {
  margin: 0;
  font-size: 11px;
  color: var(--surface-muted);
}

.catalog-card-footer {
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid color-mix(in srgb, var(--surface-edge) 50%, transparent);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.catalog-card-quotes {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  padding: 2px 6px;
  border-radius: var(--surface-radius-tag);
  background: var(--record-card-fill-strong);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 70%, transparent);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  color: var(--surface-muted);
}

.catalog-card-details-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex: none;
  padding: 3px 7px;
  border-radius: var(--surface-radius-tag);
  border: 1px solid transparent;
  background: transparent;
  font-size: 12px;
  font-weight: 600;
  color: var(--console-accent-readable, var(--surface-text));
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease;
}

.catalog-card-details-btn:hover {
  background: rgb(var(--console-accent-500, 20 184 166) / 14%);
  border-color: rgb(var(--console-accent-500, 20 184 166) / 40%);
}

.catalog-detail-arrow {
  display: inline-block;
  transition: transform 150ms ease;
}

.catalog-card-details-btn:hover .catalog-detail-arrow {
  transform: translateX(2px);
}

@media (prefers-reduced-motion: reduce) {
  .catalog-model-card,
  .catalog-copy-model,
  .catalog-card-details-btn,
  .catalog-detail-arrow {
    transition: none;
  }

  .catalog-card-details-btn:hover .catalog-detail-arrow {
    transform: none;
  }
}
</style>
