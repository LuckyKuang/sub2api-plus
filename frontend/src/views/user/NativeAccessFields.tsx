import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Select } from '@/components/ui'
import { apiClient } from '@/api/client'
import { useClipboard } from './useClipboard'

interface NativeModel {
  name: string
  platform: string
  endpoints: string[]
  conversion_endpoints: string[]
}

export interface NativeAccessFieldsProps {
  conversionEnabled?: boolean
  groupId: number | null
  client: string
  endpoint: string
  baseUrl?: string
  apiKey?: string
  editable?: boolean
  onConversionEnabledChange?: (value: boolean) => void
  onClientChange?: (value: string) => void
  onEndpointChange?: (value: string) => void
}

const clients: Record<string, string[]> = {
  codex: ['/v1/responses'],
  claude: ['/v1/messages'],
  'openai-sdk': ['/v1/responses', '/v1/chat/completions'],
  'anthropic-sdk': ['/v1/messages'],
  opencode: ['/v1/responses', '/v1/chat/completions', '/v1/messages'],
  custom: ['/v1/responses', '/v1/chat/completions', '/v1/messages']
}

const names: Record<string, string> = {
  codex: 'Codex',
  claude: 'Claude Code',
  'openai-sdk': 'OpenAI SDK',
  'anthropic-sdk': 'Anthropic SDK',
  opencode: 'OpenCode'
}

