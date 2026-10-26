<template>
  <div class="text-xs tabular-nums"><span>{{ row.input_tokens?.toLocaleString() ?? '—' }}</span> <span class="surface-muted">→</span> <span>{{ row.output_tokens?.toLocaleString() ?? '—' }}</span></div>
  <div v-if="row.cache_read_tokens != null || row.cache_creation_tokens != null" class="mt-0.5 text-[11px] surface-muted">{{ t('inspection.cacheRead') }} {{ row.cache_read_tokens?.toLocaleString() ?? '—' }} · {{ t('inspection.cacheWrite') }} {{ row.cache_creation_tokens?.toLocaleString() ?? '—' }}</div>
  <div v-if="row.image_count" class="text-xs text-pink-600">{{ t('inspection.images') }} {{ row.image_count }} {{ row.image_size }}</div>
  <div v-if="row.image_output_tokens" class="text-xs text-pink-600">{{ t('inspection.imageOutput') }} {{ row.image_output_tokens }}</div>
  <div v-if="row.audio_output_tokens" class="text-xs text-violet-600">{{ t('inspection.audioOutput') }} {{ row.audio_output_tokens }}</div>
  <div v-if="row.video_count" class="text-xs text-violet-600">{{ t('requestList.video') }} {{ row.video_count }} · {{ row.video_resolution }} · {{ row.video_duration_seconds ?? '—' }} s</div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { LiveRequest } from '@/api/liveRequests'
defineProps<{ row: LiveRequest }>()
const { t } = useI18n()
</script>
