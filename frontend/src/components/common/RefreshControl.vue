<template>
  <div class="flex flex-wrap items-center gap-2 text-xs surface-muted">
    <button v-if="switchVisible" type="button" role="switch" :aria-checked="state.enabled.value" class="surface-control inline-flex min-h-9 items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-600" @click="state.setEnabled(!state.enabled.value)">
      <span class="refresh-switch-track h-4 w-7 rounded-full transition-colors" :class="state.enabled.value ? 'bg-primary-500' : 'bg-gray-400'"><span class="block h-3 w-3 rounded-full surface-opaque transition-transform" :class="state.enabled.value ? 'translate-x-3' : ''" /></span>
      {{ t('common.autoRefresh.title') }}
    </button>
    <AutoRefreshButton :interval-only="switchVisible" :enabled="state.enabled.value" :interval-seconds="state.intervalSeconds.value" :countdown="state.countdown.value" :fetching="state.fetching.value" :paused="state.paused.value" :intervals="state.intervals" @update:enabled="state.setEnabled" @update:interval="state.setInterval" />
    <button v-if="manual" type="button" class="btn btn-secondary" :disabled="state.fetching.value" @click="state.refresh">{{ t('common.refresh') }}</button>
    <span v-if="state.error.value" role="status" class="text-amber-600">{{ t('inspection.stale') }}</span>
    <span v-else-if="state.lastUpdated.value">{{ t('inspection.updatedAt', { time: new Date(state.lastUpdated.value).toLocaleTimeString() }) }}</span>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { useAutoRefresh } from '@/composables/useAutoRefresh'
import AutoRefreshButton from './AutoRefreshButton.vue'
defineProps<{ state: ReturnType<typeof useAutoRefresh>; manual?: boolean; switchVisible?: boolean }>()
const { t } = useI18n()
</script>
<style scoped>
.refresh-switch-track { border: 1px solid var(--surface-edge); padding: 1px; }
[aria-checked="true"] .refresh-switch-track { border-color: rgb(var(--console-accent-500, 20 184 166) / 65%); }
</style>
