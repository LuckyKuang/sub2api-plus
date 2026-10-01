<template>
  <div class="flex items-center gap-2">
    <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs" :title="t('common.autoRefresh.title')" :aria-pressed="enabled" @click="emit('update:enabled', !enabled)">
      {{ enabled ? t('common.autoRefresh.countdown', { seconds: countdown }) : t('common.autoRefresh.title') }}
    </button>
    <select :value="intervalSeconds" class="input w-auto py-1.5 text-xs" :aria-label="t('common.autoRefresh.title')" @change="emit('update:interval', Number(($event.target as HTMLSelectElement).value))">
      <option v-for="sec in intervals" :key="sec" :value="sec">{{ t('common.autoRefresh.seconds', { n: sec }) }}</option>
    </select>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ enabled: boolean; intervalSeconds: number; countdown: number; intervals: readonly number[] }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:interval': [value: number] }>()
const { t } = useI18n()
</script>
