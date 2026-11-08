import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, toast } from '@/components/ui'
import {
  adminAPI,
  type PluginInstallation,
  type PluginUISession
} from '@/api/admin'
import { TotpStepUpDialog } from '@/components/auth/TotpStepUpDialog'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/components/auth/useStepUp'
import { Icon } from '@/views/admin/AccountIcon'

interface PluginBridgeMessage {
  source?: string
  bridge_token?: string
  type?: string
  request_id?: string
  config?: unknown
  height?: unknown
  level?: unknown
  message?: unknown
}

export default function PluginsView() {
  const { t } = useTranslation()
  const pluginStepUp = useStepUp()

  const [plugins, setPlugins] = useState<PluginInstallation[]>([])
  const [loading, setLoading] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [busyID, setBusyID] = useState<number | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const [rolloutValues, setRolloutValues] = useState<Record<number, number>>({})
  const [configPlugin, setConfigPlugin] = useState<PluginInstallation | null>(null)
  const [uiSession, setUiSession] = useState<PluginUISession | null>(null)
  const pluginFrame = useRef<HTMLIFrameElement>(null)
  const [uiLoading, setUiLoading] = useState(false)
  const [uiError, setUiError] = useState('')
  const [iframeHeight, setIframeHeight] = useState(640)
  const [pluginFrameLoaded, setPluginFrameLoaded] = useState(false)
  const pendingBridgeRequests = useRef(new Map<string, number>())
  const uiSessionVersion = useRef(0)

  const sessionRef = useRef<{ uiSession: PluginUISession | null; configPlugin: PluginInstallation | null }>({
    uiSession: null,
    configPlugin: null
  })
  sessionRef.current = { uiSession, configPlugin }

  function errorMessage(error: unknown): string {
    if (typeof error === 'object' && error !== null && 'message' in error) {
      return String((error as { message?: unknown }).message || t('common.unknownError'))
    }
    return t('common.unknownError')
  }

  function reportSensitiveActionError(error: unknown): void {
    if (isStepUpCancelled(error)) return
    if (isStepUpBlocked(error)) {
      toast.error(
        stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled')
      )
      return
    }
    toast.error(errorMessage(error))
  }

  async function loadPlugins(): Promise<void> {
    setLoading(true)
    try {
      const list = await adminAPI.plugins.list()
      setPlugins(list)
      const next: Record<number, number> = {}
      for (const plugin of list) next[plugin.id] = currentRollout(plugin)
      setRolloutValues(next)
    } catch (error: unknown) {
      toast.error(errorMessage(error))
    } finally {
      setLoading(false)
    }
  }

  async function handleFileSelected(event: React.ChangeEvent<HTMLInputElement>): Promise<void> {
    const target = event.target
    const file = target.files?.[0]
    target.value = ''
    if (!file || !file.name.toLowerCase().endsWith('.s2plugin')) {
      toast.error(t('admin.plugins.fileRequired'))
      return
    }
    setUploading(true)
    try {
      await pluginStepUp.run(() => adminAPI.plugins.upload(file))
      toast.success(t('admin.plugins.uploadSuccess'))
      await loadPlugins()
    } catch (error: unknown) {
      reportSensitiveActionError(error)
    } finally {
      setUploading(false)
    }
  }

  function currentRollout(plugin: PluginInstallation): number {
    return (
      plugin.bindings.find((binding) => binding.capability === 'openai.oauth.outbound_transport.v1')
        ?.rollout_percent || 100
    )
  }

  function hasEnabledBinding(plugin: PluginInstallation): boolean {
    return plugin.bindings.some((binding) => binding.enabled)
  }

  function setRollout(id: number, value: number): void {
    setRolloutValues((previous) => ({ ...previous, [id]: Math.min(100, Math.max(1, value)) }))
  }

  async function enablePlugin(plugin: PluginInstallation): Promise<void> {
    let acceptUntested = false
    if (!plugin.compatibility.tested) {
      acceptUntested = window.confirm(t('admin.plugins.confirmUntested'))
      if (!acceptUntested) return
    }
    setBusyID(plugin.id)
    try {
      await pluginStepUp.run(() =>
        adminAPI.plugins.enable(plugin.id, rolloutValues[plugin.id] || 100, acceptUntested)
      )
      toast.success(t('admin.plugins.enableSuccess'))
      await loadPlugins()
    } catch (error: unknown) {
      reportSensitiveActionError(error)
    } finally {
      setBusyID(null)
    }
  }

  async function disablePlugin(plugin: PluginInstallation): Promise<void> {
    if (!window.confirm(t('admin.plugins.confirmDisable'))) return
    setBusyID(plugin.id)
    try {
      await pluginStepUp.run(() => adminAPI.plugins.disable(plugin.id))
      toast.success(t('admin.plugins.disableSuccess'))
      await loadPlugins()
    } catch (error: unknown) {
      reportSensitiveActionError(error)
    } finally {
      setBusyID(null)
    }
  }

  async function uninstallPlugin(plugin: PluginInstallation): Promise<void> {
    if (!window.confirm(t('admin.plugins.confirmUninstall'))) return
    setBusyID(plugin.id)
    try {
      await pluginStepUp.run(() => adminAPI.plugins.remove(plugin.id))
      toast.success(t('admin.plugins.uninstallSuccess'))
      await loadPlugins()
    } catch (error: unknown) {
      reportSensitiveActionError(error)
    } finally {
      setBusyID(null)
    }
  }

  async function testPlugin(plugin: PluginInstallation): Promise<void> {
    setBusyID(plugin.id)
    try {
      const result = await pluginStepUp.run(() => adminAPI.plugins.test(plugin.id))
      if (result.success) toast.success(result.message || t('admin.plugins.testSuccess'))
      else toast.error(result.message || t('common.error'))
    } catch (error: unknown) {
      reportSensitiveActionError(error)
    } finally {
      setBusyID(null)
    }
  }

  function clearPendingBridgeRequests(): void {
    for (const timeout of pendingBridgeRequests.current.values()) window.clearTimeout(timeout)
    pendingBridgeRequests.current.clear()
  }

  async function openConfiguration(plugin: PluginInstallation): Promise<void> {
    const version = ++uiSessionVersion.current
    setConfigPlugin(plugin)
    setUiSession(null)
    setPluginFrameLoaded(false)
    clearPendingBridgeRequests()
    setUiLoading(true)
    setUiError('')
    setIframeHeight(640)
    try {
      const session = await adminAPI.plugins.createUISession(plugin.id)
      if (version !== uiSessionVersion.current) return
      setUiSession(session)
    } catch (error: unknown) {
      if (version !== uiSessionVersion.current) return
      setUiLoading(false)
      setUiError(errorMessage(error))
    }
  }

  function closeConfiguration(): void {
    uiSessionVersion.current += 1
    clearPendingBridgeRequests()
    setPluginFrameLoaded(false)
    setConfigPlugin(null)
    setUiSession(null)
    setUiLoading(false)
    setUiError('')
  }

  function handlePluginFrameLoad(): void {
    // A load can also be caused by a plugin navigating its iframe. Drop all
    // outstanding responses so a late config response is never sent to the new document.
    if (pluginFrameLoaded) clearPendingBridgeRequests()
    setPluginFrameLoaded(true)
    setUiLoading(false)
  }

  function registerBridgeRequest(requestID: string): void {
    const timeout = window.setTimeout(() => {
      pendingBridgeRequests.current.delete(requestID)
    }, 30_000)
    pendingBridgeRequests.current.set(requestID, timeout)
  }

  function postBridgeResult(request: PluginBridgeMessage, payload: Record<string, unknown>): void {
    const frame = pluginFrame.current
    const session = sessionRef.current.uiSession
    if (!frame?.contentWindow || !session) return
    const requestID = typeof request.request_id === 'string' ? request.request_id.trim() : ''
    const timeout = pendingBridgeRequests.current.get(requestID)
    if (!requestID || timeout === undefined) return
    window.clearTimeout(timeout)
    pendingBridgeRequests.current.delete(requestID)
    frame.contentWindow.postMessage(
      {
        source: 'sub2api-plugin-host',
        bridge_token: session.bridge_token,
        type: `${request.type}.result`,
        request_id: requestID,
        ...payload
      },
      // The sandboxed iframe has an opaque origin, so no fixed target origin exists.
      // Pending request tracking plus load invalidation prevents cross-navigation leaks.
      '*'
    )
  }

  async function handleBridgeMessage(event: MessageEvent): Promise<void> {
    const session = sessionRef.current.uiSession
    const plugin = sessionRef.current.configPlugin
    if (!session || !plugin || event.source !== pluginFrame.current?.contentWindow || event.origin !== 'null') return
    const message = event.data as PluginBridgeMessage
    if (!message || message.source !== 'sub2api-plugin-ui' || message.bridge_token !== session.bridge_token) return

    const requestID = typeof message.request_id === 'string' ? message.request_id.trim() : ''
    const expectsResponse =
      message.type === 'config.load' ||
      message.type === 'config.save' ||
      message.type === 'config.test' ||
      message.type === 'plugin.status'
    if (expectsResponse) {
      if (!requestID || pendingBridgeRequests.current.has(requestID)) return
      registerBridgeRequest(requestID)
    }

    try {
      switch (message.type) {
        case 'sub2api.plugin.ready':
          setUiLoading(false)
          break
        case 'config.load': {
          const config = await adminAPI.plugins.getConfig(plugin.id)
          postBridgeResult(message, { ok: true, config })
          break
        }
        case 'config.save': {
          if (!message.config || typeof message.config !== 'object' || Array.isArray(message.config)) {
            throw new Error(t('admin.plugins.bridgeRejected'))
          }
          const config = await pluginStepUp.run(() =>
            adminAPI.plugins.saveConfig(plugin.id, message.config as Record<string, unknown>)
          )
          postBridgeResult(message, { ok: true, config })
          toast.success(t('common.saved'))
          break
        }
        case 'config.test': {
          const result = await pluginStepUp.run(() => adminAPI.plugins.test(plugin.id))
          postBridgeResult(message, { ok: result.success, result })
          // A successful result is delivered back to the plugin UI, which owns how it
          // presents it (inline status, or an explicit ui.notify). Only force a host
          // toast on failure so genuine errors are never silently dropped — plugins
          // may call config.test for lightweight status polling, not just as an
          // explicit "test" action, and those must not spam a success toast.
          if (!result.success) toast.error(result.message || t('common.error'))
          break
        }
        case 'plugin.status': {
          // Read-only runtime status (the plugin's Health snapshot). It has no side
          // effects, so it is intentionally NOT step-up gated and never raises a host
          // toast — the plugin UI renders it however it likes. This is the generic
          // channel for any plugin to surface live state without abusing config.test.
          const result = await adminAPI.plugins.status(plugin.id)
          postBridgeResult(message, { ok: true, result })
          break
        }
        case 'ui.resize': {
          const height = Number(message.height)
          if (Number.isFinite(height)) setIframeHeight(Math.min(960, Math.max(520, Math.round(height))))
          break
        }
        case 'ui.notify': {
          const text = typeof message.message === 'string' ? message.message.slice(0, 500) : ''
          if (!text) break
          if (message.level === 'error') toast.error(text)
          else if (message.level === 'success') toast.success(text)
          else toast.info(text)
          break
        }
      }
    } catch (error: unknown) {
      if (isStepUpBlocked(error)) reportSensitiveActionError(error)
      postBridgeResult(message, {
        ok: false,
        error: isStepUpCancelled(error) ? t('common.cancel') : errorMessage(error)
      })
    }
  }

  useEffect(() => {
    window.addEventListener('message', handleBridgeMessage)
    void loadPlugins()
    return () => {
      window.removeEventListener('message', handleBridgeMessage)
      closeConfiguration()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function stateClass(state: PluginInstallation['state']): string {
    if (state === 'enabled')
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    if (state === 'error' || state === 'incompatible')
      return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
    if (state === 'starting') return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    return 'surface-inset surface-muted dark:surface-inset'
  }

  function compatibilityClass(status: PluginInstallation['compatibility']['status']): string {
    if (status === 'compatible')
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    if (status === 'untested') return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
  }

  return (
    <div className="workspace-page">
      <section className="card workspace-fixed flex flex-col gap-4 px-6 py-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t('admin.plugins.title')}</h2>
          <p className="mt-1 max-w-3xl text-sm surface-muted">{t('admin.plugins.description')}</p>
          <div className="mt-3 flex flex-wrap gap-2 text-xs surface-muted">
            <span className="rounded surface-inset px-2 py-1">{t('admin.plugins.onlyOpenAI')}</span>
            <span className="rounded surface-inset px-2 py-1">{t('admin.plugins.noAccountCoupling')}</span>
          </div>
        </div>

        <div className="flex flex-shrink-0 items-center gap-2">
          <input
            ref={fileInput}
            className="hidden"
            type="file"
            accept=".s2plugin,application/zip"
            onChange={(event) => void handleFileSelected(event)}
          />
          <Button variant="primary" disabled={uploading} onClick={() => fileInput.current?.click()}>
            <Icon name="upload" size="sm" />
            {uploading ? t('common.processing') : t('admin.plugins.upload')}
          </Button>
          <Button variant="secondary" disabled={loading} title={t('common.refresh')} onClick={() => void loadPlugins()}>
            <Icon name="refresh" size="sm" />
            <span className="sr-only">{t('common.refresh')}</span>
          </Button>
        </div>
      </section>

      <p className="workspace-fixed text-xs surface-muted">{t('admin.plugins.uploadHint')}</p>

      <div className="workspace-fixed rounded-xl border border-blue-200 bg-blue-50/[0.15] px-4 py-3 text-sm text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-200">
        <p>{t('admin.plugins.runtimeNotice')}</p>
        <p className="mt-1">{t('admin.plugins.menuNotice')}</p>
      </div>

      <div className="workspace-shell workspace-scroll" data-test="plugin-list-scroll">
        {loading ? (
          <div className="flex min-h-48 items-center justify-center text-sm surface-muted">{t('common.loading')}</div>
        ) : plugins.length === 0 ? (
          <div className="flex min-h-56 flex-col items-center justify-center border border-dashed border-gray-300 px-6 text-center dark:border-dark-600">
            <Icon name="cube" size="xl" className="text-gray-400" />
            <p className="mt-3 font-medium text-gray-800 dark:text-gray-200">{t('admin.plugins.empty')}</p>
            <p className="mt-1 max-w-lg text-sm surface-muted">{t('admin.plugins.emptyHint')}</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
            {plugins.map((plugin) => (
              <article key={plugin.id} className="card overflow-hidden border">
                <div className="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 p-5 dark:border-dark-700">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="truncate text-base font-semibold text-gray-900 dark:text-white">{plugin.name}</h3>
                      <span className="font-mono text-xs surface-muted">v{plugin.version}</span>
                      <span className={`rounded px-2 py-0.5 text-xs font-medium ${stateClass(plugin.state)}`}>
                        {t(`admin.plugins.${plugin.state}`)}
                      </span>
                    </div>
                    <p className="mt-1 text-xs surface-muted">
                      {plugin.plugin_key}
                      {plugin.author && <> · {plugin.author}</>}
                    </p>
                    {plugin.description && <p className="mt-2 text-sm surface-muted">{plugin.description}</p>}
                  </div>
                  <Button variant="secondary" size="sm" onClick={() => void openConfiguration(plugin)}>
                    <Icon name="cog" size="sm" />
                    {t('admin.plugins.configure')}
                  </Button>
                </div>

                <div className="grid grid-cols-1 gap-x-6 gap-y-4 p-5 md:grid-cols-2">
                  <div>
                    <p className="text-xs font-medium uppercase surface-muted">{t('admin.plugins.compatibility')}</p>
                    <div className="mt-2 flex items-center gap-2">
                      <span
                        className={`rounded px-2 py-0.5 text-xs font-medium ${compatibilityClass(plugin.compatibility.status)}`}
                      >
                        {t(`admin.plugins.${plugin.compatibility.status}`)}
                      </span>
                      <span className="text-xs surface-muted">{plugin.compatibility.message}</span>
                    </div>
                    <dl className="mt-3 grid grid-cols-[auto,1fr] gap-x-3 gap-y-1 text-xs">
                      <dt className="text-gray-500">{t('admin.plugins.currentVersion')}</dt>
                      <dd className="font-mono text-gray-800 dark:text-gray-200">
                        {plugin.compatibility.current_sub2api_version}
                      </dd>
                      <dt className="text-gray-500">{t('admin.plugins.requiredVersion')}</dt>
                      <dd className="font-mono text-gray-800 dark:text-gray-200">
                        {plugin.compatibility.required_sub2api_version}
                      </dd>
                      <dt className="text-gray-500">{t('admin.plugins.recommendedVersion')}</dt>
                      <dd className="font-mono text-gray-800 dark:text-gray-200">
                        {plugin.compatibility.recommended_sub2api_version || '-'}
                      </dd>
                    </dl>
                  </div>

                  <div>
                    <p className="text-xs font-medium uppercase surface-muted">{t('admin.plugins.runtime')}</p>
                    <div className="mt-2 flex flex-wrap gap-2 text-xs">
                      <span
                        className={`rounded px-2 py-0.5 ${
                          plugin.runtime_healthy
                            ? 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                            : 'surface-inset text-gray-600 dark:surface-inset dark:text-gray-300'
                        }`}
                      >
                        {plugin.runtime_healthy ? t('admin.plugins.healthy') : t('admin.plugins.unhealthy')}
                      </span>
                      <span className="rounded surface-inset px-2 py-0.5 surface-muted">
                        {t('admin.plugins.signature')}: {t(`admin.plugins.${plugin.signature_status}`)}
                      </span>
                    </div>
                    {plugin.last_error ? (
                      <p className="mt-3 break-words text-xs text-red-600 dark:text-red-400">{plugin.last_error}</p>
                    ) : plugin.runtime_message ? (
                      <p className="mt-3 break-words text-xs surface-muted">{plugin.runtime_message}</p>
                    ) : null}
                  </div>

                  <div className="md:col-span-2">
                    <label className="flex items-center justify-between gap-4 text-xs font-medium surface-muted">
                      <span>{t('admin.plugins.rollout')}</span>
                      <span className="w-11 text-right font-mono">
                        {rolloutValues[plugin.id] ?? currentRollout(plugin)}%
                      </span>
                    </label>
                    <input
                      value={rolloutValues[plugin.id] ?? currentRollout(plugin)}
                      type="range"
                      min="1"
                      max="100"
                      step="1"
                      className="mt-2 w-full accent-primary-600"
                      disabled={hasEnabledBinding(plugin)}
                      onChange={(event) => setRollout(plugin.id, Number(event.target.value))}
                    />
                  </div>
                </div>

                <div className="flex flex-wrap justify-end gap-2 border-t border-gray-100 px-5 py-4 dark:border-dark-700">
                  <Button variant="secondary" size="sm" disabled={busyID === plugin.id} onClick={() => void testPlugin(plugin)}>
                    <Icon name="beaker" size="sm" />
                    {t('admin.plugins.test')}
                  </Button>
                  {hasEnabledBinding(plugin) ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busyID === plugin.id}
                      onClick={() => void disablePlugin(plugin)}
                    >
                      <Icon name="ban" size="sm" />
                      {t('admin.plugins.disable')}
                    </Button>
                  ) : (
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={
                        busyID === plugin.id || plugin.state === 'starting' || !plugin.compatibility.compatible
                      }
                      onClick={() => void enablePlugin(plugin)}
                    >
                      <Icon name="play" size="sm" />
                      {t('admin.plugins.enable')}
                    </Button>
                  )}
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={busyID === plugin.id || hasEnabledBinding(plugin)}
                    onClick={() => void uninstallPlugin(plugin)}
                  >
                    <Icon name="trash" size="sm" />
                    {t('admin.plugins.uninstall')}
                  </Button>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>

      <Modal
        open={configPlugin !== null}
        onClose={closeConfiguration}
        title={t('admin.plugins.configTitle', { name: configPlugin?.name || '' })}
        size="full"
      >
        <div className="relative min-h-[520px] overflow-hidden surface-inset" style={{ height: `${iframeHeight}px` }}>
          {uiLoading && (
            <div className="absolute inset-0 z-10 flex items-center justify-center text-sm surface-muted">
              {t('admin.plugins.loadingUI')}
            </div>
          )}
          {uiError && (
            <div className="absolute inset-0 z-20 flex flex-col items-center justify-center p-8 text-center">
              <Icon name="exclamationTriangle" size="xl" className="text-amber-500" />
              <p className="mt-3 font-medium text-gray-800 dark:text-gray-200">{t('admin.plugins.uiUnavailable')}</p>
              <p className="mt-1 max-w-xl text-sm surface-muted">{uiError}</p>
            </div>
          )}
          {uiSession && (
            <iframe
              ref={pluginFrame}
              src={uiSession.url}
              sandbox="allow-scripts"
              referrerPolicy="no-referrer"
              className="h-full w-full border-0 bg-white dark:bg-dark-900"
              title={t('admin.plugins.configTitle', { name: configPlugin?.name || '' })}
              onLoad={handlePluginFrameLoad}
            />
          )}
        </div>
      </Modal>

      <TotpStepUpDialog controller={pluginStepUp} />
    </div>
  )
}
