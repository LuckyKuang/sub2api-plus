<template>
  <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="native-access">
    <p class="text-sm font-medium">{{ t('nativeAccess.title') }}</p>
    <p class="input-hint">{{ t('nativeAccess.hint') }}</p>
    <div class="grid gap-3 sm:grid-cols-2">
      <label><span class="input-label">{{ t('nativeAccess.client') }}</span><Select :model-value="client" :options="clientOptions" @update:model-value="setClient(String($event))" /></label>
      <label><span class="input-label">{{ t('nativeAccess.endpoint') }}</span><Select :model-value="endpoint" :options="endpointOptions" :disabled="loading" @update:model-value="emit('update:endpoint', String($event))" /></label>
    </div>
    <p v-if="loading" class="text-xs surface-muted">{{ t('common.loading') }}</p>
    <p v-if="error || (showValidation && !validSelection)" role="alert" class="text-sm text-red-600">{{ error || t('nativeAccess.noModels') }}</p>
    <template v-if="endpoint">
      <dl class="space-y-2 text-sm"><div><dt class="surface-muted">{{ t('nativeAccess.baseUrl') }}</dt><dd class="select-all break-all font-mono">{{ sdkBase }}</dd></div><div><dt class="surface-muted">{{ t('nativeAccess.requestUrl') }}</dt><dd class="select-all break-all font-mono">{{ requestUrl }}</dd></div></dl>
      <div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(sdkBase)">{{ t('nativeAccess.copyBase') }}</button><button v-if="apiKey" type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(apiKey)">{{ t('nativeAccess.copyKey') }}</button><button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(example)">{{ t('nativeAccess.copyExample') }}</button></div>
      <p class="text-xs surface-muted">{{ t('nativeAccess.models') }}</p>
      <ul class="max-h-40 space-y-1 overflow-auto text-xs"><li v-for="model in compatibleModels" :key="`${model.platform}:${model.name}`"><span class="font-mono">{{ model.name }}</span> · {{ model.platform }}</li></ul>
      <p v-if="!loading && !compatibleModels.length" class="text-xs surface-muted">{{ t('nativeAccess.noModels') }}</p>
      <pre class="max-h-64 overflow-auto rounded-lg surface-inset p-3 text-xs"><code>{{ example }}</code></pre>
    </template>
    <details v-if="models.length" class="text-xs"><summary class="cursor-pointer">{{ t('nativeAccess.allModels') }}</summary><div class="mt-2 space-y-2"><div v-for="model in models" :key="`${model.platform}:${model.name}`"><span class="font-mono">{{ model.name }}</span> · {{ model.platform }}<div class="break-all surface-muted">{{ model.endpoints.join(' · ') || t('nativeAccess.noModels') }}</div></div></div></details>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { useClipboard } from '@/composables/useClipboard'
import { apiClient } from '@/api/client'

