<template>
  <div data-testid="client-disconnect-events-panel">
    <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700/50">
      <div class="flex flex-col gap-3 xl:flex-row xl:items-end">
        <div class="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.userId') }}</span>
            <input v-model.trim="draft.user_id" type="number" min="1" class="input" @keyup.enter="applyFilters" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.apiKeyId') }}</span>
            <input v-model.trim="draft.api_key_id" type="number" min="1" class="input" @keyup.enter="applyFilters" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.outcome') }}</span>
            <select v-model="draft.outcome" class="input" @change="applyFilters">
              <option value="">{{ t('admin.usage.disconnectEvents.allOutcomes') }}</option>
              <option value="pending">{{ t('admin.usage.disconnectEvents.pending') }}</option>
              <option value="completed">{{ t('admin.usage.disconnectEvents.completed') }}</option>
              <option value="client_disconnected">{{ t('admin.usage.disconnectEvents.clientDisconnected') }}</option>
              <option value="neutral">{{ t('admin.usage.disconnectEvents.neutral') }}</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.completionStatus') }}</span>
            <select v-model="draft.completion_status" class="input" @change="applyFilters">
              <option value="">{{ t('admin.usage.disconnectEvents.allStatuses') }}</option>
              <option value="pending">{{ t('admin.usage.disconnectEvents.pending') }}</option>
              <option value="completed">{{ t('admin.usage.disconnectEvents.completed') }}</option>
              <option value="client_disconnected">{{ t('admin.usage.disconnectEvents.clientDisconnected') }}</option>
              <option value="upstream_failed">{{ t('admin.usage.disconnectEvents.upstreamFailed') }}</option>
              <option value="upstream_timeout">{{ t('admin.usage.disconnectEvents.upstreamTimeout') }}</option>
              <option value="usage_missing">{{ t('admin.usage.disconnectEvents.usageMissing') }}</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.usageMissing') }}</span>
            <select v-model="draft.usage_missing" class="input" @change="applyFilters">
              <option value="">{{ t('admin.usage.disconnectEvents.allValues') }}</option>
              <option value="true">{{ t('common.yes') }}</option>
              <option value="false">{{ t('common.no') }}</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('admin.usage.disconnectEvents.autoBanned') }}</span>
            <select v-model="draft.auto_banned" class="input" @change="applyFilters">
              <option value="">{{ t('admin.usage.disconnectEvents.allValues') }}</option>
              <option value="true">{{ t('common.yes') }}</option>
              <option value="false">{{ t('common.no') }}</option>
            </select>
          </label>
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" @click="resetFilters">
            {{ t('common.reset') }}
          </button>
          <button type="button" class="btn btn-primary inline-flex items-center gap-2" :disabled="loading" @click="applyFilters">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </div>
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usage.disconnectEvents.hint') }}</p>
    </div>

    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th v-for="heading in headings" :key="heading" class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
              {{ heading }}
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-800">
          <tr v-if="loading">
            <td colspan="8" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</td>
          </tr>
          <tr v-else-if="events.length === 0">
            <td colspan="8" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usage.disconnectEvents.empty') }}</td>
          </tr>
          <template v-else>
          <tr v-for="event in events" :key="`${event.user_id}:${event.generation}:${event.sequence}`" class="hover:bg-gray-50 dark:hover:bg-dark-700/60">
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div>{{ formatDateTime(event.accepted_at) }}</div>
              <div v-if="event.finalized_at" class="text-xs text-gray-400">{{ formatDateTime(event.finalized_at) }}</div>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div>UID {{ event.user_id }}</div>
              <div class="text-xs text-gray-400">Key {{ event.api_key_id ?? '-' }}</div>
            </td>
            <td class="max-w-[280px] px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="truncate font-mono" :title="event.request_id">{{ event.request_id || '-' }}</div>
              <div class="text-xs text-gray-400">{{ event.protocol || '-' }}</div>
            </td>
            <td class="whitespace-nowrap px-4 py-4">
              <span class="inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="statusClass(event.completion_status)">
                {{ statusLabel(event.completion_status) }}
              </span>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div>{{ outcomeLabel(event.outcome) }}</div>
              <div class="text-xs text-gray-400">{{ event.usage_source || '-' }}</div>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <span :class="event.usage_missing ? 'font-medium text-red-600 dark:text-red-300' : ''">
                {{ event.usage_missing ? t('common.yes') : t('common.no') }}
              </span>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div>{{ event.consecutive_after ?? '-' }} / {{ event.threshold ?? '-' }}</div>
              <div class="text-xs text-gray-400">{{ event.enforce === false ? t('admin.usage.disconnectEvents.auditOnly') : t('admin.usage.disconnectEvents.enforced') }}</div>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm">
              <span v-if="event.auto_banned" class="inline-flex rounded-md bg-red-100 px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300">
                {{ t('admin.usage.disconnectEvents.banned') }}
              </span>
              <span v-else class="text-gray-400">-</span>
            </td>
          </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination
      v-if="pagination.total > 0"
      :page="pagination.page"
      :total="pagination.total"
      :page-size="pagination.page_size"
      @update:page="changePage"
      @update:pageSize="changePageSize"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import type {
  ClientDisconnectCompletionStatus,
  ClientDisconnectEventQueryParams,
  ClientDisconnectOutcome,
  ClientDisconnectRiskEvent,
} from '@/api/admin/usage'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const events = ref<ClientDisconnectRiskEvent[]>([])
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const draft = reactive({
  user_id: '',
  api_key_id: '',
  outcome: '' as '' | ClientDisconnectOutcome,
  completion_status: '' as '' | ClientDisconnectCompletionStatus,
  usage_missing: '' as '' | 'true' | 'false',
  auto_banned: '' as '' | 'true' | 'false',
})