export function NativeAccessFields({
  conversionEnabled = false,
  groupId,
  client,
  endpoint,
  baseUrl = '',
  apiKey = '',
  editable = true,
  onConversionEnabledChange,
  onClientChange,
  onEndpointChange
}: NativeAccessFieldsProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [models, setModels] = useState<NativeModel[]>([])
  const [endpoints, setEndpoints] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [showValidation, setShowValidation] = useState(false)
  const controller = useRef<AbortController | undefined>(undefined)

  const configured = conversionEnabled
  const root = (baseUrl || window.location.origin)
    .replace(/\/(?:v1|v1beta)\/?$/, '')
    .replace(/\/+$/, '')
  const sdkBase = endpoint === '/v1/messages' ? root : `${root}/v1`
  const requestUrl = `${root}${endpoint}`

  const clientOptions = Object.keys(clients).map((value) => ({
    value,
    label: names[value] || t('nativeAccess.custom'),
    disabled: !clients[value]!.some((item) => endpoints.includes(item))
  }))
  const endpointOptions = endpoints
    .filter((value) => (clients[client] || []).includes(value))
    .map((value) => ({ value, label: `HTTP ${value}` }))
  const compatibleModels = models.filter((model) => (model.conversion_endpoints ?? []).includes(endpoint))
  const modelName = compatibleModels[0]?.name || 'MODEL'

  const shellQuote = (value: string) => "'" + value.replace(/'/g, "'\"'\"'") + "'"
  const example = (() => {
    const key = apiKey || 'YOUR_API_KEY'
    const model = modelName
    if (client === 'codex') {
      return `# config.toml\nmodel = ${JSON.stringify(model)}\nmodel_provider = "composite"\n[model_providers.composite]\nname = "Composite"\nbase_url = ${JSON.stringify(sdkBase)}\nwire_api = "responses"\nenv_key = "COMPOSITE_API_KEY"\n\n# COMPOSITE_API_KEY=${key}`
    }
    if (client === 'claude') {
      return JSON.stringify({ env: { ANTHROPIC_BASE_URL: sdkBase, ANTHROPIC_AUTH_TOKEN: key, ANTHROPIC_MODEL: model } }, null, 2)
    }
    const body =
      endpoint === '/v1/responses'
        ? { model, input: 'Hello' }
        : endpoint === '/v1/messages'
          ? { model, max_tokens: 1024, messages: [{ role: 'user', content: 'Hello' }] }
          : { model, messages: [{ role: 'user', content: 'Hello' }] }
    const headers =
      endpoint === '/v1/messages'
        ? `-H ${shellQuote(`x-api-key: ${key}`)} \\\n  -H 'anthropic-version: 2023-06-01'`
        : `-H ${shellQuote(`Authorization: Bearer ${key}`)}`
    return `curl ${shellQuote(requestUrl)} \\\n  ${headers} \\\n  -H 'Content-Type: application/json' \\\n  --data ${shellQuote(JSON.stringify(body))}`
  })()

  function toggleConversion(enabled: boolean) {
    onConversionEnabledChange?.(enabled)
    if (!enabled) {
      onClientChange?.('')
      onEndpointChange?.('')
      return
    }
    setClient(client || 'custom')
  }

  function setClient(next: string) {
    onClientChange?.(next)
    const candidates = endpoints.filter((item) => clients[next]?.includes(item))
    if (!candidates.includes(endpoint)) onEndpointChange?.(candidates[0] || '')
  }

  useEffect(() => {
    controller.current?.abort()
    setModels([])
    setEndpoints([])
    setError('')
    if (!groupId) {
      setLoading(false)
      return
    }
    const active = new AbortController()
    controller.current = active
    setLoading(true)
    void (async () => {
      try {
        const { data } = await apiClient.get<{
          models: NativeModel[]
          endpoints: string[]
          conversion_endpoints: string[]
        }>(`/groups/${groupId}/native-access`, { signal: active.signal })
        if (active.signal.aborted) return
        if (!data || !Array.isArray(data.models) || !Array.isArray(data.conversion_endpoints)) {
          throw new Error('Invalid capabilities response')
        }
        setModels(data.models)
        setEndpoints(data.conversion_endpoints)
        const nextClient = client || 'custom'
        if (conversionEnabled && !endpoint && editable) setClient(nextClient)
      } catch {
        if (!active.signal.aborted) setError(t('nativeAccess.loadFailed'))
      } finally {
        if (controller.current === active) setLoading(false)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupId])

  useEffect(() => {
    return () => controller.current?.abort()
  }, [])

  const validSelection =
    !loading && !error && endpointOptions.some((option) => option.value === endpoint) && compatibleModels.length > 0

  const validate = (): boolean => {
    setShowValidation(true)
    return !configured || validSelection
  }

  // Expose validate to the owning form through the module-level handle registry.
  nativeAccessValidateRef.current = validate

  return (
    <section className="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="native-access">
      <p className="text-sm font-medium">{t('nativeAccess.title')}</p>
      <p className="input-hint">{t('nativeAccess.hint')}</p>
      {editable && (
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="checkbox"
            data-testid="composite-conversion-toggle"
            checked={configured}
            onChange={(event) => toggleConversion(event.target.checked)}
          />
          {t('nativeAccess.enableConversion')}
        </label>
      )}
      {!configured && (
        <p className="input-hint" data-testid="composite-native-mode">
          {t('nativeAccess.nativeMode')}
        </p>
      )}
      {configured && (
        <>
          <p className="input-hint">{t('nativeAccess.conversionHint')}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <label>
              <span className="input-label">{t('nativeAccess.client')}</span>
              <Select
                value={client}
                options={clientOptions}
                disabled={!editable}
                onValueChange={(value) => setClient(String(value))}
              />
            </label>
            <label>
              <span className="input-label">{t('nativeAccess.endpoint')}</span>
              <Select
                value={endpoint}
                options={endpointOptions}
                disabled={loading || !editable}
                onValueChange={(value) => onEndpointChange?.(String(value))}
              />
            </label>
          </div>

          {(error || (showValidation && !validSelection)) && (
            <p role="alert" className="text-sm text-red-600">
              {error || t('nativeAccess.noConversion')}
            </p>
          )}
          {endpoint && (
            <>
              <dl className="space-y-2 text-sm">
                <div>
                  <dt className="surface-muted">{t('nativeAccess.baseUrl')}</dt>
                  <dd className="select-all break-all font-mono">{sdkBase}</dd>
                </div>
                <div>
                  <dt className="surface-muted">{t('nativeAccess.requestUrl')}</dt>
                  <dd className="select-all break-all font-mono">{requestUrl}</dd>
                </div>
              </dl>
              <div className="flex flex-wrap gap-2">
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => void copyToClipboard(sdkBase)}>
                  {t('nativeAccess.copyBase')}
                </button>
                {apiKey && (
                  <button type="button" className="btn btn-secondary btn-sm" onClick={() => void copyToClipboard(apiKey)}>
                    {t('nativeAccess.copyKey')}
                  </button>
                )}
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => void copyToClipboard(example)}>
                  {t('nativeAccess.copyExample')}
                </button>
              </div>
              <p className="text-xs surface-muted">{t('nativeAccess.models')}</p>
              <ul className="max-h-40 space-y-1 overflow-auto text-xs">
                {compatibleModels.map((model) => (
                  <li key={`${model.platform}:${model.name}`}>
                    <span className="font-mono">{model.name}</span> · {model.platform}
                  </li>
                ))}
              </ul>
              {!loading && compatibleModels.length === 0 && <p className="text-xs surface-muted">{t('nativeAccess.noModels')}</p>}
              <pre className="surface-inset max-h-64 overflow-auto rounded-lg p-3 text-xs">
                <code>{example}</code>
              </pre>
            </>
          )}
        </>
      )}
      {loading && <p className="text-xs surface-muted">{t('common.loading')}</p>}
      {!configured && error && (
        <p role="alert" className="text-sm text-red-600">
          {error}
        </p>
      )}
      {!configured && apiKey && (
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => void copyToClipboard(apiKey)}>
          {t('nativeAccess.copyKey')}
        </button>
      )}
      {models.length > 0 && (
        <details open={!configured} className="text-xs">
          <summary className="cursor-pointer">{t('nativeAccess.allModels')}</summary>
          <div className="mt-2 space-y-2">
            {models.map((model) => (
              <div key={`${model.platform}:${model.name}`}>
                <span className="font-mono">{model.name}</span> · {model.platform}
                <div className="break-all surface-muted">
                  {model.endpoints.map((item) => `${root}${item}`).join(' · ') || t('nativeAccess.noModels')}
                </div>
              </div>
            ))}
          </div>
        </details>
      )}
    </section>
  )
}

/** Latest validate() handle, used by the key form through `validateNativeAccess()`. */
export const nativeAccessValidateRef: { current: () => boolean } = { current: () => true }

export function validateNativeAccess(): boolean {
  return nativeAccessValidateRef.current()
}

export default NativeAccessFields
