<template>
  <article class="plaza-model-card">
    <header class="flex min-w-0 items-start gap-3">
      <span class="model-mark" :class="platformBadgeLightClass(model.platform)" :title="platformLabel(model.platform)">
        <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
        <span class="sr-only">{{ platformLabel(model.platform) }}</span>
      </span>
      <div class="min-w-0 flex-1">
        <h3 class="model-name">
          <button type="button" class="open-model" aria-haspopup="dialog" :aria-label="model.name + ' · ' + actionLabel" @click="$emit('open')">
            {{ model.name }}
          </button>
        </h3>
        <p class="mt-1 break-words text-xs surface-muted">
          {{ group.name }} <span class="ml-1 whitespace-nowrap font-mono">{{ quote.rate }}×</span>
        </p>
      </div>
      <button
        type="button"
        class="model-copy"
        :title="t('modelPlaza.table.copyModelId', { model: model.name })"
        :aria-label="t('modelPlaza.table.copyModelId', { model: model.name })"
        @click="copyToClipboard(model.name)"
      >
        <Icon :name="copied ? 'check' : 'copy'" size="sm" aria-hidden="true" />
      </button>
    </header>

    <div class="model-prices">
      <p v-if="!model.pricing" class="text-sm surface-muted">{{ t('modelPlaza.detail.noPricing') }}</p>
      <template v-else>
        <dl class="flex flex-wrap items-baseline gap-x-5 gap-y-1">
          <div v-for="price in quote.prices" :key="price.label" class="flex items-baseline gap-1.5">
            <dt class="text-xs surface-muted">{{ t(price.label) }}</dt>
            <dd class="price-number">{{ price.value }}</dd>
          </div>
        </dl>
        <p class="mt-1 flex flex-wrap gap-x-2 text-xs surface-muted">
          <span><template v-if="billingMode(model) === BILLING_MODE_TOKEN">{{ t('modelPlaza.catalog.paid') }} · </template>{{ t(quote.unit) }}</span>
          <span v-if="quote.tier">{{ quote.tier }}</span>
          <span v-if="group.peak_rate_enabled || timePricing(model)?.periods.length">{{ t('modelPlaza.catalog.standardPrice') }}</span>
        </p>
      </template>
    </div>

    <footer class="flex items-center justify-between gap-2 text-xs font-medium">
      <span>{{ actionLabel }}</span>
      <Icon name="arrowRight" size="sm" aria-hidden="true" />
    </footer>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { BILLING_MODE_TOKEN } from '@/constants/channel'
import { billingMode, summarizeOffer, timePricing } from './pricing'

const props = defineProps<{ model: PlazaModel; group: ModelPlazaGroup; groupCount: number }>()
defineEmits<{ open: [] }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const actionLabel = computed(() => props.groupCount > 1
  ? t('modelPlaza.catalog.compareGroups', { count: props.groupCount })
  : t('modelPlaza.catalog.details'))
const quote = computed(() => summarizeOffer(props))
</script>

<style scoped>
.plaza-model-card {
  position: relative;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 16px;
  padding: 18px;
  border: 1px solid var(--surface-edge);
  border-radius: var(--surface-radius-card);
  background: var(--surface-fill);
  color: var(--surface-text);
}

.plaza-model-card:hover, .plaza-model-card:focus-within { border-color: var(--console-control-hover-edge); }
.model-name { font-size: 1rem; line-height: 1.35; font-weight: 650; }
.open-model { overflow-wrap: anywhere; text-align: left; }
.open-model::after { position: absolute; inset: 0; content: ''; border-radius: var(--surface-radius-card); }
.open-model:focus-visible { outline: none; }
.open-model:focus-visible::after { outline: 2px solid var(--console-accent-readable); outline-offset: 2px; }

.model-mark, .model-copy {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: var(--control-height);
  height: var(--control-height);
  border-radius: var(--surface-radius-control);
}

.model-copy { position: relative; color: var(--surface-muted); }
.model-copy:hover, .model-copy:focus-visible { background: var(--surface-hover); color: var(--console-accent-readable); }
.model-prices { margin-top: auto; }
.price-number { font-size: 1.05rem; line-height: 1.4; font-weight: 650; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }

.plaza-model-card footer { padding-top: 10px; border-top: 1px solid var(--surface-edge); color: var(--console-accent-readable); }
@media (max-width: 639px) {
  .plaza-model-card { gap: 12px; padding: 14px 16px; }
}
</style>
