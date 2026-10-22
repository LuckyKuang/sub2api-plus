<template>
  <div class="flex flex-wrap items-center gap-2 text-xs text-gray-500">
    <AutoRefreshButton :enabled="state.enabled.value" :interval-seconds="state.intervalSeconds.value" :countdown="state.countdown.value" :fetching="state.fetching.value" :paused="state.paused.value" :intervals="state.intervals" @update:enabled="state.setEnabled" @update:interval="state.setInterval" />
    <button v-if="manual" type="button" class="btn btn-secondary" :disabled="state.fetching.value" @click="state.refresh">{{ t('common.refresh') }}</button>
    <span v-if="state.error.value" role="status" class="text-amber-600">{{ t('inspection.stale') }}</span>
    <span v-else-if="state.lastUpdated.value">{{ t('inspection.updatedAt', { time: new Date(state.lastUpdated.value).toLocaleTimeString() }) }}</span>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { useAutoRefresh } from '@/composables/useAutoRefresh'
import AutoRefreshButton from './AutoRefreshButton.vue'
defineProps<{ state: ReturnType<typeof useAutoRefresh>; manual?: boolean }>()
const { t } = useI18n()
</script>
