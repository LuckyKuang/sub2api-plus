import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { saveAs } from 'file-saver'
import { Modal, Select } from '@/components/ui'
import { buildCodexModelCatalogUrl, fetchCodexModelsManifest } from '@/api/codex'
import { buildCodexApiKeyAuthConfig } from '@/utils/codexAuthConfig'
import {
  findCodexCatalogModel,
  formatCodexReasoningEffortTomlLine,
  parseCodexCatalogModels,
  selectCodexConfigReasoningEffort
} from '@/utils/codexCatalogConfig'
import type { GroupPlatform } from '@/types'
import { Icon } from './icons'
import { useClipboard } from './useClipboard'
import { NativeAccessFields } from './NativeAccessFields'

export interface UseKeyModalProps {
  show: boolean
  apiKey: string
  baseUrl: string
  platform: GroupPlatform | null
  groupId?: number | null
  conversionEnabled?: boolean
  defaultClient?: string
  defaultEndpoint?: string
  claudeCodeOnly?: boolean
  allowMessagesDispatch?: boolean
  onClose?: () => void
}

interface TabConfig {
  id: string
  label: string
  icon: 'apple' | 'windows' | 'terminal' | 'sparkle'
}

interface FileConfig {
  path: string
  content: string
  hint?: string
  highlighted?: string
}

type CodexAuthMode = 'legacy' | 'api-key'
type CodexModelManifestState = 'idle' | 'loading' | 'ready' | 'error'

const CODEX_MODEL_CATALOG_CONFIG_PATH = '~/.codex/codex-models.json'

function TabIcon({ icon }: { icon: TabConfig['icon'] }) {
  if (icon === 'apple') {
    return (
      <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.81-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z" />
      </svg>
    )
  }
  if (icon === 'windows') {
    return (
      <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M3 12V6.75l6-1.32v6.48L3 12zm17-9v8.75l-10 .15V5.21L20 3zM3 13l6 .09v6.81l-6-1.15V13zm7 .25l10 .15V21l-10-1.91v-5.84z" />
      </svg>
    )
  }
  if (icon === 'sparkle') {
    return (
      <svg className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.5" viewBox="0 0 24 24" aria-hidden="true">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          d="M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 0 0-2.456 2.456ZM16.894 20.567 16.5 21.75l-.394-1.183a2.25 2.25 0 0 0-1.423-1.423L13.5 18.75l1.183-.394a2.25 2.25 0 0 0 1.423-1.423l.394-1.183.394 1.183a2.25 2.25 0 0 0 1.423 1.423l1.183.394-1.183.394a2.25 2.25 0 0 0-1.423 1.423Z"
        />
      </svg>
    )
  }
  return (
    <svg className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.5" viewBox="0 0 24 24" aria-hidden="true">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="m6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 17.25V6.75A2.25 2.25 0 0 0 18.75 4.5H5.25A2.25 2.25 0 0 0 3 6.75v10.5A2.25 2.25 0 0 0 5.25 20.25Z"
      />
    </svg>
  )
}

/** Compact model catalog: [id, name, context, output?] */
type ModelEntry = [string, string, number, number?]
type VariantMap = Record<string, Record<string, { effort?: string }>>

const OPENAI_MODELS: Array<ModelEntry & { variants?: VariantMap }> = [
  ['gpt-6-astra', 'GPT-6 Astra', 872000, 128000],
  ['gpt-5.2', 'GPT-5.2', 400000, 128000],
  ['gpt-5.6', 'GPT-5.6 (Sol)', 1050000, 128000],
  ['gpt-6.1-sol', 'GPT-6.1 Sol', 1050000, 128000],
  ['gpt-6.1-terra', 'GPT-6.1 Terra', 500000],
  ['gpt-6-sol', 'GPT-6 Sol', 1050000, 128000],
  ['gpt-6-luna', 'GPT-6 Luna', 1050000, 128000],
  ['gpt-5.6-sol', 'GPT-5.6 Sol', 1050000, 128000],
  ['gpt-5.6-terra', 'GPT-5.6 Terra', 1050000, 128000],
  ['gpt-5.6-luna', 'GPT-5.6 Luna', 1050000, 128000],
  ['gpt-5.5', 'GPT-5.5', 1050000, 128000],
  ['gpt-5.4', 'GPT-5.4', 1050000, 128000],
  ['gpt-5.4-mini', 'GPT-5.4 Mini', 400000, 128000],
  ['gpt-5.3-codex-spark', 'GPT-5.3 Codex Spark', 128000, 32000],
  ['codex-mini-latest', 'Codex Mini', 200000, 100000]
]

const GEMINI_MODELS: ModelEntry[] = [
  ['gemini-2.0-flash', 'Gemini 2.0 Flash', 1048576, 65536],
  ['gemini-2.5-flash', 'Gemini 2.5 Flash', 1048576, 65536],
  ['gemini-2.5-pro', 'Gemini 2.5 Pro', 2097152, 65536]
]

const CLAUDE_MODELS: Array<[string, string, number, number, 'medium' | 'high']> = [
  ['claude-opus-5-5', 'Claude Opus 5.5', 1000000, 128000, 'medium'],
  ['claude-sonnet-5-5', 'Claude Sonnet 5.5', 1000000, 128000, 'high']
]

const GROK_MODELS: ModelEntry[] = [
  ['grok-4.5', 'Grok 4.5', 500000, 64000],
  ['grok-build-0.1', 'Grok Build 0.1', 256000, 64000],
  ['grok-4.20-multi-agent-0309', 'Grok 4.20 Multi Agent (text / web_search)', 1000000, 64000],
  ['grok-4.3', 'Grok 4.3', 1000000, 64000],
  ['grok-composer-2.5-fast', 'Grok Composer 2.5 Fast', 500000, 64000]
]

const OPENCODE_VARIANTS: VariantMap = {
  low: {},
  medium: {},
  high: {},
  xhigh: {},
  max: {}
}

function buildOpenAIModels(): Record<string, unknown> {
  const models: Record<string, unknown> = {}
  for (const [id, name, context, output] of OPENAI_MODELS) {
    models[id] = {
      name,
      limit: { context, ...(output === undefined ? {} : { output }) },
      options: { store: false },
      variants: id === 'gpt-5.3-codex-spark' || id === 'codex-mini-latest' ? undefined : OPENCODE_VARIANTS
    }
  }
  return models
}

function buildGeminiModels(): Record<string, unknown> {
  const models: Record<string, unknown> = {}
  for (const [id, name, context, output] of GEMINI_MODELS) {
    models[id] = {
      name,
      limit: { context, output },
      modalities: { input: ['text', 'image', 'pdf'], output: ['text'] },
      ...(id === 'gemini-2.5-pro'
        ? { options: { thinking: { budgetTokens: 24576, type: 'enabled' } } }
        : {})
    }
  }
  return models
}

