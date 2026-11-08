import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { fetchBatch, getEntry } from '@/utils/ipGeoLookup'
import { useIpGeoEntries } from '@/composables/useIpGeoEntry'

export interface IpGeoBatchToolbarProps {
  ips: Array<string | null | undefined>
  onFailed?: () => void
}

// 当前页 IP 批量地理查询工具条:传入原始 IP 列表(可含空值),内部去重;
// 无 IP 时自身不渲染。批量失败 emit failed,由使用方弹提示。
export function IpGeoBatchToolbar({ ips, onFailed }: IpGeoBatchToolbarProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)

  const uniqueIps = Array.from(new Set(ips.filter((ip): ip is string => Boolean(ip))))
  // Re-render when shared geo entries change so the pending count stays current.
  useIpGeoEntries(uniqueIps)

  const pendingCount = uniqueIps.filter((ip) => {
    const status = getEntry(ip).status
    return status === 'idle' || status === 'error'
  }).length

  const run = async () => {
    setLoading(true)
    try {
      const ok = await fetchBatch(uniqueIps)
      if (!ok) onFailed?.()
    } finally {
      setLoading(false)
    }
  }

  if (uniqueIps.length === 0) return null

  return (
    <div className="flex flex-shrink-0 items-center justify-end gap-2 border-b border-gray-200 px-4 py-2 dark:border-dark-700">
      {pendingCount > 0 && (
        <span className="text-xs surface-muted">{t('usage.ipGeo.pending', { count: pendingCount })}</span>
      )}
      <button
        type="button"
        className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/30 surface-control"
        disabled={loading || pendingCount === 0}
        onClick={() => void run()}
      >
        {loading ? t('usage.ipGeo.batchFetching') : t('usage.ipGeo.batchFetch')}
      </button>
    </div>
  )
}
