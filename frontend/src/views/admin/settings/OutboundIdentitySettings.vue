<template>
  <div class="space-y-6" data-testid="outbound-identity-settings">
    <div>
      <h2 class="text-lg font-semibold">{{ t('admin.settings.outboundIdentity.title') }}</h2>
      <p class="mt-2 text-sm text-gray-500">{{ t('admin.settings.outboundIdentity.description') }}</p>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <template v-if="view">
      <section v-for="group in identityGroups" :key="group.key" class="card space-y-4 p-6" :data-identity-group="group.key">
        <h3 class="font-semibold">{{ group.label }}</h3>
        <p v-if="group.key === 'minimax'" class="text-sm text-gray-500">{{ t('admin.settings.outboundIdentity.minimaxModesHint') }}</p>
        <div class="grid gap-6" :class="group.presets.length > 1 ? 'xl:grid-cols-2' : ''">
        <div v-for="preset in group.presets" :key="preset" class="space-y-4" :data-identity-preset="preset">
        <h4 v-if="group.presets.length > 1" class="font-medium">{{ preset === 'minimax' ? 'OAuth' : 'API Key' }}</h4>
        <p v-if="domesticPresets.includes(preset)" class="text-sm text-gray-500" data-testid="outbound-identity-auth-scope">{{ t('admin.settings.outboundIdentity.oauthApiKeyScope') }}</p>
        <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="mb-2 text-gray-500">{{ t('admin.settings.outboundIdentity.effectiveGlobal') }}</p>
          <dl class="grid gap-2 sm:grid-cols-[auto_1fr]">
            <dt>User-Agent</dt><dd class="break-all font-mono">{{ effective(preset)?.user_agent }}</dd>
            <dt>{{ t('admin.settings.outboundIdentity.client') }}</dt><dd class="font-mono">{{ effective(preset)?.originator }}</dd>
            <dt>{{ t('admin.settings.outboundIdentity.version') }}</dt>
            <dd class="font-mono">
              <template v-if="effective(preset)?.version">{{ effective(preset)?.version }}</template>
              <span v-else class="text-gray-500">{{ t('admin.settings.outboundIdentity.versionNotDeclared') }}</span>
            </dd>
            <dt>{{ t('admin.settings.outboundIdentity.source') }}</dt><dd>{{ sourceLabel(effective(preset)?.source) }}</dd>
          </dl>
          <div class="mt-3">
            <p class="mb-1 text-gray-500">{{ t('admin.settings.outboundIdentity.headers') }}</p>
            <dl class="grid gap-1 sm:grid-cols-[auto_1fr]" data-testid="outbound-identity-headers">
              <template v-for="(value, name) in effective(preset)?.headers" :key="name">
                <dt class="font-mono text-xs text-gray-500">{{ name }}</dt>
                <dd class="break-all font-mono text-xs">{{ value === '' ? t('admin.settings.outboundIdentity.emptyOfficialValue') : value }}</dd>
              </template>
            </dl>
          </div>
        </div>
        <div v-for="wire in wireProfiles(preset)" :key="wire.protocol" class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="mb-2 text-gray-500">{{ wire.protocol }} · {{ t('admin.settings.outboundIdentity.headers') }}</p>
          <dl class="grid gap-1 sm:grid-cols-[auto_1fr]" data-testid="outbound-identity-wire-headers">
            <template v-for="(value, name) in wire.headers" :key="name">
              <dt class="font-mono text-xs text-gray-500">{{ name }}</dt>
              <dd class="break-all font-mono text-xs">{{ value === '' ? t('admin.settings.outboundIdentity.emptyOfficialValue') : value }}</dd>
            </template>
          </dl>
        </div>
        <div v-if="controlIdentity(preset)" class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="mb-2 text-gray-500">{{ t('admin.settings.outboundIdentity.controlHeaders') }}</p>
          <p v-if="preset === 'deepseek'" class="mb-3 text-xs text-gray-500" data-testid="deepseek-offset-hint">{{ t('admin.settings.outboundIdentity.deepseekOffsetHint') }}</p>
          <p v-if="preset === 'zcode'" class="mb-3 text-xs text-gray-500" data-testid="zcode-kernel-hint">{{ t('admin.settings.outboundIdentity.zcodeKernelHint') }}</p>
          <dl class="grid gap-1 sm:grid-cols-[auto_1fr]" data-testid="outbound-identity-control-headers">
            <template v-for="(value, name) in controlIdentity(preset)?.headers" :key="name">
              <dt class="font-mono text-xs text-gray-500">{{ name }}</dt>
              <dd class="break-all font-mono text-xs">{{ value === '' ? t('admin.settings.outboundIdentity.emptyOfficialValue') : value }}</dd>
            </template>
          </dl>
        </div>
        <slot v-if="preset === 'codex'" name="codex" />
        <p v-else-if="isVersionless(preset)" class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.versionlessHint') }}</p>
        <p v-else-if="preset === 'minimax_apikey'" class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.pinnedSdkHint') }}</p>
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
        <label v-if="preset === 'deepseek'" class="block text-sm">
          {{ t('admin.settings.outboundIdentity.language') }}
          <IdentityRuntimeField name="language" :model-value="profiles[preset].language" fallback="zh-CN" @update:model-value="setLanguage(preset, $event)" />
        </label>
        <label v-if="hasIdentityTimezone(preset)" class="block text-sm">
          {{ t('admin.settings.outboundIdentity.timezone') }}
          <IdentityRuntimeField name="timezone" :model-value="profiles[preset].timezone" fallback="UTC" @update:model-value="setTimezone(preset, $event)" />
          <span class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.timezoneHint') }}</span>
        </label>
        <IdentityEnvironmentSummary :declarations="presetDeclarations(preset)" />
        <div v-if="runtimeHeaders(preset).length" class="space-y-3" data-testid="outbound-identity-runtime-headers">
          <div>
            <p class="text-sm">{{ t('admin.settings.outboundIdentity.runtimeHeaders') }}</p>
            <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.runtimeHeadersHint') }}</p>
          </div>
          <label v-for="header in runtimeHeaders(preset)" :key="header.name" class="block text-sm">
            <span class="font-mono text-xs text-gray-500">{{ header.name }}</span>
            <IdentityRuntimeField
              :model-value="runtime[preset][header.name]"
              class="mt-1"
              :fallback="header.builtin"
              :name="header.name"
              @update:model-value="setRuntime(preset, header.name, $event)"
            />
          </label>
        </div>
        </div>
        </div>
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
import IdentityEnvironmentSummary from '@/components/account/IdentityEnvironmentSummary.vue'
import IdentityRuntimeField from '@/components/account/IdentityRuntimeField.vue'
import { hasIdentityTimezone, getOutboundIdentity, updateOutboundIdentity, identityNames, identityPresets, versionlessIdentityPresets, type IdentityDeclaration, type IdentityPreset, type IdentitySelection, type OutboundIdentityView } from '@/api/admin/outboundIdentity'

