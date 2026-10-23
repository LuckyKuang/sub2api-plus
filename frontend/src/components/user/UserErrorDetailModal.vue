<template>
  <DetailContentHost :embedded="embedded" :show="show" :title="t('usage.errors.detail.title')" placement="right" @close="emit('update:show', false)">
    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-10">
      <svg class="h-7 w-7 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
      </svg>
    </div>

    <!-- Error state -->
    <div v-else-if="loadError" class="py-8 text-center text-sm text-red-500">
      {{ t('usage.errors.detail.loadFailed') }}
    </div>

    <!-- Detail content -->
    <div v-else-if="detail" class="space-y-4 text-sm">
      <div class="grid grid-cols-2 gap-x-6 gap-y-3">
        <!-- Time -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.time') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ formatDateTime(detail.created_at) }}</p>
        </div>
        <!-- Model -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.model') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.model || '-' }}</p>
        </div>
        <!-- Endpoint -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.endpoint') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.inbound_endpoint || '-' }}</p>
        </div>
        <!-- Status Code -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.status') }}</span>
          <p class="mt-0.5">
            <span class="badge" :class="statusClass(detail.status_code)">{{ detail.status_code || '-' }}</span>
          </p>
        </div>
        <!-- Category -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.category') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ t('usage.errors.categories.' + detail.category) }}</p>
        </div>
        <!-- Platform -->
        <div>
          <span class="font-medium surface-muted">{{ t('usage.errors.platform') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.platform || '-' }}</p>
        </div>
        <!-- Async task correlation is safe because this detail is already owner-scoped. -->
        <div v-if="detail.async_task_id">
          <span class="font-medium surface-muted">{{ t('usage.errors.detail.asyncTaskId') }}</span>
          <p class="mt-0.5 break-all font-mono text-gray-900 dark:text-dark-100">{{ detail.async_task_id }}</p>
        </div>
        <!-- Upstream status code -->
        <div v-if="detail.upstream_status_code != null">
          <span class="font-medium surface-muted">{{ t('usage.errors.detail.upstreamStatus') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.upstream_status_code }}</p>
        </div>
      </div>

      <!-- Message -->
      <div v-if="detail.message">
        <span class="font-medium surface-muted">{{ t('usage.errors.message') }}</span>
        <p class="mt-0.5 text-gray-900 dark:text-dark-100 break-all">{{ detail.message }}</p>
      </div>

      <!-- Error Body -->
      <div v-if="detail.error_body">
        <span class="font-medium surface-muted">{{ t('usage.errors.detail.responseBody') }}</span>
        <pre class="mt-1 overflow-auto max-h-[40vh] whitespace-pre-wrap break-all rounded-lg surface-inset border border-gray-200 dark:border-dark-700 p-3 text-xs text-gray-800 dark:text-dark-200">{{ detail.error_body }}</pre>
      </div>
    </div>
  </DetailContentHost>
</template>

<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import DetailContentHost from '@/components/common/DetailContentHost.vue'
import { adminSupportContext } from '@/utils/adminSupportContext'
import { getMyErrorDetail } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import type { UserErrorRequestDetail } from '@/types'

const props = defineProps<{
  embedded?: boolean
  show: boolean
  errorId: number | null
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
}>()

const { t } = useI18n()

const loading = ref(false)
const loadError = ref(false)
const detail = ref<UserErrorRequestDetail | null>(null)
let generation = 0
onBeforeUnmount(() => { generation++ })

watch(
  () => [props.show, props.errorId, adminSupportContext.value?.userId] as const,
  ([show, id]) => {
    generation++
    if (show && id != null) {
      fetchDetail(id)
    } else if (!show) {
      detail.value = null
      loadError.value = false
    }
  }, { immediate: true }
)

async function fetchDetail(id: number) {
  const seq = generation
  loading.value = true
  loadError.value = false
  detail.value = null
  try {
    const result = await getMyErrorDetail(id)
    if (seq === generation) detail.value = result
  } catch (e) {
    console.error('[UserErrorDetailModal] Failed to load error detail:', e)
    if (seq === generation) loadError.value = true
  } finally {
    if (seq === generation) loading.value = false
  }
}

function statusClass(code: number) {
  if (code >= 500) return 'badge-danger'
  if (code === 429) return 'badge-warning'
  return 'badge-gray'
}
</script>
