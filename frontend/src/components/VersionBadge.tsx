import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import {
  performUpdate,
  restartService,
  getRollbackVersions,
  rollback as rollbackAPI,
  type RollbackVersionInfo
} from '@/api/admin/system'
import { useClipboard } from '@/composables/useClipboard'
import { normalizeDisplayVersion, toDockerImageTag } from '@/utils/version'
import { Icon } from './Icon'

const GITHUB_REPO = 'luckykuang/sub2api-plus'
// GHCR tags carry no "v" prefix and replace SemVer build metadata's + with -.
const DOCKER_IMAGE = 'ghcr.io/luckykuang/sub2api-plus'

const spinnerSvg = (
  <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
    <path
      className="opacity-75"
      fill="currentColor"
      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
    ></path>
  </svg>
)

const checkSvg = (className: string) => (
  <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" aria-hidden="true">
    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
  </svg>
)

const infoSvg = (className: string) => (
  <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" aria-hidden="true">
    <path
      strokeLinecap="round"
      strokeLinejoin="round"
      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
    />
  </svg>
)

const chevronRightSvg = (className: string) => (
  <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" aria-hidden="true">
    <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
  </svg>
)

const refreshSpinnerSvg = (className: string) => (
  <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" aria-hidden="true">
    <path
      strokeLinecap="round"
      strokeLinejoin="round"
      d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
    />
  </svg>
)

export interface VersionBadgeProps {
  className?: string
}

