<template>
  <section v-if="eligible" class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="codex-cli-editor">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="input-label mb-0">{{ t('codexCli.account.title') }}</h3>
        <p class="input-hint">{{ t('codexCli.account.taskModeHint') }}</p>
        <code class="text-xs surface-muted">/v1/codex-cli/responses</code>
      </div>
      <Toggle
        v-model="enabled"
        :disabled="disabled || saving || loading || !view || (!enabled && !runtime?.execution_available)"
        :aria-label="t('codexCli.account.title')"
        data-testid="codex-cli-mode"
      />
    </div>

    <p v-if="loading" role="status" class="text-sm surface-muted">{{ t('common.loading') }}</p>
    <div v-else-if="loadFailed" class="space-y-2">
      <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('codexCli.account.loadFailed') }}</p>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="disabled" @click="load">
        {{ t('codexCli.account.retry') }}
      </button>
    </div>
    <template v-else-if="view">
      <p v-if="!runtime?.execution_available" role="status" class="text-xs text-amber-700 dark:text-amber-300">
        {{ t('codexCli.account.runtimeUnavailable') }}
      </p>
      <div>
        <label :for="`codex-cli-config-${account.id}`" class="input-label">config.toml</label>
        <textarea
          :id="`codex-cli-config-${account.id}`"
          v-model="configTOML"
          :disabled="disabled || saving"
          class="input font-mono text-xs"
          rows="7"
          spellcheck="false"
          data-testid="codex-cli-config"
        />
        <p class="input-hint">{{ t('codexCli.account.configHint') }}</p>
      </div>

      <div class="space-y-3">
        <h4 class="input-label mb-0">{{ t('codexCli.account.authTitle') }}</h4>
        <p class="input-hint">{{ t('codexCli.account.authHint') }}</p>
        <div>
          <label :for="`codex-cli-account-${account.id}`" class="input-label">{{ t('codexCli.account.workspace') }}</label>
          <input
            :id="`codex-cli-account-${account.id}`"
            v-model="workspaceID"
            :disabled="disabled || saving"
            class="input"
            type="text"
            autocomplete="off"
            data-testid="codex-cli-workspace"
          />
        </div>
        <div v-for="secret in secretNames" :key="secret" class="space-y-2">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label :for="`codex-cli-${secret}-action-${account.id}`" class="input-label mb-0">{{ secret }}</label>
            <span class="text-xs surface-muted" :data-testid="`codex-cli-${secret}-status`">
              {{ view.auth_status[`has_${secret}`] ? t('codexCli.account.configured') : t('codexCli.account.notConfigured') }}
            </span>
          </div>
          <Select
            :id="`codex-cli-${secret}-action-${account.id}`"
            v-model="secretActions[secret]"
            :options="secretActionOptions"
            :disabled="disabled || saving"
            :searchable="false"
            :aria-label="t('codexCli.account.secretAction', { name: secret })"
            :data-testid="`codex-cli-${secret}-action`"
            @update:model-value="secretValues[secret] = ''"
          />
          <input
            v-if="secretActions[secret] === 'replace'"
            :id="`codex-cli-${secret}-${account.id}`"
            v-model="secretValues[secret]"
            :disabled="disabled || saving"
            :aria-label="secret"
            type="password"
            class="input font-mono text-xs"
            autocomplete="new-password"
            spellcheck="false"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            :data-testid="`codex-cli-${secret}-value`"
          />
        </div>
      </div>
      <p class="input-hint">{{ t('codexCli.account.saveHint') }}</p>
      <p v-if="errorKey" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { getCodexCLIRuntime, type CodexCLIRuntimeStatus } from '@/api/admin/codexCliRuntime'
import { accountOAuthOwnerSnapshot } from '@/utils/accountOAuthOwnerSnapshot'
import {
  getCodexCLIAccount,
  updateCodexCLIAccount,
  type CodexCLIAccountPatch,
  type CodexCLIAccountView,
  type CodexCLISecretName,
} from '@/api/admin/codexCli'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'

const props = withDefaults(defineProps<{ account: Account; active?: boolean; disabled?: boolean }>(), {
  active: true,
  disabled: false,
})
const { t } = useI18n()
const secretNames: CodexCLISecretName[] = ['access_token', 'refresh_token', 'id_token']
type SecretAction = 'keep' | 'replace' | 'clear'
const secretActions = reactive<Record<CodexCLISecretName, SecretAction>>({ access_token: 'keep', refresh_token: 'keep', id_token: 'keep' })
const secretValues = reactive<Record<CodexCLISecretName, string>>({ access_token: '', refresh_token: '', id_token: '' })
const configTOML = ref('')
const enabled = ref(false)
const workspaceID = ref('')
const view = ref<CodexCLIAccountView | null>(null)
const runtime = ref<CodexCLIRuntimeStatus | null>(null)
const loading = ref(false)
const saving = ref(false)
const loadFailed = ref(false)
const errorKey = ref('')
let generation = 0
let controller: AbortController | null = null

