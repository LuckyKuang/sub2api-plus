import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { Pagination, Select, type SelectOption } from '@/components/ui'
import { Icon } from '@/components/Icon'
import { getLiveRequests, type LiveRequest, type LiveRequestSnapshot } from '@/api/liveRequests'
import { useAutoRefresh, type AutoRefreshState } from '@/views/user/useAutoRefresh'
import { useColumnPreferenceKey } from '@/views/admin/shared'
import { getPersistedPageSize } from '@/views/admin/shared'
import { getAdminSupportContext, supportRequestGeneration } from '@/utils/adminSupportContext'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/format'
import {
  liveAbnormal,
  liveActive,
  liveCSVValue,
  liveFirstToken,
  liveRate,
  liveStatusKey
} from '@/utils/liveRequestPresentation'
import { usageMilliseconds, usageMoney } from '@/utils/usageInspection'
import { RefreshControl } from './RefreshControl'

export interface LiveRequestsPanelProps {
  mode: 'user' | 'admin'
  workspace?: boolean
}

interface ObservationClock {
  now: number
  receivedAt: number
  serverNow: number
  healthy: boolean
}

const FIXED_COLUMNS = ['started_at', 'model', 'status']

function LiveRequestResultCell({ row }: { row: LiveRequest }) {
  const { t } = useTranslation()
  const color =
    row.status === 'completed'
      ? 'text-emerald-600 dark:text-emerald-400'
      : liveAbnormal(row)
        ? 'text-amber-600 dark:text-amber-400'
        : liveActive(row)
          ? 'text-primary-600 dark:text-primary-400'
          : 'text-gray-500'
  return (
    <>
      <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-xs font-medium ${color}`}>
        <span
          className={`console-data-mark h-1.5 w-1.5 shrink-0 rounded-full bg-current ${
            liveActive(row) ? 'motion-safe:animate-pulse' : ''
          }`}
          aria-hidden="true"
        />
        {t(liveStatusKey(row))}
      </span>
      {row.reason && (
        <p className="mt-0.5 max-w-48 whitespace-normal text-xs surface-muted">
          {t(`requestList.reason.${row.reason}`)}
        </p>
      )}
    </>
  )
}

function LiveElapsedCell({ row, clock }: { row: LiveRequest; clock: ObservationClock }) {
  const [, force] = useState(0)
  useEffect(() => {
    const timer = setInterval(() => force((value) => value + 1), 1000)
    return () => clearInterval(timer)
  }, [])
  const elapsed = Math.max(0, row.elapsed_ms + (performance.now() - clock.receivedAt))
  return <>{usageMilliseconds(elapsed)}</>
}

function LiveRequestMetricsCell({
  row,
  clock,
  performanceColumn = false
}: {
  row: LiveRequest
  clock: ObservationClock
  performanceColumn?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState({ x: 0, y: 0 })

  const show = (event: React.SyntheticEvent) => {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    setPosition({
      x: Math.max(12, Math.min(rect.left, window.innerWidth - 268)),
      y: Math.max(12, Math.min(rect.bottom + 6, window.innerHeight - 300))
    })
    setOpen(true)
  }

  const details: Array<[string, string]> = [
    [t('usage.latencyFirstByte'), usageMilliseconds(row.first_byte_ms)],
    [t('usage.latencyFirstToken'), usageMilliseconds(liveFirstToken(row))],
    [t('usage.latencyLastToken'), usageMilliseconds(row.last_token_ms)],
    [t('usage.latencyFirstOutputKind'), row.first_output_kind ?? '—'],
    [t('requestList.firstOutput'), usageMilliseconds(row.first_output_ms)],
    [t('usage.latencyDuration'), usageMilliseconds(row.duration_ms)],
    [t('usage.latencyTps'), liveRate(row)]
  ]

  return (
    <div className="space-y-0.5 text-xs tabular-nums">
      {performanceColumn ? (
        <div>
          {t('usage.latencyFirstToken')} <span className="font-medium">{usageMilliseconds(liveFirstToken(row))}</span>
        </div>
      ) : (
        <div>
          {t('usage.latencyFirstByte')} <span className="font-medium">{usageMilliseconds(row.first_byte_ms)}</span>
        </div>
      )}
      {performanceColumn ? (
        <div className="font-medium text-cyan-700 dark:text-cyan-400">{liveRate(row)}</div>
      ) : (
        <div className="surface-muted">
          {liveActive(row) || (row.status === 'unknown' && row.duration_ms == null) ? (
            <>
              {t('inspection.elapsed')} <LiveElapsedCell row={row} clock={clock} />
            </>
          ) : (
            <>
              {t('usage.latencyDuration')} {usageMilliseconds(row.duration_ms)}
            </>
          )}
        </div>
      )}
      {!performanceColumn && (
        <button
          type="button"
          className="rounded text-primary-600 underline decoration-dotted underline-offset-2 surface-control"
          aria-label={t('usage.latencyDetails')}
          aria-expanded={open}
          onMouseEnter={show}
          onMouseLeave={() => setOpen(false)}
          onFocus={show}
          onBlur={() => setOpen(false)}
          onClick={show}
          onKeyDown={(event) => {
            if (event.key === 'Escape') setOpen(false)
          }}
        >
          {t('usage.latencyDetails')}
        </button>
      )}
      {open &&
        createPortal(
          <div
            role="tooltip"
            className="pointer-events-none fixed z-[9999] w-64 max-w-[calc(100vw-24px)] rounded-xl border border-gray-700 p-3 text-xs text-white shadow-xl surface-panel"
            style={{ left: `${position.x}px`, top: `${position.y}px` }}
          >
            <dl className="space-y-2">
              {details.map(([label, value]) => (
                <div key={label} className="flex justify-between gap-3">
                  <dt className="text-gray-400">{label}</dt>
                  <dd className="text-right tabular-nums">{value}</dd>
                </div>
              ))}
            </dl>
          </div>,
          document.body
        )}
    </div>
  )
}

function LiveRequestTokensCell({ row }: { row: LiveRequest }) {
  const { t } = useTranslation()
  return (
    <>
      <div className="text-xs tabular-nums">
        <span>{row.input_tokens?.toLocaleString() ?? '—'}</span> <span className="surface-muted">→</span>{' '}
        <span>{row.output_tokens?.toLocaleString() ?? '—'}</span>
      </div>
      {(row.cache_read_tokens != null || row.cache_creation_tokens != null) && (
        <div className="mt-0.5 text-[11px] surface-muted">
          {t('inspection.cacheRead')} {row.cache_read_tokens?.toLocaleString() ?? '—'} ·{' '}
          {t('inspection.cacheWrite')} {row.cache_creation_tokens?.toLocaleString() ?? '—'}
        </div>
      )}
      {row.image_count ? (
        <div className="text-xs text-pink-600">
          {t('inspection.images')} {row.image_count} {row.image_size}
        </div>
      ) : null}
      {row.image_output_tokens ? (
        <div className="text-xs text-pink-600">
          {t('inspection.imageOutput')} {row.image_output_tokens}
        </div>
      ) : null}
      {row.audio_output_tokens ? (
        <div className="text-xs text-violet-600">
          {t('inspection.audioOutput')} {row.audio_output_tokens}
        </div>
      ) : null}
      {row.video_count ? (
        <div className="text-xs text-violet-600">
          {t('requestList.video')} {row.video_count} · {row.video_resolution} ·{' '}
          {row.video_duration_seconds ?? '—'} s
        </div>
      ) : null}
    </>
  )
}

export function LiveRequestsPanel({ mode, workspace = false }: LiveRequestsPanelProps) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.user?.id)
  const panelRef = useRef<HTMLDivElement | null>(null)
  const mobileViewportRef = useRef<HTMLDivElement | null>(null)
  const desktop = useMediaQuery('(min-width: 768px)')

  const storageKey = useColumnPreferenceKey(`${mode}-live-requests-v2`)
  const columnKey = useColumnPreferenceKey(`${mode}-live-columns-v2`)

  const [rows, setRows] = useState<LiveRequest[]>([])
  const [snapshot, setSnapshot] = useState<LiveRequestSnapshot | null>(null)
  const [pending, setPending] = useState<LiveRequest[] | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(mode === 'admin' ? getPersistedPageSize() : 20)
  const [resultFilter, setResultFilter] = useState('all')
  const [modelFilter, setModelFilter] = useState('')
  const [groupFilter, setGroupFilter] = useState('')
  const [userFilter, setUserFilter] = useState('')
  const [hiddenColumns, setHiddenColumns] = useState<string[]>(['client'])
  const [copied, setCopied] = useState(false)

  const clockRef = useRef({ now: performance.now(), receivedAt: performance.now(), serverNow: 0 })
  const readingUntil = useRef(0)
  const controller = useRef<AbortController | undefined>(undefined)

  const allColumns = useMemo<Array<{ key: string; label: string }>>(
    () => [
      { key: 'started_at', label: t('usage.time') },
      ...(mode === 'admin' ? [{ key: 'user_id', label: t('admin.usage.user') }] : []),
      { key: 'model', label: t('usage.model') },
      { key: 'request_type', label: t('usage.type') },
      { key: 'group_name', label: t('admin.usage.group') },
      { key: 'status', label: t('requestList.result') },
      { key: 'speed', label: t('requestList.speed') },
      { key: 'performance', label: t('requestList.performance') },
      { key: 'tokens', label: t('requestList.tokens') },
      { key: 'actual_cost', label: t('usage.cost') },
      { key: 'client', label: t('requestList.client') }
    ],
    [mode, t]
  )

  const columns = useMemo<Column[]>(
    () =>
      allColumns
        .filter((column) => !hiddenColumns.includes(column.key))
        .map((column) => ({ key: column.key, label: column.label })),
    [allColumns, hiddenColumns]
  )

  const saveColumns = useCallback(
    (next: string[]) => {
      try {
        localStorage.setItem(columnKey, JSON.stringify(next))
      } catch {
        /* optional display preference */
      }
    },
    [columnKey]
  )

  const toggleColumn = (key: string) => {
    if (FIXED_COLUMNS.includes(key)) return
    setHiddenColumns((prev) => {
      const next = prev.includes(key) ? prev.filter((item) => item !== key) : [...prev, key]
      saveColumns(next)
      return next
    })
  }

  const resetColumns = () => {
    setHiddenColumns(['client'])
    saveColumns(['client'])
  }

  // Restore the saved column preference once; the default hides only the client column.
  useEffect(() => {
    try {
      const saved: unknown = JSON.parse(localStorage.getItem(columnKey) ?? 'null')
      if (Array.isArray(saved)) {
        setHiddenColumns(saved.filter((key): key is string => typeof key === 'string' && !FIXED_COLUMNS.includes(key)))
      }
    } catch {
      /* optional display preference */
    }
  }, [columnKey])

  const markReading = useCallback(() => {
    readingUntil.current = performance.now() + 5000
  }, [])

  const isReading = useCallback((): boolean => {
    if (page > 1) return true
    if (expanded.size > 0) return true
    if (performance.now() < readingUntil.current) return true
    if (window.scrollY > 160) return true
    if (window.getSelection()?.toString()) return true
    if (panelRef.current?.contains(document.activeElement)) return true
    return false
  }, [page, expanded])

  const onRefresh = useCallback(async () => {
    controller.current?.abort()
    const nextController = new AbortController()
    controller.current = nextController
    const { signal } = nextController
    const scope = supportRequestGeneration()
    const owner = useAuthStore.getState().user?.id
    const data = await getLiveRequests(mode, signal)
    if (signal.aborted || scope !== supportRequestGeneration() || owner !== useAuthStore.getState().user?.id) return
    clockRef.current = {
      now: performance.now(),
      receivedAt: performance.now(),
      serverNow: Date.parse(data.server_now)
    }
    setSnapshot(data)
    setRows((prevRows) => {
      if (prevRows.length && isReading()) {
        const latest = new Map(data.items.map((item) => [item.id, item]))
        const merged = prevRows.map((item) => {
          const fresh = latest.get(item.id)
          if (fresh) return fresh
          return liveActive(item) ? { ...item, status: 'unknown' as const } : item
        })
        setPending(
          data.items.some((item) => !merged.some((old) => old.id === item.id)) ? data.items : null
        )
        return merged
      }
      setPending(null)
      return data.items
    })
  }, [mode, isReading])

  const refresh: AutoRefreshState = useAutoRefresh({
    storageKey,
    intervals: [2, 5, 15, 30],
    defaultInterval: 2,
    defaultEnabled: true,
    temporaryPause: true,
    minimumIntervalSeconds: () =>
      Math.max(
        snapshot?.poll_after_seconds ?? 2,
        snapshot?.items.length === 0 ? 5 : 2,
        // 同账号多面板共享轮询预算
        0
      ),
    onRefresh
  })

  const clock: ObservationClock = {
    now: clockRef.current.now,
    receivedAt: clockRef.current.receivedAt,
    serverNow: clockRef.current.serverNow,
    healthy: refresh.enabled && !refresh.error && !refresh.paused
  }

  const showLatest = () => {
    if (pending) setRows(pending)
    setPending(null)
    setExpanded(new Set())
    setPage(1)
  }

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const filteredRows = useMemo(
    () =>
      rows.filter(
        (row) =>
          (resultFilter === 'all' ||
            (resultFilter === 'success' ? row.status === 'completed' : liveAbnormal(row))) &&
          (!modelFilter || row.model === modelFilter) &&
          (!groupFilter || row.group_name === groupFilter) &&
          (!userFilter || String(row.user_id) === userFilter)
      ),
    [rows, resultFilter, modelFilter, groupFilter, userFilter]
  )

  const displayedRows = useMemo(
    () => filteredRows.slice((page - 1) * pageSize, page * pageSize),
    [filteredRows, page, pageSize]
  )

  useEffect(() => {
    setPage(1)
  }, [resultFilter, modelFilter, groupFilter, userFilter, pageSize])

  useEffect(() => {
    setPage((prev) => Math.min(prev, Math.max(1, Math.ceil(filteredRows.length / pageSize))))
  }, [filteredRows.length, pageSize])

  useEffect(() => {
    void refresh.refresh()
    const timer = setInterval(() => {
      if (!document.hidden) clockRef.current.now = performance.now()
    }, 250)
    return () => {
      clearInterval(timer)
      controller.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Support-context or account switches reset the panel instead of mixing rows.
  const supportUserId = getAdminSupportContext()?.userId
  useEffect(() => {
    controller.current?.abort()
    setRows([])
    setSnapshot(null)
    setPending(null)
    setExpanded(new Set())
    setPage(1)
  }, [supportUserId, userId])

  const options = (label: string, values: string[]): SelectOption[] => [
    { value: '', label },
    ...Array.from(new Set(values.filter(Boolean)))
      .sort()
      .map((value) => ({ value, label: value }))
  ]

  const modelOptions = options(
    t('requestList.allModels'),
    rows.map((row) => row.model)
  )
  const groupOptions = options(
    t('requestList.allGroups'),
    rows.map((row) => row.group_name ?? '')
  )
  const userOptions: SelectOption[] = [
    { value: '', label: t('requestList.allUsers') },
    ...Array.from(
      new Map(
        rows.map((row) => [
          row.user_id,
          { value: String(row.user_id), label: `${row.user_email || '—'} · #${row.user_id}` }
        ])
      ).values()
    )
  ]

  const typeLabel = (row: LiveRequest): string => {
    if (row.native_compaction_v2) return t('usage.compactionOnly')
    const labels: Record<string, string> = {
      sync: t('requestList.sync'),
      stream: t('requestList.stream'),
      ws_v2: 'WS',
      live: 'Live',
      cyber: 'Cyber',
      unknown: '—'
    }
    return labels[row.request_type] ?? '—'
  }

  const cost = (row: LiveRequest): string =>
    row.actual_cost != null
      ? usageMoney(row.actual_cost)
      : t(liveActive(row) ? 'inspection.pending' : 'requestList.unsettled')

  const shortTime = (value: string): string => {
    const date = new Date(value)
    return date.toDateString() === new Date().toDateString() ? date.toLocaleTimeString() : date.toLocaleString()
  }

  const clientName = (row: LiveRequest): string => row.user_agent?.split(/[ /]/)[0] || '—'

  const copyClient = async (row: LiveRequest) => {
    try {
      await navigator.clipboard.writeText(`${row.user_agent || '—'}\n${row.ip_address || '—'}`)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  const exportWindow = () => {
    const header = [
      'time',
      ...(mode === 'admin' ? ['user_id', 'user_email', 'key_name'] : []),
      'model',
      'type',
      'group',
      'result',
      'reason',
      'first_byte_ms',
      'duration_ms',
      'first_token_ms',
      'input_tokens',
      'output_tokens',
      'cache_read_tokens',
      'cache_creation_tokens',
      'actual_cost',
      'user_agent',
      'ip_address'
    ]
    const body = filteredRows.map((row) => [
      row.started_at,
      ...(mode === 'admin' ? [row.user_id, row.user_email, row.key_name] : []),
      row.model,
      row.request_type,
      row.group_name,
      row.status,
      row.reason,
      row.first_byte_ms,
      row.duration_ms,
      row.first_token_ms,
      row.input_tokens,
      row.output_tokens,
      row.cache_read_tokens,
      row.cache_creation_tokens,
      row.actual_cost,
      row.user_agent,
      row.ip_address
    ])
    const url = URL.createObjectURL(
      new Blob(['\uFEFF' + [header, ...body].map((row) => row.map(liveCSVValue).join(',')).join('\r\n')], {
        type: 'text/csv;charset=utf-8;'
      })
    )
    const link = document.createElement('a')
    link.href = url
    link.download = `requests-window-${Date.now()}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  const renderCell = (key: string, row: LiveRequest, value: unknown): React.ReactNode => {
    switch (key) {
      case 'started_at':
        return (
          <time className="whitespace-nowrap text-xs tabular-nums" dateTime={row.started_at} title={formatDateTime(row.started_at)}>
            {shortTime(row.started_at)}
          </time>
        )
      case 'model':
        return (
          <>
            <div className="max-w-52 break-words text-sm font-medium" title={row.model}>
              {row.model}
            </div>
            {row.reasoning_effort && <div className="text-xs surface-muted">{row.reasoning_effort}</div>}
          </>
        )
      case 'user_id':
        return (
          <>
            <div className="max-w-48 break-all text-xs">{row.user_email || '—'}</div>
            <span className="text-xs surface-muted">#{row.user_id}</span>
            <div className="max-w-40 break-all text-xs surface-muted">{row.key_name}</div>
          </>
        )
      case 'request_type':
        return (
          <>
            <span className="badge badge-gray">{typeLabel(row)}</span>
            {row.turn && <div className="text-xs surface-muted">#{row.turn}</div>}
          </>
        )
      case 'group_name':
        return <span className="block max-w-36 break-words text-xs">{row.group_name || '—'}</span>
      case 'status':
        return <LiveRequestResultCell row={row} />
      case 'speed':
        return <LiveRequestMetricsCell row={row} clock={clock} />
      case 'performance':
        return <LiveRequestMetricsCell row={row} clock={clock} performanceColumn />
      case 'tokens':
        return <LiveRequestTokensCell row={row} />
      case 'actual_cost':
        return (
          <span
            className={`whitespace-nowrap text-xs font-medium tabular-nums ${
              row.actual_cost != null ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500'
            }`}
          >
            {cost(row)}
          </span>
        )
      case 'client':
        return (
          <button
            type="button"
            className="max-w-44 text-left text-xs surface-control"
            title={row.user_agent}
            onClick={() => void copyClient(row)}
          >
            <span className="block truncate">{clientName(row)}</span>
            <span className="block break-all surface-muted">{row.ip_address || '—'}</span>
          </button>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  return (
    <div
      ref={panelRef}
      className={`min-w-0 ${
        workspace ? 'workspace-body' : 'space-y-4'
      } ${mode === 'admin' ? (workspace ? 'live-admin-workspace gap-4' : 'p-3 sm:p-4') : 'live-user-workspace gap-4'}`}
      onPointerDown={markReading}
      onFocus={markReading}
      onWheel={markReading}
      onTouchStart={markReading}
    >
      <div
        className={`live-request-controls ${
          mode === 'user' || workspace ? 'card workspace-fixed space-y-3 p-4' : 'space-y-4'
        }`}
      >
        <div className={`live-request-header flex flex-wrap items-center justify-between gap-3 ${workspace ? 'workspace-fixed' : ''}`}>
          <div className="live-request-hint text-xs surface-muted">
            <p>{t('inspection.liveHint')}</p>
            <p>{t('inspection.liveWindow', { limit: snapshot?.limit ?? 200 })}</p>
          </div>
          <RefreshControl state={refresh} switchVisible manual />
        </div>
        <div className={`flex flex-wrap items-end gap-2 ${workspace ? 'workspace-fixed' : ''}`}>
          <div className="segmented-control" role="group" aria-label={t('requestList.result')}>
            {(['all', 'success', 'abnormal'] as const).map((item) => (
              <button
                key={item}
                type="button"
                className="segmented-option"
                aria-pressed={resultFilter === item}
                onClick={() => setResultFilter(item)}
              >
                {t(`requestList.${item}`)}
              </button>
            ))}
          </div>
          <Select
            value={modelFilter}
            className="min-w-0 w-44 max-w-full"
            options={modelOptions}
            ariaLabel={t('usage.model')}
            searchable={false}
            onValueChange={(value) => setModelFilter(String(value ?? ''))}
          />
          <Select
            value={groupFilter}
            className="min-w-0 w-40 max-w-full"
            options={groupOptions}
            ariaLabel={t('admin.usage.group')}
            searchable={false}
            onValueChange={(value) => setGroupFilter(String(value ?? ''))}
          />
          {mode === 'admin' && (
            <Select
              value={userFilter}
              className="min-w-0 w-36 max-w-full"
              options={userOptions}
              ariaLabel={t('admin.usage.user')}
              searchable={false}
              onValueChange={(value) => setUserFilter(String(value ?? ''))}
            />
          )}
          <div className="ml-auto flex gap-2">
            <button
              type="button"
              className="btn btn-secondary text-xs"
              disabled={!filteredRows.length}
              onClick={exportWindow}
            >
              {t('requestList.exportWindow')}
            </button>
            <details className="relative">
              <summary className="btn btn-secondary cursor-pointer list-none text-xs">{t('requestList.columns')}</summary>
              <div className="absolute right-0 z-30 mt-2 w-52 rounded-xl border border-gray-200 p-2 shadow-xl dark:border-dark-600 surface-panel">
                {allColumns.map((column) => (
                  <label
                    key={column.key}
                    className="flex min-h-9 cursor-pointer items-center gap-2 rounded px-2 text-xs hover:surface-hover"
                  >
                    <input
                      type="checkbox"
                      checked={!hiddenColumns.includes(column.key)}
                      disabled={FIXED_COLUMNS.includes(column.key)}
                      onChange={() => toggleColumn(column.key)}
                    />
                    {column.label}
                  </label>
                ))}
                <button
                  type="button"
                  className="min-h-9 w-full border-t px-2 text-left text-xs text-primary-600 surface-control"
                  onClick={resetColumns}
                >
                  {t('inspection.resetColumns')}
                </button>
              </div>
            </details>
          </div>
        </div>
        <p className={`text-xs surface-muted ${workspace ? 'workspace-fixed' : ''}`}>{t('requestList.windowFilters')}</p>
        {snapshot?.truncated && (
          <p role="status" className={`text-xs text-amber-600 ${workspace ? 'workspace-fixed' : ''}`}>
            {t('inspection.liveTruncated')}
          </p>
        )}
        {pending && (
          <button
            type="button"
            className={`btn btn-secondary ${workspace ? 'workspace-fixed' : ''}`}
            onClick={showLatest}
          >
            {t('inspection.newRecords')}
          </button>
        )}
      </div>
      <div className={mode === 'user' || workspace ? `card overflow-hidden ${workspace ? 'workspace-table-card workspace-shell' : ''}` : ''}>
        {desktop ? (
          <div className={workspace ? 'workspace-body !gap-0' : 'overflow-x-auto'}>
            <DataTable
              className={workspace ? 'workspace-table-viewport' : undefined}
              columns={columns}
              data={displayedRows}
              loading={!snapshot && refresh.fetching}
              renderCell={(key, row, cellValue) => renderCell(key, row as LiveRequest, cellValue)}
              renderEmpty={() =>
                mode === 'admin' && workspace ? (
                  <div className="live-admin-empty flex flex-col items-center" data-test="live-empty-state">
                    <Icon name="inbox" size="xl" className="live-admin-empty-icon mb-4 h-12 w-12 surface-muted" data-test="live-empty-icon" />
                    <p className="live-admin-empty-message text-lg font-medium text-gray-900 dark:text-gray-100" data-test="live-empty-message">
                      {t('empty.noData')}
                    </p>
                  </div>
                ) : (
                  <p className="p-6 text-center text-sm surface-muted">{t('empty.noData')}</p>
                )
              }
            />
          </div>
        ) : (
          <div
            ref={mobileViewportRef}
            className={`space-y-2 ${workspace ? 'workspace-scroll p-3' : ''}`}
            aria-busy={refresh.fetching}
          >
            {!snapshot && refresh.fetching && <p className="p-6 text-center text-sm surface-muted">{t('common.loading')}</p>}
            {!(!snapshot && refresh.fetching) && !filteredRows.length && (
              <p className="p-6 text-center text-sm surface-muted">{t('empty.noData')}</p>
            )}
            {displayedRows.map((row) => (
              <article key={row.id} className="min-w-0 rounded-xl border border-gray-200 surface-inset p-3 dark:border-dark-700">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 break-all text-sm font-medium">
                    {row.model} <span className="text-[11px] font-normal surface-muted">{typeLabel(row)}</span>
                  </div>
                  <span className="shrink-0 text-xs tabular-nums">{cost(row)}</span>
                </div>
                <div className="mt-1 flex flex-wrap items-center justify-between gap-1">
                  <div>
                    <LiveRequestResultCell row={row} />
                  </div>
                  <time className="text-xs text-gray-500" dateTime={row.started_at}>
                    {shortTime(row.started_at)}
                  </time>
                </div>
                <div className="mt-1 flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <LiveRequestTokensCell row={row} />
                  </div>
                  <span className="min-w-0 max-w-[40%] break-words text-right text-xs surface-muted">
                    {row.group_name || '—'}
                  </span>
                </div>
                <button
                  type="button"
                  className="mt-1 min-h-9 text-xs text-primary-600 surface-control"
                  aria-expanded={expanded.has(row.id)}
                  aria-controls={`live-fields-${row.id}`}
                  onClick={() => toggleExpanded(row.id)}
                >
                  {t(expanded.has(row.id) ? 'requestList.less' : 'requestList.more')}
                </button>
                {expanded.has(row.id) && (
                  <dl id={`live-fields-${row.id}`} className="space-y-3 border-t border-gray-200 pt-3 text-xs dark:border-dark-700">
                    {mode === 'admin' && (
                      <div>
                        <dt className="text-gray-500">{t('admin.usage.user')}</dt>
                        <dd className="break-all">
                          {row.user_email || '—'} · #{row.user_id}
                        </dd>
                      </div>
                    )}
                    <div>
                      <dt className="text-gray-500">{t('usage.apiKeyFilter')}</dt>
                      <dd className="break-all">{row.key_name || '—'}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t('usage.time')}</dt>
                      <dd>{formatDateTime(row.started_at)}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t('admin.usage.group')}</dt>
                      <dd className="break-all">{row.group_name || '—'}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t('requestList.speed')}</dt>
                      <dd>
                        <LiveRequestMetricsCell row={row} clock={clock} />
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t('requestList.performance')}</dt>
                      <dd>
                        <LiveRequestMetricsCell row={row} clock={clock} performanceColumn />
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t('usage.userAgent')}</dt>
                      <dd className="select-text break-all">{row.user_agent || '—'}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">IP</dt>
                      <dd className="select-text break-all">{row.ip_address || '—'}</dd>
                    </div>
                    <button type="button" className="btn btn-secondary text-xs" onClick={() => void copyClient(row)}>
                      {t('inspection.copy')}
                    </button>
                  </dl>
                )}
              </article>
            ))}
          </div>
        )}
      </div>
      {copied && (
        <p role="status" className={`text-xs text-primary-600 ${workspace ? 'workspace-fixed' : ''}`}>
          {t('inspection.copied')}
        </p>
      )}
      <Pagination
        className="workspace-fixed"
        page={page}
        pageSize={pageSize}
        total={filteredRows.length}
        onPageChange={setPage}
        onPageSizeChange={setPageSize}
      />
    </div>
  )
}

function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(
    () => typeof window !== 'undefined' && window.matchMedia(query).matches
  )
  useEffect(() => {
    const list = window.matchMedia(query)
    const update = () => setMatches(list.matches)
    update()
    list.addEventListener('change', update)
    return () => list.removeEventListener('change', update)
  }, [query])
  return matches
}

export default LiveRequestsPanel
