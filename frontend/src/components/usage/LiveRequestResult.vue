<template>
  <span class="inline-flex items-center gap-1.5 whitespace-nowrap text-xs font-medium" :class="color">
    <span class="console-data-mark h-1.5 w-1.5 shrink-0 rounded-full bg-current" :class="liveActive(row) ? 'motion-safe:animate-pulse' : ''" aria-hidden="true" />
    {{ t(liveStatusKey(row)) }}
  </span>
  <p v-if="row.reason" class="mt-0.5 max-w-48 whitespace-normal text-xs surface-muted">{{ t(`requestList.reason.${row.reason}`) }}</p>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LiveRequest } from '@/api/liveRequests'
import { liveAbnormal, liveActive, liveStatusKey } from '@/utils/liveRequestPresentation'
const props = defineProps<{ row: LiveRequest }>()
const { t } = useI18n()
const color = computed(() => props.row.status === 'completed' ? 'text-emerald-600 dark:text-emerald-400' : liveAbnormal(props.row) ? 'text-amber-600 dark:text-amber-400' : liveActive(props.row) ? 'text-primary-600 dark:text-primary-400' : 'text-gray-500')
</script>
