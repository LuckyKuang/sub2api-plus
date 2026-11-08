import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Button, Modal, Pagination, Select, Toggle } from '@/components/ui'
import { HelpTooltip } from '@/components/ui'
import {
  adminAPI,
  type IPAccessControlSettings,
  type IPLoginFailureState,
  type IPAccessRule,
  type IPAccessRuleKind,
  type IPAccessRuleStatus,
  type TrustedProxyStatus
} from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'
import { TotpStepUpDialog } from '@/components/auth/TotpStepUpDialog'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/components/auth/useStepUp'
import { AdminDataTable, type Column } from '@/views/admin/AdminDataTable'
import { Icon } from '@/views/admin/AccountIcon'
import { ConfirmDialog } from '@/views/admin/shared'
import { DateTimePicker } from './parts/DateTimePicker'
import { WorkspaceTabs } from './parts/WorkspaceTabs'

const deployGuideModes = ['binary', 'docker', 'standalone'] as const

export default function IPAccessControlView() {
  const { t } = useTranslation()
  const globalIpEnabled = useAppStore((s) => s.cachedPublicSettings?.global_ip_access_control_enabled === true)
  const stepUp = useStepUp()

  const [activeTab, setActiveTab] = useState('protection')
  const [showDeployGuide, setShowDeployGuide] = useState(false)
  const [deployGuideMode, setDeployGuideMode] = useState<(typeof deployGuideModes)[number]>('binary')

  const [proxyStatus, setProxyStatus] = useState<TrustedProxyStatus | null>(null)
  const [proxyStatusLoading, setProxyStatusLoading] = useState(true)

  const [settings, setSettings] = useState<IPAccessControlSettings>({
    enforcement_enabled: false,
    login_failure_auto_block_enabled: false,
    login_failure_threshold: 8,
    login_failure_window_minutes: 15,
    login_failure_block_minutes: 1440
  })
  const [settingsLoading, setSettingsLoading] = useState(true)
  const [settingsUnavailable, setSettingsUnavailable] = useState(false)
  void settingsUnavailable
  const [settingsSaving, setSettingsSaving] = useState(false)

  const [failureStatesLoading, setFailureStatesLoading] = useState(false)
  const [failureStates, setFailureStates] = useState<IPLoginFailureState[]>([])
  const [failureStatesUpdatedAt, setFailureStatesUpdatedAt] = useState<Date | null>(null)
  const [failureStatesUnavailable, setFailureStatesUnavailable] = useState(false)
  const [failureTotal, setFailureTotal] = useState(0)
  const [failurePage, setFailurePage] = useState(1)
  const [failurePageSize, setFailurePageSize] = useState(20)
  const [failureQuery, setFailureQuery] = useState('')

  const [rulesLoading, setRulesLoading] = useState(false)
  const [rules, setRules] = useState<IPAccessRule[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [ruleQuery, setRuleQuery] = useState('')
  const [ruleStatusFilter, setRuleStatusFilter] = useState<'' | IPAccessRuleStatus>('active')

  const [createVisible, setCreateVisible] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<{
    ip_or_cidr: string
    rule_kind: Extract<IPAccessRuleKind, 'manual_block' | 'allow'>
    reason: string
    expires_at: string
  }>({ ip_or_cidr: '', rule_kind: 'manual_block', reason: '', expires_at: '' })

  const [releaseTarget, setReleaseTarget] = useState<IPAccessRule | null>(null)
  const [resetCounterTarget, setResetCounterTarget] = useState<string | null>(null)
  const [manualBlockTarget, setManualBlockTarget] = useState<IPLoginFailureState | null>(null)
  const [manualBlockSubmitting, setManualBlockSubmitting] = useState(false)
  const rulesSection = useRef<HTMLElement>(null)

  const failureStatesRequestSequence = useRef(0)
  const stateRef = useRef({
    failurePage,
    failurePageSize,
    failureQuery,
    page,
    pageSize,
    ruleQuery,
    ruleStatusFilter,
    settings
  })
  stateRef.current = {
    failurePage,
    failurePageSize,
    failureQuery,
    page,
    pageSize,
    ruleQuery,
    ruleStatusFilter,
    settings
  }

  const workspaceTabs = [
    { value: 'protection', label: t('workspace.automaticProtection'), panelId: 'ip-panel-protection' },
    { value: 'proxies', label: t('workspace.trustedProxies'), panelId: 'ip-panel-proxies' },
    { value: 'failures', label: t('workspace.loginFailures'), panelId: 'ip-panel-failures' },
    { value: 'rules', label: t('workspace.rules'), panelId: 'ip-panel-rules' }
  ]

  const showError = useAppStore((s) => s.showError)
  const showSuccess = useAppStore((s) => s.showSuccess)

  // The backend returns arrays, but normalize them here as well so a stale
  // deployment or malformed response cannot take down the complete admin view.
  const trustedProxies = Array.isArray(proxyStatus?.trusted_proxies) ? proxyStatus.trusted_proxies : []
  const forwardedHeaders = Array.isArray(proxyStatus?.forwarded_headers) ? proxyStatus.forwarded_headers : []
  const featureEnabled = globalIpEnabled
  const automaticBlockingReady = proxyStatus?.automatic_blocking_ready === true
  const manualBlockingReady = proxyStatus?.manual_blocking_ready === true

  const releaseIsAllow = releaseTarget?.rule_kind === 'allow'
  const releaseDialogTitle = t(
    releaseIsAllow ? 'admin.ipAccessControl.rules.removeAllowTitle' : 'admin.ipAccessControl.rules.releaseTitle'
  )
  const releaseDialogMessage = t(
    releaseIsAllow ? 'admin.ipAccessControl.rules.removeAllowMessage' : 'admin.ipAccessControl.rules.releaseMessage'
  )
  const releaseDialogConfirmText = t(
    releaseIsAllow ? 'admin.ipAccessControl.rules.removeAllow' : 'admin.ipAccessControl.rules.releaseReset'
  )
  const manualBlockDialogMessage = t('admin.ipAccessControl.failureStates.manualBlockMessage', {
    ip: manualBlockTarget?.normalized_ip || ''
  })

  const createKindOptions = [
    { value: 'manual_block', label: t('admin.ipAccessControl.rules.manualBlock'), disabled: !manualBlockingReady },
    { value: 'allow', label: t('admin.ipAccessControl.rules.allow') }
  ]

  const statusOptions = [
    { value: 'active', label: t('admin.ipAccessControl.rules.statusActive') },
    { value: 'released', label: t('admin.ipAccessControl.rules.statusReleased') },
    { value: 'expired', label: t('admin.ipAccessControl.rules.statusExpired') }
  ]

  const failureStateColumns: Column[] = [
    { key: 'normalized_ip', label: t('admin.ipAccessControl.failureStates.ip') },
    { key: 'failure_count', label: t('admin.ipAccessControl.failureStates.failureCount') },
    { key: 'window_started_at', label: t('admin.ipAccessControl.failureStates.windowStartedAt') },
    { key: 'last_failed_at', label: t('admin.ipAccessControl.failureStates.lastFailedAt') },
    { key: 'window_expires_at', label: t('admin.ipAccessControl.failureStates.windowExpiresAt') },
    { key: 'effectively_blocked', label: t('admin.ipAccessControl.failureStates.blockStatus') },
    { key: 'actions', label: t('common.actions') }
  ]

  const columns: Column[] = [
    { key: 'ip_or_cidr', label: t('admin.ipAccessControl.rules.ip') },
    { key: 'rule_kind', label: t('admin.ipAccessControl.rules.type') },
    { key: 'status', label: t('common.status') },
    { key: 'failure_count', label: t('admin.ipAccessControl.rules.failureCount') },
    { key: 'hit_count', label: t('admin.ipAccessControl.rules.hitCount') },
    { key: 'last_seen_at', label: t('admin.ipAccessControl.rules.lastSeenAt') },
    { key: 'expires_at', label: t('admin.ipAccessControl.rules.expiresAt') },
    { key: 'actions', label: t('common.actions') }
  ]

  const proxyStateLabel = t(
    `admin.ipAccessControl.trustedProxy.states.${proxyStatus?.configuration_state || 'not_configured'}`
  )
  const proxyStateClass = (() => {
    const state = proxyStatus?.configuration_state
    if (state === 'configured')
      return 'rounded bg-emerald-100/[0.15] px-2 py-1 text-xs font-medium text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
    if (state === 'invalid')
      return 'rounded bg-rose-100/[0.15] px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300'
    return 'rounded bg-amber-100/[0.15] px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  })()

  const proxyStatusMessage = (() => {
    const status = proxyStatus
    if (!status || status.configuration_state === 'not_configured')
      return t('admin.ipAccessControl.trustedProxy.messages.notConfigured')
    if (status.configuration_state === 'empty') return t('admin.ipAccessControl.trustedProxy.messages.empty')
    if (status.configuration_state === 'invalid') return t('admin.ipAccessControl.trustedProxy.messages.invalid')
    if (status.trusted_proxy_applied) return t('admin.ipAccessControl.trustedProxy.messages.applied')
    if (status.direct_peer_trusted) return t('admin.ipAccessControl.trustedProxy.messages.trustedPeerNoForwarded')
    return t('admin.ipAccessControl.trustedProxy.messages.notApplied')
  })()

  const proxyStatusMessageClass = (() => {
    const status = proxyStatus
    if (status?.configuration_state === 'configured' && status.trusted_proxy_applied)
      return 'border-emerald-200 bg-emerald-50/[0.15] text-emerald-800 dark:border-emerald-800/70 dark:bg-emerald-950/30 dark:text-emerald-200'
    if (status?.configuration_state === 'invalid')
      return 'border-rose-200 bg-rose-50/[0.15] text-rose-800 dark:border-rose-800/70 dark:bg-rose-950/30 dark:text-rose-200'
    return 'border-amber-200 bg-amber-50/[0.15] text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200'
  })()

  function formatTimestamp(value?: string): string {
    return value ? new Date(value).toLocaleString() : '—'
  }
  function formatExpiryTime(value?: string): string {
    return value ? new Date(value).toLocaleString() : t('admin.ipAccessControl.rules.never')
  }
  function formatLastSeenTime(value?: string): string {
    return value ? new Date(value).toLocaleString() : t('admin.ipAccessControl.rules.lastSeenNever')
  }
  function statusLabel(rule: IPAccessRule): string {
    if (rule.status === 'active') return t('admin.ipAccessControl.rules.statusActive')
    if (rule.status === 'released')
      return t(rule.rule_kind === 'allow' ? 'admin.ipAccessControl.rules.statusRemoved' : 'admin.ipAccessControl.rules.statusReleased')
    return t('admin.ipAccessControl.rules.statusExpired')
  }
  function kindLabel(value: IPAccessRuleKind): string {
    return value === 'allow'
      ? t('admin.ipAccessControl.rules.allow')
      : value === 'auto_block'
        ? t('admin.ipAccessControl.rules.autoBlock')
        : t('admin.ipAccessControl.rules.manualBlock')
  }
  function statusClass(value: IPAccessRuleStatus): string {
    return value === 'active'
      ? 'rounded bg-emerald-100/[0.15] px-2 py-1 text-xs font-medium text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
      : 'rounded surface-inset px-2 py-1 text-xs font-medium surface-muted dark:surface-inset'
  }
  function kindClass(value: IPAccessRuleKind): string {
    return value === 'allow'
      ? 'rounded bg-sky-100/[0.15] px-2 py-1 text-xs font-medium text-sky-700 dark:bg-sky-950/50 dark:text-sky-300'
      : value === 'auto_block'
        ? 'rounded bg-amber-100/[0.15] px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
        : 'rounded bg-rose-100/[0.15] px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300'
  }
  function failureBlockStatusLabel(state: IPLoginFailureState): string {
    if (state.effectively_blocked) return t('admin.ipAccessControl.failureStates.blocked')
    if (state.emergency_allowlisted) return t('admin.ipAccessControl.failureStates.emergencyAllowlisted')
    if (state.suppressed_by_allow_rule) return t('admin.ipAccessControl.failureStates.suppressedByAllow')
    if (state.active_block_rule && !state.runtime_enforcement_enabled)
      return t('admin.ipAccessControl.failureStates.ruleNotEnforced')
    if (state.active_block_rule) return t('admin.ipAccessControl.failureStates.ruleActive')
    return t('admin.ipAccessControl.failureStates.observing', {
      count: state.failure_count,
      threshold: state.failure_threshold
    })
  }
  function failureBlockStatusClass(state: IPLoginFailureState): string {
    if (state.effectively_blocked)
      return 'rounded bg-rose-100/[0.15] px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300'
    if (state.emergency_allowlisted)
      return 'rounded bg-violet-100/[0.15] px-2 py-1 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300'
    if (state.suppressed_by_allow_rule)
      return 'rounded bg-sky-100/[0.15] px-2 py-1 text-xs font-medium text-sky-700 dark:bg-sky-950/50 dark:text-sky-300'
    if (state.active_block_rule)
      return 'rounded bg-amber-100/[0.15] px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
    return 'rounded surface-inset px-2 py-1 text-xs font-medium surface-muted dark:surface-inset'
  }
  function manualBlockDisabledReason(state: IPLoginFailureState): string {
    if (failureStatesUnavailable) return t('admin.ipAccessControl.failureStates.manualBlockDisabledStale')
    if (state.emergency_allowlisted) return t('admin.ipAccessControl.failureStates.manualBlockDisabledEmergencyAllow')
    if (state.suppressed_by_allow_rule) return t('admin.ipAccessControl.failureStates.manualBlockDisabledAllow')
    if (state.active_block_rule || state.effectively_blocked)
      return t('admin.ipAccessControl.failureStates.manualBlockDisabledAlreadyBlocked')
    if (!state.runtime_enforcement_enabled) return t('admin.ipAccessControl.failureStates.manualBlockDisabledEnforcement')
    if (!manualBlockingReady) return t('admin.ipAccessControl.failureStates.manualBlockDisabledIdentity')
    if (settingsSaving || failureStatesLoading || manualBlockSubmitting)
      return t('admin.ipAccessControl.failureStates.manualBlockDisabledBusy')
    return ''
  }
  function canManuallyBlockFailureState(state: IPLoginFailureState): boolean {
    return manualBlockDisabledReason(state) === ''
  }

  function reportSensitiveError(error: unknown): void {
    if (isStepUpCancelled(error)) return
    if (isStepUpBlocked(error)) {
      showError(
        stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled')
      )
      return
    }
    showError(extractApiErrorMessage(error, t('common.error')))
  }

  async function loadSettings() {
    setSettingsLoading(true)
    try {
      setSettings(await adminAPI.ipAccessControl.getSettings())
      setSettingsUnavailable(false)
    } catch (error) {
      setSettingsUnavailable(true)
      showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      setSettingsLoading(false)
    }
  }

  async function loadProxyStatus() {
    setProxyStatusLoading(true)
    try {
      setProxyStatus(await adminAPI.ipAccessControl.getTrustedProxyStatus())
    } catch (error) {
      showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      setProxyStatusLoading(false)
    }
  }

  async function saveSettings() {
    setSettingsSaving(true)
    try {
      let next = settings
      if (!next.enforcement_enabled) next = { ...next, login_failure_auto_block_enabled: false }
      next = await stepUp.run(() => adminAPI.ipAccessControl.updateSettings({ ...next }))
      setSettings(next)
      setSettingsUnavailable(false)
      await loadFailureStates()
      showSuccess(t('common.saved'))
    } catch (error) {
      reportSensitiveError(error)
      await Promise.all([loadSettings(), loadProxyStatus()])
    } finally {
      setSettingsSaving(false)
    }
  }

  function failureStateQuery() {
    const current = stateRef.current
    return {
      page: current.failurePage,
      page_size: current.failurePageSize,
      query: current.failureQuery || undefined
    }
  }

  async function loadFailureStates() {
    const requestSequence = ++failureStatesRequestSequence.current
    const requestedPage = stateRef.current.failurePage
    setFailureStatesLoading(true)
    try {
      let result = await adminAPI.ipAccessControl.listFailureStates(failureStateQuery())
      if (requestSequence !== failureStatesRequestSequence.current) return
      const lastPage = Math.max(1, Math.ceil(result.total / stateRef.current.failurePageSize))
      if (requestedPage > lastPage) {
        setFailurePage(lastPage)
        result = await adminAPI.ipAccessControl.listFailureStates(failureStateQuery())
        if (requestSequence !== failureStatesRequestSequence.current) return
      }
      setFailureStates(result.items)
      setFailureTotal(result.total)
      setFailureStatesUpdatedAt(new Date())
      setFailureStatesUnavailable(false)
    } catch (error) {
      if (requestSequence !== failureStatesRequestSequence.current) return
      setFailureStatesUnavailable(true)
      showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      if (requestSequence === failureStatesRequestSequence.current) setFailureStatesLoading(false)
    }
  }

  function searchFailureStates() {
    setFailurePage(1)
    void loadFailureStates()
  }
  function refreshFailureStates() {
    if (!failureStatesLoading) void loadFailureStates()
  }
  function changeFailurePage(value: number) {
    setFailurePage(value)
    void loadFailureStates()
  }
  function changeFailurePageSize(value: number) {
    setFailurePageSize(value)
    setFailurePage(1)
    void loadFailureStates()
  }

  function ruleQueryParams() {
    const current = stateRef.current
    return {
      page: current.page,
      page_size: current.pageSize,
      query: current.ruleQuery || undefined,
      status: current.ruleStatusFilter || undefined
    }
  }

  async function loadRules() {
    setRulesLoading(true)
    try {
      let result = await adminAPI.ipAccessControl.listRules(ruleQueryParams())
      const lastPage = Math.max(1, Math.ceil(result.total / stateRef.current.pageSize))
      if (stateRef.current.page > lastPage) {
        setPage(lastPage)
        result = await adminAPI.ipAccessControl.listRules(ruleQueryParams())
      }
      setRules(result.items)
      setTotal(result.total)
    } catch (error) {
      showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      setRulesLoading(false)
    }
  }

  function searchRules() {
    setPage(1)
    void loadRules()
  }
  function changePage(value: number) {
    setPage(value)
    void loadRules()
  }
  function changePageSize(value: number) {
    setPageSize(value)
    setPage(1)
    void loadRules()
  }

  function openCreateDialog() {
    setCreateForm({
      ip_or_cidr: '',
      rule_kind: manualBlockingReady ? 'manual_block' : 'allow',
      reason: '',
      expires_at: ''
    })
    setCreateVisible(true)
  }

  async function createRule() {
    setCreating(true)
    try {
      await stepUp.run(() =>
        adminAPI.ipAccessControl.createRule({
          ip_or_cidr: createForm.ip_or_cidr,
          rule_kind: createForm.rule_kind,
          reason: createForm.reason || undefined,
          expires_at: createForm.expires_at ? new Date(createForm.expires_at).toISOString() : undefined
        })
      )
      setCreateVisible(false)
      await Promise.all([loadRules(), loadFailureStates()])
      showSuccess(t('common.saved'))
    } catch (error) {
      reportSensitiveError(error)
    } finally {
      setCreating(false)
    }
  }

  function confirmRelease(rule: IPAccessRule) {
    setReleaseTarget(rule)
  }

  async function releaseAndReset() {
    const rule = releaseTarget
    if (!rule) return
    try {
      await stepUp.run(() => adminAPI.ipAccessControl.releaseRuleAndReset(rule.id))
      setReleaseTarget(null)
      await Promise.all([loadRules(), loadFailureStates()])
      showSuccess(t('common.saved'))
    } catch (error) {
      if (isStepUpCancelled(error)) setReleaseTarget(null)
      reportSensitiveError(error)
    }
  }

  function confirmResetCounter(ip: string) {
    setResetCounterTarget(ip)
  }

  async function resetCounter() {
    const ip = resetCounterTarget
    if (!ip) return
    try {
      await stepUp.run(() => adminAPI.ipAccessControl.resetFailureState(ip))
      setResetCounterTarget(null)
      showSuccess(t('admin.ipAccessControl.failureStates.resetSuccess'))
      await Promise.all([loadFailureStates(), loadRules()])
    } catch (error) {
      if (isStepUpCancelled(error)) setResetCounterTarget(null)
      reportSensitiveError(error)
    }
  }

  function confirmManualBlock(state: IPLoginFailureState) {
    if (!canManuallyBlockFailureState(state)) return
    const currentState = failureStates.find((item) => item.normalized_ip === state.normalized_ip)
    if (!currentState) {
      showError(t('admin.ipAccessControl.failureStates.manualBlockDisabledStale'))
      return
    }
    if (canManuallyBlockFailureState(currentState)) setManualBlockTarget(currentState)
  }

  function cancelManualBlock() {
    if (!manualBlockSubmitting) setManualBlockTarget(null)
  }

  async function manualBlockFailureState() {
    const target = manualBlockTarget
    if (!target || manualBlockSubmitting) return
    const currentState = failureStates.find((state) => state.normalized_ip === target.normalized_ip)
    if (!currentState) {
      setManualBlockTarget(null)
      showError(t('admin.ipAccessControl.failureStates.manualBlockDisabledStale'))
      return
    }
    if (!canManuallyBlockFailureState(currentState)) {
      setManualBlockTarget(null)
      showError(manualBlockDisabledReason(currentState))
      return
    }
    setManualBlockSubmitting(true)
    try {
      const result = await stepUp.run(() => adminAPI.ipAccessControl.blockFailureState({ ip: target.normalized_ip }))
      setManualBlockTarget(null)
      showSuccess(
        t(
          result.already_blocked
            ? 'admin.ipAccessControl.failureStates.alreadyBlocked'
            : 'admin.ipAccessControl.failureStates.manualBlockSuccess'
        )
      )
      await Promise.all([loadFailureStates(), loadRules()])
    } catch (error) {
      setManualBlockTarget(null)
      reportSensitiveError(error)
      if (!isStepUpCancelled(error)) await Promise.all([loadFailureStates(), loadRules()])
    } finally {
      setManualBlockSubmitting(false)
    }
  }

  async function focusRulesForFailureState(state: IPLoginFailureState, showAllActive = false) {
    setActiveTab('rules')
    setRuleStatusFilter('active')
    setRuleQuery(showAllActive ? '' : state.block_rule_ip_or_cidr || state.normalized_ip)
    setPage(1)
    await loadRules()
    requestAnimationFrame(() => {
      rulesSection.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
    })
  }

  useEffect(() => {
    void loadProxyStatus()
    void loadSettings()
    void loadFailureStates()
    void loadRules()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const deployGuide = useMemo(
    () => (
      <div className="space-y-3 text-sm surface-muted">
        <h3 className="font-semibold text-gray-900 dark:text-white">
          {t(`admin.ipAccessControl.deployGuide.${deployGuideMode}Title`)}
        </h3>
        <p>{t(`admin.ipAccessControl.deployGuide.${deployGuideMode}Where`)}</p>
        <ol className="ml-5 list-decimal space-y-1">
          {[1, 2, 3, 4, 5, 6].map((index) => {
            const key = `admin.ipAccessControl.deployGuide.${deployGuideMode}Step${index}`
            return <li key={key}>{t(key)}</li>
          })}
        </ol>
        {deployGuideMode === 'binary' && (
          <>
            <p>{t('admin.ipAccessControl.deployGuide.binaryYamlHint')}</p>
            <pre className="overflow-x-auto rounded surface-inset px-3 py-2 text-xs text-gray-800 dark:text-gray-200">
              {t('admin.ipAccessControl.deployGuide.binaryYaml')}
            </pre>
            <p>{t('admin.ipAccessControl.deployGuide.binaryStep5')}</p>
            <code className="block rounded surface-inset px-3 py-2 text-xs">
              {t('admin.ipAccessControl.deployGuide.binaryRestart')}
            </code>
            <code className="block rounded surface-inset px-3 py-2 text-xs">
              {t('admin.ipAccessControl.deployGuide.binaryStatus')}
            </code>
            <p>{t('admin.ipAccessControl.deployGuide.binaryCustomFile')}</p>
            <p>{t('admin.ipAccessControl.deployGuide.binaryEnvOverride')}</p>
          </>
        )}
        {deployGuideMode === 'docker' && (
          <>
            <code className="block rounded surface-inset px-3 py-2 text-xs">
              {t('admin.ipAccessControl.deployGuide.dockerRestart')}
            </code>
            <code className="block rounded surface-inset px-3 py-2 text-xs">
              {t('admin.ipAccessControl.deployGuide.dockerLogs')}
            </code>
            <p>{t('admin.ipAccessControl.deployGuide.dockerFileHint')}</p>
          </>
        )}
      </div>
    ),
    [deployGuideMode, t]
  )

  return (
    <div className="workspace-page ip-access-workspace">
      <section className="workspace-fixed card ip-page-heading grid grid-cols-1 items-center gap-4 p-5 md:grid-cols-[minmax(0,1fr)_auto]">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-white">{t('admin.ipAccessControl.title')}</h1>
          <p className="mt-1 max-w-3xl text-sm surface-muted">{t('admin.ipAccessControl.description')}</p>
        </div>
        <Link className="btn btn-secondary" to="/admin/settings">
          {t('admin.ipAccessControl.backToSettings')}
        </Link>
        {/* Tabs live inside the header card so the page keeps one surface. */}
        <div className="col-span-full min-w-0 w-full">
          <WorkspaceTabs
            value={activeTab}
            tabs={workspaceTabs}
            label={t('admin.ipAccessControl.title')}
            embedded
            onValueChange={setActiveTab}
          />
        </div>
      </section>

      <section
        id="ip-panel-failures"
        data-testid="ip-panel-failures"
        role="tabpanel"
        aria-label={t('workspace.loginFailures')}
        className={`card workspace-table-card workspace-shell ip-data-panel ${activeTab === 'failures' ? '' : 'hidden'}`}
      >
        <div className="workspace-fixed ip-table-heading border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 className="font-semibold text-gray-900 dark:text-white">
            {t('admin.ipAccessControl.failureStates.title')}
          </h2>
          <div className="ip-failure-meta flex flex-wrap items-center gap-3">
            {failureStatesUnavailable && (
              <span className="rounded bg-rose-100/[0.15] px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300">
                {t('admin.ipAccessControl.failureStates.unavailable')}
              </span>
            )}
            {failureStatesUpdatedAt && (
              <span className="text-xs surface-muted">
                {t('admin.ipAccessControl.failureStates.lastUpdated', { time: failureStatesUpdatedAt.toLocaleString() })}
              </span>
            )}
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={failureStatesLoading}
              onClick={refreshFailureStates}
            >
              {t('admin.ipAccessControl.failureStates.refresh')}
            </button>
          </div>
          <p className="ip-table-description text-sm surface-muted">
            {t('admin.ipAccessControl.failureStates.description')}
          </p>
        </div>
        <div className="workspace-fixed ip-table-filters ip-failure-filters filter-bar border-b border-gray-100 p-4 dark:border-dark-700">
          <label className="ip-filter-field filter-field filter-field--search">
            <span className="input-label">{t('common.search')}</span>
            <input
              className="input"
              placeholder={t('admin.ipAccessControl.failureStates.searchPlaceholder')}
              value={failureQuery}
              onChange={(event) => setFailureQuery(event.target.value.trim())}
              onKeyDown={(event) => {
                if (event.key === 'Enter') searchFailureStates()
              }}
            />
          </label>
          <button type="button" className="btn btn-secondary" disabled={failureStatesLoading} onClick={searchFailureStates}>
            {t('common.search')}
          </button>
        </div>
        <AdminDataTable
          className="workspace-table-viewport"
          columns={failureStateColumns}
          data={failureStates}
          loading={failureStatesLoading}
          rowKey="normalized_ip"
          renderCell={(key, row, value) => {
            const state = row as IPLoginFailureState
            if (key === 'normalized_ip') return <span className="font-mono text-sm">{String(value ?? '')}</span>
            if (key === 'failure_count')
              return (
                <span className="font-medium text-gray-900 dark:text-white">
                  {state.failure_count} / {state.failure_threshold}
                </span>
              )
            if (key === 'window_started_at' || key === 'last_failed_at' || key === 'window_expires_at')
              return <span className="whitespace-nowrap text-sm surface-muted">{formatTimestamp(value as string)}</span>
            if (key === 'effectively_blocked')
              return <span className={failureBlockStatusClass(state)}>{failureBlockStatusLabel(state)}</span>
            if (key === 'actions') {
              return (
                <div className="flex flex-wrap items-center gap-2">
                  <button type="button" className="btn btn-secondary btn-sm" onClick={() => confirmResetCounter(state.normalized_ip)}>
                    {t('admin.ipAccessControl.failureStates.resetCounter')}
                  </button>
                  {state.emergency_allowlisted ? (
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      disabled
                      title={t('admin.ipAccessControl.failureStates.manualBlockDisabledEmergencyAllow')}
                    >
                      {t('admin.ipAccessControl.failureStates.emergencyAllowActive')}
                    </button>
                  ) : state.suppressed_by_allow_rule ? (
                    <button type="button" className="btn btn-secondary btn-sm" onClick={() => void focusRulesForFailureState(state, true)}>
                      {t('admin.ipAccessControl.failureStates.handleAllow')}
                    </button>
                  ) : state.active_block_rule ? (
                    <button type="button" className="btn btn-secondary btn-sm" onClick={() => void focusRulesForFailureState(state)}>
                      {t('admin.ipAccessControl.failureStates.viewRule')}
                    </button>
                  ) : (
                    <button
                      type="button"
                      className="btn btn-danger btn-sm"
                      disabled={!canManuallyBlockFailureState(state)}
                      title={manualBlockDisabledReason(state)}
                      onClick={() => confirmManualBlock(state)}
                    >
                      {t('admin.ipAccessControl.failureStates.manualBlock')}
                    </button>
                  )}
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
          empty={<div className="py-10 text-center text-sm surface-muted">{t('admin.ipAccessControl.failureStates.empty')}</div>}
        />
        <p className="workspace-fixed ip-table-note border-t border-gray-100 px-5 py-3 text-xs dark:border-dark-700 surface-muted">
          {t('admin.ipAccessControl.failureStates.operationHint')}
        </p>
        <div className="workspace-fixed ip-pagination border-t border-gray-100 dark:border-dark-700" data-testid="ip-failures-pagination">
          {failureTotal > 0 && (
            <Pagination
              total={failureTotal}
              page={failurePage}
              pageSize={failurePageSize}
              onPageChange={changeFailurePage}
              onPageSizeChange={changeFailurePageSize}
            />
          )}
        </div>
      </section>

      <section
        id="ip-panel-proxies"
        data-testid="ip-panel-proxies"
        role="tabpanel"
        aria-label={t('workspace.trustedProxies')}
        className={`card workspace-shell workspace-body workspace-scroll ${activeTab === 'proxies' ? '' : 'hidden'}`}
      >
        <div className="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div>
            <div className="flex flex-wrap items-center">
              <h2 className="font-semibold text-gray-900 dark:text-white">{t('admin.ipAccessControl.trustedProxy.title')}</h2>
              <HelpTooltip width-class="w-80" content={<p>{t('admin.ipAccessControl.trustedProxy.guideHelp')}</p>} />
              <button
                type="button"
                className="ml-2 text-sm text-primary-600 underline hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
                onClick={() => setShowDeployGuide(true)}
              >
                {t('admin.ipAccessControl.trustedProxy.guideLink')}
              </button>
            </div>
            <p className="mt-1 text-sm surface-muted">{t('admin.ipAccessControl.trustedProxy.description')}</p>
          </div>
          {proxyStatus && <span className={proxyStateClass}>{proxyStateLabel}</span>}
        </div>
        {proxyStatusLoading ? (
          <div className="p-6 text-sm surface-muted">{t('common.loading')}</div>
        ) : proxyStatus ? (
          <div>
            <div className="grid grid-cols-1 divide-y divide-gray-100 sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4 dark:divide-dark-700">
              <div className="min-w-0 px-5 py-4">
                <p className="text-xs font-medium surface-muted">{t('admin.ipAccessControl.trustedProxy.clientIp')}</p>
                <p className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {proxyStatus.client_ip || '—'}
                </p>
              </div>
              <div className="min-w-0 px-5 py-4">
                <p className="text-xs font-medium surface-muted">{t('admin.ipAccessControl.trustedProxy.directPeerIp')}</p>
                <p className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {proxyStatus.direct_peer_ip || '—'}
                </p>
              </div>
              <div className="min-w-0 px-5 py-4">
                <p className="text-xs font-medium surface-muted">{t('admin.ipAccessControl.trustedProxy.directPeerTrust')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                  {proxyStatus.direct_peer_trusted ? t('common.yes') : t('common.no')}
                </p>
              </div>
              <div className="min-w-0 px-5 py-4">
                <p className="text-xs font-medium surface-muted">{t('admin.ipAccessControl.trustedProxy.forwardedHeaders')}</p>
                <p className="mt-1 break-words font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {forwardedHeaders.length ? forwardedHeaders.join(', ') : t('admin.ipAccessControl.trustedProxy.none')}
                </p>
              </div>
            </div>
            <div className="border-t border-gray-100 px-5 py-4 dark:border-dark-700">
              <p className="text-xs font-medium surface-muted">{t('admin.ipAccessControl.trustedProxy.loadedCidrs')}</p>
              {trustedProxies.length ? (
                <div className="mt-2 flex flex-wrap gap-2">
                  {trustedProxies.map((proxy) => (
                    <span key={proxy} className="rounded surface-inset px-2 py-1 font-mono text-xs surface-muted">
                      {proxy}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="mt-1 text-sm surface-muted">{t('admin.ipAccessControl.trustedProxy.noneConfigured')}</p>
              )}
              <p className={`mt-4 rounded-md border px-3 py-2 text-xs leading-5 ${proxyStatusMessageClass}`}>
                {proxyStatusMessage}
              </p>
              <div className="mt-4 grid grid-cols-1 gap-3 text-xs sm:grid-cols-3 xl:grid-cols-5">
                <p className="rounded border border-gray-200 px-3 py-2 dark:border-dark-700 surface-muted">
                  <span className="font-medium">{t('admin.ipAccessControl.trustedProxy.identitySafety')}</span>
                  <br />
                  {proxyStatus.safe_for_enforcement
                    ? t('admin.ipAccessControl.trustedProxy.safe')
                    : t('admin.ipAccessControl.trustedProxy.unsafe')}
                </p>
                <p className="rounded border border-gray-200 px-3 py-2 dark:border-dark-700 surface-muted">
                  <span className="font-medium">{t('admin.ipAccessControl.trustedProxy.legacyMode')}</span>
                  <br />
                  {proxyStatus.legacy_forwarded_mode
                    ? t('admin.ipAccessControl.trustedProxy.enabled')
                    : t('admin.ipAccessControl.trustedProxy.disabled')}
                </p>
                <p className="rounded border border-gray-200 px-3 py-2 dark:border-dark-700 surface-muted">
                  <span className="font-medium">{t('admin.ipAccessControl.trustedProxy.emergencyAllowlist')}</span>
                  <br />
                  {proxyStatus.emergency_allowlist_configured
                    ? t('admin.ipAccessControl.trustedProxy.emergencyConfigured', {
                        count: proxyStatus.emergency_allowlist_count
                      })
                    : t('admin.ipAccessControl.trustedProxy.none')}
                </p>
                <p className="rounded border border-gray-200 px-3 py-2 dark:border-dark-700 surface-muted">
                  <span className="font-medium">{t('admin.ipAccessControl.trustedProxy.globalEnforcement')}</span>
                  <br />
                  {automaticBlockingReady
                    ? t('admin.ipAccessControl.trustedProxy.ready')
                    : t('admin.ipAccessControl.trustedProxy.notReady')}
                </p>
                <p className="rounded border border-gray-200 px-3 py-2 dark:border-dark-700 surface-muted">
                  <span className="font-medium">{t('admin.ipAccessControl.trustedProxy.manualBlocking')}</span>
                  <br />
                  {manualBlockingReady
                    ? t('admin.ipAccessControl.trustedProxy.ready')
                    : t('admin.ipAccessControl.trustedProxy.notReady')}
                </p>
              </div>
            </div>
          </div>
        ) : null}
      </section>

      <section
        id="ip-panel-protection"
        data-testid="ip-panel-protection"
        role="tabpanel"
        aria-label={t('workspace.automaticProtection')}
        className={`card workspace-shell workspace-body workspace-scroll ${activeTab === 'protection' ? '' : 'hidden'}`}
      >
        <div className="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 className="font-semibold text-gray-900 dark:text-white">{t('admin.ipAccessControl.protection.title')}</h2>
          <p className="mt-1 text-sm surface-muted">{t('admin.ipAccessControl.protection.description')}</p>
        </div>
        {settingsLoading ? (
          <div className="p-6 text-sm surface-muted">{t('common.loading')}</div>
        ) : (
          <div className="space-y-5 p-5">
            <div className="flex items-center justify-between gap-5">
              <div>
                <label className="font-medium text-gray-900 dark:text-white">
                  {t('admin.ipAccessControl.protection.enforcement')}
                </label>
                <p className="mt-1 text-sm surface-muted">{t('admin.ipAccessControl.protection.enforcementHint')}</p>
              </div>
              <Toggle
                checked={settings.enforcement_enabled}
                disabled={proxyStatusLoading || (!settings.enforcement_enabled && !automaticBlockingReady)}
                onCheckedChange={(next) => setSettings({ ...settings, enforcement_enabled: next })}
              />
            </div>
            <div className="flex items-center justify-between gap-5 border-t border-gray-100 pt-5 dark:border-dark-700">
              <div>
                <label className="font-medium text-gray-900 dark:text-white">
                  {t('admin.ipAccessControl.protection.autoBlock')}
                </label>
                <p className="mt-1 text-sm surface-muted">{t('admin.ipAccessControl.protection.autoBlockHint')}</p>
              </div>
              <Toggle
                checked={settings.login_failure_auto_block_enabled}
                disabled={
                  !settings.enforcement_enabled ||
                  (!settings.login_failure_auto_block_enabled && !automaticBlockingReady)
                }
                onCheckedChange={(next) => setSettings({ ...settings, login_failure_auto_block_enabled: next })}
              />
            </div>
            {settings.enforcement_enabled && (
              <div className="grid grid-cols-1 gap-4 border-t border-gray-100 pt-5 sm:grid-cols-3 dark:border-dark-700">
                {settings.login_failure_auto_block_enabled && (
                  <label className="block">
                    <span className="input-label">{t('admin.ipAccessControl.protection.threshold')}</span>
                    <input
                      value={settings.login_failure_threshold}
                      min={2}
                      max={100}
                      type="number"
                      className="input"
                      onChange={(event) =>
                        setSettings({ ...settings, login_failure_threshold: Number(event.target.value) })
                      }
                    />
                  </label>
                )}
                {settings.login_failure_auto_block_enabled && (
                  <label className="block">
                    <span className="input-label">{t('admin.ipAccessControl.protection.window')}</span>
                    <input
                      value={settings.login_failure_window_minutes}
                      min={1}
                      max={525600}
                      type="number"
                      className="input"
                      onChange={(event) =>
                        setSettings({ ...settings, login_failure_window_minutes: Number(event.target.value) })
                      }
                    />
                  </label>
                )}
                <label className="block">
                  <span className="input-label">{t('admin.ipAccessControl.protection.duration')}</span>
                  <input
                    value={settings.login_failure_block_minutes}
                    min={1}
                    max={525600}
                    type="number"
                    className="input"
                    onChange={(event) =>
                      setSettings({ ...settings, login_failure_block_minutes: Number(event.target.value) })
                    }
                  />
                </label>
              </div>
            )}
            {!featureEnabled && (
              <p className="rounded-md border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">
                {t('admin.ipAccessControl.protection.masterSwitchOff')}
              </p>
            )}
            <p className="rounded-md border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">
              {t('admin.ipAccessControl.protection.proxyHint')}
            </p>
            {!proxyStatusLoading && !automaticBlockingReady && (
              <p className="rounded-md border border-rose-200 bg-rose-50/[0.15] px-3 py-2 text-xs leading-5 text-rose-800 dark:border-rose-800/70 dark:bg-rose-950/30 dark:text-rose-200">
                {t('admin.ipAccessControl.protection.proxyNotReady')}
                <button type="button" className="ml-1 underline surface-control" onClick={() => setShowDeployGuide(true)}>
                  {t('admin.ipAccessControl.trustedProxy.guideLink')}
                </button>
              </p>
            )}
            <div className="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700">
              <button type="button" className="btn btn-primary" disabled={settingsSaving} onClick={() => void saveSettings()}>
                {settingsSaving ? t('common.saving') : t('common.save')}
              </button>
            </div>
          </div>
        )}
      </section>

      <section
        ref={rulesSection}
        id="ip-panel-rules"
        data-testid="ip-panel-rules"
        role="tabpanel"
        aria-label={t('workspace.rules')}
        className={`card workspace-table-card workspace-shell ip-data-panel ${activeTab === 'rules' ? '' : 'hidden'}`}
      >
        <div className="workspace-fixed ip-table-heading border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 className="font-semibold text-gray-900 dark:text-white">{t('admin.ipAccessControl.rules.title')}</h2>
          <button type="button" className="btn btn-primary" onClick={openCreateDialog}>
            <Icon name="plus" size="sm" className="mr-1.5" />
            {t('admin.ipAccessControl.rules.add')}
          </button>
          <p className="ip-table-description text-sm surface-muted">{t('admin.ipAccessControl.rules.description')}</p>
        </div>
        <div className="workspace-fixed ip-table-filters filter-bar border-b border-gray-100 p-4 dark:border-dark-700">
          <label className="ip-filter-field filter-field filter-field--search">
            <span className="input-label">{t('common.search')}</span>
            <input
              className="input"
              placeholder={t('admin.ipAccessControl.rules.searchPlaceholder')}
              value={ruleQuery}
              onChange={(event) => setRuleQuery(event.target.value.trim())}
              onKeyDown={(event) => {
                if (event.key === 'Enter') searchRules()
              }}
            />
          </label>
          <label className="ip-filter-field filter-field">
            <span className="input-label">{t('common.status')}</span>
            <Select
              value={ruleStatusFilter}
              options={statusOptions}
              searchable={false}
              onValueChange={(next) => {
                setRuleStatusFilter((next ?? '') as '' | IPAccessRuleStatus)
                searchRules()
              }}
            />
          </label>
          <button type="button" className="btn btn-secondary" disabled={rulesLoading} onClick={searchRules}>
            {t('common.search')}
          </button>
        </div>
        <AdminDataTable
          className="workspace-table-viewport"
          columns={columns}
          data={rules}
          loading={rulesLoading}
          rowKey="id"
          renderCell={(key, row, value) => {
            const rule = row as IPAccessRule
            if (key === 'ip_or_cidr') return <span className="font-mono text-sm">{String(value ?? '')}</span>
            if (key === 'rule_kind') return <span className={kindClass(rule.rule_kind)}>{kindLabel(rule.rule_kind)}</span>
            if (key === 'status') return <span className={statusClass(rule.status)}>{statusLabel(rule)}</span>
            if (key === 'failure_count') return <span>{rule.failure_count || '—'}</span>
            if (key === 'hit_count') return <span>{rule.hit_count ?? 0}</span>
            if (key === 'last_seen_at')
              return <span className="whitespace-nowrap text-sm surface-muted">{formatLastSeenTime(value as string)}</span>
            if (key === 'expires_at')
              return <span className="whitespace-nowrap text-sm surface-muted">{formatExpiryTime(value as string)}</span>
            if (key === 'actions') {
              return (
                <div className="flex flex-wrap items-center gap-2">
                  {rule.status === 'active' && !rule.ip_or_cidr.includes('/') && rule.rule_kind !== 'allow' && (
                    <button type="button" className="btn btn-secondary btn-sm" onClick={() => confirmResetCounter(rule.ip_or_cidr)}>
                      {t('admin.ipAccessControl.rules.resetCounter')}
                    </button>
                  )}
                  {rule.status === 'active' ? (
                    <button type="button" className="btn btn-danger btn-sm" onClick={() => confirmRelease(rule)}>
                      {rule.rule_kind === 'allow'
                        ? t('admin.ipAccessControl.rules.removeAllow')
                        : t('admin.ipAccessControl.rules.releaseReset')}
                    </button>
                  ) : (
                    <span className="text-xs surface-muted">—</span>
                  )}
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
          empty={<div className="py-10 text-center text-sm surface-muted">{t('admin.ipAccessControl.rules.empty')}</div>}
        />
        <p className="workspace-fixed ip-table-note border-t border-gray-100 px-5 py-3 text-xs dark:border-dark-700 surface-muted">
          {t('admin.ipAccessControl.rules.hitCountHint')}
        </p>
        <div className="workspace-fixed ip-pagination border-t border-gray-100 dark:border-dark-700" data-testid="ip-rules-pagination">
          {total > 0 && <Pagination total={total} page={page} pageSize={pageSize} onPageChange={changePage} onPageSizeChange={changePageSize} />}
        </div>
      </section>

      <Modal
        open={createVisible}
        onClose={() => setCreateVisible(false)}
        title={t('admin.ipAccessControl.rules.addTitle')}
        size="sm"
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreateVisible(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              loading={creating}
              disabled={!createForm.ip_or_cidr || (createForm.rule_kind === 'manual_block' && !manualBlockingReady)}
              onClick={() => void createRule()}
            >
              {creating ? t('common.saving') : t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-4 py-2">
          <label className="block">
            <span className="input-label">{t('admin.ipAccessControl.rules.ip')}</span>
            <input
              className="input font-mono"
              placeholder="203.0.113.8 or 2001:db8::/32"
              value={createForm.ip_or_cidr}
              onChange={(event) => setCreateForm({ ...createForm, ip_or_cidr: event.target.value.trim() })}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.ipAccessControl.rules.type')}</span>
            <Select
              value={createForm.rule_kind}
              options={createKindOptions}
              searchable={false}
              onValueChange={(next) =>
                setCreateForm({ ...createForm, rule_kind: next as Extract<IPAccessRuleKind, 'manual_block' | 'allow'> })
              }
            />
          </label>
          {!manualBlockingReady && (
            <p className="rounded-md border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">
              {t('admin.ipAccessControl.rules.manualBlockUnavailable')}
            </p>
          )}
          <label className="block">
            <span className="input-label">{t('admin.ipAccessControl.rules.reason')}</span>
            <textarea
              rows={3}
              className="input resize-y"
              value={createForm.reason}
              onChange={(event) => setCreateForm({ ...createForm, reason: event.target.value.trim() })}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.ipAccessControl.rules.expiresAt')}</span>
            <DateTimePicker
              ariaLabel={t('admin.ipAccessControl.rules.expiresAt')}
              type="datetime-local"
              className="input"
              value={createForm.expires_at}
              onValueChange={(next) => setCreateForm({ ...createForm, expires_at: next })}
            />
          </label>
        </div>
      </Modal>

      <ConfirmDialog
        open={releaseTarget !== null}
        title={releaseDialogTitle}
        message={releaseDialogMessage}
        confirmText={releaseDialogConfirmText}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void releaseAndReset()}
        onCancel={() => setReleaseTarget(null)}
      />

      <ConfirmDialog
        open={resetCounterTarget !== null}
        title={t('admin.ipAccessControl.failureStates.resetTitle')}
        message={t('admin.ipAccessControl.failureStates.resetMessage', { ip: resetCounterTarget })}
        confirmText={t('admin.ipAccessControl.failureStates.resetCounter')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void resetCounter()}
        onCancel={() => setResetCounterTarget(null)}
      />

      <ConfirmDialog
        open={manualBlockTarget !== null}
        title={t('admin.ipAccessControl.failureStates.manualBlockTitle')}
        message={manualBlockDialogMessage}
        confirmText={
          manualBlockSubmitting ? t('admin.ipAccessControl.failureStates.blocking') : t('admin.ipAccessControl.failureStates.manualBlock')
        }
        cancelText={t('common.cancel')}
        loading={manualBlockSubmitting}
        danger
        onConfirm={() => void manualBlockFailureState()}
        onCancel={cancelManualBlock}
      />

      <Modal
        open={showDeployGuide}
        onClose={() => setShowDeployGuide(false)}
        title={t('admin.ipAccessControl.deployGuide.title')}
        size="lg"
        footer={
          <div className="mt-4 text-right">
            <Button variant="primary" size="sm" onClick={() => setShowDeployGuide(false)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        <p className="mb-2 text-sm surface-muted">{t('admin.ipAccessControl.deployGuide.intro')}</p>
        <p className="mb-4 text-sm surface-muted">{t('admin.ipAccessControl.deployGuide.whenNeeded')}</p>

        <p className="mb-3 text-sm font-medium surface-muted">{t('admin.ipAccessControl.deployGuide.pickDeploy')}</p>
        <div className="mb-5 flex flex-wrap gap-2">
          {deployGuideModes.map((mode) => (
            <button
              key={mode}
              type="button"
              className={`rounded-lg px-3 py-1.5 text-sm surface-control ${
                deployGuideMode === mode
                  ? 'bg-primary-600 text-white'
                  : 'surface-field text-gray-700 dark:surface-field dark:text-gray-300'
              }`}
              onClick={() => setDeployGuideMode(mode)}
            >
              {t(`admin.ipAccessControl.deployGuide.${mode}`)}
            </button>
          ))}
        </div>

        {deployGuideMode === 'binary' && deployGuide}

        <div className="mt-5 space-y-2 text-sm surface-muted">
          <h3 className="font-semibold text-gray-900 dark:text-white">{t('admin.ipAccessControl.deployGuide.proxyTitle')}</h3>
          <p>{t('admin.ipAccessControl.deployGuide.proxyHint')}</p>
          <p>{t('admin.ipAccessControl.deployGuide.proxyCloudflareHint')}</p>
          <pre className="overflow-x-auto rounded surface-inset px-3 py-2 text-xs text-gray-800 dark:text-gray-200">
            {t('admin.ipAccessControl.deployGuide.proxyCloudflareHttp')}
          </pre>
          <p>{t('admin.ipAccessControl.deployGuide.proxyHeaderHint')}</p>
          <pre className="overflow-x-auto rounded surface-inset px-3 py-2 text-xs text-gray-800 dark:text-gray-200">
            {t('admin.ipAccessControl.deployGuide.proxyNginx')}
          </pre>
          <p>{t('admin.ipAccessControl.deployGuide.proxyVerify')}</p>
          <p>{t('admin.ipAccessControl.deployGuide.proxyTunnelHint')}</p>
        </div>
        <div className="mt-5 rounded-lg surface-inset p-3 text-sm surface-muted">
          <p className="font-medium">{t('admin.ipAccessControl.deployGuide.afterTitle')}</p>
          <p className="mt-1">{t('admin.ipAccessControl.deployGuide.afterHint')}</p>
        </div>
        <div className="mt-4 rounded-lg bg-amber-50/[0.15] p-3 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
          <p className="font-medium">{t('admin.ipAccessControl.deployGuide.warningsTitle')}</p>
          <ul className="mt-2 list-disc space-y-1 pl-4">
            <li>{t('admin.ipAccessControl.deployGuide.warningCloudflare')}</li>
            <li>{t('admin.ipAccessControl.deployGuide.warningEnv')}</li>
            <li>{t('admin.ipAccessControl.deployGuide.warningLegacy')}</li>
            <li>{t('admin.ipAccessControl.deployGuide.warningWizard')}</li>
          </ul>
        </div>
      </Modal>

      <TotpStepUpDialog controller={stepUp} />
    </div>
  )
}
