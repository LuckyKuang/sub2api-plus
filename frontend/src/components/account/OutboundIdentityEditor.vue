<template>
  <section v-if="visible" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
    <h3 class="text-sm font-semibold">{{ t('admin.settings.outboundIdentity.title') }}</h3>
    <select :value="modelValue?.preset || ''" :aria-label="t('admin.settings.outboundIdentity.title')" class="input" @change="selectPreset(($event.target as HTMLSelectElement).value)">
      <option value="">{{ t('admin.settings.outboundIdentity.inherit') }}</option>
      <option v-for="preset in availablePresets" :key="preset" :value="preset">{{ identityNames[preset] }}</option>
    </select>
    <template v-if="isVersionlessSelection">
      <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.versionlessHint') }}</p>
    </template>
    <p v-else-if="modelValue?.preset === 'minimax_apikey'" class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.pinnedSdkHint') }}</p>
    <template v-else-if="modelValue?.preset && !(platform === 'openai' && modelValue.preset === 'codex')">
      <input :value="modelValue.version" class="input font-mono" :aria-label="t('admin.settings.outboundIdentity.version')" :placeholder="t('admin.settings.outboundIdentity.version')" @input="update('version', ($event.target as HTMLInputElement).value)" />
      <details>
        <summary class="cursor-pointer text-sm">{{ t('admin.settings.outboundIdentity.advanced') }}</summary>
        <input :value="modelValue.user_agent" class="input mt-2 font-mono text-sm" placeholder="User-Agent" @input="update('user_agent', ($event.target as HTMLInputElement).value)" />
      </details>
    </template>
    <div v-if="runtimeHeaders.length" class="space-y-2" data-testid="outbound-identity-account-runtime-headers">
      <div>
        <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.runtimeHeaders') }}</p>
        <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.runtimeHeadersAccountHint') }}</p>
      </div>
      <label v-for="header in runtimeHeaders" :key="header.name" class="block text-xs">
        <span class="font-mono text-gray-500">{{ header.name }}</span>
        <input
          :value="modelValue?.headers?.[header.name]"
          class="input mt-1 font-mono text-sm"
          :placeholder="header.builtin"
          :aria-label="header.name"
          @input="setHeader(header.name, ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>
    <p v-if="error" role="alert" class="text-xs text-red-600">{{ error }}</p>
    <div v-else-if="preview" class="space-y-1 text-xs text-gray-500">
      <p>{{ t('admin.settings.outboundIdentity.effectiveAccount') }} · {{ t(`admin.settings.outboundIdentity.sources.${preview.source}`) }}</p>
      <dl class="grid gap-1 sm:grid-cols-[auto_1fr]" data-testid="outbound-identity-account-headers">
        <template v-for="(value, name) in preview.headers" :key="name">
          <dt class="font-mono text-gray-500">{{ name }}</dt>
          <dd class="break-all font-mono">{{ value }}</dd>
        </template>
      </dl>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getOutboundIdentity, identityNames, identityPresets, versionlessIdentityPresets, previewOutboundIdentity, type IdentityDeclaration, type IdentityPreset, type IdentitySelection, type PresetDeclarations, type ResolvedIdentity } from '@/api/admin/outboundIdentity'
const props = defineProps<{ platform: string; accountType: string; modelValue?: IdentitySelection | null; codexUserAgent?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: IdentitySelection | null] }>()
const { t } = useI18n()
const preview = ref<ResolvedIdentity>()
const error = ref('')
const declarations = ref<PresetDeclarations[]>([])
const nativePresets: Record<string, IdentityPreset> = { anthropic: 'claude', gemini: 'gemini', grok: 'grok', antigravity: 'antigravity', deepseek: 'deepseek', minimax: 'minimax', kimi: 'kimi', zhipu: 'zcode' }
const isVersionlessSelection = computed(() => !!props.modelValue?.preset && versionlessIdentityPresets.includes(props.modelValue.preset as IdentityPreset))
const nativePreset = computed<IdentityPreset>(() => props.platform === 'minimax' && props.accountType === 'apikey' ? 'minimax_apikey' : nativePresets[props.platform] || 'codex')
const effectivePreset = computed<IdentityPreset>(() => (props.modelValue?.preset as IdentityPreset) || nativePreset.value)
const visible = computed(() => props.platform && props.platform !== 'composite' && (props.platform !== 'openai' || ['apikey', 'upstream'].includes(props.accountType)))
const availablePresets = computed(() => ['oauth', 'setup-token'].includes(props.accountType) ? [nativePreset.value] : identityPresets)
// The backend declares which headers each preset renders and which of them
// accept an account value, so this editor never hard-codes a preset's block.
const runtimeHeaders = computed<IdentityDeclaration[]>(() => {
  if (!visible.value || (props.platform === 'openai' && effectivePreset.value === 'codex')) return []
  return declarations.value.find(item => item.preset === effectivePreset.value)?.headers.filter(header => header.editable) ?? []
})
function selectPreset(preset: string) { emit('update:modelValue', preset ? { preset: preset as IdentityPreset } : null) }
function update(field: 'user_agent' | 'version', value: string) { if (props.modelValue) emit('update:modelValue', { ...props.modelValue, [field]: value }) }
// Setting a runtime declaration makes the preset explicit, because the value is
// only meaningful for that preset's declaration block.
function setHeader(name: string, value: string) {
  const current = props.modelValue ?? { preset: effectivePreset.value }
  const headers = { ...(current.headers ?? {}) }
  const trimmed = value.trim()
  if (trimmed) headers[name] = trimmed
  else delete headers[name]
  emit('update:modelValue', { ...current, preset: current.preset || effectivePreset.value, headers: Object.keys(headers).length ? headers : undefined })
}
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
// The declaration registry is a preset -> headers map owned by the backend, so
// the editor loads it once per mount and reuses it for every platform change.
let declarationsRequested = false
watch(visible, async (isVisible) => {
  if (!isVisible || declarationsRequested) return
  declarationsRequested = true
  try { declarations.value = (await getOutboundIdentity()).declarations ?? [] } catch { declarationsRequested = false }
}, { immediate: true })
watch(() => [props.platform, props.accountType, props.modelValue, props.codexUserAgent] as const, () => {
  const current = ++generation
  clearTimeout(timer)
  preview.value = undefined
  error.value = ''
  if (!visible.value) return
  timer = setTimeout(async () => {
    try {
      const result = props.platform === 'openai' && props.codexUserAgent
        ? await previewOutboundIdentity(props.platform, props.accountType, props.modelValue || undefined, props.codexUserAgent)
        : await previewOutboundIdentity(props.platform, props.accountType, props.modelValue || undefined)
      if (current === generation) { preview.value = result; error.value = '' }
    } catch { if (current === generation) error.value = t('admin.settings.outboundIdentity.previewFailed') }
  }, 250)
}, { immediate: true, deep: true })
onBeforeUnmount(() => { ++generation; clearTimeout(timer) })
watch(() => [props.platform, props.accountType], () => { if (props.modelValue && !availablePresets.value.includes(props.modelValue.preset as IdentityPreset)) selectPreset('') })
</script>
