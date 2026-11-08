import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react'
import {
  useVirtualizer,
  observeElementRect as observeElementRectDefault,
  type Virtualizer
} from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from './Icon'
import { useUserTableLayout } from '@/composables/useUserTableLayout'

export interface Column {
  key: string
  label: string
  sortable?: boolean
  /** Pin this column using its measured width; false opts out of default pinning. */
  sticky?: 'left' | 'right' | false
  class?: string
  formatter?: (value: any, row: any) => string
}

export interface DataTableSortContext {
  /** Current sort key ('' when unsorted). */
  key: string
  order: 'asc' | 'desc'
}

export interface DataTableProps {
  columns: Column[]
  data: any[]
  loading?: boolean
  stickyFirstColumn?: boolean
  stickyActionsColumn?: boolean
  expandableActions?: boolean
  actionsCount?: number // 操作按钮总数，用于判断是否需要展开功能
  rowKey?: string | ((row: any) => string | number)
  /**
   * Default sort configuration (only applied when there is no persisted sort state)
   */
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  /**
   * Persist sort state (key + order) to localStorage using this key.
   * If provided, DataTable will load the stored sort state on mount.
   */
  sortStorageKey?: string
  /**
   * Enable server-side sorting mode. When true, clicking sort headers
   * will emit 'sort' events instead of performing client-side sorting.
   */
  serverSideSort?: boolean
  /** Emit 'rowClick' on row/card click and show pointer cursor (interactive cells should stop propagation) */
  clickableRows?: boolean
  activeRowKey?: string | number | null
  /** Estimated row height in px for the virtualizer (default 56) */
  estimateRowHeight?: number
  /** Number of rows to render beyond the visible area (default 5) */
  overscan?: number
  /**
   * Only virtualize when the row count exceeds this threshold (default 100).
   * Smaller lists render in full, avoiding the scroll-compensation jank caused by
   * estimated-vs-actual row heights when rows have variable height.
   */
  virtualizeThreshold?: number
  /** Enable controlled row selection. Stable row keys are strongly recommended. */
  selectable?: boolean
  /** Selected row keys. Keys outside the current data page are preserved. */
  selectedKeys?: Array<string | number>
  /** Accessible label for a row selection checkbox. */
  selectionLabel?: string | ((row: any) => string)
  /** Render prop for the Vue `cell-${key}` slots; key is the column key. */
  renderCell?: (key: string, row: any, value: any, expanded: boolean) => ReactNode
  /** Render prop for the Vue `header-${key}` slots; key is the column key. */
  renderHeader?: (key: string, column: Column, sort: DataTableSortContext) => ReactNode
  /** Render prop for the Vue `empty` slot. */
  renderEmpty?: () => ReactNode
  /** Vue `sort` emit. */
  onSort?: (key: string, order: 'asc' | 'desc') => void
  /** Vue `rowClick` emit. */
  onRowClick?: (row: any) => void
  /** Vue `update:selectedKeys` emit. */
  onSelectedKeysChange?: (keys: Array<string | number>) => void
  /** Vue `selectionChange` emit. */
  onSelectionChange?: (keys: Array<string | number>) => void
  className?: string
}

export interface DataTableHandle {
  sortedData: any[]
  resolveRowKey: (row: any, index: number) => string | number
  shouldVirtualize: boolean
  actionsColumnNeedsExpanding: boolean
  tableWrapperEl: HTMLDivElement | null
  // User tables can render cards even at desktop widths when columns do not fit.
  scrollViewportEl: HTMLDivElement | null
  virtualizer: Virtualizer<HTMLDivElement, HTMLTableRowElement> | null
}

const desktopViewportQuery = '(min-width: 768px)'

interface PersistedSortState {
  key: string
  order: 'asc' | 'desc'
}

type RowIdentityToken = string | number | object | symbol

const collator = new Intl.Collator(undefined, {
  numeric: true,
  sensitivity: 'base'
})

const isNullishOrEmpty = (value: any) => value === null || value === undefined || value === ''

const toFiniteNumberOrNull = (value: any): number | null => {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null
  if (typeof value === 'boolean') return value ? 1 : 0
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return null
    const n = Number(trimmed)
    return Number.isFinite(n) ? n : null
  }
  return null
}

