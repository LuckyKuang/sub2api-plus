<template>
  <BaseDialog :show="show" :title="t('usage.exporting')" width="narrow" @close="handleCancel">
    <div class="space-y-4">
      <div class="text-sm surface-muted">
        {{ t('usage.exportingProgress') }}
      </div>
      <div class="flex items-center justify-between text-sm surface-muted">
        <span>{{ t('usage.exportedCount', { current, total }) }}</span>
        <span class="font-medium text-gray-900 dark:text-white">{{ normalizedProgress }}%</span>
      </div>
      <div class="h-2 w-full rounded-full bg-gray-200 dark:bg-dark-700">
        <div
          role="progressbar"
          :aria-valuenow="normalizedProgress"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-label="`${t('usage.exportingProgress')}: ${normalizedProgress}%`"
          class="console-data-mark h-2 rounded-full bg-primary-600 transition-all"
          :style="{ width: `${normalizedProgress}%` }"
        ></div>
      </div>
      <div v-if="estimatedTime" class="text-xs surface-muted" aria-live="polite" aria-atomic="true">
        {{ t('usage.estimatedTime', { time: estimatedTime }) }}
      </div>
    </div>

    <template #footer>
      <button
        @click="handleCancel"
        type="button"
        class="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium hover:surface-hover focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:border-dark-600 dark:focus:ring-offset-dark-800 surface-control surface-muted"
      >
        {{ t('usage.cancelExport') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'

interface Props {
  show: boolean
  progress: number
  current: number
  total: number
  estimatedTime: string
}

interface Emits {
  (e: 'cancel'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const { t } = useI18n()

const normalizedProgress = computed(() => {
  const value = Number.isFinite(props.progress) ? props.progress : 0
  return Math.min(100, Math.max(0, Math.round(value)))
})

const handleCancel = () => {
  emit('cancel')
}
</script>
