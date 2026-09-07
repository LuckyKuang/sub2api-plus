<template>
  <div data-testid="client-disconnect-events-panel">
    <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700/50">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4 2xl:grid-cols-5">
        <div ref="userSearchRef" class="relative">
          <label class="input-label">{{ t('admin.usage.disconnectEvents.userAccount') }}</label>
          <input
            v-model="userKeyword"
            data-testid="disconnect-user-filter"
            type="text"
            class="input pr-9"
            :placeholder="t('admin.usage.searchUserPlaceholder')"
            @input="searchUsers"
            @focus="showUserDropdown = true"
          />
          <button
            v-if="draft.user_id"
            type="button"
            class="absolute right-2 top-8 p-1 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
            :title="t('admin.usage.disconnectEvents.clearFilter')"
            @click="onClearUser"
          >
            <Icon name="x" size="xs" />
          </button>
          <div
            v-if="showUserDropdown && userResults.length > 0"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
          >
            <button
              v-for="user in userResults"
              :key="user.id"
              type="button"
              class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
              @click="selectUser(user)"
            >
              <span>{{ user.email }}</span>
              <span v-if="user.deleted" class="ml-2 text-xs text-gray-400">{{ t('admin.usage.userDeletedBadge') }}</span>
            </button>
          </div>
        </div>

        <div ref="apiKeySearchRef" class="relative">
          <label class="input-label">{{ t('admin.usage.disconnectEvents.apiKeyName') }}</label>
          <input
            v-model="apiKeyKeyword"
            data-testid="disconnect-api-key-filter"
            type="text"
            class="input pr-9"
            :placeholder="t('admin.usage.searchApiKeyPlaceholder')"
            @input="searchAPIKeys"
            @focus="openAPIKeyDropdown"
          />
          <button
            v-if="draft.api_key_id"
            type="button"
            class="absolute right-2 top-8 p-1 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
            :title="t('admin.usage.disconnectEvents.clearFilter')"
            @click="onClearAPIKey"
          >
            <Icon name="x" size="xs" />
          </button>
          <div
            v-if="showAPIKeyDropdown && apiKeyResults.length > 0"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
          >
            <button
              v-for="key in apiKeyResults"
              :key="key.id"
              type="button"
              class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
              @click="selectAPIKey(key)"
            >
              {{ key.name || `#${key.id}` }}
            </button>
          </div>
        </div>

        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.requestId') }}</span>
          <input v-model.trim="draft.request_id" data-testid="disconnect-request-filter" type="text" class="input" @keyup.enter="applyFilters" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.sessionId') }}</span>
          <input v-model.trim="draft.session_id" data-testid="disconnect-session-filter" type="text" class="input" @keyup.enter="applyFilters" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.protocol') }}</span>
          <input v-model.trim="draft.protocol" data-testid="disconnect-protocol-filter" type="text" class="input" @keyup.enter="applyFilters" />
        </label>

        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.acceptedFrom') }}</span>
          <input v-model="draft.accepted_from" data-testid="disconnect-accepted-from" type="datetime-local" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.acceptedTo') }}</span>
          <input v-model="draft.accepted_to" data-testid="disconnect-accepted-to" type="datetime-local" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.finalizedFrom') }}</span>
          <input v-model="draft.finalized_from" data-testid="disconnect-finalized-from" type="datetime-local" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.finalizedTo') }}</span>
          <input v-model="draft.finalized_to" data-testid="disconnect-finalized-to" type="datetime-local" class="input" />
        </label>

        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.outcome') }}</span>
          <select v-model="draft.outcome" data-testid="disconnect-outcome-filter" class="input" @change="applyFilters">
            <option value="">{{ t('admin.usage.disconnectEvents.allOutcomes') }}</option>
            <option value="pending">{{ t('admin.usage.disconnectEvents.pending') }}</option>
            <option value="completed">{{ t('admin.usage.disconnectEvents.completed') }}</option>
            <option value="client_disconnected">{{ t('admin.usage.disconnectEvents.clientDisconnected') }}</option>
            <option value="neutral">{{ t('admin.usage.disconnectEvents.neutral') }}</option>
          </select>
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.completionStatus') }}</span>
          <select v-model="draft.completion_status" data-testid="disconnect-completion-filter" class="input" @change="applyFilters">
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
          <span class="input-label">{{ t('admin.usage.disconnectEvents.usageSource') }}</span>
          <select v-model="draft.usage_source" data-testid="disconnect-usage-source-filter" class="input" @change="applyFilters">
            <option value="">{{ t('admin.usage.disconnectEvents.allSources') }}</option>
            <option value="upstream_exact">{{ t('admin.usage.disconnectEvents.upstreamExact') }}</option>
            <option value="partial">{{ t('admin.usage.disconnectEvents.partial') }}</option>
            <option value="estimated">{{ t('admin.usage.disconnectEvents.estimated') }}</option>
            <option value="reconciled">{{ t('admin.usage.disconnectEvents.reconciled') }}</option>
          </select>
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.usageMissing') }}</span>
          <select v-model="draft.usage_missing" data-testid="disconnect-usage-missing-filter" class="input" @change="applyFilters">
            <option value="">{{ t('admin.usage.disconnectEvents.allValues') }}</option>
            <option value="true">{{ t('common.yes') }}</option>
            <option value="false">{{ t('common.no') }}</option>
          </select>
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.enforcement') }}</span>
          <select v-model="draft.enforce" data-testid="disconnect-enforce-filter" class="input" @change="applyFilters">
            <option value="">{{ t('admin.usage.disconnectEvents.allValues') }}</option>
            <option value="true">{{ t('admin.usage.disconnectEvents.enforced') }}</option>
            <option value="false">{{ t('admin.usage.disconnectEvents.auditOnly') }}</option>
          </select>
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.usage.disconnectEvents.autoBanned') }}</span>
          <select v-model="draft.auto_banned" data-testid="disconnect-auto-ban-filter" class="input" @change="applyFilters">
            <option value="">{{ t('admin.usage.disconnectEvents.allValues') }}</option>
            <option value="true">{{ t('common.yes') }}</option>
            <option value="false">{{ t('common.no') }}</option>
          </select>
        </label>
      </div>

      <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usage.disconnectEvents.hint') }}</p>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" @click="resetFilters">{{ t('common.reset') }}</button>
          <button type="button" class="btn btn-primary inline-flex items-center gap-2" :disabled="loading" @click="applyFilters">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </div>
    </div>

    <div class="overflow-x-auto">
      <table class="min-w-[2200px] divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th v-for="heading in headings" :key="heading" class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ heading }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-800">
          <tr v-if="loading">
            <td :colspan="headings.length" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</td>
          </tr>
          <tr v-else-if="events.length === 0">
            <td :colspan="headings.length" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usage.disconnectEvents.empty') }}</td>
          </tr>
          <template v-else>
          <tr v-for="event in events" :key="`${event.user_id}:${event.generation}:${event.request_id}`" class="hover:bg-gray-50 dark:hover:bg-dark-700/60">
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(event.accepted_at) }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ event.finalized_at ? formatDateTime(event.finalized_at) : '-' }}</td>
            <td class="max-w-[240px] truncate px-4 py-4 text-sm text-gray-700 dark:text-gray-300" :title="event.user_email || ''">{{ event.user_email || '-' }}</td>
            <td class="max-w-[220px] truncate px-4 py-4 text-sm text-gray-700 dark:text-gray-300" :title="event.api_key_name || ''">{{ event.api_key_name || '-' }}</td>
            <td class="max-w-[240px] px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="flex items-center gap-2">
                <span class="truncate font-mono" :title="event.request_id">{{ event.request_id || '-' }}</span>
                <button v-if="event.request_id" type="button" class="shrink-0 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200" :title="t('common.copy')" @click="copyValue(event.request_id)"><Icon name="copy" size="xs" /></button>
              </div>
            </td>
            <td class="max-w-[240px] px-4 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="flex items-center gap-2">
                <span class="truncate font-mono" :title="event.session_id || ''">{{ event.session_id || '-' }}</span>
                <button v-if="event.session_id" type="button" class="shrink-0 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200" :title="t('common.copy')" @click="copyValue(event.session_id)"><Icon name="copy" size="xs" /></button>
              </div>
            </td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ event.protocol || '-' }}</td>
            <td class="whitespace-nowrap px-4 py-4"><span class="inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="statusClass(event.completion_status)">{{ statusLabel(event.completion_status) }}</span></td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ outcomeLabel(event.outcome) }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ event.usage_source ? usageSourceLabel(event.usage_source) : '-' }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm" :class="event.usage_missing ? 'font-medium text-red-600 dark:text-red-300' : 'text-gray-700 dark:text-gray-300'">{{ event.usage_missing ? t('common.yes') : t('common.no') }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ event.consecutive_after ?? '-' }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ event.threshold ?? '-' }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-700 dark:text-gray-300">{{ enforcementLabel(event.enforce) }}</td>
            <td class="whitespace-nowrap px-4 py-4 text-sm">
              <span v-if="event.auto_banned" class="inline-flex rounded-md bg-red-100 px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300">{{ t('admin.usage.disconnectEvents.banned') }}</span>
              <span v-else class="text-gray-400">-</span>
            </td>
          </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="changePage" @update:pageSize="changePageSize" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import type { ClientDisconnectCompletionStatus, ClientDisconnectEventQueryParams, ClientDisconnectOutcome, ClientDisconnectRiskEvent, SimpleApiKey, SimpleUser } from '@/api/admin/usage'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'