const props = withDefaults(defineProps<{ groupId: number | null; client: string; endpoint: string; baseUrl?: string; apiKey?: string }>(), { baseUrl: '', apiKey: '' })
const emit = defineEmits<{ 'update:client': [value: string]; 'update:endpoint': [value: string] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
interface NativeModel { name: string; platform: string; endpoints: string[] }
const models = ref<NativeModel[]>([])
const endpoints = ref<string[]>([])
const loading = ref(false)
const error = ref('')
const showValidation = ref(false)
let controller: AbortController | undefined
const clients: Record<string, string[]> = {
  codex: ['/v1/responses'], claude: ['/v1/messages'], 'openai-sdk': ['/v1/responses', '/v1/chat/completions'],
  'anthropic-sdk': ['/v1/messages'], gemini: ['/v1beta'], opencode: ['/v1/responses', '/v1/chat/completions', '/v1/messages', '/v1beta'],
  custom: ['/v1/responses', '/v1/chat/completions', '/v1/messages', '/v1beta', '/v1/systemone'],
}
const names: Record<string, string> = { codex: 'Codex', claude: 'Claude Code', 'openai-sdk': 'OpenAI SDK', 'anthropic-sdk': 'Anthropic SDK', gemini: 'Gemini', opencode: 'OpenCode' }
const clientOptions = computed(() => Object.keys(clients).map(value => ({ value, label: names[value] || t('nativeAccess.custom'), disabled: !clients[value]!.some(endpoint => endpoints.value.includes(endpoint)) })))
const endpointOptions = computed(() => endpoints.value.filter(value => (clients[props.client] || []).includes(value)).map(value => ({ value, label: value === '/v1beta' ? 'Gemini REST /v1beta' : `HTTP ${value}` })))
const compatibleModels = computed(() => models.value.filter(model => model.endpoints.includes(props.endpoint)))
const root = computed(() => (props.baseUrl || window.location.origin).replace(/\/(?:v1|v1beta)\/?$/, '').replace(/\/+$/, ''))
const sdkBase = computed(() => props.endpoint === '/v1/messages' ? root.value : `${root.value}${props.endpoint === '/v1beta' ? '/v1beta' : '/v1'}`)
const modelName = computed(() => compatibleModels.value[0]?.name || 'MODEL')
const requestUrl = computed(() => `${root.value}${props.endpoint === '/v1beta' ? `/v1beta/models/${encodeURIComponent(modelName.value)}:generateContent` : props.endpoint}`)
const shellQuote = (value: string) => "'" + value.replace(/'/g, "'\"'\"'") + "'"
const example = computed(() => {
  const key = props.apiKey || 'YOUR_API_KEY'
  const model = modelName.value
  if (props.client === 'codex') return `# config.toml\nmodel = ${JSON.stringify(model)}\nmodel_provider = "composite"\n[model_providers.composite]\nname = "Composite"\nbase_url = ${JSON.stringify(sdkBase.value)}\nwire_api = "responses"\nenv_key = "COMPOSITE_API_KEY"\n\n# COMPOSITE_API_KEY=${key}`
  if (props.client === 'claude') return JSON.stringify({ env: { ANTHROPIC_BASE_URL: sdkBase.value, ANTHROPIC_AUTH_TOKEN: key, ANTHROPIC_MODEL: model } }, null, 2)
  const body = props.endpoint === '/v1/responses' ? { model, input: 'Hello' } : props.endpoint === '/v1/messages' ? { model, max_tokens: 1024, messages: [{ role: 'user', content: 'Hello' }] } : props.endpoint === '/v1/systemone' ? { model, state: 'Hello', questions: { greeting: { type: 'noul', instructions: 'Is this a greeting?', criteria: { true: 'Yes', false: 'No' } } } } : props.endpoint === '/v1beta' ? { contents: [{ parts: [{ text: 'Hello' }] }] } : { model, messages: [{ role: 'user', content: 'Hello' }] }
  const headers = props.endpoint === '/v1/messages' ? `-H ${shellQuote(`x-api-key: ${key}`)} \\\n  -H 'anthropic-version: 2023-06-01'` : `-H ${shellQuote(`Authorization: Bearer ${key}`)}`
  return `curl ${shellQuote(requestUrl.value)} \\\n  ${headers} \\\n  -H 'Content-Type: application/json' \\\n  --data ${shellQuote(JSON.stringify(body))}`
})
function setClient(client: string) {
  emit('update:client', client)
  const candidates = endpoints.value.filter(endpoint => clients[client]?.includes(endpoint))
  if (!candidates.includes(props.endpoint)) emit('update:endpoint', candidates[0] || '')
}
watch(() => props.groupId, async groupId => {
  controller?.abort()
  models.value = []; endpoints.value = []; error.value = ''
  if (!groupId) { loading.value = false; return }
  const active = new AbortController(); controller = active; loading.value = true
  try {
    const { data } = await apiClient.get<{ models: NativeModel[]; endpoints: string[] }>(`/groups/${groupId}/native-access`, { signal: active.signal })
    if (active.signal.aborted) return
    models.value = data.models; endpoints.value = data.endpoints
    const client = props.client || 'custom'
    if (!props.client) emit('update:client', client)
    if (!data.endpoints.includes(props.endpoint) || !clients[client]?.includes(props.endpoint)) emit('update:endpoint', data.endpoints.find(endpoint => clients[client]?.includes(endpoint)) || '')
  } catch {
    if (!active.signal.aborted) error.value = t('nativeAccess.loadFailed')
  } finally { if (controller === active) loading.value = false }
}, { immediate: true })
onUnmounted(() => controller?.abort())
const validSelection = computed(() => !loading.value && !error.value && endpointOptions.value.some(option => option.value === props.endpoint) && compatibleModels.value.length > 0)
function validate() { showValidation.value = true; return validSelection.value }
defineExpose({ validate })
</script>
