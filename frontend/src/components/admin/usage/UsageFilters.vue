<template>
  <div :class="[flat ? (workspace ? '' : 'p-4 sm:p-6') : 'card p-6', { 'usage-workspace-filters': workspace }]">
    <!-- Toolbar: left filters (multi-line) + right actions -->
    <div class="usage-filter-layout filter-bar">
      <!-- Left: filters (allowed to wrap to multiple rows) -->
      <div class="usage-filter-fields filter-bar--inline">
        <!-- User Search -->
        <div ref="userSearchRef" class="usage-filter-dropdown filter-field relative">
          <label class="input-label">{{ t('admin.usage.userFilter') }}</label>
          <input
            v-model="userKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchUserPlaceholder')"
            @input="debounceUserSearch"
            @focus="showUserDropdown = true"
          />
          <button
            v-if="filters.user_id"
            type="button"
            @click="clearUser"
            class="absolute right-2 top-9 surface-control surface-muted"
            aria-label="Clear user filter"
          >
            ✕
          </button>
          <Teleport to="body" :disabled="!workspace">
          <div
            v-if="showUserDropdown && (userResults.length > 0 || userKeyword)"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border shadow-lg surface-panel"
            :style="workspace ? userDropdownStyle : undefined"
            data-testid="usage-user-options"
            @click.stop
          >
            <button
              v-for="u in userResults"
              :key="u.id"
              type="button"
              @click="selectUser(u)"
              class="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            >
              <span>{{ u.email }}<span v-if="u.deleted" class="ml-1 text-xs surface-muted">（{{ t('admin.usage.userDeletedBadge') }}）</span></span>
              <span class="ml-2 text-xs surface-muted">#{{ u.id }}</span>
            </button>
          </div>
          </Teleport>
        </div>

        <!-- API Key Search -->
        <div ref="apiKeySearchRef" class="usage-filter-dropdown filter-field relative">
          <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
          <input
            v-model="apiKeyKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchApiKeyPlaceholder')"
            @input="debounceApiKeySearch"
            @focus="onApiKeyFocus"
          />
          <button
            v-if="filters.api_key_id"
            type="button"
            @click="onClearApiKey"
            class="absolute right-2 top-9 surface-control surface-muted"
            aria-label="Clear API key filter"
          >
            ✕
          </button>
          <Teleport to="body" :disabled="!workspace">
          <div
            v-if="showApiKeyDropdown && apiKeyResults.length > 0"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border shadow-lg surface-panel"
            :style="workspace ? apiKeyDropdownStyle : undefined"
            data-testid="usage-api-key-options"
            @click.stop
          >
            <button
              v-for="k in apiKeyResults"
              :key="k.id"
              type="button"
              @click="selectApiKey(k)"
              class="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            >
              <span class="truncate">{{ k.name || `#${k.id}` }}</span>
              <span class="ml-2 text-xs surface-muted">#{{ k.id }}</span>
            </button>
          </div>
          </Teleport>
        </div>

        <!-- Model Filter -->
        <div class="filter-field">
          <label class="input-label">{{ t('usage.model') }}</label>
          <Select v-model="filters.model" :options="modelOptions" searchable @change="emitChange" />
        </div>

        <!-- Account Filter -->
        <div ref="accountSearchRef" class="usage-filter-dropdown filter-field relative">
          <label class="input-label">{{ t('admin.usage.account') }}</label>
          <input
            v-model="accountKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchAccountPlaceholder')"
            @input="debounceAccountSearch"
            @focus="showAccountDropdown = true"
          />
          <button
            v-if="filters.account_id"
            type="button"
            @click="clearAccount"
            class="absolute right-2 top-9 surface-control surface-muted"
            aria-label="Clear account filter"
          >
            ✕
          </button>
          <Teleport to="body" :disabled="!workspace">
          <div
            v-if="showAccountDropdown && (accountResults.length > 0 || accountKeyword)"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border shadow-lg surface-panel"
            :style="workspace ? accountDropdownStyle : undefined"
            data-testid="usage-account-options"
            @click.stop
          >
            <button
              v-for="a in accountResults"
              :key="a.id"
              type="button"
              @click="selectAccount(a)"
              class="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            >
              <span class="truncate">{{ a.name }}</span>
              <span class="ml-2 text-xs surface-muted">#{{ a.id }}</span>
            </button>
          </div>
          </Teleport>
        </div>

        <!-- Advanced filters (collapsed by default to keep the primary row calm) -->
        <div v-show="showAdvanced" class="usage-filter-advanced">
        <!-- Request Type Filter (usage only) -->
        <div v-if="mode !== 'errors'" class="filter-field">
          <label class="input-label">{{ t('usage.type') }}</label>
          <Select v-model="filters.request_type" :options="requestTypeOptions" @change="emitChange" />
        </div>

        <!-- Native compaction is independent of the transport request type. -->
        <div v-if="mode !== 'errors'" class="filter-field">
          <label class="input-label">{{ t('usage.compactionFilter') }}</label>
          <Select v-model="filters.native_compaction_v2" :options="compactionOptions" @change="emitChange" />
        </div>

        <!-- Billing Type Filter (usage only) -->
        <div v-if="mode !== 'errors'" class="filter-field">
          <label class="input-label">{{ t('admin.usage.billingType') }}</label>
          <Select v-model="filters.billing_type" :options="billingTypeOptions" @change="emitChange" />
        </div>

        <!-- Billing Mode Filter (usage only；用户排行的 user-breakdown 接口不支持该维度) -->
        <div v-if="mode === 'usage'" class="filter-field">
          <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
          <Select v-model="filters.billing_mode" :options="billingModeOptions" @change="emitChange" />
        </div>

        <div v-if="mode === 'usage'" class="filter-field">
          <label class="input-label">{{ t('admin.usage.upstreamModelAudit') }}</label>
          <Select v-model="filters.upstream_model_mismatch" :options="upstreamModelMismatchOptions" @change="emitChange" />
        </div>

		<div v-if="mode === 'usage'" class="filter-field">
		  <label class="input-label">{{ t('admin.usage.completionStatus') }}</label>
		  <Select v-model="filters.completion_status" :options="completionStatusOptions" @change="emitChange" />
		</div>

		<div v-if="mode === 'usage'" class="filter-field">
		  <label class="input-label">{{ t('admin.usage.usageSource') }}</label>
		  <Select v-model="filters.usage_source" :options="usageSourceOptions" @change="emitChange" />
		</div>

        <!-- Error record view (errors only) -->
        <div v-if="mode === 'errors'" class="filter-field">
          <label class="input-label">{{ t('admin.ops.errorDetails.view') }}</label>
          <Select :model-value="errorView" :options="errorViewOptions" @update:model-value="updateErrorView" />
        </div>

        <!-- Error Phase Filter (errors only) -->
        <div v-if="mode === 'errors'" class="filter-field">
          <label class="input-label">{{ t('admin.ops.errorLog.type') }}</label>
          <Select v-model="filters.error_phase" :options="errorPhaseOptions" @change="emitChange" />
        </div>

        <!-- Error Category Filter (errors only) -->
        <div v-if="mode === 'errors'" class="filter-field">
          <label class="input-label">{{ t('usage.errors.category') }}</label>
          <Select v-model="filters.error_category" :options="errorCategoryOptions" @change="emitChange" />
        </div>

        <!-- Status Code Filter (errors only) -->
        <div v-if="mode === 'errors'" class="filter-field">
          <label class="input-label">{{ t('admin.ops.errorLog.status') }}</label>
          <Select v-model="filters.status_code" :options="statusCodeOptions" @change="emitChange" />
        </div>

        <!-- Group Filter -->
        <div class="filter-field">
          <label class="input-label">{{ t('admin.usage.group') }}</label>
          <Select v-model="filters.group_id" :options="groupOptions" searchable @change="emitChange" />
        </div>
        </div>

        <button
          type="button"
          class="usage-filter-more inline-flex items-center gap-1 rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium surface-muted surface-control transition-colors hover:surface-hover dark:border-dark-600"
          :aria-expanded="showAdvanced"
          @click="showAdvanced = !showAdvanced"
        >
          <span>{{ showAdvanced ? t('admin.usage.fewerFilters') : t('admin.usage.moreFilters') }}</span>
          <!-- A collapsed advanced block keeps its filters active, so the count
               is the only cue that the visible row is not the whole query. -->
          <span
            v-if="!showAdvanced && activeAdvancedCount > 0"
            class="usage-filter-more-badge rounded-full bg-primary-500/15 px-1.5 py-0.5 text-xs font-semibold text-primary-700 dark:text-primary-300"
            data-testid="usage-advanced-filter-count"
          >{{ activeAdvancedCount }}</span>
          <Icon name="chevronDown" size="sm" :class="{ 'rotate-180': showAdvanced }" />
        </button>
      </div>

      <!-- Right: actions -->
      <div v-if="showActions" class="usage-filter-actions filter-actions">
        <button type="button" @click="$emit('refresh')" class="btn btn-secondary">
          {{ t('common.refresh') }}
        </button>
        <button type="button" @click="$emit('reset')" class="btn btn-secondary">
          {{ t('common.reset') }}
        </button>
        <slot name="after-reset" />
        <template v-if="mode === 'usage'">
          <button type="button" @click="$emit('cleanup')" class="btn btn-danger">
            {{ t('admin.usage.cleanup.button') }}
          </button>
          <button type="button" @click="$emit('export')" :disabled="exporting" class="btn btn-primary">
            {{ t('usage.exportExcel') }}
          </button>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, toRef, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAuthStore } from '@/stores'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'