export function VersionBadge({ className }: VersionBadgeProps) {
  const { t } = useTranslation()

  const isAdmin = useAuthStore((s) => s.isAdmin())

  // Use store's cached version state
  const loading = useAppStore((s) => s.versionLoading)
  const storeCurrentVersion = useAppStore((s) => s.currentVersion)
  const storeLatestVersion = useAppStore((s) => s.latestVersion)
  const hasUpdate = useAppStore((s) => s.hasUpdate)
  const releaseInfo = useAppStore((s) => s.releaseInfo)
  const buildType = useAppStore((s) => s.buildType)
  const fetchVersion = useAppStore((s) => s.fetchVersion)
  const invalidateVersionCheck = useAppStore((s) => s.invalidateVersionCheck)
  const clearVersionCache = useAppStore((s) => s.clearVersionCache)

  const currentVersion = normalizeDisplayVersion(storeCurrentVersion)
  const latestVersion = normalizeDisplayVersion(storeLatestVersion)

  const [dropdownOpen, setDropdownOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement | null>(null)

  // Update process states (local to this component)
  const [updating, setUpdating] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [needRestart, setNeedRestart] = useState(false)
  const [updateError, setUpdateError] = useState('')
  const [updateSuccess, setUpdateSuccess] = useState(false)
  const [restartCountdown, setRestartCountdown] = useState(0)
  // Distinguishes the success + restart panel between update and rollback flows
  const [successKind, setSuccessKind] = useState<'update' | 'rollback'>('update')

  // Rollback states
  const [rollbackPanelOpen, setRollbackPanelOpen] = useState(false)
  const [rollbackVersions, setRollbackVersions] = useState<RollbackVersionInfo[]>([])
  const [rollbackVersionsLoading, setRollbackVersionsLoading] = useState(false)
  const [rollbackVersionsError, setRollbackVersionsError] = useState('')
  const [selectedRollbackVersion, setSelectedRollbackVersion] = useState('')
  const [rollingBack, setRollingBack] = useState(false)
  const [rollbackError, setRollbackError] = useState('')

  const { copied, copyToClipboard } = useClipboard()

  // Manual rollback methods differ by deployment: script installs use install.sh,
  // docker deployments pin the image tag instead
  const [manualTab, setManualTab] = useState<'script' | 'docker'>('script')

  const manualTabs = [
    { key: 'script' as const, label: t('version.deployScript') },
    { key: 'docker' as const, label: t('version.deployDocker') }
  ]

  const scriptRollbackCommand = (() => {
    if (!selectedRollbackVersion) return ''
    const tag = `v${selectedRollbackVersion}`
    return `curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/${tag}/deploy/install.sh | sudo bash -s -- rollback ${tag}`
  })()

  const dockerRollbackCommand = (() => {
    if (!selectedRollbackVersion) return ''
    return [
      `# ${t('version.dockerEditCompose')}`,
      `image: ${DOCKER_IMAGE}:${toDockerImageTag(selectedRollbackVersion)}`,
      '',
      `# ${t('version.dockerRecreate')}`,
      'docker compose up -d'
    ].join('\n')
  })()

  const activeManualCommand = manualTab === 'docker' ? dockerRollbackCommand : scriptRollbackCommand

  // Only show update check for release builds (binary/docker deployment)
  const isReleaseBuild = buildType === 'release'

  const toggleDropdown = () => {
    if (!isAdmin) return
    setDropdownOpen((open) => !open)
  }

  const closeDropdown = () => setDropdownOpen(false)

  const resetRollbackState = useCallback(() => {
    setRollbackPanelOpen(false)
    setRollbackVersions([])
    setRollbackVersionsError('')
    setSelectedRollbackVersion('')
    setRollbackError('')
    setManualTab('script')
  }, [])

  const refreshVersion = async (force = true) => {
    if (!isAdmin) return

    // Reset update states when refreshing
    setUpdateError('')
    setUpdateSuccess(false)
    setNeedRestart(false)
    resetRollbackState()

    await fetchVersion(force)
  }

  const handleUpdate = async () => {
    if (!isAdmin || updating) return

    setUpdating(true)
    setUpdateError('')
    setUpdateSuccess(false)

    try {
      const result = await performUpdate()
      setSuccessKind('update')
      setUpdateSuccess(true)
      setNeedRestart(result.need_restart)
      invalidateVersionCheck()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } }; message?: string }
      setUpdateError(err.response?.data?.message || err.message || t('version.updateFailed'))
    } finally {
      setUpdating(false)
    }
  }

  const toggleRollbackPanel = async () => {
    if (!isAdmin) return
    const next = !rollbackPanelOpen
    setRollbackPanelOpen(next)
    // Source builds only show a hint, no version list to fetch
    if (next && isReleaseBuild && rollbackVersions.length === 0 && !rollbackVersionsLoading) {
      await loadRollbackVersions()
    }
  }

  const loadRollbackVersions = async () => {
    if (!isAdmin) return
    setRollbackVersionsLoading(true)
    setRollbackVersionsError('')
    try {
      const data = await getRollbackVersions()
      setRollbackVersions(data.versions || [])
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } }; message?: string }
      setRollbackVersionsError(
        err.response?.data?.message || err.message || t('version.loadVersionsFailed')
      )
    } finally {
      setRollbackVersionsLoading(false)
    }
  }

  const selectRollbackVersion = (version: string) => {
    if (!isAdmin || rollingBack) return
    setRollbackError('')
    setSelectedRollbackVersion((current) => (current === version ? '' : version))
  }

  function formatPublishedAt(publishedAt: string): string {
    if (!publishedAt) return ''
    const date = new Date(publishedAt)
    if (Number.isNaN(date.getTime())) return ''
    return date.toLocaleDateString()
  }

  const handleRollback = async () => {
    if (!isAdmin) return
    if (rollingBack || !selectedRollbackVersion) return

    setRollingBack(true)
    setRollbackError('')

    try {
      const result = await rollbackAPI(selectedRollbackVersion)
      setSuccessKind('rollback')
      setUpdateSuccess(true)
      setNeedRestart(result.need_restart)
      setRollbackPanelOpen(false)
      invalidateVersionCheck()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } }; message?: string }
      setRollbackError(err.response?.data?.message || err.message || t('version.rollbackFailed'))
    } finally {
      setRollingBack(false)
    }
  }

  const checkServiceAndReload = async () => {
    const maxRetries = 5
    const retryDelay = 1000

    for (let i = 0; i < maxRetries; i++) {
      try {
        const response = await fetch('/health', {
          method: 'GET',
          cache: 'no-store'
        })
        if (response.ok) {
          // Service is back, reload page
          window.location.reload()
          return
        }
      } catch {
        // Service not ready yet
      }

      if (i < maxRetries - 1) {
        await new Promise((resolve) => setTimeout(resolve, retryDelay))
      }
    }

    // After retries, reload anyway
    window.location.reload()
  }

  const handleRestart = async () => {
    if (!isAdmin || restarting) return

    setRestarting(true)
    setRestartCountdown(8)

    try {
      await restartService()
      // Service will restart, page will reload automatically or show disconnected
    } catch {
      // Expected - connection will be lost during restart
      console.log('Service restarting...')
    }

    // Start countdown
    const countdownInterval = setInterval(() => {
      setRestartCountdown((value) => {
        const next = value - 1
        if (next <= 0) {
          clearInterval(countdownInterval)
          // Try to check if service is back before reload
          void checkServiceAndReload()
        }
        return next
      })
    }, 1000)
  }

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node
      const button = (event.target as Element).closest('button')
      if (dropdownRef.current && !dropdownRef.current.contains(target) && !button?.contains(target)) {
        closeDropdown()
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  // Replaces the Vue watch(isAdmin, ..., { immediate: true }): losing admin
  // rights drops the cached update check and resets the panel.
  useEffect(() => {
    if (isAdmin) {
      void fetchVersion(false)
      return
    }
    closeDropdown()
    resetRollbackState()
    setUpdateError('')
    setUpdateSuccess(false)
    setNeedRestart(false)
    clearVersionCache()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin])

  if (!isAdmin) return null

  return (
    <div className={cn('relative', className)}>
      <button
        onClick={toggleDropdown}
        className="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors surface-control"
        title={hasUpdate ? t('version.updateAvailable') : t('version.upToDate')}
      >
        {currentVersion ? (
          <span className="font-medium">v{currentVersion}</span>
        ) : (
          <span className="h-3 w-12 animate-pulse rounded bg-gray-200 font-medium dark:bg-dark-600"></span>
        )}
        {/* Update indicator */}
        {hasUpdate && (
          <span className="relative flex h-2 w-2">
            <span className="console-data-mark absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"></span>
            <span className="console-data-mark relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
          </span>
        )}
      </button>

      {/* Dropdown */}
      {dropdownOpen && (
        <div
          ref={dropdownRef}
          className="version-dropdown glass-surface absolute left-0 z-50 mt-2 w-80 whitespace-normal"
        >
          {/* Header with refresh button */}
          <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <span className="text-sm font-medium surface-muted">{t('version.currentVersion')}</span>
            <button
              onClick={() => void refreshVersion(true)}
              className="rounded-lg p-1.5 transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-dark-200 surface-control surface-muted"
              disabled={loading}
              title={t('version.refresh')}
            >
              <Icon
                name="refresh"
                size="sm"
                strokeWidth={2}
                className={loading ? 'animate-spin' : ''}
              />
            </button>
          </div>

          <div className="p-4">
            {/* Loading state */}
            {loading ? (
              <div className="flex items-center justify-center py-6">
                <svg className="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24" aria-hidden="true">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  ></path>
                </svg>
              </div>
            ) : (
              /* Content */
              <>
                {/* Version display - centered and prominent */}
                <div className="mb-4 text-center">
                  <div className="inline-flex items-center gap-2">
                    {currentVersion ? (
                      <span className="text-2xl font-bold text-gray-900 dark:text-white">
                        v{currentVersion}
                      </span>
                    ) : (
                      <span className="text-2xl font-bold surface-muted">--</span>
                    )}
                    {/* Show check mark when up to date */}
                    {!hasUpdate && (
                      <span className="flex h-5 w-5 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
                        <svg
                          className="h-3 w-3 text-green-600 dark:text-green-400"
                          fill="currentColor"
                          viewBox="0 0 20 20"
                          aria-hidden="true"
                        >
                          <path
                            fillRule="evenodd"
                            d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                            clipRule="evenodd"
                          />
                        </svg>
                      </span>
                    )}
                  </div>
                  <p className="mt-1 text-xs surface-muted">
                    {hasUpdate ? t('version.latestVersion') + ': v' + latestVersion : t('version.upToDate')}
                  </p>
                </div>

                {/* Priority 1: Update error (must check before hasUpdate) */}
                {updateError ? (
                  <div className="space-y-2">
                    <div className="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50/[0.15] p-3 dark:border-red-800/50 dark:bg-red-900/20">
                      <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-red-100/[0.15] dark:bg-red-900/50">
                        <Icon
                          name="x"
                          size="sm"
                          strokeWidth={2}
                          className="text-red-600 dark:text-red-400"
                        />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-red-700 dark:text-red-300">
                          {t('version.updateFailed')}
                        </p>
                        <p className="truncate text-xs text-red-600/70 dark:text-red-400/70">{updateError}</p>
                      </div>
                    </div>

                    {/* Retry button */}
                    <button
                      onClick={() => void handleUpdate()}
                      disabled={updating}
                      className="flex w-full items-center justify-center gap-2 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {t('version.retry')}
                    </button>
                  </div>
                ) : updateSuccess && needRestart ? (
                  /* Priority 2: Update success - need restart */
                  <div className="space-y-2">
                    <div className="flex items-center gap-3 rounded-lg border border-green-200 bg-green-50/[0.15] p-3 dark:border-green-800/50 dark:bg-green-900/20">
                      <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/50">
                        {checkSvg('h-4 w-4 text-green-600 dark:text-green-400')}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-green-700 dark:text-green-300">
                          {successKind === 'rollback'
                            ? t('version.rollbackComplete')
                            : t('version.updateComplete')}
                        </p>
                        <p className="text-xs text-green-600/70 dark:text-green-400/70">
                          {t('version.restartRequired')}
                        </p>
                      </div>
                    </div>

                    {/* Restart button with countdown */}
                    <button
                      onClick={() => void handleRestart()}
                      disabled={restarting}
                      className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {restarting ? (
                        spinnerSvg
                      ) : (
                        refreshSpinnerSvg('h-4 w-4')
                      )}
                      {restarting ? (
                        <>
                          <span>{t('version.restarting')}</span>
                          {restartCountdown > 0 && <span className="tabular-nums">({restartCountdown}s)</span>}
                        </>
                      ) : (
                        <span>{t('version.restartNow')}</span>
                      )}
                    </button>
                  </div>
                ) : hasUpdate && !isReleaseBuild ? (
                  /* Priority 3: Update available for source build - show git pull hint */
                  <div className="space-y-2">
                    {releaseInfo?.html_url && releaseInfo.html_url !== '#' && (
                      <a
                        href={releaseInfo.html_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="group flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 transition-colors hover:bg-amber-100/25 dark:border-amber-800/50 dark:bg-amber-900/20 dark:hover:bg-amber-900/30"
                      >
                        <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100/[0.15] dark:bg-amber-900/50">
                          <Icon
                            name="download"
                            size="sm"
                            strokeWidth={2}
                            className="text-amber-600 dark:text-amber-400"
                          />
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="text-sm font-medium text-amber-700 dark:text-amber-300">
                            {t('version.updateAvailable')}
                          </p>
                          <p className="text-xs text-amber-600/70 dark:text-amber-400/70">
                            v{latestVersion}
                          </p>
                        </div>
                        {chevronRightSvg(
                          'h-4 w-4 text-amber-500 transition-transform group-hover:translate-x-0.5 dark:text-amber-400'
                        )}
                      </a>
                    )}
                    {/* Source build hint */}
                    <div className="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50/[0.15] p-2 dark:border-blue-800/50 dark:bg-blue-900/20">
                      {infoSvg('h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400')}
                      <p className="text-xs text-blue-600 dark:text-blue-400">
                        {t('version.sourceModeHint')}
                      </p>
                    </div>
                  </div>
                ) : hasUpdate && isReleaseBuild ? (
                  /* Priority 4: Update available for release build - show update button */
                  <div className="space-y-2">
                    {/* Update info card */}
                    <div className="flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 dark:border-amber-800/50 dark:bg-amber-900/20">
                      <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100/[0.15] dark:bg-amber-900/50">
                        <Icon
                          name="download"
                          size="sm"
                          strokeWidth={2}
                          className="text-amber-600 dark:text-amber-400"
                        />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-amber-700 dark:text-amber-300">
                          {t('version.updateAvailable')}
                        </p>
                        <p className="text-xs text-amber-600/70 dark:text-amber-400/70">
                          v{latestVersion}
                        </p>
                      </div>
                    </div>

                    {/* Update button */}
                    <button
                      onClick={() => void handleUpdate()}
                      disabled={updating}
                      className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {updating ? spinnerSvg : <Icon name="download" size="sm" strokeWidth={2} />}
                      {updating ? t('version.updating') : t('version.updateNow')}
                    </button>

                    {/* View release link */}
                    {releaseInfo?.html_url && releaseInfo.html_url !== '#' && (
                      <a
                        href={releaseInfo.html_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center justify-center gap-1 text-xs transition-colors hover:text-gray-700 dark:hover:text-dark-200 surface-muted"
                      >
                        {t('version.viewChangelog')}
                        <Icon name="externalLink" size="xs" strokeWidth={2} />
                      </a>
                    )}
                  </div>
                ) : (
                  /* Priority 5: Up to date - GitHub link + version rollback */
                  <div className="space-y-2">
                    {releaseInfo?.html_url && releaseInfo.html_url !== '#' && (
                      <a
                        href={releaseInfo.html_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center justify-center gap-2 py-2 text-sm transition-colors hover:text-gray-700 dark:hover:text-dark-200 surface-muted"
                      >
                        <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                          <path
                            fillRule="evenodd"
                            clipRule="evenodd"
                            d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                          />
                        </svg>
                        {t('version.viewRelease')}
                      </a>
                    )}

                    {/* Version rollback entry */}
                    <div className="border-t border-gray-100 pt-2 dark:border-dark-700">
                      <button
                        onClick={() => void toggleRollbackPanel()}
                        className="group flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
                      >
                        <span className="flex items-center gap-1.5">
                          <Icon name="clock" size="xs" strokeWidth={2} />
                          {t('version.rollback')}
                        </span>
                        <Icon
                          name="chevronDown"
                          size="xs"
                          strokeWidth={2}
                          className={cn(
                            'transition-transform duration-200',
                            rollbackPanelOpen && 'rotate-180'
                          )}
                        />
                      </button>

                      {rollbackPanelOpen && (
                        <div className="mt-2 space-y-2">
                          {/* Source build: online rollback unavailable, use git instead */}
                          {!isReleaseBuild ? (
                            <div className="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50/[0.15] p-2 dark:border-blue-800/50 dark:bg-blue-900/20">
                              {infoSvg('h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400')}
                              <p className="min-w-0 flex-1 text-xs leading-4 text-blue-600 dark:text-blue-400">
                                {t('version.rollbackSourceHint')}
                              </p>
                            </div>
                          ) : rollbackVersionsLoading ? (
                            /* Loading versions */
                            <div className="flex items-center justify-center py-4">
                              <svg
                                className="h-5 w-5 animate-spin text-primary-500"
                                fill="none"
                                viewBox="0 0 24 24"
                                aria-hidden="true"
                              >
                                <circle
                                  className="opacity-25"
                                  cx="12"
                                  cy="12"
                                  r="10"
                                  stroke="currentColor"
                                  strokeWidth="4"
                                ></circle>
                                <path
                                  className="opacity-75"
                                  fill="currentColor"
                                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                                ></path>
                              </svg>
                            </div>
                          ) : rollbackVersionsError ? (
                            /* Load error + retry */
                            <div className="space-y-2">
                              <p className="rounded-lg border border-red-200 bg-red-50/[0.15] p-2.5 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400">
                                {rollbackVersionsError}
                              </p>
                              <button
                                onClick={() => void loadRollbackVersions()}
                                className="w-full rounded-lg border border-gray-200 py-1.5 text-xs transition-colors hover:surface-hover hover:text-gray-700 dark:border-dark-700 dark:hover:text-dark-200 surface-control surface-muted"
                              >
                                {t('version.retry')}
                              </button>
                            </div>
                          ) : rollbackVersions.length === 0 ? (
                            /* No versions available */
                            <p className="py-3 text-center text-xs surface-muted">
                              {t('version.noRollbackVersions')}
                            </p>
                          ) : (
                            /* Version list */
                            <>
                              <p className="px-0.5 text-[11px] surface-muted">
                                {t('version.rollbackSelectVersion')}
                              </p>

                              {rollbackVersions.map((item) => (
                                <button
                                  key={item.version}
                                  onClick={() => selectRollbackVersion(item.version)}
                                  disabled={rollingBack}
                                  className={cn(
                                    'flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60 surface-control',
                                    selectedRollbackVersion === item.version
                                      ? 'border-amber-300 bg-amber-50/[0.15] shadow-sm dark:border-amber-700 dark:bg-amber-900/20'
                                      : 'border-gray-200 hover:border-gray-300 hover:surface-hover dark:border-dark-700 dark:hover:border-dark-600 dark:hover:surface-hover'
                                  )}
                                >
                                  <span className="flex items-center gap-2">
                                    <span
                                      className={cn(
                                        'flex h-3.5 w-3.5 items-center justify-center rounded-full border transition-colors',
                                        selectedRollbackVersion === item.version
                                          ? 'border-amber-500'
                                          : 'border-gray-300 dark:border-dark-500'
                                      )}
                                    >
                                      {selectedRollbackVersion === item.version && (
                                        <span className="console-data-mark h-1.5 w-1.5 rounded-full bg-amber-500"></span>
                                      )}
                                    </span>
                                    <span
                                      className={cn(
                                        'text-sm font-semibold',
                                        selectedRollbackVersion === item.version
                                          ? 'text-amber-700 dark:text-amber-300'
                                          : 'surface-muted'
                                      )}
                                    >
                                      v{item.version}
                                    </span>
                                  </span>
                                  <span className="text-[11px] tabular-nums surface-muted">
                                    {formatPublishedAt(item.published_at)}
                                  </span>
                                </button>
                              ))}

                              {/* Selected version: manual command (per deploy method) + confirm */}
                              {selectedRollbackVersion && (
                                <div className="space-y-2">
                                  <p className="px-0.5 text-[11px] surface-muted">
                                    {t('version.manualRollbackCommand')}
                                  </p>

                                  {/* Terminal-style block with deploy-method tabs */}
                                  <div className="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
                                    <div className="flex items-center justify-between border-b border-gray-200 surface-inset px-2 py-1.5 dark:border-dark-600">
                                      <div className="flex items-center gap-0.5 rounded-md surface-inset p-0.5">
                                        {manualTabs.map((tab) => (
                                          <button
                                            key={tab.key}
                                            onClick={() => setManualTab(tab.key)}
                                            className={cn(
                                              'rounded px-2 py-0.5 text-[11px] font-medium transition-colors surface-control',
                                              manualTab === tab.key
                                                ? 'surface-field text-gray-700 shadow-sm dark:surface-field dark:text-dark-100'
                                                : 'text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200'
                                            )}
                                          >
                                            {tab.label}
                                          </button>
                                        ))}
                                      </div>
                                      <button
                                        onClick={() => void copyToClipboard(activeManualCommand)}
                                        className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-dark-200 surface-control surface-muted"
                                      >
                                        <Icon
                                          name={copied ? 'check' : 'copy'}
                                          size="xs"
                                          strokeWidth={2}
                                          className={copied ? 'text-green-500' : ''}
                                        />
                                        {copied ? t('version.copied') : t('version.copyCommand')}
                                      </button>
                                    </div>
                                    <code className="block select-all whitespace-pre-wrap break-all surface-inset p-2.5 font-mono text-[10px] leading-relaxed surface-muted">
                                      {activeManualCommand}
                                    </code>
                                  </div>

                                  <p className="flex items-start gap-1.5 px-0.5 text-[11px] leading-4 text-amber-600 dark:text-amber-400">
                                    <Icon
                                      name="exclamationTriangle"
                                      size="xs"
                                      strokeWidth={2}
                                      className="mt-px flex-shrink-0"
                                    />
                                    {t('version.rollbackWarning')}
                                  </p>

                                  {rollbackError && (
                                    <p className="rounded-lg border border-red-200 bg-red-50/[0.15] p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400">
                                      {rollbackError}
                                    </p>
                                  )}

                                  <button
                                    onClick={() => void handleRollback()}
                                    disabled={rollingBack}
                                    className="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-amber-600 disabled:cursor-not-allowed disabled:opacity-50"
                                  >
                                    {rollingBack ? spinnerSvg : <Icon name="clock" size="sm" strokeWidth={2} />}
                                    <span>
                                      {rollingBack
                                        ? t('version.rollingBack')
                                        : t('version.rollbackConfirm', {
                                            version: 'v' + selectedRollbackVersion
                                          })}
                                    </span>
                                  </button>
                                </div>
                              )}
                            </>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
