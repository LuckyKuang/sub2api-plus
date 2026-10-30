<template>
  <section class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="codex-cli-runtime-settings">
    <div>
      <h3 class="text-sm font-semibold">{{ t('codexCliRuntime.title') }}</h3>
      <p class="mt-1 text-xs surface-muted">{{ t('codexCliRuntime.description') }}</p>
    </div>
    <p class="text-xs surface-muted">
      <code>POST /v1/codex-cli/responses</code>
      {{ t('codexCliRuntime.taskHint') }}
    </p>
    <p class="text-xs text-amber-700 dark:text-amber-300">{{ t('codexCliRuntime.permissionsHint') }}</p>

    <p v-if="checking" role="status" class="text-sm surface-muted">{{ t('codexCliRuntime.checking') }}</p>
    <dl v-if="status" class="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[auto_minmax(0,1fr)]">
      <dt class="surface-muted">{{ t('codexCliRuntime.targetVersion') }}</dt>
      <dd class="font-mono" data-testid="codex-cli-target-version">{{ status.target_version }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.installedVersion') }}</dt>
      <dd class="font-mono" data-testid="codex-cli-installed-version">{{ status.installed_version || t(status.installed ? 'codexCliRuntime.versionUnavailable' : 'codexCliRuntime.notInstalled') }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.source') }}</dt>
      <dd data-testid="codex-cli-runtime-source">{{ sourceLabel }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.verified') }}</dt>
      <dd>{{ status.source_verified ? t('codexCliRuntime.yes') : t('codexCliRuntime.no') }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.path') }}</dt>
      <dd class="break-all font-mono text-xs" data-testid="codex-cli-runtime-path">{{ status.path || '—' }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.platform') }}</dt>
      <dd class="font-mono">{{ status.platform }}</dd>
      <dt class="surface-muted">{{ t('codexCliRuntime.status') }}</dt>
      <dd :class="ready ? 'text-emerald-700 dark:text-emerald-300' : 'text-amber-700 dark:text-amber-300'" data-testid="codex-cli-runtime-readiness">
        {{ t(ready ? 'codexCliRuntime.ready' : 'codexCliRuntime.notReady') }}
      </dd>
    </dl>
    <p v-if="status && !ready && !checking && !errorKey" class="text-xs surface-muted" data-testid="codex-cli-runtime-reason">
      {{ reasonLabel }}
    </p>
    <p v-if="errorKey" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
    <p v-if="installedSuccessfully" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('codexCliRuntime.installedSuccessfully') }}</p>

    <div class="flex flex-wrap gap-2">
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="busy"
        data-testid="codex-cli-runtime-check"
        @click="refresh"
      >
        {{ t('codexCliRuntime.check') }}
      </button>
      <button
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="busy || !status?.can_install || ready || errorKey === 'codexCliRuntime.checkFailed'"
        data-testid="codex-cli-runtime-install"
        @click="install"
      >
        {{ installing ? t('codexCliRuntime.installing') : t('codexCliRuntime.installTarget', { version: status?.target_version || '—' }) }}
      </button>
    </div>
    <p class="text-xs surface-muted">{{ t('codexCliRuntime.versionHint') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexCLIRuntime, installCodexCLI, type CodexCLIRuntimeStatus } from '@/api/admin/codexCliRuntime'

const { t } = useI18n()
const status = ref<CodexCLIRuntimeStatus | null>(null)
const checking = ref(false)
const installing = ref(false)
const errorKey = ref('')
const installedSuccessfully = ref(false)
let generation = 0
const busy = computed(() => checking.value || installing.value)
const ready = computed(() => !errorKey.value && status.value?.execution_available
  && status.value.installed && status.value.version_match && status.value.source_verified)
const sourceLabel = computed(() => {
  if (status.value?.source === 'managed') return t('codexCliRuntime.managed')
  if (status.value?.source === 'system') return t('codexCliRuntime.system')
  return t('codexCliRuntime.noSource')
})
const reasonLabel = computed(() => {
  const code = status.value?.unavailable_code
  if (code === 'not_installed') return t('codexCliRuntime.notInstalledHint')
  if (code === 'version_mismatch') return t('codexCliRuntime.versionMismatch')
  if (code === 'unsupported_platform') return t('codexCliRuntime.unsupportedPlatform')
  if (code === 'source_unverified') return t('codexCliRuntime.sourceUnverified')
  if (code === 'integrity_check_failed') return t('codexCliRuntime.integrityFailed')
  if (code === 'host_configuration_conflict') return t('codexCliRuntime.hostConfigurationConflict')
  if (code === 'host_configuration_check_failed') return t('codexCliRuntime.hostConfigurationCheckFailed')
  return t('codexCliRuntime.detectionFailed')
})

async function refresh() {
  if (busy.value) return
  const current = ++generation
  checking.value = true
  errorKey.value = ''
  installedSuccessfully.value = false
  try {
    const next = await getCodexCLIRuntime()
    if (current === generation) status.value = next
  } catch {
    if (current === generation) errorKey.value = 'codexCliRuntime.checkFailed'
  } finally {
    if (current === generation) checking.value = false
  }
}

async function install() {
  if (busy.value || !status.value?.can_install || ready.value || errorKey.value === 'codexCliRuntime.checkFailed') return
  const current = ++generation
  installing.value = true
  errorKey.value = ''
  installedSuccessfully.value = false
  try {
    const next = await installCodexCLI()
    if (current !== generation) return
    status.value = next
    if (next.execution_available && next.installed && next.version_match && next.source_verified) {
      installedSuccessfully.value = true
    } else {
      errorKey.value = 'codexCliRuntime.installFailed'
    }
  } catch {
    if (current === generation) errorKey.value = 'codexCliRuntime.installFailed'
  } finally {
    if (current === generation) installing.value = false
  }
}

onMounted(refresh)
onBeforeUnmount(() => { generation += 1 })
</script>
