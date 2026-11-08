import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  status as fetchChannelMonitorDetail,
  type UserMonitorDetail,
  type UserMonitorView
} from '@/api/channelMonitor'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from './channelMonitorConstants'
import { useAutoRefresh } from './useAutoRefresh'
import { MonitorCardGrid } from './monitor/MonitorCardGrid'
import { MonitorDetailDialog } from './MonitorDetailDialog'
import { MonitorHero, type MonitorWindow, type OverallStatus } from './monitor/MonitorHero'

export default function ChannelStatusV1View() {
  const { t } = useTranslation()
  const appStore = useAppStore()

  const [items, setItems] = useState<UserMonitorView[]>([])
  const [loading, setLoading] = useState(false)
  const [currentWindow, setCurrentWindow] = useState<MonitorWindow>('7d')
  const [detailCache, setDetailCache] = useState<Record<number, UserMonitorDetail>>({})
  const [showDetail, setShowDetail] = useState(false)
  const [detailTarget, setDetailTarget] = useState<UserMonitorView | null>(null)

  const abortRef = useRef<AbortController | null>(null)
  const detailCacheRef = useRef<Record<number, UserMonitorDetail>>({})
  const itemsRef = useRef<UserMonitorView[]>([])
  const windowRef = useRef<MonitorWindow>(currentWindow)
  const resetCountdownRef = useRef<() => void>(() => {})
  windowRef.current = currentWindow
  itemsRef.current = items
  detailCacheRef.current = detailCache

  const loadDetail = useCallback(
    async (id: number, force = false) => {
      if (!force && detailCacheRef.current[id]) return
      try {
        const detail = await fetchChannelMonitorDetail(id)
        detailCacheRef.current = { ...detailCacheRef.current, [id]: detail }
        setDetailCache(detailCacheRef.current)
      } catch (err: unknown) {
        useAppStore.getState().showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
      }
    },
    [t]
  )

  const ensureDetailsForWindow = useCallback(
    async (window: MonitorWindow) => {
      if (window === '7d') return
      await Promise.all(itemsRef.current.map((it) => loadDetail(it.id)))
    },
    [loadDetail]
  )

  const reload = useCallback(
    async (silent = false) => {
      if (abortRef.current) abortRef.current.abort()
      const ctrl = new AbortController()
      abortRef.current = ctrl
      if (!silent) setLoading(true)
      try {
        const res = await listChannelMonitorViews({ signal: ctrl.signal })
        if (ctrl.signal.aborted || abortRef.current !== ctrl) return
        setItems(res.items || [])
      } catch (err: unknown) {
        const e = err as { name?: string; code?: string }
        if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
        useAppStore.getState().showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
      } finally {
        if (abortRef.current === ctrl) {
          if (!silent) setLoading(false)
          resetCountdownRef.current()
          abortRef.current = null
        }
      }
    },
    [t]
  )

  const refresh = useCallback(
    async (silent = false) => {
      await reload(silent)
      // After base reload, refresh any cached detail records so non-7d availability
      // values stay in sync without forcing the user to switch tabs again.
      if (windowRef.current !== '7d') {
        await Promise.all(itemsRef.current.map((it) => loadDetail(it.id, true)))
      }
    },
    [loadDetail, reload]
  )

  const autoRefresh = useAutoRefresh({
    storageKey: 'channel-status-auto-refresh',
    intervals: [30, 60, 120] as const,
    defaultInterval: DEFAULT_INTERVAL_SECONDS,
    onRefresh: () => refresh(true),
    shouldPause: () => document.hidden || loading
  })
  resetCountdownRef.current = autoRefresh.resetCountdown

  const overallStatus: OverallStatus = (() => {
    if (items.length === 0) return 'operational'
    for (const it of items) {
      if (it.primary_status === 'failed' || it.primary_status === 'error') return 'degraded'
      if (it.primary_status !== STATUS_OPERATIONAL) return 'degraded'
    }
    return 'operational'
  })()

  const detailTitle = detailTarget?.name || t('channelStatus.detailTitle')

  const handleWindowChange = useCallback(
    async (value: MonitorWindow) => {
      setCurrentWindow(value)
      await ensureDetailsForWindow(value)
    },
    [ensureDetailsForWindow]
  )

  const openDetail = useCallback((row: UserMonitorView) => {
    setDetailTarget(row)
    setShowDetail(true)
  }, [])

  const closeDetail = useCallback(() => {
    setShowDetail(false)
    setDetailTarget(null)
  }, [])

  // Replaces the Vue `watch(items, ensureDetailsForWindow)`.
  useEffect(() => {
    void ensureDetailsForWindow(windowRef.current)
  }, [items, ensureDetailsForWindow])

  // Replaces the Vue `watch(channel_monitor_enabled)`.
  const monitorEnabled = appStore.cachedPublicSettings?.channel_monitor_enabled
  useEffect(() => {
    if (monitorEnabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled) autoRefresh.start()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [monitorEnabled])

  useEffect(() => {
    void reload(false)
    if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false && autoRefresh.enabled) {
      autoRefresh.start()
    }
    return () => {
      if (abortRef.current) abortRef.current.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="workspace-page legacy-status-page">
      <MonitorHero
        className="card workspace-fixed legacy-status-heading"
        overallStatus={overallStatus}
        intervalSeconds={DEFAULT_INTERVAL_SECONDS}
        window={currentWindow}
        loading={loading}
        autoRefresh={autoRefresh}
        onWindowChange={(value) => void handleWindowChange(value)}
        onRefresh={() => void refresh()}
      />

      <div className="workspace-scroll legacy-status-cards" tabIndex={0}>
        <MonitorCardGrid
          items={items}
          window={currentWindow}
          countdownSeconds={autoRefresh.countdown}
          loading={loading}
          detailCache={detailCache}
          onCardClick={openDetail}
        />
      </div>

      <MonitorDetailDialog
        show={showDetail}
        monitorId={detailTarget?.id ?? null}
        title={detailTitle}
        onClose={closeDetail}
      />
    </div>
  )
}