const headings = computed(() => [
  t('admin.usage.disconnectEvents.time'),
  t('admin.usage.disconnectEvents.identity'),
  t('admin.usage.disconnectEvents.request'),
  t('admin.usage.disconnectEvents.completionStatus'),
  t('admin.usage.disconnectEvents.outcome'),
  t('admin.usage.disconnectEvents.usageMissing'),
  t('admin.usage.disconnectEvents.streak'),
  t('admin.usage.disconnectEvents.action'),
])

function positiveID(value: string): number | undefined {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined
}

function booleanFilter(value: '' | 'true' | 'false'): boolean | undefined {
  return value === '' ? undefined : value === 'true'
}

function queryParams(): ClientDisconnectEventQueryParams {
  return {
    user_id: positiveID(draft.user_id),
    api_key_id: positiveID(draft.api_key_id),
    outcome: draft.outcome || undefined,
    completion_status: draft.completion_status || undefined,
    usage_missing: booleanFilter(draft.usage_missing),
    auto_banned: booleanFilter(draft.auto_banned),
    page: pagination.page,
    page_size: pagination.page_size,
  }
}

async function loadEvents() {
  loading.value = true
  try {
    const result = await adminUsageAPI.listClientDisconnectEvents(queryParams())
    events.value = result.items
    pagination.total = result.total
    pagination.page = result.page
    pagination.page_size = result.page_size
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.usage.disconnectEvents.failedToLoad')))
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  pagination.page = 1
  void loadEvents()
}

function resetFilters() {
  Object.assign(draft, { user_id: '', api_key_id: '', outcome: '', completion_status: '', usage_missing: '', auto_banned: '' })
  applyFilters()
}

function changePage(page: number) {
  pagination.page = page
  void loadEvents()
}

function changePageSize(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadEvents()
}

function statusLabel(status: ClientDisconnectCompletionStatus): string {
  return t(`admin.usage.disconnectEvents.${status.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())}`)
}

function outcomeLabel(outcome: ClientDisconnectOutcome): string {
  return t(`admin.usage.disconnectEvents.${outcome.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())}`)
}

function statusClass(status: ClientDisconnectCompletionStatus): string {
  if (status === 'client_disconnected' || status === 'usage_missing') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (status === 'completed') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (status === 'pending') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

onMounted(() => {
  void loadEvents()
})
</script>
