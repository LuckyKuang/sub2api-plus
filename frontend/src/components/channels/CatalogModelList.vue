<template>
  <div class="catalog-list min-w-0" data-testid="catalog-model-list">
    <div class="catalog-dir-card">
      <p v-if="rateUnavailable" class="catalog-dir-notice">
        {{ t('availableChannels.catalog.referenceRange') }}
      </p>

      <div class="catalog-dir-header" aria-hidden="true">
        <span>{{ t('availableChannels.catalog.colModelPlatform') }}</span>
        <span class="text-right">{{ t('availableChannels.catalog.colInputPrice') }}</span>
        <span class="text-right">{{ t('availableChannels.catalog.colOutputPrice') }}</span>
        <span class="text-right">{{ t('availableChannels.catalog.colAvailableQuotes') }}</span>
      </div>

      <ol class="catalog-dir-rows">
        <li
          v-for="model in models"
          :key="model.key"
          :data-platform="model.platform"
          class="catalog-list-row"
        >
          <button
            type="button"
            class="catalog-list-open focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500"
            aria-haspopup="dialog"
            :aria-label="t('availableChannels.catalog.detailsFor', { model: model.name })"
            @click="$emit('details', model)"
          />

          <article class="catalog-row-grid">
            <div class="catalog-model-cell">
              <span class="catalog-platform-badge" :class="platformTextClass(model.platform)" aria-hidden="true">
                <PlatformIcon :platform="model.platform" size="sm" />
              </span>

              <div class="catalog-model-info">
                <div class="catalog-model-title-row">
                  <h3 class="catalog-model-name" :title="model.name">{{ model.name }}</h3>
                  <button
                    type="button"
                    class="catalog-copy-model focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
                    :class="{ 'is-copied': copiedKey === model.key }"
                    :aria-label="t('availableChannels.catalog.copyModel', { model: model.name })"
                    @click="onCopyModel(model)"
                  >
                    <Icon :name="copiedKey === model.key ? 'check' : 'copy'" size="xs" />
                  </button>
                </div>

                <div class="catalog-model-sub">
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
                    v-if="hasExclusive(model)"
                    class="catalog-tag-chip text-purple-700 dark:text-purple-300"
                    :title="t('availableChannels.catalog.exclusiveQuote')"
                  >
                    {{ t('availableChannels.exclusive') }}
                  </span>
                  <span
                    v-if="hasPersonalRate(model)"
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

                <p
                  v-for="notice in incompleteNotices(model)"
                  :key="notice"
                  class="catalog-incomplete-note"
                >
                  {{ notice }}
                </p>
              </div>
            </div>

            <div class="catalog-price-cell">
              <div v-for="price in model.prices" :key="`in-${price.unit}`" class="catalog-price-entry">
                <span class="catalog-mobile-price-label">
                  {{ t(price.unit === 'token' ? 'availableChannels.pricing.inputPrice' : 'availableChannels.catalog.unitPrice') }}
                </span>
                <span v-if="model.prices.length > 1" class="catalog-price-unit">
                  {{ t(`availableChannels.catalog.units.${price.unit}`) }}
                </span>
                <span class="catalog-price-val font-mono tabular-nums" :class="{ 'is-muted': !price.input }">
                  {{ catalogRange(price.input, price.unit) }}
                </span>
              </div>
            </div>

            <div class="catalog-price-cell">
              <div v-for="price in model.prices" :key="`out-${price.unit}`" class="catalog-price-entry">
                <span class="catalog-mobile-price-label">
                  {{ t('availableChannels.pricing.outputPrice') }}
                </span>
                <span v-if="model.prices.length > 1" class="catalog-price-unit">
                  {{ t(`availableChannels.catalog.units.${price.unit}`) }}
                </span>
                <span
                  class="catalog-price-val font-mono tabular-nums"
                  :class="{ 'is-muted': price.unit !== 'token' || !price.output }"
                >
                  {{ price.unit === 'token' ? catalogRange(price.output, price.unit) : '—' }}
                </span>
              </div>
            </div>

            <div class="catalog-action-cell">
              <span class="catalog-quote-pill">
                {{ t('availableChannels.catalog.quoteCountShort', { count: model.quotes.length }) }}
              </span>
              <span class="catalog-detail-cta">
                {{ t('availableChannels.catalog.details') }}
                <span class="catalog-detail-arrow" aria-hidden="true">→</span>
              </span>
            </div>
          </article>
        </li>
      </ol>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformTextClass } from '@/utils/platformColors'
import { catalogRange, rateSource, type CatalogModel } from './catalog'

defineProps<{ models: CatalogModel[]; rateUnavailable: boolean }>()
defineEmits<{ details: [model: CatalogModel] }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedKey = ref<string | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

function onCopyModel(model: CatalogModel) {
  void copyToClipboard(model.name)
  copiedKey.value = model.key
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedKey.value = null
  }, 1400)
}

function hasExclusive(model: CatalogModel) {
  return model.quotes.some(q => q.group.is_exclusive)
}

function hasPersonalRate(model: CatalogModel) {
  return model.quotes.some(q => rateSource(q.group, q.offer) === 'personal')
}

function incompleteNotices(model: CatalogModel) {
  const notices = new Set<string>()
  for (const price of model.prices) {
    if (!price.incomplete) continue
    notices.add(
      t(price.input || price.output
        ? 'availableChannels.catalog.partialUnknown'
        : 'availableChannels.catalog.reasons.pricing_unavailable')
    )
  }
  return [...notices]
}

