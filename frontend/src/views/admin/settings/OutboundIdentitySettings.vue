<template>
  <div class="space-y-6" data-testid="outbound-identity-settings">
    <div>
      <h2 class="text-lg font-semibold">{{ t('admin.settings.outboundIdentity.title') }}</h2>
      <p class="mt-2 text-sm text-gray-500">{{ t('admin.settings.outboundIdentity.description') }}</p>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <template v-if="view">
      <section v-for="preset in identityPresets" :key="preset" class="card space-y-4 p-6">
        <h3 class="font-semibold">{{ identityNames[preset] }}</h3>
        <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="mb-2 text-gray-500">{{ t('admin.settings.outboundIdentity.effectiveGlobal') }}</p>
          <dl class="grid gap-2 sm:grid-cols-[auto_1fr]">
            <dt>User-Agent</dt><dd class="break-all font-mono">{{ effective(preset)?.user_agent }}</dd>
            <dt>{{ t('admin.settings.outboundIdentity.client') }}</dt><dd class="font-mono">{{ effective(preset)?.originator }}</dd>
            <dt>{{ t('admin.settings.outboundIdentity.version') }}</dt><dd class="font-mono">{{ effective(preset)?.version }}</dd>
            <dt>{{ t('admin.settings.outboundIdentity.source') }}</dt><dd>{{ sourceLabel(effective(preset)?.source) }}</dd>
          </dl>
          <details class="mt-3">
            <summary class="cursor-pointer">{{ t('admin.settings.outboundIdentity.headers') }}</summary>
            <pre class="mt-2 overflow-auto text-xs">{{ JSON.stringify(effective(preset)?.headers, null, 2) }}</pre>
          </details>
        </div>
        <slot v-if="preset === 'codex'" name="codex" />
        <template v-else>
          <label class="block text-sm">
            {{ t('admin.settings.outboundIdentity.version') }}
            <input v-model="profiles[preset].version" class="input mt-2 max-w-xs font-mono" :placeholder="builtin(preset)?.version" />
          </label>
          <details>
            <summary class="cursor-pointer text-sm">{{ t('admin.settings.outboundIdentity.advanced') }}</summary>
            <label class="mt-3 block text-sm">
              User-Agent
              <input v-model="profiles[preset].user_agent" class="input mt-2 font-mono text-sm" :placeholder="builtin(preset)?.user_agent" />
            </label>
          </details>
          <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.inheritHint') }}</p>
        </template>
      </section>
      <section class="card space-y-4 p-6">
        <h3 class="font-semibold">{{ t('admin.settings.outboundIdentity.defaults') }}</h3>
        <p class="text-sm text-gray-500">{{ t('admin.settings.outboundIdentity.defaultsHint') }}</p>
        <label v-for="mapping in mappings" :key="mapping.key" class="flex flex-wrap items-center justify-between gap-3 text-sm">
          <span>{{ mapping.label }}</span>
          <select v-model="defaults[mapping.key]" class="input w-48">
            <option value="">{{ t('admin.settings.outboundIdentity.builtin') }}</option>
            <option v-for="preset in identityPresets" :key="preset" :value="preset">{{ identityNames[preset] }}</option>
          </select>
        </label>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getOutboundIdentity, updateOutboundIdentity, identityNames, identityPresets, type IdentityPreset, type IdentitySelection, type OutboundIdentityView } from '@/api/admin/outboundIdentity'

const { t } = useI18n()
const view = ref<OutboundIdentityView>()
const error = ref('')
const loading = ref(false)
const profiles = reactive(Object.fromEntries(identityPresets.map(preset => [preset, { preset, user_agent: '', version: '' }])) as Record<IdentityPreset, IdentitySelection>)
const defaults = reactive<Record<string, IdentityPreset | ''>>({})
const savedForm = ref('')
const mappings = [
  { key: 'openai:apikey', label: 'OpenAI-compatible · API Key' },
  { key: 'openai:upstream', label: 'OpenAI-compatible · Upstream' },
  { key: 'anthropic:apikey', label: 'Anthropic · API Key' },
  { key: 'anthropic:bedrock', label: 'Bedrock' },
  { key: 'anthropic:service_account', label: 'Vertex · Claude' },
  { key: 'gemini:service_account', label: 'Vertex · Gemini' },
  { key: 'gemini:apikey', label: 'Gemini · API Key' },
  { key: 'grok:apikey', label: 'Grok · API Key' },
  { key: 'anthropic:upstream', label: 'Anthropic · Upstream' },
  { key: 'gemini:upstream', label: 'Gemini · Upstream' },
  { key: 'grok:upstream', label: 'Grok · Upstream' },
  { key: 'antigravity:upstream', label: 'Antigravity · Upstream' },
  ...['kimi', 'zhipu', 'deepseek', 'minimax'].map(platform => ({ key: `${platform}:apikey`, label: `${platform} · API Key` }))
]
const effective = (preset: IdentityPreset) => view.value?.effective.find(item => item.preset === preset)
const builtin = (preset: IdentityPreset) => view.value?.presets.find(item => item.preset === preset)
const sourceLabel = (source?: string) => t(`admin.settings.outboundIdentity.sources.${source || 'compiled_default'}`)
function formSettings() {
  const selectedProfiles = Object.fromEntries(Object.entries(profiles).filter(([preset, selection]) => preset !== 'codex' && (selection.user_agent?.trim() || selection.version?.trim())).map(([preset, selection]) => [preset, { ...selection }]))
  const selectedDefaults = Object.fromEntries(Object.entries(defaults).filter(([, preset]) => preset)) as Record<string, IdentityPreset>
  return { profiles: selectedProfiles, defaults: selectedDefaults }
}
const isDirty = computed(() => !!view.value && JSON.stringify(formSettings()) !== savedForm.value)
async function refresh() {
  loading.value = true
  error.value = ''
  try {
    const updated = await getOutboundIdentity()
    // A settings save also refreshes Codex's effective identity. Preserve any
    // edits made while that request was in flight.
    if (!isDirty.value) {
      for (const preset of identityPresets) Object.assign(profiles[preset], { preset, user_agent: '', version: '' }, updated.settings.profiles?.[preset])
      for (const key of Object.keys(defaults)) delete defaults[key]
      Object.assign(defaults, updated.settings.defaults)
      for (const mapping of mappings) defaults[mapping.key] ||= ''
      savedForm.value = JSON.stringify(formSettings())
    }
    view.value = updated
  } catch {
    error.value = t('admin.settings.outboundIdentity.loadFailed')
  } finally { loading.value = false }
}
async function save() {
  if (!view.value) throw new Error(t('admin.settings.outboundIdentity.loadFailed'))
  if (!isDirty.value) return
  const settings = formSettings()
  const submittedForm = JSON.stringify(settings)
  view.value = await updateOutboundIdentity(settings)
  savedForm.value = submittedForm
}
onMounted(refresh)
defineExpose({ save, refresh, isDirty })
</script>
