import { useTranslation } from 'react-i18next'
import type { UserSupportedModelPricing } from '@/api/channels'
import { catalogMoney } from './catalogQuote'

type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_write_1h_price' | 'cache_read_price' | 'per_request_price' | 'image_input_price' | 'image_output_price'

const columns: { key: PriceKey; label: string }[] = [
  { key: 'input_price', label: 'inputPrice' }, { key: 'output_price', label: 'outputPrice' },
  { key: 'cache_write_price', label: 'cacheWrite5mPrice' }, { key: 'cache_write_1h_price', label: 'cacheWrite1hPrice' },
  { key: 'cache_read_price', label: 'cacheReadPrice' }, { key: 'image_input_price', label: 'imageInputPrice' },
  { key: 'image_output_price', label: 'imageOutputPrice' }, { key: 'per_request_price', label: 'perRequestPrice' }
]

export interface CatalogPriceTableProps {
  pricing: UserSupportedModelPricing
  rate?: number
  token?: boolean
}

export function CatalogPriceTable({ pricing, rate = 1, token = true }: CatalogPriceTableProps) {
  const { t } = useTranslation()

  const rows: { label: string; pricing: Partial<Record<PriceKey, number | null>> }[] = [
    { label: t('availableChannels.catalog.baseTier'), pricing: pricing },
    ...pricing.intervals.map(iv => ({
      label: `(${iv.min_tokens.toLocaleString()}, ${iv.max_tokens == null ? '∞' : iv.max_tokens.toLocaleString()}]`,
      pricing: iv as Partial<Record<PriceKey, number | null>>
    }))
  ]
  const fields = columns.filter(f => rows.some(r => r.pricing[f.key] != null))

  return (
    <div
      className="record-table-scroll record-table-dialog-scroll max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700"
      tabIndex={0}
      aria-label={t('availableChannels.catalog.prices')}
    >
      <table className="record-table record-table-compact w-full text-left text-xs">
        <thead className="surface-inset surface-muted"><tr>
          <th className="px-3 py-2">{t('availableChannels.catalog.context')}</th>
          {fields.map(field => (
            <th key={field.key} className="whitespace-nowrap px-3 py-2">
              {t(field.key === 'per_request_price' && !token ? 'availableChannels.catalog.unitPrice' : `availableChannels.pricing.${field.label}`)}
            </th>
          ))}
        </tr></thead>
        <tbody className="divide-y divide-gray-100 dark:divide-dark-700">
          {rows.map((row, index) => (
            <tr key={index}>
              <th className="whitespace-nowrap px-3 py-2 font-normal">{row.label}</th>
              {fields.map(field => (
                <td key={field.key} className="whitespace-nowrap px-3 py-2 font-mono">
                  {catalogMoney(row.pricing[field.key], rate, token && field.key !== 'per_request_price')}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default CatalogPriceTable
