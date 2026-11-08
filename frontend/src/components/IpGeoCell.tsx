import { useTranslation } from 'react-i18next'
import { Icon } from './Icon'
import { useIpGeoEntry } from '@/composables/useIpGeoEntry'
import { fetchOne } from '@/utils/ipGeoLookup'

export interface IpGeoCellProps {
  ip: string
}

export function IpGeoCell({ ip }: IpGeoCellProps) {
  const { t } = useTranslation()
  const entry = useIpGeoEntry(ip)

  const tooltipText = (() => {
    const detail = entry.detail
    if (!detail) return ''
    const lines = [
      detail.organization ? `${t('usage.ipGeo.detailOrg')}: ${detail.organization}` : '',
      detail.timezone ? `${t('usage.ipGeo.detailTimezone')}: ${detail.timezone}` : '',
      detail.accuracy != null ? `${t('usage.ipGeo.detailAccuracy')}: ${detail.accuracy}km` : '',
      detail.latitude && detail.longitude
        ? `${t('usage.ipGeo.detailCoordinates')}: ${detail.latitude}, ${detail.longitude}`
        : ''
    ].filter(Boolean)
    return lines.join('\n')
  })()

  const handleFetch = () => {
    void fetchOne(ip)
  }

  const handleRefresh = () => {
    void fetchOne(ip, true)
  }

  const handleOpenDetail = () => {
    window.open(
      `https://www.iplocation.net/ip-lookup?query=${encodeURIComponent(ip)}`,
      '_blank',
      'noopener,noreferrer'
    )
  }

  if (entry.status === 'idle') {
    return (
      <div className="mt-0.5 text-xs">
        <button
          type="button"
          className="text-primary-600 underline decoration-dashed underline-offset-2 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
          onClick={handleFetch}
        >
          {t('usage.ipGeo.fetch')}
        </button>
      </div>
    )
  }

  if (entry.status === 'loading') {
    return (
      <div className="mt-0.5 flex items-center gap-1 text-xs surface-muted">
        <svg className="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {t('usage.ipGeo.fetching')}
      </div>
    )
  }

  if (entry.status === 'success') {
    return (
      <div className="mt-0.5 flex items-center gap-1 text-xs">
        <button
          type="button"
          className="truncate underline decoration-dotted underline-offset-2 hover:text-primary-600 dark:hover:text-primary-400 surface-control surface-muted"
          title={tooltipText}
          onClick={handleOpenDetail}
        >
          {entry.label}
        </button>
        <button
          type="button"
          className="hover:text-primary-600 dark:hover:text-primary-400 surface-control surface-muted"
          title={t('usage.ipGeo.refreshTitle')}
          onClick={handleRefresh}
        >
          <Icon name="refresh" size="xs" />
        </button>
      </div>
    )
  }

  if (entry.status === 'error') {
    return (
      <div className="mt-0.5 text-xs">
        <button
          type="button"
          className="text-red-600 underline decoration-dashed underline-offset-2 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300 surface-control"
          onClick={handleFetch}
        >
          {t('usage.ipGeo.failed')}
        </button>
      </div>
    )
  }

  return <div className="mt-0.5 text-xs surface-muted">{t('usage.ipGeo.private')}</div>
}
