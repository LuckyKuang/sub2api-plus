<template>
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <strong class="catalog-effective-rate rounded px-1.5 py-0.5 text-xs font-semibold leading-4" :class="source === 'personal' ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300' : source === 'media' ? 'bg-cyan-100 text-cyan-800 dark:bg-cyan-900/30 dark:text-cyan-300' : 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300'">
          {{ t(`availableChannels.catalog.rateLabels.${source}`, { rate }) }}
        </strong>
        <span v-if="source !== 'group'" class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.defaultRate', { rate: group.rate_multiplier }) }}</span>
        <span v-if="source === 'media' && group.user_rate_multiplier != null" class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.overriddenPersonalRate', { rate: group.user_rate_multiplier }) }}</span>
      </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CatalogGroup, CatalogOffer } from '@/api/channels'
import { effectiveRate, rateSource } from './catalog'
const props = defineProps<{ group: CatalogGroup; offer: CatalogOffer }>()
const { t } = useI18n()
const rate = computed(() => effectiveRate(props.group, props.offer))
const source = computed(() => rateSource(props.group, props.offer))
</script>