import { useAnchoredDropdown } from '@/composables/useAnchoredDropdown'

type ModelValue = Record<string, any>

interface Props {
  modelValue: ModelValue
  exporting: boolean
  startDate: string
  endDate: string
  showActions?: boolean
  modelOptions?: string[]
  /**
   * errors 模式:隐藏用量专属字段/按钮,显示错误类型+状态码(错误请求 tab 用)
   * ranking 模式:同 usage 但隐藏计费模式筛选与清理/导出按钮(用户排行 tab 用)
  */
  mode?: 'usage' | 'errors' | 'ranking'
  /** Current Ops error record view; meaningful only in errors mode. */
  errorView?: 'errors' | 'excluded' | 'all'
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
  workspace?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showActions: true,
  mode: 'usage',
  errorView: 'errors',
  flat: false,
  workspace: false
})
const emit = defineEmits([
  'update:modelValue',
  'change',
  'refresh',
  'reset',
  'export',
  'cleanup',
  'update:errorView'
])

const { t } = useI18n()
const filters = toRef(props, 'modelValue')

const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const accountSearchRef = ref<HTMLElement | null>(null)

const userKeyword = ref('')
const userResults = ref<SimpleUser[]>([])
const showAdvanced = ref(false)

// Fields rendered inside the collapsed advanced block, keyed by the v-if that
// gates them. A field the current mode never renders is not part of the query,
// so counting it would make the badge lie.
const ADVANCED_FILTER_FIELDS: Record<'usage' | 'errors' | 'ranking', readonly string[]> = {
  usage: ['request_type', 'native_compaction_v2', 'billing_type', 'billing_mode', 'upstream_model_mismatch', 'completion_status', 'usage_source', 'group_id'],
  ranking: ['request_type', 'native_compaction_v2', 'billing_type', 'upstream_model_mismatch', 'completion_status', 'usage_source', 'group_id'],
  errors: ['error_phase', 'error_category', 'status_code', 'group_id'],
}
const activeAdvancedCount = computed(() => {
  const values = filters.value as Record<string, unknown>
  const applied = ADVANCED_FILTER_FIELDS[props.mode].filter(field => {
    const value = values[field]
    return value !== undefined && value !== null && value !== ''
  }).length
  // errors mode also hides the record view in the advanced block; its default
  // shows only errors, so any other value changes which rows come back.
  if (props.mode === 'errors' && props.errorView !== 'errors') return applied + 1
  return applied
})
const showUserDropdown = ref(false)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0

