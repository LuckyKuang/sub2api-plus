<template>
  <section class="py-3 md:py-4">
    <div class="flex items-center justify-end gap-3 flex-wrap">
      <div
        role="tablist"
        class="segmented-control text-xs"
      >
        <button
          v-for="opt in windowOptions"
          :key="opt.value"
          type="button"
          role="tab"
          :aria-selected="window === opt.value"
          class="segmented-option"
          @click="emit('update:window', opt.value)"
        >
          {{ opt.label }}
        </button>
      </div>

      <span
        class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-semibold tracking-wider uppercase"
        :class="overallChipClass"
      >
        <span
          class="console-data-mark w-1.5 h-1.5 rounded-full mr-1.5"
          :class="overallDotClass"
        ></span>
        {{ overallLabel }}
      </span>

      <button
        type="button"
        class="btn btn-secondary btn-icon"
        :disabled="loading"
        :title="t('common.refresh')"
        @click="emit('refresh')"
      >
        <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
      </button>

      <AutoRefreshButton
        v-if="autoRefresh"
        :enabled="autoRefresh.enabled.value"
        :interval-seconds="autoRefresh.intervalSeconds.value"
        :countdown="autoRefresh.countdown.value"
        :intervals="autoRefresh.intervals"
        @update:enabled="autoRefresh.setEnabled"
        @update:interval="autoRefresh.setInterval"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AutoRefreshButton from '@/components/common/AutoRefreshButton.vue'
export type MonitorWindow = '7d' | '15d' | '30d'
export type OverallStatus = 'operational' | 'degraded'

const props = defineProps<{
  overallStatus: OverallStatus
  intervalSeconds: number
  window: MonitorWindow
  loading: boolean
  autoRefresh?: {
    enabled: { value: boolean }
    intervalSeconds: { value: number }
    countdown: { value: number }
    intervals: readonly number[]
    setEnabled: (v: boolean) => void
    setInterval: (v: number) => void
  }
}>()

const emit = defineEmits<{
  (e: 'update:window', value: MonitorWindow): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const windowOptions = computed<{ value: MonitorWindow; label: string }[]>(() => [
  { value: '7d', label: t('channelStatus.windowTab.7d') },
  { value: '15d', label: t('channelStatus.windowTab.15d') },
  { value: '30d', label: t('channelStatus.windowTab.30d') },
])

const overallLabel = computed(() => t(`channelStatus.overall.${props.overallStatus}`))

const overallChipClass = computed(() => {
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-500/[0.15] dark:text-emerald-300'
    case 'degraded':
    default:
      return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-500/[0.15] dark:text-amber-300'
  }
})

const overallDotClass = computed(() => {
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-500 animate-pulse'
    case 'degraded':
    default:
      return 'bg-amber-500 animate-pulse'
  }
})

</script>
