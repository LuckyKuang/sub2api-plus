<template>
  <div class="catalog-list space-y-6" data-testid="catalog-model-list">
    <section v-for="section in sections" :key="section.platform" class="surface-card rounded-xl border border-gray-200 px-4 py-4 dark:border-dark-700" :aria-label="section.platform">
      <header class="mb-2 flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 pb-3 dark:border-dark-800">
        <h2 class="flex min-w-0 items-center gap-2 text-sm font-semibold" :class="platformTextClass(section.platform)">
          <PlatformIcon :platform="section.platform" size="sm" />{{ section.platform }}
          <span class="text-xs font-normal surface-muted">{{ t('availableChannels.catalog.modelCount', { count: section.models.length }) }}</span>
        </h2>
        <p class="text-xs surface-muted">{{ t(rateUnavailable ? 'availableChannels.catalog.referenceRange' : 'availableChannels.catalog.effectiveRange') }}</p>
      </header>
      <div class="catalog-list-menu relative isolate" @pointerleave="hoveredRow = null" @keydown="hoveredRow = null" @focusout="focusedRow = null">
        <div class="catalog-list-highlight" :class="{ 'is-active': !!activeRow && highlight?.platform === section.platform }" :style="highlight?.platform === section.platform ? highlight.style : undefined" aria-hidden="true" />
        <ol class="catalog-list-rows">
          <li v-for="(model, index) in section.models" :key="model.key" :data-platform="section.platform" class="catalog-list-row relative z-10 min-w-0 rounded-xl border-b border-gray-100 px-3 py-4 last:border-0 dark:border-dark-800" @pointerenter="onPointerEnter" @focusin="focusedRow = $event.currentTarget as HTMLElement">
            <button type="button" class="catalog-list-open absolute inset-0 w-full cursor-pointer rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500" aria-haspopup="dialog" :aria-label="t('availableChannels.catalog.detailsFor', { model: model.name })" @click="$emit('details', model)" />
            <article class="pointer-events-none relative flex min-w-0 items-start gap-3">
              <span class="pt-1 font-mono text-xs tabular-nums surface-muted" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</span>
              <div class="min-w-0 flex-1 space-y-2">
                <div class="flex min-w-0 items-start gap-2">
                  <h3 class="min-w-0 break-words font-mono text-sm font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">{{ model.name }}</h3>
                  <button type="button" class="catalog-copy-model pointer-events-auto relative shrink-0 rounded p-0.5 hover:text-primary-500 focus-visible:ring-2 focus-visible:ring-primary-500 surface-control surface-muted" :aria-label="t('availableChannels.catalog.copyModel', { model: model.name })" @click="copyToClipboard(model.name)">
                    <Icon name="copy" size="sm" />
                  </button>
                  <span class="mt-2.5 min-w-0 flex-1 border-b border-dotted border-gray-300 dark:border-dark-600" aria-hidden="true" />
                </div>
                <div class="space-y-1.5 text-xs">
                  <div v-for="price in model.prices" :key="price.unit" class="space-y-1">
                    <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                      <span class="surface-muted">{{ t(`availableChannels.catalog.units.${price.unit}`) }}</span>
                      <span class="flex flex-wrap gap-x-3 gap-y-1 font-mono tabular-nums">
                        <span><span class="mr-1 font-sans surface-muted">{{ t(price.unit === 'token' ? 'availableChannels.pricing.inputPrice' : 'availableChannels.catalog.unitPrice') }}</span>{{ catalogRange(price.input, price.unit) }}</span>
                        <span v-if="price.unit === 'token'"><span class="mr-1 font-sans surface-muted">{{ t('availableChannels.pricing.outputPrice') }}</span>{{ catalogRange(price.output, price.unit) }}</span>
                      </span>
                    </div>
                    <p v-if="price.incomplete" class="surface-muted">{{ t(price.input || price.output ? 'availableChannels.catalog.partialUnknown' : 'availableChannels.catalog.reasons.pricing_unavailable') }}</p>
                  </div>
                </div>
                <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs surface-muted">
                  <span>{{ t('availableChannels.catalog.quoteCount', { count: model.quotes.length }) }}<span v-if="model.quotes.some(q => q.group.is_exclusive)" class="whitespace-nowrap font-medium text-purple-700 dark:text-purple-300" :title="t('availableChannels.catalog.exclusiveQuote')">{{ ' ' + t('availableChannels.exclusive') }}</span></span>
                  <span v-if="model.quotes.some(q => rateSource(q.group, q.offer) === 'personal')" class="text-emerald-700 dark:text-emerald-300">{{ t('availableChannels.catalog.personalRate') }}</span>
                  <span v-if="model.tiered !== 'none'" class="text-blue-700 dark:text-blue-300">{{ t(model.tiered === 'all' ? 'availableChannels.pricing.intervals' : 'availableChannels.catalog.someTiered') }}</span>
                  <span v-if="model.dynamic !== 'none'" class="text-amber-700 dark:text-amber-300">{{ t(model.dynamic === 'all' ? 'availableChannels.catalog.dynamicBilling' : 'availableChannels.catalog.someDynamic') }}</span>
                </div>
              </div>
            </article>
          </li>
        </ol>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useEventListener, useResizeObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformTextClass } from '@/utils/platformColors'