const apiKeyKeyword = ref('')
const apiKeyResults = ref<SimpleApiKey[]>([])
const showApiKeyDropdown = ref(false)
let apiKeySearchTimeout: ReturnType<typeof setTimeout> | null = null

interface SimpleAccount {
  id: number
  name: string
}
const accountKeyword = ref('')
const accountResults = ref<SimpleAccount[]>([])
const showAccountDropdown = ref(false)
const userDropdownStyle = useAnchoredDropdown(userSearchRef, () => props.workspace && showUserDropdown.value)
const apiKeyDropdownStyle = useAnchoredDropdown(apiKeySearchRef, () => props.workspace && showApiKeyDropdown.value)
const accountDropdownStyle = useAnchoredDropdown(accountSearchRef, () => props.workspace && showAccountDropdown.value)
let accountSearchTimeout: ReturnType<typeof setTimeout> | null = null

const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...(props.modelOptions ?? []).map((m) => ({ value: m, label: m })),
])
const groupOptions = ref<SelectOption[]>([{ value: null, label: t('admin.usage.allGroups') }])

const requestTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
  { value: 'cyber', label: t('usage.cyber') }
])

const compactionOptions = ref<SelectOption[]>([
  { value: null, label: t('usage.allCompactionTypes') },
  { value: true, label: t('usage.compactionOnly') }
])

const billingTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') }
])

// 错误类型对应后端 phase 参数(与错误表"类型"徽章同语义)
const errorPhaseOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'upstream', label: t('admin.ops.errorLog.typeUpstream') },
  { value: 'account_auth', label: t('admin.ops.errorLog.typeAccountAuth') },
  { value: 'request', label: t('admin.ops.errorLog.typeRequest') },
  { value: 'auth', label: t('admin.ops.errorLog.typeAuth') },
  { value: 'routing', label: t('admin.ops.errorLog.typeRouting') },
  { value: 'internal', label: t('admin.ops.errorLog.typeInternal') },
])

// 分类码同用户端 /usage 错误筛选;"other" 无法反查为过滤条件,刻意不列
const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber', 'security_audit']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

const statusCodeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const errorViewOptions = computed<SelectOption[]>(() => [
  { value: 'errors', label: t('admin.ops.errorDetails.viewErrors') },
  { value: 'excluded', label: t('admin.ops.errorDetails.viewExcluded') },
  { value: 'all', label: t('admin.ops.errorDetails.viewAll') },
])

const billingModeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') }
])

const upstreamModelMismatchOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allUpstreamModelAudit') },
  { value: true, label: t('admin.usage.upstreamModelMismatchOnly') },
  { value: false, label: t('admin.usage.upstreamModelMatchedOnly') }
])

const completionStatusOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allCompletionStatuses') },
  { value: 'completed', label: t('admin.usage.completionCompleted') },
  { value: 'client_disconnected', label: t('usage.clientDisconnected') },
  { value: 'incomplete', label: t('usage.incomplete') },
  { value: 'unknown', label: t('usage.unknown') }
])

const usageSourceOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allUsageSources') },
  { value: 'upstream_exact', label: t('admin.usage.usageSourceExact') },
  { value: 'partial', label: t('admin.usage.usageSourcePartial') },
  { value: 'estimated', label: t('admin.usage.usageSourceEstimated') },
  { value: 'reconciled', label: t('admin.usage.usageSourceReconciled') },
  { value: 'unknown', label: t('usage.unknown') }
])

const emitChange = () => emit('change')

const updateErrorView = (value: unknown) => {
  if (value === 'errors' || value === 'excluded' || value === 'all') {
    emit('update:errorView', value)
  }
}

const clearPendingUserSearch = () => {
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
    userSearchTimeout = null
  }
  userSearchSequence += 1
}

const debounceUserSearch = () => {
  clearPendingUserSearch()
  const query = userKeyword.value.trim()
  if (!query) {
    userResults.value = []
    return
  }

  const sequence = userSearchSequence
  userSearchTimeout = setTimeout(async () => {
    userSearchTimeout = null
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === userSearchSequence) {
        // The signed-in administrator stays first so an admin auditing their own
        // traffic can switch back to that view without retyping.
        const adminId = useAuthStore().user?.id
        const ordered = [...results].sort((a, b) => Number(a.deleted) - Number(b.deleted))
        userResults.value = adminId === undefined
          ? ordered
          : [...ordered.filter(u => u.id === adminId), ...ordered.filter(u => u.id !== adminId)]
      }
    } catch {
      if (sequence === userSearchSequence) {
        userResults.value = []
      }
    }
  }, 300)
}

const debounceApiKeySearch = () => {
  if (apiKeySearchTimeout) clearTimeout(apiKeySearchTimeout)
  apiKeySearchTimeout = setTimeout(async () => {
    try {
      apiKeyResults.value = await adminAPI.usage.searchApiKeys(
        filters.value.user_id,
        apiKeyKeyword.value || ''
      )
    } catch {
      apiKeyResults.value = []
    }
  }, 300)
}

const selectUser = async (u: SimpleUser) => {
  clearPendingUserSearch()
  userKeyword.value = u.email
  showUserDropdown.value = false
  filters.value.user_id = u.id
  clearApiKey()

  // Auto-load API keys for this user
  try {
    apiKeyResults.value = await adminAPI.usage.searchApiKeys(u.id, '')
  } catch {
    apiKeyResults.value = []
  }

  emitChange()
}

const clearUser = () => {
  clearPendingUserSearch()
  userKeyword.value = ''
  userResults.value = []
  showUserDropdown.value = false
  filters.value.user_id = undefined
  clearApiKey()
  emitChange()
}