onUnmounted(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
</script>

<style scoped>
.catalog-list {
  container-type: inline-size;
}

.catalog-dir-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 14px 14px;
  border-radius: var(--surface-radius);
  background-color: var(--surface-fill);
  background-image: linear-gradient(135deg, var(--surface-glint), transparent 65%);
  border: 1px solid var(--surface-edge);
  box-shadow: var(--surface-shadow);
}

.catalog-dir-notice {
  margin: 0 0 2px;
  padding: 0 14px;
  font-size: 11.5px;
  color: var(--surface-muted);
}

.catalog-dir-header {
  display: grid;
  grid-template-columns: minmax(250px, 1.4fr) minmax(185px, 1fr) minmax(190px, 1fr) 138px;
  align-items: center;
  gap: 16px;
  padding: 4px 14px 8px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--surface-muted);
}

.catalog-dir-rows {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0;
  padding: 0;
}

.catalog-list-row {
  position: relative;
  list-style: none;
  min-width: 0;
  padding: 11px 14px;
  border-radius: var(--surface-radius-item);
  background: var(--record-card-fill);
  border: 1px solid var(--surface-edge);
  transition: background-color 150ms ease, border-color 150ms ease;
}

.catalog-list-row:hover,
.catalog-list-row:focus-within {
  background: var(--surface-hover);
  border-color: rgb(var(--console-accent-500, 20 184 166) / 55%);
}

.catalog-list-open {
  position: absolute;
  inset: 0;
  width: 100%;
  border-radius: var(--surface-radius-item);
  cursor: pointer;
}

.catalog-row-grid {
  position: relative;
  z-index: 1;
  pointer-events: none;
  display: grid;
  grid-template-columns: minmax(250px, 1.4fr) minmax(185px, 1fr) minmax(190px, 1fr) 138px;
  align-items: center;
  gap: 16px;
  min-width: 0;
}

.catalog-model-cell {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
}

.catalog-platform-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: var(--surface-radius-control);
  background: color-mix(in srgb, currentColor 12%, transparent);
  border: 1px solid color-mix(in srgb, currentColor 24%, transparent);
}

.catalog-model-info {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  flex: 1;
}

.catalog-model-title-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.catalog-model-name {
  margin: 0;
  min-width: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 13.5px;
  font-weight: 600;
  color: var(--surface-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.catalog-copy-model {
  pointer-events: auto;
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
  opacity: 0.65;
  cursor: pointer;
  transition: opacity 140ms ease, background-color 140ms ease, border-color 140ms ease, color 140ms ease;
}

.catalog-list-row:hover .catalog-copy-model,
.catalog-list-row:focus-within .catalog-copy-model,
.catalog-copy-model:focus-visible,
.catalog-copy-model.is-copied {
  opacity: 1;
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

.catalog-model-sub {
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

.catalog-incomplete-note {
  margin: 1px 0 0;
  font-size: 11px;
  color: var(--surface-muted);
}

.catalog-price-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  min-width: 0;
}

.catalog-price-entry {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 1px;
  max-width: 100%;
}

.catalog-mobile-price-label {
  display: none;
  font-size: 11px;
  color: var(--surface-muted);
}

.catalog-price-unit {
  font-size: 10.5px;
  color: var(--surface-muted);
  white-space: nowrap;
}

.catalog-price-val {
  font-size: 13px;
  font-weight: 600;
  color: var(--surface-text);
  white-space: nowrap;
}

.catalog-price-val.is-muted {
  color: var(--surface-muted);
  font-weight: 500;
}

.catalog-action-cell {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  white-space: nowrap;
}

.catalog-quote-pill {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: var(--surface-radius-tag);
  background: var(--record-card-fill-strong);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 70%, transparent);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  color: var(--surface-muted);
}

.catalog-detail-cta {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 12px;
  font-weight: 600;
  color: var(--surface-muted);
  transition: color 150ms ease;
}

.catalog-detail-arrow {
  display: inline-block;
  transition: transform 150ms ease;
}

.catalog-list-row:hover .catalog-detail-cta,
.catalog-list-row:focus-within .catalog-detail-cta {
  color: var(--console-accent-readable, var(--surface-text));
}

.catalog-list-row:hover .catalog-detail-arrow,
.catalog-list-row:focus-within .catalog-detail-arrow {
  transform: translateX(2px);
}

@container (max-width: 900px) {
  .catalog-dir-card {
    padding: 10px;
  }

  .catalog-dir-header {
    display: none;
  }

  .catalog-row-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px 12px;
    align-items: start;
  }

  .catalog-model-cell {
    grid-column: 1 / -1;
    align-items: flex-start;
  }

  .catalog-model-name {
    white-space: normal;
    overflow: visible;
    text-overflow: unset;
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .catalog-price-cell {
    align-items: flex-start;
  }

  .catalog-price-entry {
    align-items: flex-start;
  }

  .catalog-mobile-price-label {
    display: inline;
  }

  .catalog-price-val {
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .catalog-action-cell {
    grid-column: 1 / -1;
    justify-content: space-between;
    padding-top: 8px;
    border-top: 1px solid color-mix(in srgb, var(--surface-edge) 45%, transparent);
  }
}

@container (max-width: 360px) {
  .catalog-row-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (prefers-reduced-motion: reduce) {
  .catalog-list-row,
  .catalog-copy-model,
  .catalog-detail-cta,
  .catalog-detail-arrow {
    transition: none;
  }

  .catalog-list-row:hover .catalog-detail-arrow,
  .catalog-list-row:focus-within .catalog-detail-arrow {
    transform: none;
  }
}
</style>