const eligible = computed(() => {
  const authModes = [props.account.credentials?.auth_mode, props.account.credentials?.openai_auth_mode]
    .map(value => String(value || '').trim().replace(/_/g, '').toLowerCase())
  return props.account.platform === 'openai' && props.account.type === 'oauth'
    && props.account.parent_account_id == null
    && !authModes.some(mode => ['agentidentity', 'personalaccesstoken', 'pat'].includes(mode))
    && !props.account.credentials?.agent_private_key
    && !props.account.credentials?.personal_access_token
})
const secretActionOptions = computed(() => [
  { value: 'keep', label: t('codexCli.account.keep') },
  { value: 'replace', label: t('codexCli.account.replace') },
  { value: 'clear', label: t('codexCli.account.clear') },
])

function resetSecrets() {
  for (const name of secretNames) {
    secretActions[name] = 'keep'
    secretValues[name] = ''
  }
}

function applyView(next: CodexCLIAccountView) {
  view.value = next
  enabled.value = next.enabled
  configTOML.value = next.config_toml
  workspaceID.value = next.auth.tokens.account_id || ''
  errorKey.value = ''
  resetSecrets()
}

async function load() {
  const current = ++generation
  controller?.abort()
  controller = null
  view.value = null
  runtime.value = null
  enabled.value = false
  configTOML.value = ''
  workspaceID.value = ''
  resetSecrets()
  errorKey.value = ''
  loadFailed.value = false
  saving.value = false
  loading.value = false
  if (!props.active || !eligible.value) return
  const accountID = props.account.id
  controller = new AbortController()
  loading.value = true
  try {
    const [accountResult, runtimeResult] = await Promise.allSettled([
      getCodexCLIAccount(accountID, { signal: controller.signal }),
      getCodexCLIRuntime(),
    ])
    if (current !== generation || !props.active || accountID !== props.account.id) return
    if (accountResult.status === 'rejected') throw accountResult.reason
    runtime.value = runtimeResult.status === 'fulfilled' ? runtimeResult.value : null
    applyView(accountResult.value)
  } catch {
    if (current === generation && props.active) loadFailed.value = true
  } finally {
    if (current === generation) loading.value = false
  }
}

interface PreparedSave {
  accountID: number
  generation: number
  payload: CodexCLIAccountPatch
}

function prepareSave(): PreparedSave | null {
  errorKey.value = ''
  if (!eligible.value || !props.active || !view.value) return null
  if (enabled.value && !view.value.enabled && !runtime.value?.execution_available) {
    errorKey.value = 'codexCli.account.runtimeUnavailable'
    throw new Error(t(errorKey.value))
  }
  const tokens: NonNullable<NonNullable<CodexCLIAccountPatch['auth']>['tokens']> = {}
  if (workspaceID.value !== view.value.auth.tokens.account_id) tokens.account_id = workspaceID.value.trim() || null
  for (const name of secretNames) {
    if (secretActions[name] === 'replace') {
      if (!secretValues[name].trim()) {
        errorKey.value = 'codexCli.account.replacementRequired'
        throw new Error(t(errorKey.value))
      }
      tokens[name] = secretValues[name].trim()
    } else if (secretActions[name] === 'clear') {
      tokens[name] = null
    }
  }
  if (enabled.value === view.value.enabled && configTOML.value === view.value.config_toml && Object.keys(tokens).length === 0) return null
  if (!/^[0-9a-f]{64}$/.test(view.value.expected_owner)) {
    errorKey.value = 'codexCli.account.ownerChanged'
    throw new Error(t(errorKey.value))
  }
  return {
    accountID: props.account.id,
    generation,
    payload: {
      expected_owner: view.value.expected_owner,
      ...(enabled.value !== view.value.enabled ? { enabled: enabled.value } : {}),
      config_toml: configTOML.value,
      ...(Object.keys(tokens).length ? { auth: { auth_mode: 'chatgpt', tokens } } : {}),
    },
  }
}

async function save(prepared: PreparedSave) {
  if (prepared.generation !== generation || prepared.accountID !== props.account.id || !props.active) {
    throw new Error(t('codexCli.account.ownerChanged'))
  }
  saving.value = true
  errorKey.value = ''
  try {
    const next = await updateCodexCLIAccount(prepared.accountID, prepared.payload)
    if (prepared.generation === generation && prepared.accountID === props.account.id && props.active) applyView(next)
    return next
  } catch {
    if (prepared.generation === generation && props.active) errorKey.value = 'codexCli.account.saveFailed'
    throw new Error(t('codexCli.account.saveFailed'))
  } finally {
    if (prepared.generation === generation) saving.value = false
  }
}

watch(
  [() => props.active, () => accountOAuthOwnerSnapshot(props.account), () => eligible.value],
  load,
  { immediate: true, flush: 'sync' },
)
onBeforeUnmount(() => {
  generation += 1
  controller?.abort()
  resetSecrets()
})
defineExpose({ prepareSave, save })
</script>