type UsageSource = NonNullable<ClientDisconnectEventQueryParams['usage_source']>

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const events = ref<ClientDisconnectRiskEvent[]>([])
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const userKeyword = ref('')
const apiKeyKeyword = ref('')
const userResults = ref<SimpleUser[]>([])
const apiKeyResults = ref<SimpleApiKey[]>([])
const showUserDropdown = ref(false)
const showAPIKeyDropdown = ref(false)
let userSearchTimer: ReturnType<typeof setTimeout> | undefined
let apiKeySearchTimer: ReturnType<typeof setTimeout> | undefined
let userSearchSequence = 0
let apiKeySearchSequence = 0
let eventLoadSequence = 0

const draft = reactive({
  user_id: undefined as number | undefined,
  api_key_id: undefined as number | undefined,
  request_id: '', session_id: '', protocol: '',
  accepted_from: '', accepted_to: '', finalized_from: '', finalized_to: '',
  outcome: '' as '' | ClientDisconnectOutcome,
  completion_status: '' as '' | ClientDisconnectCompletionStatus,
  usage_source: '' as '' | UsageSource,
  usage_missing: '' as '' | 'true' | 'false',
  enforce: '' as '' | 'true' | 'false',
  auto_banned: '' as '' | 'true' | 'false',
})