import { catalogRange, rateSource, type CatalogModel } from './catalog'

const props = defineProps<{ models: CatalogModel[]; rateUnavailable: boolean }>()
defineEmits<{ details: [model: CatalogModel] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const hoveredRow = shallowRef<HTMLElement | null>(null)
const focusedRow = shallowRef<HTMLElement | null>(null)
const activeRow = computed(() => hoveredRow.value ?? focusedRow.value)
const highlight = ref<{ platform: string; style: Record<string, string> } | null>(null)
function onPointerEnter(event: PointerEvent) {
  if (event.pointerType !== 'touch') hoveredRow.value = event.currentTarget as HTMLElement
}
function updateHighlight() {
  const row = activeRow.value
  const menu = row?.closest('.catalog-list-menu')
  if (!row?.isConnected || !menu) return
  const box = row.getBoundingClientRect()
  const origin = menu.getBoundingClientRect()
  highlight.value = {
    platform: row.dataset.platform!,
    style: { transform: `translate(${box.left - origin.left}px, ${box.top - origin.top}px)`, width: `${box.width}px`, height: `${box.height}px` }
  }
}
watch(activeRow, updateHighlight, { flush: 'post' })
watch(() => props.models, () => { hoveredRow.value = null; focusedRow.value = null })
useResizeObserver(activeRow, updateHighlight)
useEventListener('resize', updateHighlight)
const sections = computed(() => {
  const platforms = new Map<string, CatalogModel[]>()
  for (const model of props.models) {
    const models = platforms.get(model.platform) ?? []
    models.push(model)
    platforms.set(model.platform, models)
  }
  return [...platforms].map(([platform, models]) => ({ platform, models }))
})
</script>

<style scoped>
.catalog-list { container-type: inline-size; }
.catalog-list-rows { display: grid; grid-template-columns: minmax(0, 1fr); column-gap: 40px; }
.catalog-list-highlight {
  position: absolute;
  top: 0;
  left: 0;
  pointer-events: none;
  border-radius: 12px;
  background: var(--surface-hover);
  box-shadow: 0 0 0 1px var(--surface-edge), var(--surface-shadow);
  opacity: 0;
  transition: transform 180ms ease, width 180ms ease, height 180ms ease, opacity 120ms ease;
}
.catalog-list-highlight.is-active { opacity: 1; }
@media (prefers-reduced-motion: reduce) {
  .catalog-list-highlight { transition: none !important; }
}
@container (min-width: 900px) {
  .catalog-list-rows { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
