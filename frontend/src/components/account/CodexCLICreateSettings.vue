<template>
  <section v-if="active && runtime?.bridge_enabled" class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="codex-cli-create-settings">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="input-label mb-0">{{ t('codexCli.account.title') }}</h3>
        <p class="input-hint">{{ t('codexCli.account.taskModeHint') }}</p>
      </div>
      <Toggle
        :model-value="enabled"
        :disabled="disabled || checking || (!enabled && !runtime?.execution_available)"
        :aria-label="t('codexCli.account.title')"
        data-testid="codex-cli-create-mode"
        @update:model-value="$emit('update:enabled', $event)"
      />
    </div>
    <p v-if="checking" role="status" class="input-hint">{{ t('common.loading') }}</p>
    <p v-else-if="!runtime?.execution_available" role="status" class="input-hint">{{ t('codexCli.account.runtimeUnavailable') }}</p>
    <div>
      <label for="create-codex-cli-config" class="input-label">config.toml</label>
      <textarea
        id="create-codex-cli-config"
        :value="configToml"
        :disabled="disabled"
        class="input font-mono text-xs"
        rows="7"
        spellcheck="false"
        data-testid="codex-cli-create-config"
        @input="$emit('update:configToml', ($event.target as HTMLTextAreaElement).value)"
      />
      <p class="input-hint">{{ t('codexCli.account.configHint') }}</p>
      <p class="input-hint">{{ t('codexCli.account.createHint') }}</p>
    </div>
    <p v-if="errorKey" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexCLIRuntime, type CodexCLIRuntimeStatus } from '@/api/admin/codexCliRuntime'
import Toggle from '@/components/common/Toggle.vue'

const props = defineProps<{ enabled: boolean; configToml: string; disabled?: boolean; active: boolean }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:configToml': [value: string]; 'initialize:configToml': [value: string] }>()
const { t } = useI18n()
const runtime = ref<CodexCLIRuntimeStatus | null>(null)
const checking = ref(false)
const errorKey = ref('')
let generation = 0
watch(() => props.active, async active => {
  const current = ++generation
  runtime.value = null
  errorKey.value = ''
  checking.value = active
  if (!active) return
  try {
    const next = await getCodexCLIRuntime()
    if (current === generation) {
      runtime.value = next
      if (next.bridge_enabled && next.default_config_toml && !props.configToml.trim()) {
        emit('initialize:configToml', next.default_config_toml)
      }
    }
  } catch {
    // Keep credentials and configuration local; the server validates readiness again on creation.
  } finally {
    if (current === generation) checking.value = false
  }
}, { immediate: true })
onBeforeUnmount(() => { generation += 1 })

function prepareCreate() {
  errorKey.value = ''
  if (!props.active || !runtime.value?.bridge_enabled) return null
  if (props.enabled && !runtime.value?.execution_available) {
    errorKey.value = 'codexCli.account.runtimeUnavailable'
    throw new Error(t(errorKey.value))
  }
  return { enabled: props.enabled, config_toml: props.configToml }
}
defineExpose({ prepareCreate })
</script>