const headings = computed(() => [
  t('admin.usage.disconnectEvents.acceptedAt'), t('admin.usage.disconnectEvents.finalizedAt'),
  t('admin.usage.disconnectEvents.userAccount'), t('admin.usage.disconnectEvents.apiKeyName'),
  t('admin.usage.disconnectEvents.requestId'), t('admin.usage.disconnectEvents.sessionId'),
  t('admin.usage.disconnectEvents.protocol'), t('admin.usage.disconnectEvents.completionStatus'),
  t('admin.usage.disconnectEvents.outcome'), t('admin.usage.disconnectEvents.usageSource'),
  t('admin.usage.disconnectEvents.usageMissing'), t('admin.usage.disconnectEvents.consecutiveCount'),
  t('admin.usage.disconnectEvents.threshold'), t('admin.usage.disconnectEvents.enforcement'),
  t('admin.usage.disconnectEvents.action'),
])

function booleanFilter(value: '' | 'true' | 'false'): boolean | undefined {
  return value === '' ? undefined : value === 'true'
}

function toRFC3339(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function queryParams(): ClientDisconnectEventQueryParams {
  return {
    user_id: draft.user_id, api_key_id: draft.api_key_id,
    request_id: draft.request_id || undefined, session_id: draft.session_id || undefined,
    protocol: draft.protocol || undefined,
    accepted_from: toRFC3339(draft.accepted_from), accepted_to: toRFC3339(draft.accepted_to),
    finalized_from: toRFC3339(draft.finalized_from), finalized_to: toRFC3339(draft.finalized_to),
    outcome: draft.outcome || undefined, completion_status: draft.completion_status || undefined,
    usage_source: draft.usage_source || undefined, usage_missing: booleanFilter(draft.usage_missing),
    enforce: booleanFilter(draft.enforce), auto_banned: booleanFilter(draft.auto_banned),
    page: pagination.page, page_size: pagination.page_size,
  }
}

async function loadEvents() {
  eventLoadSequence += 1
  const sequence = eventLoadSequence
  loading.value = true
  try {
    const result = await adminUsageAPI.listClientDisconnectEvents(queryParams())
    if (sequence !== eventLoadSequence) return
    events.value = result.items
    Object.assign(pagination, { total: result.total, page: result.page, page_size: result.page_size })
  } catch (error: unknown) {
    if (sequence !== eventLoadSequence) return
    appStore.showError(extractApiErrorMessage(error, t('admin.usage.disconnectEvents.failedToLoad')))
  } finally {
    if (sequence === eventLoadSequence) loading.value = false
  }
}

function searchUsers() {
  const clearedAppliedIdentity = draft.user_id !== undefined || draft.api_key_id !== undefined
  draft.user_id = undefined
  clearAPIKey()
  if (clearedAppliedIdentity) applyFilters()
  userSearchSequence += 1
  const sequence = userSearchSequence
  if (userSearchTimer) {
    clearTimeout(userSearchTimer)
    userSearchTimer = undefined
  }
  const query = userKeyword.value.trim()
  if (!query) { userResults.value = []; return }
  userSearchTimer = setTimeout(async () => {
    try {
      const result = await adminUsageAPI.searchUsers(query)
      if (sequence === userSearchSequence) userResults.value = result
    } catch {
      if (sequence === userSearchSequence) userResults.value = []
    }
  }, 300)
}

function selectUser(user: SimpleUser) {
  if (userSearchTimer) {
    clearTimeout(userSearchTimer)
    userSearchTimer = undefined
  }
  userSearchSequence += 1
  userKeyword.value = user.email
  draft.user_id = user.id
  userResults.value = []
  showUserDropdown.value = false
  clearAPIKey()
  void loadAPIKeys('')
  applyFilters()
}

function clearUser() {
  if (userSearchTimer) {
    clearTimeout(userSearchTimer)
    userSearchTimer = undefined
  }
  userSearchSequence += 1
  userKeyword.value = ''
  draft.user_id = undefined
  userResults.value = []
  showUserDropdown.value = false
  clearAPIKey()
}

function onClearUser() {
  clearUser()
  applyFilters()
}

async function loadAPIKeys(query: string) {
  apiKeySearchSequence += 1
  const sequence = apiKeySearchSequence
  try {
    const result = await adminUsageAPI.searchApiKeys(draft.user_id, query)
    if (sequence === apiKeySearchSequence) apiKeyResults.value = result
  } catch {
    if (sequence === apiKeySearchSequence) apiKeyResults.value = []
  }
}

function searchAPIKeys() {
  const clearedAppliedAPIKey = draft.api_key_id !== undefined
  draft.api_key_id = undefined
  if (clearedAppliedAPIKey) applyFilters()
  apiKeySearchSequence += 1
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  apiKeySearchTimer = setTimeout(() => void loadAPIKeys(apiKeyKeyword.value.trim()), 300)
}

function openAPIKeyDropdown() {
  showAPIKeyDropdown.value = true
  if (apiKeyResults.value.length === 0) void loadAPIKeys(apiKeyKeyword.value.trim())
}

function selectAPIKey(key: SimpleApiKey) {
  if (apiKeySearchTimer) {
    clearTimeout(apiKeySearchTimer)
    apiKeySearchTimer = undefined
  }
  apiKeySearchSequence += 1
  apiKeyKeyword.value = key.name || String(key.id)
  draft.api_key_id = key.id
  showAPIKeyDropdown.value = false
  applyFilters()
}

function clearAPIKey() {
  if (apiKeySearchTimer) {
    clearTimeout(apiKeySearchTimer)
    apiKeySearchTimer = undefined
  }
  apiKeySearchSequence += 1
  apiKeyKeyword.value = ''
  draft.api_key_id = undefined
  apiKeyResults.value = []
  showAPIKeyDropdown.value = false
}

function onClearAPIKey() {
  clearAPIKey()
  applyFilters()
}

function applyFilters() { pagination.page = 1; void loadEvents() }
function resetFilters() {
  clearUser()
  Object.assign(draft, {
    request_id: '', session_id: '', protocol: '', accepted_from: '', accepted_to: '',
    finalized_from: '', finalized_to: '', outcome: '', completion_status: '', usage_source: '',
    usage_missing: '', enforce: '', auto_banned: '',
  })
  applyFilters()
}
function changePage(page: number) { pagination.page = page; void loadEvents() }
function changePageSize(pageSize: number) { pagination.page_size = pageSize; pagination.page = 1; void loadEvents() }

function statusLabel(status: ClientDisconnectCompletionStatus): string {
  return t(`admin.usage.disconnectEvents.${status.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())}`)
}
function outcomeLabel(outcome: ClientDisconnectOutcome): string {
  return t(`admin.usage.disconnectEvents.${outcome.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())}`)
}
function usageSourceLabel(source: UsageSource): string {
  return t(`admin.usage.disconnectEvents.${source.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())}`)
}
function enforcementLabel(enforce: boolean | undefined): string {
  if (enforce === undefined) return '-'
  return enforce ? t('admin.usage.disconnectEvents.enforced') : t('admin.usage.disconnectEvents.auditOnly')
}
function statusClass(status: ClientDisconnectCompletionStatus): string {
  if (status === 'client_disconnected' || status === 'usage_missing') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (status === 'completed') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (status === 'pending') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}
function copyValue(value: string) { void navigator.clipboard?.writeText(value) }

function closeDropdowns(event: MouseEvent) {
  const target = event.target as Node | null
  if (!target) return
  if (!userSearchRef.value?.contains(target)) showUserDropdown.value = false
  if (!apiKeySearchRef.value?.contains(target)) showAPIKeyDropdown.value = false
}

onMounted(() => { document.addEventListener('click', closeDropdowns); void loadEvents() })
onUnmounted(() => {
  eventLoadSequence += 1
  if (userSearchTimer) clearTimeout(userSearchTimer)
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  document.removeEventListener('click', closeDropdowns)
})
</script>
