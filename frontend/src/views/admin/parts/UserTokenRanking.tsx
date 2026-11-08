import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState
} from 'react'
import { useTranslation } from 'react-i18next'
import { Select, type SelectOption } from '@/components/ui'
import { Spinner } from '@/components/ui'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import { formatTokenShare } from '@/utils/tokenShare'
import type { UserBreakdownItem } from '@/types'

type SortKey = NonNullable<UserBreakdownParams['sort_by']>

export interface UserTokenRankingProps {
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
  totalTokens?: number
  workspace?: boolean
  /** Keeps the panel mounted while another tab is active. */
  hidden?: boolean
  onSelectUser?: (userId: number, email: string) => void
}

const sortableColumns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'admin.usage.tokenRanking.columns.requests' },
  { key: 'input_tokens', label: 'admin.usage.tokenRanking.columns.inputTokens' },
  { key: 'output_tokens', label: 'admin.usage.tokenRanking.columns.outputTokens' },
  { key: 'cache_tokens', label: 'admin.usage.tokenRanking.columns.cacheTokens' },
  { key: 'total_tokens', label: 'admin.usage.tokenRanking.columns.totalTokens' },
  { key: 'actual_cost', label: 'admin.usage.tokenRanking.columns.cost' }
]

const limitOptions: SelectOption[] = [
  { value: 20, label: 'Top 20' },
  { value: 50, label: 'Top 50' },
  { value: 100, label: 'Top 100' },
  { value: 200, label: 'Top 200' }
]

// 前三名金/银/铜徽章
const RANK_BADGE_CLASSES = [
  'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-500/20 dark:text-amber-400',
  'surface-inset surface-muted dark:bg-gray-500/20',
  'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-500/20 dark:text-orange-400'
]

export interface UserTokenRankingHandle {
  reload: () => void
}

export const UserTokenRanking = forwardRef<UserTokenRankingHandle, UserTokenRankingProps>(
  function UserTokenRanking(
    { startDate, endDate, filters, model, totalTokens, workspace = false, hidden = false, onSelectUser },
    ref
  ) {
  const { t } = useTranslation()

  const [items, setItems] = useState<UserBreakdownItem[]>([])
  const [loading, setLoading] = useState(false)
  const [sortBy, setSortBy] = useState<SortKey>('total_tokens')
  const [limit, setLimit] = useState(50)
  const reqSeq = useRef(0)
  const filtersKey = JSON.stringify(filters)

  const fmtTokens = (value: number) => formatCompactNumber(value)
  const fmtCost = (value: number) => formatCostFixed(value, 4)
  const tokenShare = (tokens: number | null | undefined): string | null => formatTokenShare(tokens, totalTokens)

  const load = useCallback(async () => {
    const seq = ++reqSeq.current
    setLoading(true)
    try {
      const params: UserBreakdownParams = {
        ...filters,
        start_date: startDate,
        end_date: endDate,
        sort_by: sortBy,
        limit
      }
      if (model) params.model = model
      const res = await getUserBreakdown(params)
      if (seq !== reqSeq.current) return
      setItems(res.users || [])
    } catch {
      if (seq !== reqSeq.current) return
      setItems([])
    } finally {
      if (seq === reqSeq.current) setLoading(false)
    }
    // filters is serialized into filtersKey so object identity churn is ignored.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [startDate, endDate, filtersKey, sortBy, limit, model])

  useImperativeHandle(ref, (): UserTokenRankingHandle => ({ reload: () => void load() }), [load])

  // Reload when the shared filters / date range / model change.
  useEffect(() => {
    void load()
  }, [load])

  const setSort = (key: SortKey) => {
    if (sortBy === key) return
    setSortBy(key)
    void load()
  }

  return (
    <div className={workspace ? 'workspace-body !gap-0' : undefined} hidden={hidden}>
      {/* Toolbar */}
      <div
        className={`flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700/50 sm:px-6 ${
          workspace ? 'workspace-fixed' : ''
        }`}
      >
        <p className="text-xs surface-muted">{t('admin.usage.tokenRanking.subtitle')}</p>
        <div className="flex items-center gap-3">
          {!loading && items.length > 0 && (
            <span className="text-xs surface-muted">
              {t('admin.usage.tokenRanking.userCount', { count: items.length })}
            </span>
          )}
          <div className="w-28">
            <Select
              value={limit}
              options={limitOptions}
              searchable={false}
              onValueChange={(value) => setLimit(Number(value ?? 50))}
              onChange={() => void load()}
            />
          </div>
        </div>
      </div>

      {/* Table */}
      <div className={`record-table-scroll overflow-x-auto ${workspace ? 'workspace-table-viewport' : ''}`}>
        <table className="record-table w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
          <thead className="surface-inset">
            <tr>
              <th className="w-16 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider sm:px-6 surface-muted">
                #
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                {t('admin.usage.tokenRanking.columns.user')}
              </th>
              {sortableColumns.map((col) => (
                <th
                  key={col.key}
                  className={`cursor-pointer select-none whitespace-nowrap px-4 py-3 text-right text-xs font-medium uppercase tracking-wider transition-colors hover:surface-hover ${
                    sortBy === col.key ? 'text-primary-600 dark:text-primary-400' : 'surface-muted'
                  }`}
                  onClick={() => setSort(col.key)}
                >
                  {t(col.label)}
                  {sortBy === col.key && <span aria-hidden="true">↓</span>}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 surface-inset dark:divide-dark-700">
            {loading && (
              <tr>
                <td colSpan={sortableColumns.length + 2} className="py-12 text-center">
                  <Spinner />
                </td>
              </tr>
            )}
            {!loading && items.length === 0 && (
              <tr>
                <td colSpan={sortableColumns.length + 2} className="py-12 text-center text-sm surface-muted">
                  {t('admin.dashboard.noDataAvailable')}
                </td>
              </tr>
            )}
            {!loading &&
              items.map((item, index) => (
                <tr
                  key={item.user_id}
                  className="cursor-pointer transition-colors hover:surface-hover"
                  title={t('admin.usage.tokenRanking.rowHint')}
                  onClick={() => onSelectUser?.(item.user_id, item.email)}
                >
                  <td className="px-4 py-3 sm:px-6">
                    {index < 3 ? (
                      <span
                        className={`inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold ${RANK_BADGE_CLASSES[index]}`}
                      >
                        {index + 1}
                      </span>
                    ) : (
                      <span className="inline-block w-6 text-center text-sm tabular-nums surface-muted">{index + 1}</span>
                    )}
                  </td>
                  <td className="max-w-[260px] truncate px-4 py-3 text-sm font-medium surface-muted" title={item.email}>
                    {item.email || `User #${item.user_id}`}
                    <span className="ml-1 font-normal surface-muted">#{item.user_id}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums surface-muted">
                    {item.requests.toLocaleString()}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums surface-muted">
                    {fmtTokens(item.input_tokens)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums surface-muted">
                    {fmtTokens(item.output_tokens)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums surface-muted">
                    {fmtTokens(item.cache_tokens)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-900 dark:text-gray-100">
                    {fmtTokens(item.total_tokens)}{' '}
                    {tokenShare(item.total_tokens) && (
                      <span className="text-xs font-normal surface-muted">({tokenShare(item.total_tokens)})</span>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums text-green-600 dark:text-green-400">
                    ${fmtCost(item.actual_cost)}
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </div>
  )
})

export default UserTokenRanking