function buildClaudeModels(): Record<string, unknown> {
  const models: Record<string, unknown> = {}
  for (const [id, name, context, output, effort] of CLAUDE_MODELS) {
    models[id] = {
      name,
      limit: { context, output },
      modalities: { input: ['text', 'image', 'pdf'], output: ['text'] },
      options: { thinking: { type: 'adaptive' }, effort },
      variants: {
        low: { effort: 'low' },
        medium: { effort: 'medium' },
        high: { effort: 'high' },
        xhigh: { effort: 'xhigh' },
        max: { effort: 'max' }
      }
    }
  }
  return models
}

function buildGrokModels(): Record<string, unknown> {
  const models: Record<string, unknown> = {}
  for (const [id, name, context, output] of GROK_MODELS) {
    models[id] = { name, limit: { context, output } }
  }
  return models
}

export function UseKeyModal({
  show,
  apiKey,
  baseUrl,
  platform,
  groupId,
  conversionEnabled,
  defaultClient,
  defaultEndpoint,
  claudeCodeOnly = false,
  allowMessagesDispatch = false,
  onClose
}: UseKeyModalProps) {
  const { t } = useTranslation()
  const { copyToClipboard: clipboardCopy } = useClipboard()

  const [nativeClient, setNativeClient] = useState('')
  const [nativeEndpoint, setNativeEndpoint] = useState('')
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null)
  const [activeTab, setActiveTab] = useState('unix')
  const [activeClientTab, setActiveClientTab] = useState('claude')
  const [codexAuthMode, setCodexAuthMode] = useState<CodexAuthMode>('api-key')
  const [codexModelManifestState, setCodexModelManifestState] = useState<CodexModelManifestState>('idle')
  const [codexModelManifestContent, setCodexModelManifestContent] = useState('')
  const [codexModelManifestModelCount, setCodexModelManifestModelCount] = useState(0)
  const [codexModelCatalogMode, setCodexModelCatalogMode] = useState<'remote' | 'file'>('remote')
  const [codexModelManifestResponseBytes, setCodexModelManifestResponseBytes] = useState(0)

  const codexModelManifestController = useRef<AbortController | null>(null)
  const codexModelManifestRequestID = useRef(0)

  useEffect(() => {
    setNativeClient(defaultClient || '')
    setNativeEndpoint(defaultEndpoint || '')
  }, [show, defaultClient, defaultEndpoint])

  const codexModelCatalogOversized = codexModelManifestResponseBytes > 1024 * 1024
  const codexModelCatalogUrl = buildCodexModelCatalogUrl(baseUrl)
  const codexLocalCatalogToml =
    codexModelCatalogMode === 'file' ? `model_catalog_json = "${CODEX_MODEL_CATALOG_CONFIG_PATH}"\n` : ''

  const showCodexModelCatalog =
    show && (activeClientTab === 'codex' || (platform === 'openai' && activeClientTab === 'codex-ws'))

  const codexModelCatalogPath = (() => {
    const isWindows = activeTab === 'windows'
    const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
    return joinConfigPath(configDir, 'codex-models.json', isWindows)
  })()

  // Codex expands a leading ~/ on every platform but not %userprofile%, which it
  // resolves relative to the config directory, so config.toml always uses ~/.
  const codexManifestContext = showCodexModelCatalog ? `${platform}|${baseUrl}|${apiKey}` : ''

  const defaultClientTab = (() => {
    if (claudeCodeOnly) return 'claude'
    switch (platform) {
      case 'openai':
        return 'codex'
      case 'grok':
        return 'grok'
      case 'gemini':
        return 'gemini'
      case 'antigravity':
        return 'claude'
      case 'typesafe':
        return 'systemone'
      default:
        return 'claude'
    }
  })()

  // Reset tabs when platform changes
  useEffect(() => {
    setActiveTab('unix')
    setActiveClientTab(defaultClientTab)
    setCodexAuthMode('api-key')
  }, [platform, claudeCodeOnly, defaultClientTab])

  useEffect(() => {
    if (show) {
      setCodexAuthMode('api-key')
    } else {
      resetCodexModelManifest()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show])

  const resetManifest = useCallback(() => {
    codexModelManifestController.current?.abort()
    codexModelManifestController.current = null
    codexModelManifestRequestID.current += 1
    setCodexModelManifestState('idle')
    setCodexModelManifestContent('')
    setCodexModelManifestModelCount(0)
    setCodexModelManifestResponseBytes(0)
  }, [])

  useEffect(() => {
    resetManifest()
  }, [codexManifestContext, resetManifest])

  const resetCodexModelManifest = resetManifest

  // Reset shell tab when client changes
  useEffect(() => {
    setActiveTab('unix')
  }, [activeClientTab])

  async function loadCodexModelManifest() {
    if (!showCodexModelCatalog || !apiKey) return

    codexModelManifestController.current?.abort()
    const controller = new AbortController()
    const requestID = ++codexModelManifestRequestID.current
    codexModelManifestController.current = controller
    setCodexModelManifestState('loading')

    try {
      const result = await fetchCodexModelsManifest(baseUrl, apiKey, controller.signal)
      if (requestID !== codexModelManifestRequestID.current) return
      setCodexModelManifestContent(result.content)
      setCodexModelManifestModelCount(result.modelCount)
      setCodexModelManifestResponseBytes(result.responseBytes)
      if (result.responseBytes > 1024 * 1024) setCodexModelCatalogMode('file')
      setCodexModelManifestState('ready')
    } catch (error) {
      const errorName = error && typeof error === 'object' && 'name' in error ? String((error as { name?: unknown }).name || '') : ''
      if (requestID !== codexModelManifestRequestID.current || errorName === 'AbortError') return
      setCodexModelManifestState('error')
    } finally {
      if (requestID === codexModelManifestRequestID.current) {
        codexModelManifestController.current = null
      }
    }
  }

  function downloadCodexModelManifest() {
    if (!codexModelManifestContent) return
    saveAs(new Blob([codexModelManifestContent], { type: 'application/json;charset=utf-8' }), 'codex-models.json')
  }

  const codexCatalogModelSlugs = useMemo(
    () => parseCodexCatalogModels(codexModelManifestContent).map((model) => model.slug),
    [codexModelManifestContent]
  )

  function selectCodexCatalogModel(preferredModel: string): string {
    if (codexCatalogModelSlugs.includes(preferredModel)) return preferredModel
    return codexCatalogModelSlugs[0] || preferredModel
  }

  function codexReasoningEffortTomlLine(modelSlug: string): string {
    return formatCodexReasoningEffortTomlLine(
      selectCodexConfigReasoningEffort(findCodexCatalogModel(codexModelManifestContent, modelSlug))
    )
  }

  const escapeHtml = (value: string) =>
    value
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;')

  const wrapToken = (className: string, value: string) => `<span class="${className}">${escapeHtml(value)}</span>`
  const keyword = (value: string) => wrapToken('text-emerald-700 dark:text-emerald-300', value)
  const variable = (value: string) => wrapToken('text-sky-800 dark:text-sky-200', value)
  const operator = (value: string) => wrapToken('text-slate-600 dark:text-slate-400', value)
  const string = (value: string) => wrapToken('text-amber-800 dark:text-amber-200', value)
  const comment = (value: string) => wrapToken('text-slate-600 dark:text-slate-400', value)

  const clientTabs: TabConfig[] = (() => {
    if (!platform) return []
    if (claudeCodeOnly) {
      return [{ id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' }]
    }
    switch (platform) {
      case 'openai': {
        const tabs: TabConfig[] = [
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'codex-ws', label: t('keys.useKeyModal.cliTabs.codexCliWs'), icon: 'terminal' }
        ]
        if (allowMessagesDispatch) {
          tabs.push({ id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' })
        }
        tabs.push({ id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' })
        return tabs
      }
      case 'gemini':
        return [
          { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: 'sparkle' },
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' }
        ]
      case 'antigravity':
        return [
          { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' },
          { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: 'sparkle' },
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' }
        ]
      case 'grok':
        return [
          { id: 'grok', label: t('keys.useKeyModal.cliTabs.grokCli'), icon: 'terminal' },
          { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' },
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' }
        ]
      case 'typesafe':
        return [{ id: 'systemone', label: t('keys.useKeyModal.cliTabs.systemOne'), icon: 'terminal' }]
      case 'deepseek':
      case 'minimax':
        return [
          { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' },
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' }
        ]
      default:
        return [
          { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: 'terminal' },
          { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: 'terminal' },
          { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: 'terminal' }
        ]
    }
  })()

  const shellTabs: TabConfig[] = [
    { id: 'unix', label: 'macOS / Linux', icon: 'apple' },
    { id: 'cmd', label: 'Windows CMD', icon: 'windows' },
    { id: 'powershell', label: 'PowerShell', icon: 'windows' }
  ]

  const openaiTabs: TabConfig[] = [
    { id: 'unix', label: 'macOS / Linux', icon: 'apple' },
    { id: 'windows', label: 'Windows', icon: 'windows' }
  ]

  const showShellTabs = activeClientTab !== 'opencode'
  const showCodexAuthMode = platform === 'openai' && (activeClientTab === 'codex' || activeClientTab === 'codex-ws')

  const currentTabs = (() => {
    if (!showShellTabs) return []
    if (activeClientTab === 'codex' || activeClientTab === 'codex-ws' || activeClientTab === 'grok') {
      return openaiTabs
    }
    return shellTabs
  })()

  const platformDescription = (() => {
    if (
      activeClientTab === 'codex' &&
      platform !== 'openai' &&
      platform !== 'grok' &&
      platform !== 'deepseek' &&
      platform !== 'minimax' &&
      platform !== 'composite'
    ) {
      return t('keys.useKeyModal.routedCodex.description')
    }
    switch (platform) {
      case 'openai':
        if (activeClientTab === 'claude') return t('keys.useKeyModal.description')
        return t('keys.useKeyModal.openai.description')
      case 'gemini':
        return t('keys.useKeyModal.gemini.description')
      case 'antigravity':
        return t('keys.useKeyModal.antigravity.description')
      case 'grok':
        if (activeClientTab === 'claude') return t('keys.useKeyModal.grok.claudeDescription')
        if (activeClientTab === 'codex') return t('keys.useKeyModal.grok.codexDescription')
        return t('keys.useKeyModal.grok.description')
      case 'deepseek':
        return activeClientTab === 'codex'
          ? t('keys.useKeyModal.deepseek.codexDescription')
          : t('keys.useKeyModal.deepseek.description')
      case 'minimax':
        return activeClientTab === 'codex'
          ? t('keys.useKeyModal.minimax.codexDescription')
          : t('keys.useKeyModal.minimax.description')
      case 'typesafe':
        return t('keys.useKeyModal.typesafe.description')
      default:
        return t('keys.useKeyModal.description')
    }
  })()

  const platformNote = (() => {
    if (
      activeClientTab === 'codex' &&
      platform !== 'openai' &&
      platform !== 'grok' &&
      platform !== 'deepseek' &&
      platform !== 'minimax' &&
      platform !== 'composite'
    ) {
      return t('keys.useKeyModal.routedCodex.note')
    }
    switch (platform) {
      case 'openai':
        if (activeClientTab === 'claude') return t('keys.useKeyModal.note')
        return activeTab === 'windows' ? t('keys.useKeyModal.openai.noteWindows') : t('keys.useKeyModal.openai.note')
      case 'gemini':
        return t('keys.useKeyModal.gemini.note')
      case 'antigravity':
        return activeClientTab === 'claude'
          ? t('keys.useKeyModal.antigravity.claudeNote')
          : t('keys.useKeyModal.antigravity.geminiNote')
      case 'grok':
        if (activeClientTab === 'claude') return t('keys.useKeyModal.grok.claudeNote')
        if (activeClientTab === 'codex') {
          return activeTab === 'windows'
            ? t('keys.useKeyModal.grok.codexNoteWindows')
            : t('keys.useKeyModal.grok.codexNote')
        }
        if (activeClientTab === 'grok' && (activeTab === 'cmd' || activeTab === 'powershell')) {
          return t('keys.useKeyModal.grok.noteWindows')
        }
        if (activeClientTab === 'grok' && activeTab === 'windows') {
          return t('keys.useKeyModal.grok.noteWindows')
        }
        return t('keys.useKeyModal.grok.note')
      case 'deepseek':
        return activeClientTab === 'codex' ? t('keys.useKeyModal.deepseek.codexNote') : t('keys.useKeyModal.note')
      case 'minimax':
        return activeClientTab === 'codex' ? t('keys.useKeyModal.minimax.codexNote') : t('keys.useKeyModal.note')
      case 'typesafe':
        return t('keys.useKeyModal.typesafe.note')
      default:
        return t('keys.useKeyModal.note')
    }
  })()

  const showPlatformNote = activeClientTab !== 'opencode'

  function generateSystemOneCurl(baseUrl: string, key: string): FileConfig {
    const endpoint = `${baseUrl}/v1/systemone`
    const payload = `{
  "model": "jev-latest",
  "state": "Text to evaluate",
  "questions": {
    "safety": {
      "type": "noul",
      "instructions": "Evaluate whether the text is unsafe"
    }
  }
}`
    if (activeTab === 'powershell') {
      return {
        path: 'PowerShell',
        content: `$headers = @{ Authorization = "Bearer ${key}" }
$body = @'
${payload}
'@
Invoke-RestMethod -Method Post -Uri "${endpoint}" -Headers $headers -ContentType "application/json" -Body $body`
      }
    }
    if (activeTab === 'cmd') {
      return {
        path: 'Command Prompt',
        content: `curl -X POST "${endpoint}" ^
  -H "Authorization: Bearer ${key}" ^
  -H "Content-Type: application/json" ^
  --data "{\\"model\\":\\"jev-latest\\",\\"state\\":\\"Text to evaluate\\",\\"questions\\":{\\"safety\\":{\\"type\\":\\"noul\\",\\"instructions\\":\\"Evaluate whether the text is unsafe\\"}}}"`
      }
    }
    return {
      path: 'Terminal',
      content: `curl -X POST "${endpoint}" \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  --data '${payload}'`
    }
  }

  function generateAnthropicFiles(url: string, key: string): FileConfig[] {
    let path: string
    let content: string

    switch (activeTab) {
      case 'unix':
        path = 'Terminal'
        content = `export ANTHROPIC_BASE_URL="${url}"
export ANTHROPIC_AUTH_TOKEN="${key}"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
        break
      case 'cmd':
        path = 'Command Prompt'
        content = `set ANTHROPIC_BASE_URL=${url}
set ANTHROPIC_AUTH_TOKEN=${key}
set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
        break
      case 'powershell':
        path = 'PowerShell'
        content = `$env:ANTHROPIC_BASE_URL="${url}"
$env:ANTHROPIC_AUTH_TOKEN="${key}"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
        break
      default:
        path = 'Terminal'
        content = ''
    }

    const vscodeSettingsPath =
      activeTab === 'unix' ? '~/.claude/settings.json' : '%USERPROFILE%\\.claude\\settings.json'

    const vscodeContent = `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "ANTHROPIC_BASE_URL": "${url}",
    "ANTHROPIC_AUTH_TOKEN": "${key}",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}`

    return [
      { path, content },
      { path: vscodeSettingsPath, content: vscodeContent, hint: t('keys.useKeyModal.claudeSettingsHint') }
    ]
  }

  function generateGrokClaudeFiles(url: string, key: string): FileConfig[] {
    const environment: Record<string, string> = {
      ANTHROPIC_BASE_URL: url,
      ANTHROPIC_AUTH_TOKEN: key,
      ANTHROPIC_MODEL: 'grok-4.5',
      ANTHROPIC_DEFAULT_OPUS_MODEL: 'grok-4.5',
      ANTHROPIC_DEFAULT_SONNET_MODEL: 'grok-4.5',
      ANTHROPIC_DEFAULT_HAIKU_MODEL: 'grok-4.5',
      ANTHROPIC_DEFAULT_FABLE_MODEL: 'grok-4.5',
      CLAUDE_CODE_SUBAGENT_MODEL: 'grok-4.5',
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
    }
    let path: string
    let content: string

    switch (activeTab) {
      case 'unix':
        path = 'Terminal'
        content = Object.entries(environment)
          .map(([name, value]) => `export ${name}="${value}"`)
          .join('\n')
        break
      case 'cmd':
        path = 'Command Prompt'
        content = Object.entries(environment)
          .map(([name, value]) => `set ${name}=${value}`)
          .join('\n')
        break
      case 'powershell':
        path = 'PowerShell'
        content = Object.entries(environment)
          .map(([name, value]) => `$env:${name}="${value}"`)
          .join('\n')
        break
      default:
        path = 'Terminal'
        content = ''
    }

    const settingsPath =
      activeTab === 'unix' ? '~/.claude/settings.json' : '%USERPROFILE%\\.claude\\settings.json'

    return [
      { path, content },
      {
        path: settingsPath,
        content: JSON.stringify(
          { $schema: 'https://json.schemastore.org/claude-code-settings.json', env: environment },
          null,
          2
        ),
        hint: t('keys.useKeyModal.claudeSettingsHint')
      }
    ]
  }

  function generateGeminiCliContent(url: string, key: string): FileConfig {
    const model = 'gemini-2.0-flash'
    const modelComment = t('keys.useKeyModal.gemini.modelComment')
    let path: string
    let content: string
    let highlighted: string

    switch (activeTab) {
      case 'unix':
        path = 'Terminal'
        content = `export GOOGLE_GEMINI_BASE_URL="${url}"
export GEMINI_API_KEY="${key}"
export GEMINI_MODEL="${model}"  # ${modelComment}`
        highlighted = `${keyword('export')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${url}"`)}
${keyword('export')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${key}"`)}
${keyword('export')} ${variable('GEMINI_MODEL')}${operator('=')}${string(`"${model}"`)}  ${comment(`# ${modelComment}`)}`
        break
      case 'cmd':
        path = 'Command Prompt'
        content = `set GOOGLE_GEMINI_BASE_URL=${url}
set GEMINI_API_KEY=${key}
set GEMINI_MODEL=${model}`
        highlighted = `${keyword('set')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(url)}
${keyword('set')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(key)}
${keyword('set')} ${variable('GEMINI_MODEL')}${operator('=')}${string(model)}
${comment(`REM ${modelComment}`)}`
        break
      case 'powershell':
        path = 'PowerShell'
        content = `$env:GOOGLE_GEMINI_BASE_URL="${url}"
$env:GEMINI_API_KEY="${key}"
$env:GEMINI_MODEL="${model}"  # ${modelComment}`
        highlighted = `${keyword('$env:')}${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${url}"`)}
${keyword('$env:')}${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${key}"`)}
${keyword('$env:')}${variable('GEMINI_MODEL')}${operator('=')}${string(`"${model}"`)}  ${comment(`# ${modelComment}`)}`
        break
      default:
        path = 'Terminal'
        content = ''
        highlighted = ''
    }

    return { path, content, highlighted }
  }

  function generateCodexProviderAuthConfig(key: string): string {
    if (codexAuthMode === 'api-key') return buildCodexApiKeyAuthConfig(key)
    return 'requires_openai_auth = true'
  }

  function buildOpenAICodexFileConfigs(configDir: string, configContent: string, key: string): FileConfig[] {
    const files: FileConfig[] = [
      { path: `${configDir}/config.toml`, content: configContent, hint: t('keys.useKeyModal.openai.configTomlHint') }
    ]
    if (codexAuthMode === 'legacy') {
      files.push({ path: `${configDir}/auth.json`, content: JSON.stringify({ OPENAI_API_KEY: key }, null, 2) })
    }
    return files
  }

  function generateOpenAIFiles(url: string, key: string): FileConfig[] {
    const isWindows = activeTab === 'windows'
    const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
    const model = selectCodexCatalogModel('gpt-6.1-sol')
    const reasoningEffortLine = codexReasoningEffortTomlLine(model)

    const configContent = `model_provider = "OpenAI"
model = "${model}"
review_model = "${model}"
web_search = "live"
${reasoningEffortLine}disable_response_storage = true
${codexLocalCatalogToml}network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${url}"
${codexModelCatalogMode === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(url))}"\n` : ''}wire_api = "responses"
supports_websockets = false
${generateCodexProviderAuthConfig(key)}

[features]
${codexModelCatalogMode === 'remote' ? 'api_key_model_discovery = true\n' : ''}${codexAuthMode === 'api-key' ? 'image_generation = true\n' : ''}goals = true`

    return buildOpenAICodexFileConfigs(configDir, configContent, key)
  }

  function generateGrokFiles(url: string, key: string): FileConfig[] {
    // Prefer unix/cmd/powershell when shell tabs are shown; fall back to windows tab.
    const shell = activeTab
    const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
    const configDir = isWindowsPath ? '%userprofile%\\.grok' : '~/.grok'

    let envPath: string
    let envContent: string
    switch (shell) {
      case 'cmd':
        envPath = 'Command Prompt'
        envContent = `set GROK_MODELS_BASE_URL=${url}
set XAI_API_KEY=${key}`
        break
      case 'powershell':
      case 'windows':
        envPath = 'PowerShell'
        envContent = `$env:GROK_MODELS_BASE_URL="${url}"
$env:XAI_API_KEY="${key}"`
        break
      default:
        envPath = 'Terminal'
        envContent = `export GROK_MODELS_BASE_URL="${url}"
export XAI_API_KEY="${key}"`
    }

    // Shape follows Grok Build user guide (~/.grok/docs + custom-models) and production-ready Sub2API setups.
    // Text models only (Responses). Image/video: Imagine model IDs on media endpoints / feature overrides.
    // Credential order: api_key field → env_key → signed-in session → XAI_API_KEY global fallback.
    const modelsListUrl = `${url.replace(/\/+$/, '')}/models`
    const configContent = `# Grok Build CLI → Sub2API Grok group (API key auth).
# Docs: ~/.grok/docs/user-guide/05-configuration.md + 11-custom-models.md
# Verify after save: grok inspect
#
# IMPORTANT: api_backend must be "responses" for Sub2API Grok (POST /v1/responses).
# If omitted, Grok Build defaults to chat_completions (/v1/chat/completions).
# Keep api_backend = "responses" on every model entry.
#
# Prefer env_key over hardcoding api_key (never commit secrets).
# Also export GROK_MODELS_BASE_URL + XAI_API_KEY in the shell block above.

# Global inference / catalog endpoints (same role as env GROK_MODELS_BASE_URL).
# When models_base_url is set, Grok uses API-key Bearer auth (no grok login required).
[endpoints]
models_base_url = "${url}"              # inference base; model list defaults to {base}/models
models_list_url = "${modelsListUrl}"        # optional override (env: GROK_MODELS_LIST_URL)
xai_api_base_url = "${url}"             # public xAI API base override for gateway routing
cli_chat_proxy_base_url = "${url}"      # CLI chat-proxy base (env: GROK_CLI_CHAT_PROXY_BASE_URL)

# Prefer API key when using a custom gateway (matches Sub2API).
# Requires XAI_API_KEY env or per-model env_key / api_key.
[auth]
preferred_method = "api_key"

[model."grok-4.5"]
model = "grok-4.5"                          # id sent to the API
name = "Grok 4.5"                           # shown in /model picker
description = "Grok 4.5 via Sub2API Plus (Responses)"
# base_url inherits from [endpoints].models_base_url; override only if needed:
# base_url = "${url}"
env_key = "XAI_API_KEY"                     # or: api_key = "${key}"  (not recommended)
api_backend = "responses"                   # chat_completions | responses | messages
context_window = 500000                     # drives auto-compaction timing
# Optional sampling (global defaults can live under [models] instead):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# Server-side (backend) web_search tools — only if your gateway exposes them:
supports_backend_search = true

[model."grok-build-0.1"]
model = "grok-build-0.1"
name = "Grok Build"
description = "Coding / agent sessions (xAI recommends grok-build* for coding)"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 256000
supports_backend_search = true

# Text multi-agent / client web_search sub-agent (NOT Imagine image/video).
[model."grok-4.20-multi-agent-0309"]
model = "grok-4.20-multi-agent-0309"
name = "Grok 4.20 Multi Agent (text / web_search)"
description = "Text multi-agent; use for web_search sub-agent, not image/video"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

[model."grok-4.3"]
model = "grok-4.3"
name = "Grok 4.3"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

# Optional short alias for /model grok:
# [model."grok"]
# model = "grok-4.5"
# name = "Grok"
# env_key = "XAI_API_KEY"
# api_backend = "responses"
# context_window = 1000000
# supports_backend_search = true

[models]
# xAI recommends grok-build* for coding/agent sessions; use grok-4.5 for general chat.
default = "grok-4.5"
web_search = "grok-4.5"                     # client-side web_search tool model (must exist as [model.*])
image_description = "grok-4.5"              # vision/describe-image helper model
# Optional environment-wide sampling defaults (per-model values win):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# max_retries = 8

[session]
auto_compact_threshold_percent = 80         # auto-compact at this % of context_window (default 85)

# Imagine tools: model IDs go to Sub2API media endpoints (not the text [model.*] catalog).
# Enable only if the Grok group allows image/video generation.
[features]
image_gen = true
video_gen = true
image_gen_model_override = "grok-imagine-image-quality"   # or grok-imagine-image
image_edit_model_override = "grok-imagine-edit"
# Optional feature flags (defaults shown in docs):
# telemetry = false
# remote_fetch = true                         # set false for air-gapped / pure-gateway catalogs
# lsp_tools = false`

    return [
      { path: envPath, content: envContent },
      { path: joinConfigPath(configDir, 'config.toml', isWindowsPath), content: configContent, hint: t('keys.useKeyModal.grok.configTomlHint') }
    ]
  }

  function generateGrokCodexFiles(url: string, key: string): FileConfig[] {
    // Codex config reference: wire_api = "responses" only; prefer env_key over experimental_bearer_token.
    // Non-OpenAI gateways should set supports_websockets = false (HTTP/SSE).
    const shell = activeTab
    const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
    const configDir = isWindowsPath ? '%userprofile%\\.codex' : '~/.codex'
    const model = selectCodexCatalogModel('grok-4.5')

    let envPath: string
    let envContent: string
    switch (shell) {
      case 'cmd':
        envPath = 'Command Prompt'
        envContent = `set SUB2API_API_KEY=${key}`
        break
      case 'powershell':
      case 'windows':
        envPath = 'PowerShell'
        envContent = `$env:SUB2API_API_KEY="${key}"`
        break
      default:
        envPath = 'Terminal'
        envContent = `export SUB2API_API_KEY="${key}"`
    }

    const configContent = `# Codex CLI → Sub2API Plus Grok group
# Docs: Codex config reference (model_providers.*, wire_api = "responses")
#
# Text models only. Image/video: grok-imagine-image / grok-imagine-video on media endpoints.
# Switch model: grok-4.5 | grok-4.3 | grok-build-0.1 | grok-4.20-multi-agent-0309 (text / web_search)

model_provider = "sub2api"
model = "${model}"
${codexLocalCatalogToml}# Optional:
# review_model = "${model}"
# model_reasoning_effort = "medium"
# model_context_window = 500000
# disable_response_storage = true
# network_access = "enabled"
# windows_wsl_setup_acknowledged = true

[model_providers.sub2api]
name = "Sub2API Plus Grok"
base_url = "${url}"
${codexModelCatalogMode === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(url))}"\n` : ''}# Prefer env_key (variable NAME). Do not combine with experimental_bearer_token.
env_key = "SUB2API_API_KEY"
# Fallback only if you cannot set env (discouraged — keeps secret on disk):
# experimental_bearer_token = "${key}"
wire_api = "responses"
# API-key providers: do not require ChatGPT OAuth login
requires_openai_auth = false
# Grok/Sub2API path is HTTP/SSE; disable WS (Codex may otherwise try WebSocket first)
supports_websockets = false

${codexModelCatalogMode === 'remote' ? '[features]\napi_key_model_discovery = true\n\n# Optional:' : '# Optional:\n# [features]'}
# goals = true`

    return [
      { path: envPath, content: envContent },
      { path: joinConfigPath(configDir, 'config.toml', isWindowsPath), content: configContent, hint: t('keys.useKeyModal.grok.codexConfigTomlHint') }
    ]
  }

  function generateRoutedCodexFiles(url: string, key: string, routedPlatform: GroupPlatform): FileConfig[] {
    const isWindows = activeTab === 'windows'
    const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
    const preferredModels: Partial<Record<GroupPlatform, string>> = {
      openai: 'gpt-6.1-sol',
      anthropic: 'claude-sonnet-4-6',
      gemini: 'gemini-2.5-pro',
      antigravity: 'claude-sonnet-4-6',
      grok: 'grok-4.5',
      kimi: 'kimi-k2.5',
      zhipu: 'glm-4.7',
      deepseek: 'deepseek-v4-pro',
      minimax: 'MiniMax-M3',
      stepfun: '<model-id>',
      opencode_go: 'glm-5.3',
      composite: 'MODEL',
      custom: 'MODEL'
    }
    const preferredModel = preferredModels[routedPlatform] || ''
    const model = selectCodexCatalogModel(preferredModel)
    const labels: Record<GroupPlatform, string> = {
      anthropic: 'Anthropic',
      openai: 'OpenAI',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      grok: 'Grok',
      kimi: 'Kimi',
      zhipu: 'Zhipu',
      deepseek: 'DeepSeek',
      minimax: 'MiniMax',
      stepfun: 'StepFun',
      opencode_go: 'OpenCode',
      typesafe: 'TypeSafe / Jev',
      command_code: 'Command Code',
      cline: 'Cline',
      composite: 'Composite',
      custom: 'Custom'
    }
    const label = labels[routedPlatform]
    const envContent = isWindows ? `$env:SUB2API_API_KEY="${key}"` : `export SUB2API_API_KEY="${key}"`

    const configContent = `# Codex CLI -> Sub2API ${label} group
model_provider = "sub2api"
model = "${model}"
review_model = "${model}"
disable_response_storage = true
${codexLocalCatalogToml}
[model_providers.sub2api]
name = "Sub2API Plus ${label}"
base_url = "${url}"
${codexModelCatalogMode === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(url))}"\n` : ''}env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false${codexModelCatalogMode === 'remote' ? '\n\n[features]\napi_key_model_discovery = true' : ''}`

    return [
      { path: isWindows ? 'PowerShell' : 'Terminal', content: envContent },
      {
        path: joinConfigPath(configDir, 'config.toml', isWindows),
        content: configContent,
        hint: t(
          routedPlatform === 'deepseek' || routedPlatform === 'minimax' || routedPlatform === 'composite'
            ? `keys.useKeyModal.${routedPlatform}.codexConfigTomlHint`
            : 'keys.useKeyModal.routedCodex.configTomlHint'
        )
      }
    ]
  }

  function generateOpenAIWsFiles(url: string, key: string): FileConfig[] {
    const isWindows = activeTab === 'windows'
    const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
    const model = selectCodexCatalogModel('gpt-6.1-sol')
    const reasoningEffortLine = codexReasoningEffortTomlLine(model)

    const configContent = `model_provider = "OpenAI"
model = "${model}"
review_model = "${model}"
web_search = "live"
${reasoningEffortLine}disable_response_storage = true
${codexLocalCatalogToml}network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${url}"
${codexModelCatalogMode === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(url))}"\n` : ''}wire_api = "responses"
supports_websockets = true
${generateCodexProviderAuthConfig(key)}

[features]
${codexModelCatalogMode === 'remote' ? 'api_key_model_discovery = true\n' : ''}${codexAuthMode === 'api-key' ? 'image_generation = true\n' : ''}responses_websockets_v2 = true
goals = true`

    return buildOpenAICodexFileConfigs(configDir, configContent, key)
  }

  function generateOpenCodeConfig(
    opencodePlatform: string,
    url: string,
    key: string,
    pathLabel?: string
  ): FileConfig {
    const provider: Record<string, any> = {
      [opencodePlatform]: {
        options: {
          baseURL: url,
          apiKey: key
        }
      }
    }

    if (opencodePlatform === 'gemini') {
      provider[opencodePlatform].npm = '@ai-sdk/google'
      provider[opencodePlatform].models = buildGeminiModels()
    } else if (opencodePlatform === 'anthropic') {
      provider[opencodePlatform].npm = '@ai-sdk/anthropic'
      provider[opencodePlatform].models = buildClaudeModels()
    } else if (opencodePlatform === 'antigravity-claude') {
      provider[opencodePlatform].npm = '@ai-sdk/anthropic'
      provider[opencodePlatform].name = 'Antigravity (Claude)'
      provider[opencodePlatform].models = buildClaudeModels()
    } else if (opencodePlatform === 'antigravity-gemini') {
      provider[opencodePlatform].npm = '@ai-sdk/google'
      provider[opencodePlatform].name = 'Antigravity (Gemini)'
      provider[opencodePlatform].models = buildGeminiModels()
    } else if (opencodePlatform === 'openai') {
      provider[opencodePlatform].models = buildOpenAIModels()
    } else if (opencodePlatform === 'grok') {
      // Custom provider pointing at Sub2API OpenAI-compatible Responses/Chat endpoints.
      provider[opencodePlatform].npm = '@ai-sdk/openai-compatible'
      provider[opencodePlatform].name = 'Grok via Sub2API Plus'
      provider[opencodePlatform].models = buildGrokModels()
    }

    const agent =
      opencodePlatform === 'openai'
        ? {
            build: { options: { store: false } },
            plan: { options: { store: false } }
          }
        : undefined

    const content = JSON.stringify(
      { provider, ...(agent ? { agent } : {}), $schema: 'https://opencode.ai/config.json' },
      null,
      2
    )

    return { path: pathLabel ?? 'opencode.json', content, hint: t('keys.useKeyModal.opencode.hint') }
  }

  const currentFiles: FileConfig[] = (() => {
    const resolvedBaseUrl = baseUrl || window.location.origin
    const resolvedApiKey = apiKey
    const baseRoot = resolvedBaseUrl.replace(/\/v1\/?$/, '').replace(/\/+$/, '')
    const ensureV1 = (value: string) => {
      const trimmed = value.replace(/\/+$/, '')
      return trimmed.endsWith('/v1') ? trimmed : `${trimmed}/v1`
    }
    const apiBase = ensureV1(baseRoot)
    const antigravityBase = ensureV1(`${baseRoot}/antigravity`)
    const antigravityGeminiBase = (() => {
      const trimmed = `${baseRoot}/antigravity`.replace(/\/+$/, '')
      return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
    })()
    const geminiBase = (() => {
      const trimmed = baseRoot.replace(/\/+$/, '')
      return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
    })()

    if (activeClientTab === 'opencode') {
      switch (platform) {
        case 'anthropic':
          return [generateOpenCodeConfig('anthropic', apiBase, resolvedApiKey)]
        case 'openai':
          return [generateOpenCodeConfig('openai', apiBase, resolvedApiKey)]
        case 'gemini':
          return [generateOpenCodeConfig('gemini', geminiBase, resolvedApiKey)]
        case 'antigravity':
          return [
            generateOpenCodeConfig('antigravity-claude', antigravityBase, resolvedApiKey, 'opencode.json (Claude)'),
            generateOpenCodeConfig('antigravity-gemini', antigravityGeminiBase, resolvedApiKey, 'opencode.json (Gemini)')
          ]
        case 'grok':
          return [generateOpenCodeConfig('grok', apiBase, resolvedApiKey)]
        default:
          return [generateOpenCodeConfig('openai', apiBase, resolvedApiKey)]
      }
    }

    switch (platform) {
      case 'typesafe':
        return [generateSystemOneCurl(baseRoot, resolvedApiKey)]
      case 'openai':
        if (activeClientTab === 'claude') return generateAnthropicFiles(resolvedBaseUrl, resolvedApiKey)
        if (activeClientTab === 'codex-ws') return generateOpenAIWsFiles(apiBase, resolvedApiKey)
        return generateOpenAIFiles(apiBase, resolvedApiKey)
      case 'gemini':
        if (activeClientTab === 'codex') return generateRoutedCodexFiles(apiBase, resolvedApiKey, 'gemini')
        return [generateGeminiCliContent(resolvedBaseUrl, resolvedApiKey)]
      case 'antigravity':
        if (activeClientTab === 'codex') return generateRoutedCodexFiles(apiBase, resolvedApiKey, 'antigravity')
        if (activeClientTab === 'gemini') return [generateGeminiCliContent(`${resolvedBaseUrl}/antigravity`, resolvedApiKey)]
        return generateAnthropicFiles(`${resolvedBaseUrl}/antigravity`, resolvedApiKey)
      case 'grok':
        if (activeClientTab === 'claude') return generateGrokClaudeFiles(baseRoot, resolvedApiKey)
        if (activeClientTab === 'codex') return generateGrokCodexFiles(apiBase, resolvedApiKey)
        return generateGrokFiles(apiBase, resolvedApiKey)
      case 'deepseek':
        if (activeClientTab === 'codex') return generateRoutedCodexFiles(apiBase, resolvedApiKey, 'deepseek')
        return generateAnthropicFiles(baseRoot, resolvedApiKey)
      case 'minimax':
        if (activeClientTab === 'codex') return generateRoutedCodexFiles(apiBase, resolvedApiKey, 'minimax')
        return generateAnthropicFiles(baseRoot, resolvedApiKey)
      default:
        if (activeClientTab === 'codex' && platform) {
          return generateRoutedCodexFiles(apiBase, resolvedApiKey, platform)
        }
        return generateAnthropicFiles(resolvedBaseUrl, resolvedApiKey)
    }
  })()

  const copyContent = async (content: string, index: number) => {
    const success = await clipboardCopy(content, t('keys.copied'))
    if (success) {
      setCopiedIndex(index)
      setTimeout(() => setCopiedIndex(null), 2000)
    }
  }

  return (
    <Modal open={show} onClose={onClose} title={t('keys.useKeyModal.title')} size="lg"
      footer={
        <div className="flex justify-end">
          <button type="button" className="btn btn-secondary" onClick={onClose}>
            {t('common.close')}
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        {!platform && (
          <div className="flex items-start gap-3 rounded-lg border border-yellow-200 bg-yellow-50/[0.15] p-4 dark:border-yellow-800 dark:bg-yellow-900/20">
            <svg className="mt-0.5 h-5 w-5 flex-shrink-0 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
            </svg>
            <div>
              <p className="text-sm font-medium text-yellow-800 dark:text-yellow-200">{t('keys.useKeyModal.noGroupTitle')}</p>
              <p className="mt-1 text-sm text-yellow-700 dark:text-yellow-300">{t('keys.useKeyModal.noGroupDescription')}</p>
            </div>
          </div>
        )}

        {platform === 'composite' ? (
          <NativeAccessFields
            editable={false}
            conversionEnabled={conversionEnabled || false}
            client={nativeClient}
            endpoint={nativeEndpoint}
            onClientChange={setNativeClient}
            onEndpointChange={setNativeEndpoint}
            groupId={groupId || null}
            baseUrl={baseUrl}
            apiKey={apiKey}
          />
        ) : (
          <>
            <p className="text-sm surface-muted">{platformDescription}</p>

            {clientTabs.length > 0 && (
              <div className="overflow-x-auto py-0.5">
                <nav className="card-tabs min-w-max" role="tablist" aria-label="Client">
                  {clientTabs.map((tab) => (
                    <button
                      key={tab.id}
                      type="button"
                      role="tab"
                      className="card-tab"
                      aria-selected={activeClientTab === tab.id}
                      onClick={() => setActiveClientTab(tab.id)}
                    >
                      <span className="flex items-center gap-2">
                        <TabIcon icon={tab.icon} />
                        {tab.label}
                      </span>
                    </button>
                  ))}
                </nav>
              </div>
            )}

            {platform === 'openai' && (activeClientTab === 'codex' || activeClientTab === 'codex-ws') && (
              <p className="text-xs surface-muted" data-testid="codex-transport-hint">
                {t(
                  activeClientTab === 'codex-ws'
                    ? 'keys.useKeyModal.openai.wsTransportHint'
                    : 'keys.useKeyModal.openai.httpTransportHint'
                )}
              </p>
            )}

            {showCodexAuthMode && (
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
                <div className="mb-2">
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('keys.useKeyModal.openai.authModeTitle')}</p>
                  <p className="mt-0.5 text-xs surface-muted">{t('keys.useKeyModal.openai.authModeDescription')}</p>
                </div>
                <div
                  className="grid grid-cols-2 gap-1 rounded-lg surface-inset p-1"
                  role="radiogroup"
                  aria-label={t('keys.useKeyModal.openai.authModeTitle')}
                >
                  <button
                    type="button"
                    role="radio"
                    data-testid="codex-auth-mode-legacy"
                    aria-checked={codexAuthMode === 'legacy'}
                    className={`rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                      codexAuthMode === 'legacy'
                        ? 'surface-inset text-primary-700 shadow-sm dark:surface-inset dark:text-primary-300'
                        : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                    }`}
                    onClick={() => setCodexAuthMode('legacy')}
                  >
                    {t('keys.useKeyModal.openai.authModeLegacy')}
                  </button>
                  <button
                    type="button"
                    role="radio"
                    data-testid="codex-auth-mode-api-key"
                    aria-checked={codexAuthMode === 'api-key'}
                    className={`rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                      codexAuthMode === 'api-key'
                        ? 'surface-inset text-primary-700 shadow-sm dark:surface-inset dark:text-primary-300'
                        : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                    }`}
                    onClick={() => setCodexAuthMode('api-key')}
                  >
                    {t('keys.useKeyModal.openai.authModeApiKey')}
                  </button>
                </div>
                {codexAuthMode === 'api-key' && (
                  <div
                    data-testid="codex-api-key-restart-notice"
                    className="mt-3 flex items-start gap-2 rounded-xl border border-amber-400 bg-amber-50/[0.15] px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-500 dark:bg-amber-950/30 dark:text-amber-200"
                  >
                    <Icon name="exclamationCircle" size="sm" className="mt-0.5 flex-shrink-0" />
                    <p>{t('keys.useKeyModal.openai.authModeApiKeyRestartNotice')}</p>
                  </div>
                )}
              </div>
            )}

            {showShellTabs && (
              <div className="overflow-x-auto py-0.5">
                <nav className="card-tabs min-w-max" role="tablist" aria-label="Tabs">
                  {currentTabs.map((tab) => (
                    <button
                      key={tab.id}
                      type="button"
                      role="tab"
                      className="card-tab"
                      aria-selected={activeTab === tab.id}
                      onClick={() => setActiveTab(tab.id)}
                    >
                      <span className="flex items-center gap-2">
                        <TabIcon icon={tab.icon} />
                        {tab.label}
                      </span>
                    </button>
                  ))}
                </nav>
              </div>
            )}

            <div className="space-y-4">
              {currentFiles.map((file, index) => (
                <div key={index} className="relative">
                  {file.hint && (
                    <p className="mb-1.5 flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
                      <Icon name="exclamationCircle" size="sm" className="flex-shrink-0" />
                      {file.hint}
                    </p>
                  )}
                  <div className="surface-inset overflow-hidden rounded-xl">
                    <div className="surface-inset flex items-center justify-between border-b border-gray-700 px-4 py-2 dark:border-dark-700">
                      <span className="min-w-0 truncate font-mono text-xs surface-muted">{file.path}</span>
                      <button
                        type="button"
                        onClick={() => void copyContent(file.content, index)}
                        className={`surface-control flex flex-shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${
                          copiedIndex === index
                            ? 'bg-green-500/20 text-green-800 dark:text-green-400'
                            : 'surface-field text-gray-700 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-600 dark:hover:text-white'
                        }`}
                      >
                        {copiedIndex === index ? (
                          <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2">
                            <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                          </svg>
                        ) : (
                          <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5">
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184"
                            />
                          </svg>
                        )}
                        {copiedIndex === index ? t('keys.useKeyModal.copied') : t('keys.useKeyModal.copy')}
                      </button>
                    </div>
                    <pre className="overflow-x-auto p-4 font-mono text-sm text-gray-900 dark:text-gray-100">
                      {file.highlighted ? (
                        <code dangerouslySetInnerHTML={{ __html: file.highlighted }} />
                      ) : (
                        <code>{file.content}</code>
                      )}
                    </pre>
                  </div>
                </div>
              ))}
            </div>

            {showCodexModelCatalog && (
              <section
                data-testid="codex-model-catalog"
                className="surface-inset overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700"
              >
                <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0">
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white">
                      {t('keys.useKeyModal.codexModelCatalog.title')}
                    </h3>
                    <p className="mt-1 text-xs surface-muted">{t('keys.useKeyModal.codexModelCatalog.description')}</p>
                    <p className="mt-1 truncate font-mono text-xs surface-muted">
                      {codexModelCatalogMode === 'remote' ? codexModelCatalogUrl : codexModelCatalogPath}
                    </p>
                    <Select
                      value={codexModelCatalogMode}
                      data-testid="codex-model-catalog-mode"
                      ariaLabel={t('keys.useKeyModal.codexModelCatalog.mode')}
                      className="mt-2"
                      triggerClass="text-sm"
                      searchable={false}
                      options={[
                        { value: 'remote', label: t('keys.useKeyModal.codexModelCatalog.remote'), disabled: codexModelCatalogOversized },
                        { value: 'file', label: t('keys.useKeyModal.codexModelCatalog.local') }
                      ]}
                      onValueChange={(value) => setCodexModelCatalogMode(value === 'file' ? 'file' : 'remote')}
                    />
                    {codexModelCatalogOversized && (
                      <p className="mt-2 text-xs text-amber-700 dark:text-amber-300">
                        {t('keys.useKeyModal.codexModelCatalog.oversized')}
                      </p>
                    )}
                  </div>
                  {codexModelManifestState === 'ready' ? (
                    <button
                      type="button"
                      className="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
                      onClick={downloadCodexModelManifest}
                    >
                      <Icon name="download" size="sm" className="mr-1.5" />
                      {t('keys.useKeyModal.codexModelCatalog.download')}
                    </button>
                  ) : (
                    <button
                      type="button"
                      data-testid="codex-model-catalog-fetch"
                      className="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
                      disabled={codexModelManifestState === 'loading' || !apiKey}
                      onClick={() => void loadCodexModelManifest()}
                    >
                      <Icon
                        name="refresh"
                        size="sm"
                        className={`mr-1.5 ${codexModelManifestState === 'loading' ? 'animate-spin' : ''}`}
                      />
                      {codexModelManifestState === 'error'
                        ? t('keys.useKeyModal.codexModelCatalog.retry')
                        : t('keys.useKeyModal.codexModelCatalog.fetch')}
                    </button>
                  )}
                </div>
                {codexModelManifestState === 'ready' ? (
                  <p className="border-t border-gray-200 px-4 py-2 text-xs text-emerald-700 dark:border-dark-700 dark:text-emerald-300">
                    {t('keys.useKeyModal.codexModelCatalog.modelsCount', { count: codexModelManifestModelCount })}
                  </p>
                ) : codexModelManifestState === 'error' ? (
                  <p className="border-t border-red-200 px-4 py-2 text-xs text-red-700 dark:border-red-900 dark:text-red-300">
                    {t('keys.useKeyModal.codexModelCatalog.errorDescription')}
                  </p>
                ) : null}
              </section>
            )}

            {showPlatformNote && (
              <div className="flex items-start gap-3 rounded-lg border border-blue-100 bg-blue-50/[0.15] p-3 dark:border-blue-800 dark:bg-blue-900/20">
                <Icon name="infoCircle" size="md" className="mt-0.5 flex-shrink-0 text-blue-500" />
                <p className="text-sm text-blue-700 dark:text-blue-300">{platformNote}</p>
              </div>
            )}
          </>
        )}
      </div>
    </Modal>
  )
}

function joinConfigPath(dir: string, file: string, windows: boolean): string {
  if (!windows) return `${dir}/${file}`
  return `${dir}\\${file}`
}

function escapeTomlBasicString(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

export default UseKeyModal