const { t } = useI18n()
const identityGroups = identityPresets.filter(preset => preset !== 'minimax_apikey').map(preset => ({
  key: preset, label: preset === 'minimax' ? 'MiniMax Code' : identityNames[preset],
  presets: preset === 'minimax' ? ['minimax', 'minimax_apikey'] as IdentityPreset[] : [preset]
}))
const domesticPresets: IdentityPreset[] = ['deepseek', 'kimi', 'zcode', 'stepfun']
const view = ref<OutboundIdentityView>()
const error = ref('')
const loading = ref(false)
const profiles = reactive(Object.fromEntries(identityPresets.map(preset => [preset, { preset, user_agent: '', version: '' }])) as Record<IdentityPreset, IdentitySelection>)
// Operator values for each preset's runtime declarations. An empty entry means
// "use the built-in value the official client's own host resolution produces".
const runtime = reactive(Object.fromEntries(identityPresets.map(preset => [preset, {}])) as Record<IdentityPreset, Record<string, string>>)
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
  { key: 'typesafe:apikey', label: 'TypeSafe / Jev · API Key' },
  ...['kimi', 'zhipu', 'deepseek', 'minimax', 'stepfun'].map(platform => ({ key: `${platform}:apikey`, label: `${platform} · API Key` }))
]
const effective = (preset: IdentityPreset) => view.value?.effective.find(item => item.preset === preset)
const wireProfiles = (preset: IdentityPreset) => view.value?.wire_profiles?.filter(item => item.preset === preset) ?? []
const controlIdentity = (preset: IdentityPreset) => view.value?.control_plane?.find(item => item.preset === preset)
const builtin = (preset: IdentityPreset) => view.value?.presets.find(item => item.preset === preset)
const isVersionless = (preset: IdentityPreset) => versionlessIdentityPresets.includes(preset)
const sourceLabel = (source?: string) => t(`admin.settings.outboundIdentity.sources.${source || 'compiled_default'}`)
// The backend declares which headers a preset renders and which of them accept
// a configured value, so this page never hard-codes a preset's header block.
const presetDeclarations = (preset: IdentityPreset) => view.value?.declarations?.find(item => item.preset === preset)?.headers ?? []
const runtimeHeaders = (preset: IdentityPreset): IdentityDeclaration[] =>
  presetDeclarations(preset).filter(header => header.editable)