const selectApiKey = (k: SimpleApiKey) => {
  apiKeyKeyword.value = k.name || String(k.id)
  showApiKeyDropdown.value = false
  filters.value.api_key_id = k.id
  emitChange()
}

const clearApiKey = () => {
  apiKeyKeyword.value = ''
  apiKeyResults.value = []
  showApiKeyDropdown.value = false
  filters.value.api_key_id = undefined
}

const onClearApiKey = () => {
  clearApiKey()
  emitChange()
}

const debounceAccountSearch = () => {
  if (accountSearchTimeout) clearTimeout(accountSearchTimeout)
  accountSearchTimeout = setTimeout(async () => {
    if (!accountKeyword.value) {
      accountResults.value = []
      return
    }
    try {
      const res = await adminAPI.accounts.list(1, 20, { search: accountKeyword.value })
      accountResults.value = res.items.map((a) => ({ id: a.id, name: a.name }))
    } catch {
      accountResults.value = []
    }
  }, 300)
}

const selectAccount = (a: SimpleAccount) => {
  accountKeyword.value = a.name
  showAccountDropdown.value = false
  filters.value.account_id = a.id
  emitChange()
}

const clearAccount = () => {
  accountKeyword.value = ''
  accountResults.value = []
  showAccountDropdown.value = false
  filters.value.account_id = undefined
  emitChange()
}

const onApiKeyFocus = () => {
  showApiKeyDropdown.value = true
  // Trigger search if no results yet
  if (apiKeyResults.value.length === 0) {
    debounceApiKeySearch()
  }
}

const onDocumentClick = (e: MouseEvent) => {
  const target = e.target as Node | null
  if (!target) return

  const clickedInsideUser = userSearchRef.value?.contains(target) ?? false
  const clickedInsideApiKey = apiKeySearchRef.value?.contains(target) ?? false
  const clickedInsideAccount = accountSearchRef.value?.contains(target) ?? false

  if (!clickedInsideUser) showUserDropdown.value = false
  if (!clickedInsideApiKey) showApiKeyDropdown.value = false
  if (!clickedInsideAccount) showAccountDropdown.value = false
}

watch(
  () => props.startDate,
  (value) => {
    filters.value.start_date = value
  },
  { immediate: true }
)

watch(
  () => props.endDate,
  (value) => {
    filters.value.end_date = value
  },
  { immediate: true }
)

watch(
  () => filters.value.user_id,
  (userId) => {
    if (!userId) {
      clearPendingUserSearch()
      userKeyword.value = ''
      userResults.value = []
    }
  }
)

watch(
  () => filters.value.api_key_id,
  (apiKeyId) => {
    if (!apiKeyId) {
      apiKeyKeyword.value = ''
      apiKeyResults.value = []
    }
  }
)

watch(
  () => filters.value.account_id,
  (accountId) => {
    if (!accountId) {
      accountKeyword.value = ''
      accountResults.value = []
    }
  }
)

onMounted(async () => {
  document.addEventListener('click', onDocumentClick)
  try {
    const gs = await adminAPI.groups.list(1, 1000)
    groupOptions.value.push(...gs.items.map((g: any) => ({ value: g.id, label: g.name })))
  } catch {
    // Ignore filter option loading errors (page still usable)
  }
})

onUnmounted(() => {
  clearPendingUserSearch()
  document.removeEventListener('click', onDocumentClick)
})

// 供外部(如用户排行下钻)在程序化设置 user_id 后回显选中的用户邮箱
const setUserKeyword = (email: string) => {
  clearPendingUserSearch()
  userKeyword.value = email
  userResults.value = []
  showUserDropdown.value = false
}

const getUserSearchRevision = () => userSearchSequence

defineExpose({ getUserSearchRevision, setUserKeyword })
</script>

<style scoped>
.usage-filter-advanced { display: contents; }
.usage-filter-more { flex: none; height: var(--filter-control-height, 38px); }
.usage-workspace-filters .input-label { min-height: 16px; margin-bottom: 4px; font-size: 12px; line-height: 16px; }
.usage-workspace-filters .usage-filter-actions { gap: 8px; }
.usage-workspace-filters .usage-filter-dropdown > button { top: 27px; }
@media (min-width: 1024px) and (max-height: 799px) {
  .usage-workspace-filters { --filter-control-height: 32px; }
  .usage-workspace-filters .usage-filter-layout { row-gap: 6px; }
  .usage-workspace-filters .input-label { margin-bottom: 2px; }
}
</style>
