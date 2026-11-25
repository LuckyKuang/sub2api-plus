<template>
  <BaseDialog
    :show="show"
    :title="t('keys.testModal.title')"
    width="normal"
    @close="emit('close')"
  >
    <div v-if="apiKey" class="space-y-4">
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="key-test-summary">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ apiKey.name }}</p>
            <p class="mt-1 text-xs surface-muted" data-testid="key-test-scope">{{ scopeText }}</p>
          </div>
          <span
            class="shrink-0 rounded px-2 py-0.5 text-xs font-medium"
            :class="apiKey.status === 'active'
              ? 'bg-green-100/50 text-green-700 dark:bg-green-500/20 dark:text-green-300'
              : 'surface-inset surface-muted'"
          >
            {{ statusText }}
          </span>
        </div>
      </div>

      <div
        v-if="blockedReason"
        role="alert"
        data-testid="key-test-blocked"
        class="flex items-start gap-2 rounded-lg border border-amber-300 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:border-amber-600 dark:bg-amber-950/30 dark:text-amber-200"
      >
        <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
        <p>{{ blockedText }}</p>
      </div>

      <template v-else>
        <div v-if="loading" class="flex items-center gap-2 text-sm surface-muted" data-testid="key-test-loading">
          <Icon name="refresh" size="sm" class="animate-spin" />
          <span>{{ t('keys.testModal.loading') }}</span>
        </div>

        <div
          v-else-if="loadError"
          role="alert"
          data-testid="key-test-load-error"
          class="space-y-2 rounded-lg border border-red-200 bg-red-50/[0.12] px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/20 dark:text-red-300"
        >
          <p>{{ t('keys.testModal.loadFailed') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" data-testid="key-test-retry" @click="loadCandidates">
            {{ t('keys.testModal.retry') }}
          </button>
        </div>

        <div
          v-else-if="!candidates.length"
          role="alert"
          data-testid="key-test-no-capability"
          class="rounded-lg border border-amber-300 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:border-amber-600 dark:bg-amber-950/30 dark:text-amber-200"
        >
          {{ noCapabilityText }}
        </div>

        <template v-else>
          <div>
            <label class="input-label" for="key-test-model">{{ t('keys.testModal.model') }}</label>
            <Select
              id="key-test-model"
              v-model="selectedId"
              :options="modelOptions"
              :disabled="testing"
              :placeholder="t('keys.testModal.modelPlaceholder')"
              data-testid="key-test-model"
            />
            <p v-if="selectedCandidate" class="input-hint" data-testid="key-test-endpoint">
              {{ t('keys.testModal.endpointHint', { protocol: protocolLabel(selectedCandidate.protocol), endpoint: selectedEndpointPath }) }}
            </p>
          </div>

          <div
            v-if="result"
            role="status"
            data-testid="key-test-result"
            class="rounded-lg border px-3 py-2 text-sm"
            :class="result.status === 'success'
              ? 'border-green-200 bg-green-50/[0.12] text-green-800 dark:border-green-900 dark:bg-green-950/20 dark:text-green-300'
              : 'border-red-200 bg-red-50/[0.12] text-red-700 dark:border-red-900 dark:bg-red-950/20 dark:text-red-300'"
          >
            <div class="flex items-center gap-2">
              <Icon :name="result.status === 'success' ? 'checkCircle' : 'xCircle'" size="sm" class="flex-shrink-0" />
              <p class="font-medium">{{ result.status === 'success' ? t('keys.testModal.passed') : t('keys.testModal.failed') }}</p>
            </div>
            <p v-if="result.status === 'success'" class="mt-1" data-testid="key-test-success-detail">
              {{ t('keys.testModal.successDetail', { model: result.model, ms: result.latencyMs }) }}
            </p>
            <p v-else class="mt-1" data-testid="key-test-failure-reason">{{ failureText }}</p>
            <p v-if="result.status === 'failure' && result.requestId" class="mt-1 break-all font-mono text-xs" data-testid="key-test-request-id">
              {{ t('keys.testModal.requestId', { id: result.requestId }) }}
            </p>
          </div>
        </template>
      </template>
    </div>

    <template #footer>
      <div class="flex w-full flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p v-if="showCostHint" class="text-xs surface-muted" data-testid="key-test-cost-hint">
          {{ t('keys.testModal.costHint') }}
        </p>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="emit('close')">
            {{ t('common.close') }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            data-testid="key-test-start"
            :disabled="!canStart"
            @click="startTest"
          >
            <Icon :name="testing ? 'refresh' : 'play'" size="sm" :class="testing ? 'animate-spin' : ''" />
            {{ testing ? t('keys.testModal.testing') : t('keys.testModal.start') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ApiKey } from '@/types'
import { adminSupportContext } from '@/utils/adminSupportContext'
import {
  KEY_TEST_TIMEOUT_MS,
  accessPathForKey,
  buildCandidates,
  buildTestRequest,
  classifyFailure,
  extractOutputText,
  fetchNativeAccess,
  hasErrorEnvelope,
  isSupportedTestEndpoint,
  type KeyTestFailureReason,
  type KeyTestProtocol,
  type KeyTestRequest,
  type TestCandidate,
  requestIdFromPayload,
} from '@/api/keyTest'

interface Props {
  show: boolean
  apiKey: ApiKey | null
  baseUrl?: string
}

type BlockedReason = 'readonly' | 'disabled' | 'expired' | 'quota' | 'ungrouped' | 'dedicatedClient' | 'conversionEndpoint'

type TestResult =
  | { status: 'success'; model: string; latencyMs: number; requestId: string }
  | { status: 'failure'; model: string; reason: KeyTestFailureReason; requestId: string }

const props = withDefaults(defineProps<Props>(), { baseUrl: '' })
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const isReadOnly = computed(() => adminSupportContext.value !== null)
const mode = computed(() => props.apiKey?.group_binding_mode || 'single')
const isAggregate = computed(() => mode.value === 'all' || mode.value === 'custom')
const isComposite = computed(() => props.apiKey?.group?.platform === 'composite')
const conversionRequested = computed(() => isComposite.value && !!props.apiKey?.conversion_enabled)
const conversionEndpoint = computed(() => {
  const endpoint = props.apiKey?.default_endpoint || ''
  return conversionRequested.value && isSupportedTestEndpoint(endpoint) ? endpoint : ''
})
const pathPrefix = computed(() => {
  if (isAggregate.value || isComposite.value) return ''
  return props.apiKey?.group?.platform === 'antigravity' ? '/antigravity' : ''
})

const statusText = computed(() => props.apiKey ? t(`keys.status.${props.apiKey.status}`) : '')
const scopeText = computed(() => {
  const key = props.apiKey
  if (!key) return ''
  if (isAggregate.value) {
    return mode.value === 'all'
      ? t('keys.testModal.scopeAll')
      : t('keys.testModal.scopeCustom', { count: key.aggregate_group_ids?.length || 0 })
  }
  if (key.group_id == null) return t('keys.testModal.scopeUngrouped')
  return t('keys.testModal.scopeSingle', { name: key.group?.name || `#${key.group_id}` })
})

const blockedReason = computed<BlockedReason | ''>(() => {
  const key = props.apiKey
  if (!key) return ''
  if (isReadOnly.value) return 'readonly'
  if (key.status === 'inactive' || key.status === 'disabled') return 'disabled'
  if (key.status === 'expired') return 'expired'
  if (key.status === 'quota_exhausted') return 'quota'
  if (key.expires_at && Number.isFinite(Date.parse(key.expires_at)) && Date.parse(key.expires_at) <= Date.now()) return 'expired'
  if (key.quota > 0 && key.quota_used >= key.quota) return 'quota'
  if (!isAggregate.value && key.group_id == null) return 'ungrouped'
  if (!isAggregate.value && key.group?.claude_code_only) return 'dedicatedClient'
  if (conversionRequested.value && !conversionEndpoint.value) return 'conversionEndpoint'
  return ''
})

const blockedMessages: Record<BlockedReason, () => string> = {
  readonly: () => t('keys.testModal.blocked.readonly'),
  disabled: () => t('keys.testModal.blocked.disabled'),
  expired: () => t('keys.testModal.blocked.expired'),
  quota: () => t('keys.testModal.blocked.quota'),
  ungrouped: () => t('keys.testModal.blocked.ungrouped'),
  dedicatedClient: () => t('keys.testModal.blocked.dedicatedClient'),
  conversionEndpoint: () => t('keys.testModal.blocked.conversionEndpoint'),
}
const blockedText = computed(() => blockedReason.value ? blockedMessages[blockedReason.value]() : '')

const loading = ref(false)
const loadError = ref(false)
const candidates = ref<TestCandidate[]>([])
const selectedId = ref('')
const testing = ref(false)
const result = ref<TestResult | null>(null)

let loadSequence = 0
let testSequence = 0
let loadController: AbortController | null = null
let testController: AbortController | null = null

const selectedCandidate = computed(() => candidates.value.find(candidate => candidate.id === selectedId.value) || null)
const selectedEndpointPath = computed(() => selectedCandidate.value
  ? `${selectedCandidate.value.pathPrefix}${selectedCandidate.value.endpoint}`
  : '')
const noCapabilityText = computed(() => {
  const key = props.apiKey
  if (isAggregate.value && mode.value === 'custom' && !(key?.aggregate_group_ids?.length)) {
    return t('keys.testModal.emptyScope')
  }
  return t('keys.testModal.noCapability')
})
const modelOptions = computed(() => candidates.value.map(candidate => ({
  value: candidate.id,
  label: candidate.model,
  description: protocolLabel(candidate.protocol),
})))
const showCostHint = computed(() => !blockedReason.value && !loading.value && !loadError.value && candidates.value.length > 0)
const canStart = computed(() => !!(
  props.show &&
  props.apiKey &&
  selectedCandidate.value &&
  !blockedReason.value &&
  !loading.value &&
  !loadError.value &&
  !testing.value
))

const protocolLabels: Record<KeyTestProtocol, () => string> = {
  responses: () => t('keys.testModal.protocol.responses'),
  chat: () => t('keys.testModal.protocol.chat'),
  messages: () => t('keys.testModal.protocol.messages'),
  gemini: () => t('keys.testModal.protocol.gemini'),
}
function protocolLabel(protocol: KeyTestProtocol): string {
  return protocolLabels[protocol]()
}

const failureMessages: Record<KeyTestFailureReason, () => string> = {
  auth: () => t('keys.testModal.failure.auth'),
  access: () => t('keys.testModal.failure.access'),
  quota: () => t('keys.testModal.failure.quota'),
  rate: () => t('keys.testModal.failure.rate'),
  model: () => t('keys.testModal.failure.model'),
  upstream: () => t('keys.testModal.failure.upstream'),
  invalidRequest: () => t('keys.testModal.failure.invalidRequest'),
  policy: () => t('keys.testModal.failure.policy'),
  clientRestriction: () => t('keys.testModal.failure.clientRestriction'),
  network: () => t('keys.testModal.failure.network'),
  timeout: () => t('keys.testModal.failure.timeout'),
  noText: () => t('keys.testModal.failure.noText'),
  unknown: () => t('keys.testModal.failure.unknown'),
}
const failureText = computed(() => result.value?.status === 'failure' ? failureMessages[result.value.reason]() : '')

function clearLoad() {
  loadSequence += 1
  loadController?.abort()
  loadController = null
  loading.value = false
}

function clearTest() {
  testSequence += 1
  testController?.abort()
  testController = null
  testing.value = false
}

function reset() {
  clearLoad()
  clearTest()
  candidates.value = []
  selectedId.value = ''
  loadError.value = false
  result.value = null
}

async function loadCandidates() {
  const key = props.apiKey
  const path = key ? accessPathForKey(key) : null
  candidates.value = []
  selectedId.value = ''
  result.value = null
  loadError.value = false
  if (!key || !path || blockedReason.value) return

  const sequence = ++loadSequence
  loadController?.abort()
  const controller = new AbortController()
  loadController = controller
  loading.value = true
  try {
    const payload = await fetchNativeAccess(path, controller.signal)
    if (sequence !== loadSequence || controller.signal.aborted) return
    const next = buildCandidates(payload.models, { conversionEndpoint: conversionEndpoint.value, pathPrefix: pathPrefix.value })
    candidates.value = next
    selectedId.value = next[0]?.id || ''
  } catch {
    if (sequence !== loadSequence || controller.signal.aborted) return
    loadError.value = true
  } finally {
    if (sequence === loadSequence) {
      loading.value = false
      if (loadController === controller) loadController = null
    }
  }
}

function readRequestId(response: Response, payload: unknown): string {
  const header = (response.headers.get('X-Request-ID') || response.headers.get('X-Request-Id') || '').trim()
  return (header || requestIdFromPayload(payload)).slice(0, 128)
}

function parseJson(text: string): unknown {
  if (!text.trim()) return null
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

function now(): number {
  return typeof performance !== 'undefined' ? performance.now() : Date.now()
}

function fail(model: string, reason: KeyTestFailureReason, requestId = '') {
  result.value = { status: 'failure', model, reason, requestId }
}

async function startTest() {
  if (!canStart.value || !props.apiKey || !selectedCandidate.value) return
  const key = props.apiKey
  const candidate = selectedCandidate.value
  const sequence = ++testSequence
  testController?.abort()
  const controller = new AbortController()
  testController = controller
  testing.value = true
  result.value = null
  let timedOut = false
  let headerRequestId = ''
  const timer = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, KEY_TEST_TIMEOUT_MS)
  const startedAt = now()
  let request: KeyTestRequest
  try {
    request = buildTestRequest(props.baseUrl, key.key, candidate)
  } catch {
    clearTimeout(timer)
    testing.value = false
    fail(candidate.model, 'unknown')
    return
  }

  try {
    const response = await fetch(request.url, {
      method: 'POST',
      headers: request.headers,
      body: request.body,
      signal: controller.signal,
      credentials: 'omit',
      cache: 'no-store',
    })
    headerRequestId = readRequestId(response, null)
    const text = await response.text()
    if (sequence !== testSequence) return
    if (controller.signal.aborted) {
      if (timedOut) fail(candidate.model, 'timeout', headerRequestId)
      return
    }
    const payload = parseJson(text)
    const latencyMs = Math.round(now() - startedAt)
    const requestId = readRequestId(response, payload)

    if (!response.ok) {
      fail(candidate.model, classifyFailure(response.status, payload), requestId)
      return
    }
    if (hasErrorEnvelope(payload)) {
      const reason = classifyFailure(response.status, payload)
      fail(candidate.model, reason === 'unknown' ? 'noText' : reason, requestId)
      return
    }
    const output = extractOutputText(candidate.protocol, payload)
    if (!output.trim()) {
      fail(candidate.model, 'noText', requestId)
      return
    }
    result.value = { status: 'success', model: candidate.model, latencyMs, requestId }
  } catch {
    if (sequence !== testSequence) return
    if (controller.signal.aborted && !timedOut) return
    fail(candidate.model, timedOut ? 'timeout' : 'network', headerRequestId)
  } finally {
    clearTimeout(timer)
    if (sequence === testSequence) {
      testing.value = false
      if (testController === controller) testController = null
    }
  }
}

watch(
  () => [props.show, props.apiKey?.id ?? 0, blockedReason.value],
  () => {
    reset()
    if (props.show && props.apiKey && !blockedReason.value) void loadCandidates()
  },
  { immediate: true },
)

watch(selectedId, () => {
  if (!testing.value) result.value = null
})

onBeforeUnmount(reset)
</script>