const toSortableString = (value: any): string => {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (value instanceof Date) return value.toISOString()
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

const compareSortValues = (a: any, b: any): number => {
  const aEmpty = isNullishOrEmpty(a)
  const bEmpty = isNullishOrEmpty(b)
  if (aEmpty && bEmpty) return 0
  if (aEmpty) return 1
  if (bEmpty) return -1

  const aNum = toFiniteNumberOrNull(a)
  const bNum = toFiniteNumberOrNull(b)
  if (aNum !== null && bNum !== null) {
    if (aNum === bNum) return 0
    return aNum < bNum ? -1 : 1
  }

  const aStr = toSortableString(a)
  const bStr = toSortableString(b)
  const res = collator.compare(aStr, bStr)
  if (res === 0) return 0
  return res < 0 ? -1 : 1
}

// 兜底高度:表格区域大致 = 视口高度 - 顶栏/外边距/筛选/分页 ≈ 320px
const estimatedViewportHeight = () => {
  if (typeof window === 'undefined') return 600
  return Math.max(window.innerHeight - 320, 400)
}

// 覆写默认 observeElementRect:过滤掉 0 高度读数(根治整表空白的关键)
const observeElementRectNonZero = (
  instance: Virtualizer<HTMLDivElement, HTMLTableRowElement>,
  cb: (rect: { width: number; height: number }) => void
) =>
  observeElementRectDefault(instance, (rect) => {
    if (rect.height > 0) cb(rect)
  })

export const DataTable = forwardRef<DataTableHandle, DataTableProps>(function DataTable(props, ref) {
  const {
    columns,
    data,
    loading = false,
    stickyFirstColumn = true,
    stickyActionsColumn = true,
    expandableActions = true,
    actionsCount: _actionsCount,
    rowKey,
    defaultSortKey,
    defaultSortOrder = 'asc',
    sortStorageKey,
    serverSideSort = false,
    clickableRows = false,
    activeRowKey = null,
    estimateRowHeight,
    overscan,
    virtualizeThreshold,
    selectable = false,
    selectedKeys,
    selectionLabel,
    renderCell,
    renderHeader,
    renderEmpty,
    onSort,
    onRowClick,
    onSelectedKeysChange,
    onSelectionChange,
    className,
    ...rest
  } = props

  const { t } = useTranslation()
  const userTableLayout = useUserTableLayout()

  const [isDesktopViewport, setIsDesktopViewport] = useState(
    () => typeof window === 'undefined' || window.matchMedia(desktopViewportQuery).matches
  )
  const [cardsWrapper, setCardsWrapper] = useState<HTMLDivElement | null>(null)
  const [tableWrapper, setTableWrapper] = useState<HTMLDivElement | null>(null)
  // Ref mirrors keep measurement callbacks stable across element swaps, so the
  // hidden fit probe cannot re-trigger itself through state identity changes.
  const cardsWrapperRef = useRef<HTMLDivElement | null>(null)
  cardsWrapperRef.current = cardsWrapper
  const tableWrapperRef = useRef<HTMLDivElement | null>(null)
  tableWrapperRef.current = tableWrapper
  const [tableDoesNotFit, setTableDoesNotFit] = useState(false)
  const [probingTable, setProbingTable] = useState(false)
  const [probeWidth, setProbeWidth] = useState(0)
  const [actionsExpanded, setActionsExpanded] = useState(false)
  const [actionsColumnNeedsExpanding, setActionsColumnNeedsExpanding] = useState(false)
  const [sortKey, setSortKey] = useState('')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
  const [stickyOffsets, setStickyOffsets] = useState<Record<string, number>>({})

  const requiredTableWidthRef = useRef(0)
  const fitResizeObserverRef = useRef<ResizeObserver | null>(null)
  const fitMeasurementPendingRef = useRef(false)
  const unmountedRef = useRef(false)
  const resizeObserverRef = useRef<ResizeObserver | null>(null)
  const resizeHandlerRef = useRef<(() => void) | null>(null)
  const didInitSortRef = useRef(false)
  const actionsExpandedRef = useRef(actionsExpanded)
  actionsExpandedRef.current = actionsExpanded
  const measuringActionsRef = useRef(false)
  const wasExpandedRef = useRef(false)

  const showDesktopTable = isDesktopViewport && (!userTableLayout || !tableDoesNotFit)
  const showDesktopTableRef = useRef(showDesktopTable)
  showDesktopTableRef.current = showDesktopTable
  const isDesktopViewportRef = useRef(isDesktopViewport)
  isDesktopViewportRef.current = isDesktopViewport
  const probingTableRef = useRef(probingTable)
  probingTableRef.current = probingTable

  const tableProbeStyle: CSSProperties = {
    position: 'absolute',
    visibility: 'hidden',
    pointerEvents: 'none',
    width: `${probeWidth}px`,
    top: '0',
    left: '0'
  }

  // --- Sorting ---------------------------------------------------------------

  const getSortableKeys = useCallback(() => {
    const keys = new Set<string>()
    for (const col of columns) {
      if (col.sortable) keys.add(col.key)
    }
    return keys
  }, [columns])

  const normalizeSortKey = useCallback(
    (candidate: string) => {
      if (!candidate) return ''
      const sortableKeys = getSortableKeys()
      return sortableKeys.has(candidate) ? candidate : ''
    },
    [getSortableKeys]
  )

  const normalizeSortOrder = (candidate: any): 'asc' | 'desc' => {
    return candidate === 'desc' ? 'desc' : 'asc'
  }

  const readPersistedSortState = useCallback((): PersistedSortState | null => {
    if (!sortStorageKey) return null
    try {
      const raw = localStorage.getItem(sortStorageKey)
      if (!raw) return null
      const parsed = JSON.parse(raw) as Partial<PersistedSortState>
      const key = normalizeSortKey(typeof parsed.key === 'string' ? parsed.key : '')
      if (!key) return null
      return { key, order: normalizeSortOrder(parsed.order) }
    } catch (e) {
      console.error('[DataTable] Failed to read persisted sort state:', e)
      return null
    }
  }, [sortStorageKey, normalizeSortKey])

  const writePersistedSortState = useCallback(
    (state: PersistedSortState) => {
      if (!sortStorageKey) return
      try {
        localStorage.setItem(sortStorageKey, JSON.stringify(state))
      } catch (e) {
        console.error('[DataTable] Failed to persist sort state:', e)
      }
    },
    [sortStorageKey]
  )

  const resolveInitialSortState = useCallback((): PersistedSortState | null => {
    const persisted = readPersistedSortState()
    if (persisted) return persisted

    const key = normalizeSortKey(defaultSortKey || '')
    if (!key) return null
    return { key, order: normalizeSortOrder(defaultSortOrder) }
  }, [readPersistedSortState, normalizeSortKey, defaultSortKey, defaultSortOrder])

  const applySortState = useCallback((state: PersistedSortState | null) => {
    if (!state) return
    setSortKey(state.key)
    setSortOrder(state.order)
  }, [])

  const getSortIndicatorClass = (key: string, order: 'asc' | 'desc') => {
    return sortKey === key && sortOrder === order
      ? 'text-primary-600 dark:text-primary-400'
      : 'text-gray-300 transition-colors dark:text-dark-500'
  }

  const getColumnAriaSort = (key: string) => {
    if (sortKey !== key) return 'none'
    return sortOrder === 'asc' ? 'ascending' : 'descending'
  }

  const getHeaderContentAlignmentClass = (column: Column) => {
    const columnClassName = column.class || ''
    if (columnClassName.includes('text-center')) return 'justify-center'
    if (columnClassName.includes('text-right')) return 'justify-end'
    return 'justify-start'
  }

  const handleSort = (key: string) => {
    let newOrder: 'asc' | 'desc' = 'asc'
    if (sortKey === key) {
      newOrder = sortOrder === 'asc' ? 'desc' : 'asc'
    }

    setSortKey(key)
    setSortOrder(newOrder)
    if (serverSideSort) {
      // Server-side sort mode: emit event and update internal state for UI feedback
      onSort?.(key, newOrder)
    }
    // Client-side sort mode: just update internal state
  }

  // Init persisted/default sort state
  useEffect(() => {
    const initial = resolveInitialSortState()
    applySortState(initial)
    didInitSortRef.current = true
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const columnsSignature = useMemo(
    () =>
      columns
        .map((column) => `${column.key}:${column.label}:${column.sortable ? '1' : '0'}:${column.sticky}`)
        .join('|'),
    [columns]
  )

  const columnsSignatureInitRef = useRef(true)
  useEffect(() => {
    if (columnsSignatureInitRef.current) {
      columnsSignatureInitRef.current = false
      return
    }
    // If current sort key is no longer sortable/visible, fall back to default/persisted.
    const normalized = normalizeSortKey(sortKey)
    if (!sortKey) {
      applySortState(resolveInitialSortState())
      return
    }

    if (!normalized) {
      const fallback = resolveInitialSortState()
      if (fallback) {
        applySortState(fallback)
      } else {
        setSortKey('')
        setSortOrder('asc')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [columnsSignature])

  useEffect(() => {
    if (!didInitSortRef.current) return
    if (!sortStorageKey) return
    const key = normalizeSortKey(sortKey)
    if (!key) return
    writePersistedSortState({ key, order: normalizeSortOrder(sortOrder) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sortKey, sortOrder])

  // --- Row keys and client-side sorting --------------------------------------

  const resolveStableRowKey = useCallback(
    (row: any): string | number | undefined => {
      if (typeof rowKey === 'function') {
        const key = rowKey(row)
        return key ?? undefined
      }
      if (typeof rowKey === 'string' && rowKey) {
        const key = row?.[rowKey]
        return key ?? undefined
      }
      const key = row?.id
      return key ?? undefined
    },
    [rowKey]
  )

  const resolveRowKey = useCallback(
    (row: any, index: number) => resolveStableRowKey(row) ?? index,
    [resolveStableRowKey]
  )

  const sortedData = useMemo(() => {
    // Server-side sort mode: return data as-is (server handles sorting)
    if (serverSideSort || !sortKey || !data) return data

    const key = sortKey
    const order = sortOrder

    // Stable sort (tie-break with original index) to avoid jitter when values are equal.
    return data
      .map((row, index) => ({ row, index }))
      .sort((a, b) => {
        const cmp = compareSortValues(a.row?.[key], b.row?.[key])
        if (cmp !== 0) return order === 'asc' ? cmp : -cmp
        return a.index - b.index
      })
      .map((item) => item.row)
  }, [data, sortKey, sortOrder, serverSideSort])

  const dataColumns = useMemo(() => columns.filter((column) => column.key !== 'actions'), [columns])
  const actionsColumn = useMemo(
    () => columns.find((column) => column.key === 'actions'),
    [columns]
  )

  const tableColumnCount = columns.length + (selectable ? 1 : 0)
  const selectedKeySet = useMemo(() => new Set(selectedKeys), [selectedKeys])
  const visibleRowKeys = useMemo(
    () => (sortedData ?? []).map((row, index) => resolveRowKey(row, index)),
    [sortedData, resolveRowKey]
  )
  const allVisibleSelected =
    visibleRowKeys.length > 0 && visibleRowKeys.every((key) => selectedKeySet.has(key))
  const someVisibleSelected =
    !allVisibleSelected && visibleRowKeys.some((key) => selectedKeySet.has(key))

  const emitSelection = useCallback(
    (next: Set<string | number>) => {
      const keys = Array.from(next)
      onSelectedKeysChange?.(keys)
      onSelectionChange?.(keys)
    },
    [onSelectedKeysChange, onSelectionChange]
  )

  const isRowSelected = useCallback(
    (row: any, index: number) => selectedKeySet.has(resolveRowKey(row, index)),
    [selectedKeySet, resolveRowKey]
  )

  const getRowSelectionLabel = useCallback(
    (row: any, index: number) => {
      if (typeof selectionLabel === 'function') return selectionLabel(row)
      if (selectionLabel) return selectionLabel
      return `${t('common.selectOption')} ${resolveRowKey(row, index)}`
    },
    [selectionLabel, resolveRowKey, t]
  )

  const toggleRowSelection = useCallback(
    (row: any, index: number, checked: boolean) => {
      const next = new Set(selectedKeys)
      const key = resolveRowKey(row, index)
      if (checked) next.add(key)
      else next.delete(key)
      emitSelection(next)
    },
    [selectedKeys, resolveRowKey, emitSelection]
  )

  const toggleAllVisible = useCallback(
    (checked: boolean) => {
      const next = new Set(selectedKeys)
      for (const key of visibleRowKeys) {
        if (checked) next.add(key)
        else next.delete(key)
      }
      emitSelection(next)
    },
    [selectedKeys, visibleRowKeys, emitSelection]
  )

  function handleRowClick(event: React.MouseEvent, row: unknown) {
    if (
      !clickableRows ||
      (event.target as Element | null)?.closest('button, a, input, select, textarea, [role="button"]')
    ) {
      return
    }
    onRowClick?.(row)
  }

  // --- Sticky columns ---------------------------------------------------------

  const hasSelectColumn = columns.length > 0 && columns[0].key === 'select'

  const getStickyColumnSide = useCallback(
    (column: Column, index: number): 'left' | 'right' | undefined => {
      if (userTableLayout) return undefined
      if (column.sticky !== undefined) return column.sticky || undefined
      if (stickyFirstColumn && (index === 0 || (hasSelectColumn && index === 1))) return 'left'
      if (stickyActionsColumn && column.key === 'actions') return 'right'
      return undefined
    },
    [userTableLayout, stickyFirstColumn, hasSelectColumn, stickyActionsColumn]
  )

  // Read actual header widths, including translated labels, selection controls and
  // user column preferences. The same offsets are used by headers and body cells.
  const updateStickyOffsets = useCallback(() => {
    const headers = [...(tableWrapper?.querySelectorAll<HTMLTableCellElement>('thead th') ?? [])]
    const offsets: Record<string, number> = {}
    for (const side of ['left', 'right'] as const) {
      let offset = 0
      for (const cell of side === 'left' ? headers : [...headers].reverse()) {
        if (cell.dataset.sticky !== side) continue
        if (cell.dataset.field) offsets[cell.dataset.field] = offset
        offset += cell.getBoundingClientRect().width
      }
    }
    setStickyOffsets((previous) =>
      JSON.stringify(previous) === JSON.stringify(offsets) ? previous : offsets
    )
  }, [tableWrapper])

  const getStickyColumnStyle = useCallback(
    (column: Column, index: number): CSSProperties | undefined => {
      const side = getStickyColumnSide(column, index)
      return side ? ({ [side]: `${stickyOffsets[column.key] ?? 0}px` } as CSSProperties) : undefined
    },
    [getStickyColumnSide, stickyOffsets]
  )

  // Preserve the shared default while allowing any explicitly declared prefix.
  const getStickyColumnClass = useCallback(
    (column: Column, index: number) => {
      const side = getStickyColumnSide(column, index)
      return side ? `sticky-col sticky-col-${side}` : ''
    },
    [getStickyColumnSide]
  )

  // 根据列数自适应调整内边距
  const getAdaptivePaddingClass = () => {
    const columnCount = columns.length

    // 列数越多，内边距越小
    if (columnCount >= 10) {
      return 'px-2' // 8px
    } else if (columnCount >= 7) {
      return 'px-3' // 12px
    } else if (columnCount >= 5) {
      return 'px-4' // 16px
    } else {
      return 'px-6' // 24px (原始值)
    }
  }

  // --- Desktop table measurement ---------------------------------------------

  const measureUserTable = useCallback(() => {
    if (!userTableLayout || !isDesktopViewport) return
    const wrapper = tableWrapperRef.current
    const table = wrapper?.querySelector<HTMLTableElement>('table.record-table')
    if (!wrapper || !table || wrapper.clientWidth <= 0) return
    const width = Math.max(table.scrollWidth, table.getBoundingClientRect().width)
    requiredTableWidthRef.current = Math.ceil(width + wrapper.offsetWidth - wrapper.clientWidth)
    setTableDoesNotFit(width > wrapper.clientWidth + 1)
  }, [userTableLayout, isDesktopViewport])

  const observeUserTableContainer = useCallback(() => {
    fitResizeObserverRef.current?.disconnect()
    if (!userTableLayout || typeof ResizeObserver === 'undefined') return
    const root = cardsWrapperRef.current ?? tableWrapperRef.current
    if (!root?.parentElement) return
    fitResizeObserverRef.current = new ResizeObserver(() => {
      if (showDesktopTableRef.current) measureUserTable()
      else if (
        isDesktopViewportRef.current &&
        (cardsWrapperRef.current?.getBoundingClientRect().width ?? 0) >= requiredTableWidthRef.current
      ) {
        setTableDoesNotFit(false)
      }
    })
    fitResizeObserverRef.current.observe(root.parentElement)
  }, [userTableLayout, measureUserTable])

  // Keep the current cards (and their local state) mounted while measuring the
  // updated slots; this hidden probe is removed before the next paint.
  const refreshUserTableFit = useCallback(() => {
    if (!userTableLayout || !isDesktopViewport || unmountedRef.current) return
    if (probingTableRef.current || fitMeasurementPendingRef.current) return
    if (showDesktopTableRef.current) {
      measureUserTable()
      return
    }
    const width = cardsWrapperRef.current?.getBoundingClientRect().width ?? 0
    if (width <= 0) return
    fitMeasurementPendingRef.current = true
    setProbeWidth(width)
    setProbingTable(true)
  }, [userTableLayout, isDesktopViewport, measureUserTable])

  // API polling often replaces arrays without changing their contents. Reuse the
  // last measurement in that case; changed rows, labels or selected columns probe
  // again even while cards are visible, so stale width estimates cannot persist.
  const userFitContent = useMemo(() => {
    if (!userTableLayout) return ''
    try {
      return JSON.stringify(data)
    } catch {
      return data
    }
  }, [userTableLayout, data])

  // 检查操作列是否需要展开
  const checkActionsColumnWidth = useCallback(() => {
    if (!expandableActions || userTableLayout) {
      setActionsColumnNeedsExpanding(false)
      setActionsExpanded(false)
      return
    }
    const wrapper = tableWrapper
    if (!wrapper) return

    // 查找第一行的操作列单元格
    const firstActionCell = wrapper.querySelector('tbody tr:first-child td:last-child')
    if (!firstActionCell) return

    // 查找操作列内容的容器div
    const actionsContainer = firstActionCell.querySelector('div')
    if (!actionsContainer) return

    // 临时展开以测量完整宽度
    wasExpandedRef.current = actionsExpandedRef.current
    measuringActionsRef.current = true
    setActionsExpanded(true)
  }, [expandableActions, userTableLayout, tableWrapper])

  // Measure the temporarily expanded actions column, then restore its state.
  useEffect(() => {
    if (!measuringActionsRef.current) return
    measuringActionsRef.current = false
    const wrapper = tableWrapper
    const firstActionCell = wrapper?.querySelector('tbody tr:first-child td:last-child')
    const actionsContainer = firstActionCell?.querySelector('div')
    if (!actionsContainer) {
      setActionsExpanded(wasExpandedRef.current)
      return
    }

    // 测量所有按钮的总宽度
    const actionItems = actionsContainer.querySelectorAll('button, a, [role="button"]')
    if (actionItems.length <= 2) {
      setActionsColumnNeedsExpanding(false)
      setActionsExpanded(wasExpandedRef.current)
      return
    }

    // 计算所有按钮的总宽度（包括gap）
    let totalWidth = 0
    actionItems.forEach((item, index) => {
      totalWidth += (item as HTMLElement).offsetWidth
      if (index < actionItems.length - 1) {
        totalWidth += 4 // gap-1 = 4px
      }
    })

    // 获取单元格可用宽度（减去padding）
    const cellWidth = (firstActionCell as HTMLElement).clientWidth - 32 // 减去左右padding

    // 如果总宽度超过可用宽度，需要展开功能
    setActionsColumnNeedsExpanding(totalWidth > cellWidth)

    // 恢复原来的展开状态
    setActionsExpanded(wasExpandedRef.current)
  }, [actionsExpanded, tableWrapper])

  const detachDesktopTableTracking = useCallback(() => {
    resizeObserverRef.current?.disconnect()
    resizeObserverRef.current = null
    if (resizeHandlerRef.current) {
      window.removeEventListener('resize', resizeHandlerRef.current)
      resizeHandlerRef.current = null
    }
  }, [])

  const attachDesktopTableTracking = useCallback(() => {
    checkActionsColumnWidth()
    updateStickyOffsets()
    measureUserTable()
    const wrapper = tableWrapper
    if (wrapper && typeof ResizeObserver !== 'undefined') {
      const observer = new ResizeObserver(() => {
        checkActionsColumnWidth()
        updateStickyOffsets()
        measureUserTable()
      })
      observer.observe(wrapper)
      wrapper.querySelectorAll('thead th').forEach((cell) => observer.observe(cell))
      resizeObserverRef.current = observer
    } else {
      // 降级方案：不支持 ResizeObserver 时使用 window resize
      const handler = () => {
        checkActionsColumnWidth()
        updateStickyOffsets()
        measureUserTable()
      }
      window.addEventListener('resize', handler)
      resizeHandlerRef.current = handler
    }
  }, [checkActionsColumnWidth, updateStickyOffsets, measureUserTable, tableWrapper])

  const refreshHeaderTracking = useCallback(() => {
    updateStickyOffsets()
    const wrapper = tableWrapper
    const observer = resizeObserverRef.current
    if (wrapper && observer) {
      observer.disconnect()
      observer.observe(wrapper)
      wrapper.querySelectorAll('thead th').forEach((cell) => observer.observe(cell))
    }
  }, [updateStickyOffsets, tableWrapper])

  // Viewport tracking
  useEffect(() => {
    if (typeof window === 'undefined') return
    const mediaQuery = window.matchMedia(desktopViewportQuery)
    setIsDesktopViewport(mediaQuery.matches)
    const listener = (event: MediaQueryListEvent) => {
      setIsDesktopViewport(event.matches)
    }
    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', listener)
    } else {
      mediaQuery.addListener(listener)
    }
    return () => {
      if (typeof mediaQuery.removeEventListener === 'function') {
        mediaQuery.removeEventListener('change', listener)
      } else {
        mediaQuery.removeListener(listener)
      }
    }
  }, [])

  useEffect(
    () => () => {
      unmountedRef.current = true
      fitResizeObserverRef.current?.disconnect()
      detachDesktopTableTracking()
    },
    [detachDesktopTableTracking]
  )

  useEffect(() => {
    detachDesktopTableTracking()
    if (!showDesktopTable) return
    if (unmountedRef.current || !showDesktopTableRef.current) return
    attachDesktopTableTracking()
  }, [showDesktopTable, attachDesktopTableTracking, detachDesktopTableTracking])

  useEffect(() => {
    observeUserTableContainer()
    return () => fitResizeObserverRef.current?.disconnect()
  }, [cardsWrapper, tableWrapper, userTableLayout, observeUserTableContainer])

  // Measure the hidden probe table once it is mounted, then remove it again.
  // The probe element attaches through its ref callback, so wait for the
  // follow-up render before measuring.
  useEffect(() => {
    if (!probingTable) return
    if (!tableWrapperRef.current) return
    measureUserTable()
    setProbingTable(false)
    fitMeasurementPendingRef.current = false
  }, [probingTable, measureUserTable, tableWrapper])

  useEffect(() => {
    refreshUserTableFit()
  }, [
    userFitContent,
    columnsSignature,
    selectable,
    userTableLayout,
    isDesktopViewport,
    refreshUserTableFit
  ])

  // 数据/列变化时重新检查操作布局和固定列偏移。
  // 注意：不能监听 actionsExpanded，因为 checkActionsColumnWidth 会临时修改它，会导致无限循环
  useEffect(() => {
    checkActionsColumnWidth()
    updateStickyOffsets()
    const wrapper = tableWrapper
    const observer = resizeObserverRef.current
    if (wrapper && observer) {
      observer.disconnect()
      observer.observe(wrapper)
      wrapper.querySelectorAll('thead th').forEach((cell) => observer.observe(cell))
    }
  }, [
    data.length,
    columnsSignature,
    selectable,
    stickyFirstColumn,
    stickyActionsColumn,
    checkActionsColumnWidth,
    updateStickyOffsets,
    tableWrapper
  ])

  // 展开操作按钮后，列宽可能变化。
  useEffect(() => {
    updateStickyOffsets()
  }, [actionsExpanded, updateStickyOffsets])

  // The shared static-header layout notifies its viewport after each measurement.
  useEffect(() => {
    const wrapper = tableWrapper
    if (!wrapper) return
    const handler = () => refreshHeaderTracking()
    wrapper.addEventListener('static-table-header-measured', handler)
    return () => wrapper.removeEventListener('static-table-header-measured', handler)
  }, [tableWrapper, refreshHeaderTracking])

  // --- Virtual scrolling ------------------------------------------------------

  const shouldVirtualize = showDesktopTable && (sortedData?.length ?? 0) > (virtualizeThreshold ?? 100)

  const rowVirtualizer = useVirtualizer<HTMLDivElement, HTMLTableRowElement>({
    count: shouldVirtualize ? (sortedData?.length ?? 0) : 0,
    getScrollElement: () => tableWrapper,
    // 用行主键(与模板 :key 一致)而非默认的 index 作为 itemSizeCache 键,
    // 这样排序/筛选/跨阈值来回都能复用正确的已测行高,而不是残留的按 index 缓存 → 消除高度校正抖动。
    getItemKey: (index: number) => {
      const row = sortedData?.[index]
      return row != null ? resolveRowKey(row, index) : index
    },
    estimateSize: () => estimateRowHeight ?? 56,
    overscan: overscan ?? 5,
    // 兜底高度:首个有效高度读数到来前,先按一屏渲染,避免空白帧
    initialRect: { width: 0, height: estimatedViewportHeight() },
    // 关键:过滤 0 高度读数,杜绝 scrollRect 被钉成 0 → calculateRange 返回 null → 整表空白
    observeElementRect: observeElementRectNonZero,
    // 把测量类 ResizeObserver 回调批到 rAF,避免滚动中同步 reflow 风暴导致的校正抖动/空白
    useAnimationFrameWithResizeObserver: true
  })

  const virtualItems = rowVirtualizer.getVirtualItems()

  const virtualPaddingTop = virtualItems.length > 0 ? virtualItems[0].start : 0
  const virtualPaddingBottom =
    virtualItems.length === 0 ? 0 : rowVirtualizer.getTotalSize() - virtualItems[virtualItems.length - 1].end

  const rowIdentityKeys = useMemo<RowIdentityToken[]>(
    () =>
      (sortedData ?? []).map((row) => {
        const stableKey = resolveStableRowKey(row)
        if (stableKey !== undefined) return stableKey

        // Object references survive pure reordering but change across page/filter results.
        // Primitive rows have no stable identity, so force conservative invalidation.
        return row !== null && typeof row === 'object' ? row : Symbol('unstable-row')
      }),
    [sortedData, resolveStableRowKey]
  )

  const hasSameRowIdentitySet = (current: RowIdentityToken[], previous: RowIdentityToken[]) => {
    if (current.length !== previous.length) return false
    const currentKeys = new Set(current)
    const previousKeys = new Set(previous)
    // Duplicate keys make row-to-cache ownership ambiguous, even when the unique
    // key set looks unchanged (for example [1, 1, 2] -> [1, 2, 2]).
    if (currentKeys.size !== current.length || previousKeys.size !== previous.length) return false
    return [...currentKeys].every((key) => previousKeys.has(key))
  }

  const previousIdentityRef = useRef<RowIdentityToken[] | null>(null)
  useEffect(() => {
    const previous = previousIdentityRef.current
    previousIdentityRef.current = rowIdentityKeys
    if (!previous) return
    if (hasSameRowIdentitySet(rowIdentityKeys, previous)) return

    // The virtualizer owns caches across option updates. A new page/filter result
    // must release detached rows and sizes, while pure reordering keeps them.
    rowVirtualizer.measureElement(null)
    rowVirtualizer.measure()
  })

  // 统一的渲染行列表:虚拟化开启时只取窗口内的行(需 measure 交给虚拟器测量),
  // 关闭时取全部行(无需测量)。模板据此渲染,两种模式共用同一套单元格结构。
  const renderRows = useMemo<Array<{ index: number; row: any; measure: boolean }>>(() => {
    const rows = sortedData ?? []
    if (shouldVirtualize) {
      return virtualItems.map((vr) => ({ index: vr.index, row: rows[vr.index], measure: true }))
    }
    return rows.map((row, index) => ({ index, row, measure: false }))
  }, [sortedData, shouldVirtualize, virtualItems])

  const hasActionsColumn = actionsColumn !== undefined

  useImperativeHandle(
    ref,
    (): DataTableHandle => ({
      sortedData,
      resolveRowKey,
      shouldVirtualize,
      actionsColumnNeedsExpanding,
      tableWrapperEl: tableWrapper,
      scrollViewportEl: showDesktopTable ? tableWrapper : cardsWrapper,
      virtualizer: rowVirtualizer
    }),
    [
      sortedData,
      resolveRowKey,
      shouldVirtualize,
      actionsColumnNeedsExpanding,
      tableWrapper,
      cardsWrapper,
      rowVirtualizer,
      showDesktopTable
    ]
  )

  // --- Rendering --------------------------------------------------------------

  const setIndeterminate = useCallback(
    (element: HTMLInputElement | null) => {
      if (element) element.indeterminate = someVisibleSelected
    },
    [someVisibleSelected]
  )

  const selectAllCheckbox = (testId: string) => (
    <input
      type="checkbox"
      className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
      checked={allVisibleSelected}
      ref={setIndeterminate}
      aria-label={t('common.selectAll')}
      data-testid={testId}
      onChange={(event) => toggleAllVisible(event.target.checked)}
    />
  )

  const rowCheckbox = (row: any, index: number) => (
    <input
      type="checkbox"
      className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
      checked={isRowSelected(row, index)}
      aria-label={getRowSelectionLabel(row, index)}
      data-testid="select-row"
      onClick={(event) => event.stopPropagation()}
      onChange={(event) => toggleRowSelection(row, index, event.target.checked)}
    />
  )

  const renderCellContent = (column: Column, row: any, expanded: boolean) => {
    if (renderCell) return renderCell(column.key, row, row?.[column.key], expanded)
    if (column.formatter) return column.formatter(row?.[column.key], row)
    return row?.[column.key]
  }

  const emptyContent = renderEmpty ? renderEmpty() : (
    <>
      <Icon name="inbox" size="xl" className="mb-4 h-12 w-12 surface-muted" />
      <p className="text-lg font-medium text-gray-900 dark:text-gray-100">{t('empty.noData')}</p>
    </>
  )

  const cardsLayout = (
    <div
      ref={setCardsWrapper}
      {...rest}
      className={cn('record-cards space-y-3', userTableLayout && 'user-record-cards', className)}
      aria-busy={loading}
    >
      {loading && !data?.length ? (
        Array.from({ length: 5 }, (_, skeletonIndex) => (
          <div
            key={skeletonIndex}
            className="glass-inset rounded-2xl border border-gray-200 p-4 dark:border-dark-700"
          >
            <div className="space-y-3">
              {dataColumns.map((column) => (
                <div key={column.key} className="flex justify-between">
                  <div className="h-4 w-20 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                  <div className="h-4 w-32 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                </div>
              ))}
              {hasActionsColumn && (
                <div className="border-t border-gray-200 pt-3 dark:border-dark-700">
                  <div className="h-8 w-full animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                </div>
              )}
            </div>
          </div>
        ))
      ) : !data || data.length === 0 ? (
        <div className="glass-inset rounded-2xl border border-gray-200 p-12 text-center dark:border-dark-700">
          {emptyContent}
        </div>
      ) : (
        <>
          {selectable && (
            <div className="flex items-center justify-end gap-2 px-1">
              <label className="flex items-center gap-2 text-sm font-medium surface-muted">
                {selectAllCheckbox('select-all-mobile')}
                <span>{t('common.selectAll')}</span>
              </label>
            </div>
          )}
          {(sortedData ?? []).map((row, index) => (
            <div
              key={resolveRowKey(row, index)}
              className={cn(
                'glass-inset rounded-2xl border border-gray-200 p-4 dark:border-dark-700',
                clickableRows && 'cursor-pointer',
                ((selectable && isRowSelected(row, index)) || activeRowKey === resolveRowKey(row, index)) &&
                  'table-row-selected'
              )}
              onClick={(event) => handleRowClick(event, row)}
            >
              <div
                className={cn(
                  'record-card-fields',
                  userTableLayout ? 'user-record-fields' : 'space-y-3'
                )}
              >
                {selectable && (
                  <div
                    className={cn(
                      'record-card-selection flex justify-end',
                      userTableLayout && 'user-record-selection'
                    )}
                  >
                    {rowCheckbox(row, index)}
                  </div>
                )}
                {dataColumns.map((column) => (
                  <div
                    key={column.key}
                    data-field={column.key}
                    className="flex min-w-0 items-start justify-between gap-4"
                  >
                    <span className="text-xs font-medium uppercase tracking-wider surface-muted">
                      {column.label}
                    </span>
                    <div className="min-w-0 max-w-full text-right text-sm text-gray-900 dark:text-gray-100">
                      {renderCellContent(column, row, actionsExpanded)}
                    </div>
                  </div>
                ))}
                {actionsColumn && (
                  <div
                    className={cn(
                      'record-card-actions border-t border-gray-200 pt-3 dark:border-dark-700',
                      userTableLayout && 'user-record-actions'
                    )}
                  >
                    {renderCellContent(actionsColumn, row, actionsExpanded)}
                  </div>
                )}
              </div>
            </div>
          ))}
        </>
      )}
    </div>
  )

  const tableLayout = (
    <div
      ref={setTableWrapper}
      {...rest}
      className={cn(
        'table-wrapper table-container record-table-scroll',
        probingTable && !showDesktopTable && 'data-table-fit-probe',
        actionsExpanded && 'actions-expanded',
        className
      )}
      style={probingTable && !showDesktopTable ? tableProbeStyle : undefined}
      aria-hidden={probingTable && !showDesktopTable ? true : undefined}
      inert={probingTable && !showDesktopTable ? true : undefined}
      aria-busy={loading}
    >
      <table className="record-table w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
        <thead className="table-header surface-inset">
          <tr>
            {selectable && (
              <th
                scope="col"
                className={cn(
                  'sticky-header-cell w-11 min-w-11 px-3 py-3 text-center',
                  stickyFirstColumn && !userTableLayout && 'sticky-col sticky-col-left'
                )}
                data-sticky={stickyFirstColumn && !userTableLayout ? 'left' : undefined}
                style={stickyFirstColumn && !userTableLayout ? { left: '0px' } : undefined}
              >
                {selectAllCheckbox('select-all')}
              </th>
            )}
            {columns.map((column, index) => (
              <th
                key={column.key}
                data-field={column.key}
                scope="col"
                aria-sort={column.sortable ? getColumnAriaSort(column.key) : undefined}
                data-sticky={getStickyColumnSide(column, index)}
                style={getStickyColumnStyle(column, index)}
                className={cn(
                  'sticky-header-cell py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted',
                  getAdaptivePaddingClass(),
                  column.sortable && 'cursor-pointer hover:surface-hover dark:hover:surface-hover',
                  getStickyColumnClass(column, index),
                  column.class
                )}
                onClick={column.sortable ? () => handleSort(column.key) : undefined}
              >
                <div className={cn('flex items-center space-x-1', getHeaderContentAlignmentClass(column))}>
                  {renderHeader ? (
                    renderHeader(column.key, column, { key: sortKey, order: sortOrder })
                  ) : (
                    <span>{column.label}</span>
                  )}
                  {column.sortable && (
                    <span
                      className="inline-flex h-5 w-4 flex-col items-center justify-center"
                      aria-hidden="true"
                    >
                      <svg
                        className={cn('h-2.5 w-2.5', getSortIndicatorClass(column.key, 'asc'))}
                        fill="currentColor"
                        viewBox="0 0 10 10"
                      >
                        <path d="M5 2L1.5 6.5h7L5 2z" />
                      </svg>
                      <svg
                        className={cn('-mt-0.5 h-2.5 w-2.5', getSortIndicatorClass(column.key, 'desc'))}
                        fill="currentColor"
                        viewBox="0 0 10 10"
                      >
                        <path d="M5 8L1.5 3.5h7L5 8z" />
                      </svg>
                    </span>
                  )}
                </div>
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="table-body divide-y divide-gray-200 dark:divide-dark-700">
          {loading && !data?.length ? (
            Array.from({ length: 5 }, (_, skeletonIndex) => (
              <tr key={skeletonIndex}>
                {selectable && (
                  <td className="w-11 min-w-11 px-3 py-4">
                    <div className="mx-auto h-4 w-4 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                  </td>
                )}
                {columns.map((column) => (
                  <td key={column.key} className={cn('whitespace-nowrap py-4', getAdaptivePaddingClass())}>
                    <div className="animate-pulse">
                      <div className="h-4 w-3/4 rounded bg-gray-200 dark:bg-dark-700"></div>
                    </div>
                  </td>
                ))}
              </tr>
            ))
          ) : !data || data.length === 0 ? (
            /* Empty state: centered within the visible scrollport (not the full
               min-w-max table width) so the message stays visible and keeps the
               pinned left columns covered when the table is wider than the viewport. */
            <tr>
              <td colSpan={tableColumnCount} className={cn('empty-state-cell', getAdaptivePaddingClass())}>
                <div className="empty-state-fill surface-muted">{emptyContent}</div>
              </td>
            </tr>
          ) : (
            <>
              {virtualPaddingTop > 0 && (
                <tr aria-hidden="true">
                  <td
                    colSpan={tableColumnCount}
                    style={{ height: `${virtualPaddingTop}px`, padding: 0, border: 'none' }}
                  ></td>
                </tr>
              )}
              {renderRows.map((item) => (
                <tr
                  key={resolveRowKey(item.row, item.index)}
                  data-row-id={resolveRowKey(item.row, item.index)}
                  data-index={item.index}
                  ref={item.measure ? rowVirtualizer.measureElement : undefined}
                  className={cn(
                    'table-data-row',
                    clickableRows && 'cursor-pointer',
                    ((selectable && isRowSelected(item.row, item.index)) ||
                      activeRowKey === resolveRowKey(item.row, item.index)) &&
                      'table-row-selected'
                  )}
                  onClick={(event) => handleRowClick(event, item.row)}
                >
                  {selectable && (
                    <td
                      className={cn(
                        'w-11 min-w-11 px-3 py-4 text-center',
                        stickyFirstColumn && !userTableLayout && 'sticky-col sticky-col-left'
                      )}
                      style={stickyFirstColumn && !userTableLayout ? { left: '0px' } : undefined}
                    >
                      {rowCheckbox(item.row, item.index)}
                    </td>
                  )}
                  {columns.map((column, colIndex) => (
                    <td
                      key={column.key}
                      data-field={column.key}
                      style={getStickyColumnStyle(column, colIndex)}
                      className={cn(
                        'whitespace-nowrap py-4 text-sm text-gray-900 dark:text-gray-100',
                        getAdaptivePaddingClass(),
                        getStickyColumnClass(column, colIndex),
                        column.class
                      )}
                    >
                      {renderCellContent(column, item.row, actionsExpanded)}
                    </td>
                  ))}
                </tr>
              ))}
              {virtualPaddingBottom > 0 && (
                <tr aria-hidden="true">
                  <td
                    colSpan={tableColumnCount}
                    style={{ height: `${virtualPaddingBottom}px`, padding: 0, border: 'none' }}
                  ></td>
                </tr>
              )}
            </>
          )}
        </tbody>
      </table>
    </div>
  )

  return (
    <>
      {!showDesktopTable && cardsLayout}
      {(showDesktopTable || probingTable) && tableLayout}
    </>
  )
})