function setTimezone(preset: IdentityPreset, value: string) {
  if (value) profiles[preset].timezone = value
  else delete profiles[preset].timezone
}

function setLanguage(preset: IdentityPreset, value: string) {
  if (value) profiles[preset].language = value
  else delete profiles[preset].language
}
function setRuntime(preset: IdentityPreset, name: string, value: string) {
  const trimmed = value.trim()
  if (trimmed) runtime[preset][name] = trimmed
  else delete runtime[preset][name]
}
// A versionless family exposes no editable declaration, so a profile the
// management API already persisted for it cannot be re-derived from user input.
// Keep that profile through unrelated saves rather than silently dropping
// configuration this page never presented a control for.
function isPreservedProfile(preset: string) {
  return isVersionless(preset as IdentityPreset) && Boolean(view.value?.settings.profiles?.[preset as IdentityPreset])
}
function formSettings() {
  const selectedProfiles = Object.fromEntries(Object.entries(profiles).filter(([preset, selection]) => preset !== 'codex' && (selection.user_agent?.trim() || selection.version?.trim() || selection.timezone || selection.language || isPreservedProfile(preset))).map(([preset, selection]) => [preset, { ...selection }]))
  const selectedDefaults = Object.fromEntries(Object.entries(defaults).filter(([, preset]) => preset)) as Record<string, IdentityPreset>
  const selectedRuntime = Object.fromEntries(Object.entries(runtime).filter(([, values]) => Object.keys(values).length).map(([preset, values]) => [preset, { ...values }]))
  return { profiles: selectedProfiles, defaults: selectedDefaults, runtime: selectedRuntime }
}
const isDirty = computed(() => !!view.value && JSON.stringify(formSettings()) !== savedForm.value)
async function refresh() {
  loading.value = true
  error.value = ''
  try {
    const updated = await getOutboundIdentity()
    // A settings save also refreshes Codex's effective identity. Preserve any
    // edits made while that request was in flight.
    const dirty = isDirty.value
    if (!dirty) {
      for (const preset of identityPresets) {
        // API profiles may include runtime overrides. Present them in the same
        // editable fields and save one global runtime map, preserving their value.
        const { headers, ...profile } = updated.settings.profiles?.[preset] ?? {}
        profiles[preset] = { preset, user_agent: '', version: '', ...profile }
        for (const name of Object.keys(runtime[preset])) delete runtime[preset][name]
        Object.assign(runtime[preset], updated.settings.runtime?.[preset], headers)
      }
      for (const key of Object.keys(defaults)) delete defaults[key]
      Object.assign(defaults, updated.settings.defaults)
      for (const mapping of mappings) defaults[mapping.key] ||= ''
    }
    view.value = updated
    // The saved baseline is computed against the freshly loaded view, because a
    // preserved versionless profile is derived from the server's saved settings.
    if (!dirty) savedForm.value = JSON.stringify(formSettings())
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
