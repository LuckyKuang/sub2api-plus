import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type CatalogModel } from './catalog'
import { catalogRange } from './catalogQuote'
import { rateSource } from './catalogQuote'
import { Icon } from './icons'
import { PlatformIcon } from './PlatformIcon'
import { useClipboard } from './useClipboard'
import { platformTextClass } from '@/utils/platformColors'

export interface CatalogModelListProps {
  models: CatalogModel[]
  rateUnavailable: boolean
  onDetails?: (model: CatalogModel) => void
}

interface Highlight {
  platform: string
  style: { transform: string; width: string; height: string }
}

export function CatalogModelList({ models, rateUnavailable, onDetails }: CatalogModelListProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [hoveredRow, setHoveredRow] = useState<HTMLElement | null>(null)
  const [focusedRow, setFocusedRow] = useState<HTMLElement | null>(null)
  const [highlight, setHighlight] = useState<Highlight | null>(null)

  const activeRow = hoveredRow ?? focusedRow

  function updateHighlight() {
    const row = activeRow
    const menu = row?.closest('.catalog-list-menu')
    if (!row?.isConnected || !menu) return
    const box = row.getBoundingClientRect()
    const origin = menu.getBoundingClientRect()
    setHighlight({
      platform: row.dataset.platform!,
      style: {
        transform: `translate(${box.left - origin.left}px, ${box.top - origin.top}px)`,
        width: `${box.width}px`,
        height: `${box.height}px`
      }
    })
  }

  useEffect(() => {
    updateHighlight()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRow])

  useEffect(() => {
    const onResize = () => updateHighlight()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    setHoveredRow(null)
    setFocusedRow(null)
  }, [models])

  const sections = (() => {
    const platforms = new Map<string, CatalogModel[]>()
    for (const model of models) {
      const list = platforms.get(model.platform) ?? []
      list.push(model)
      platforms.set(model.platform, list)
    }
    return [...platforms].map(([platform, list]) => ({ platform, models: list }))
  })()

  return (
    <div className="catalog-list space-y-6" data-testid="catalog-model-list">
      {sections.map((section) => (
        <section
          key={section.platform}
          className="surface-card rounded-xl border border-gray-200 px-4 py-4 dark:border-dark-700"
          aria-label={section.platform}
        >
          <header className="mb-2 flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 pb-3 dark:border-dark-800">
            <h2 className={`flex min-w-0 items-center gap-2 text-sm font-semibold ${platformTextClass(section.platform)}`}>
              <PlatformIcon platform={section.platform} size="sm" />
              {section.platform}
              <span className="text-xs font-normal surface-muted">
                {t('availableChannels.catalog.modelCount', { count: section.models.length })}
              </span>
            </h2>
            <p className="text-xs surface-muted">
              {t(rateUnavailable ? 'availableChannels.catalog.referenceRange' : 'availableChannels.catalog.effectiveRange')}
            </p>
          </header>
          <div
            className="catalog-list-menu relative isolate"
            onPointerLeave={() => setHoveredRow(null)}
            onKeyDown={() => setHoveredRow(null)}
            onBlur={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget as Node)) setFocusedRow(null)
            }}
          >
            <div
              className={`catalog-list-highlight ${!!activeRow && highlight?.platform === section.platform ? 'is-active' : ''}`}
              style={highlight?.platform === section.platform ? highlight.style : undefined}
              aria-hidden="true"
            />
            <ol className="catalog-list-rows">
              {section.models.map((model, index) => (
                <li
                  key={model.key}
                  data-platform={section.platform}
                  className="catalog-list-row relative z-10 min-w-0 rounded-xl border-b border-gray-100 px-3 py-4 last:border-0 dark:border-dark-800"
                  onPointerEnter={(event) => {
                    if (event.pointerType !== 'touch') setHoveredRow(event.currentTarget)
                  }}
                  onFocus={(event) => setFocusedRow(event.currentTarget)}
                >
                  <button
                    type="button"
                    className="catalog-list-open absolute inset-0 w-full cursor-pointer rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500"
                    aria-haspopup="dialog"
                    aria-label={t('availableChannels.catalog.detailsFor', { model: model.name })}
                    onClick={() => onDetails?.(model)}
                  />
                  <article className="pointer-events-none relative flex min-w-0 items-start gap-3">
                    <span className="pt-1 font-mono text-xs tabular-nums surface-muted" aria-hidden="true">
                      {String(index + 1).padStart(2, '0')}
                    </span>
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex min-w-0 items-start gap-2">
                        <h3 className="min-w-0 break-words font-mono text-sm font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">
                          {model.name}
                        </h3>
                        <button
                          type="button"
                          className="catalog-copy-model pointer-events-auto relative shrink-0 rounded p-0.5 hover:text-primary-500 focus-visible:ring-2 focus-visible:ring-primary-500 surface-control surface-muted"
                          aria-label={t('availableChannels.catalog.copyModel', { model: model.name })}
                          onClick={() => void copyToClipboard(model.name)}
                        >
                          <Icon name="copy" size="sm" />
                        </button>
                        <span className="mt-2.5 min-w-0 flex-1 border-b border-dotted border-gray-300 dark:border-dark-600" aria-hidden="true" />
                      </div>
                      <div className="space-y-1.5 text-xs">
                        {model.prices.map((price) => (
                          <div key={price.unit} className="space-y-1">
                            <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                              <span className="surface-muted">{t(`availableChannels.catalog.units.${price.unit}`)}</span>
                              <span className="flex flex-wrap gap-x-3 gap-y-1 font-mono tabular-nums">
                                <span>
                                  <span className="mr-1 font-sans surface-muted">
                                    {t(
                                      price.unit === 'token'
                                        ? 'availableChannels.pricing.inputPrice'
                                        : 'availableChannels.catalog.unitPrice'
                                    )}
                                  </span>
                                  {catalogRange(price.input, price.unit)}
                                </span>
                                {price.unit === 'token' && (
                                  <span>
                                    <span className="mr-1 font-sans surface-muted">{t('availableChannels.pricing.outputPrice')}</span>
                                    {catalogRange(price.output, price.unit)}
                                  </span>
                                )}
                              </span>
                            </div>
                            {price.incomplete && (
                              <p className="surface-muted">
                                {t(
                                  price.input || price.output
                                    ? 'availableChannels.catalog.partialUnknown'
                                    : 'availableChannels.catalog.reasons.pricing_unavailable'
                                )}
                              </p>
                            )}
                          </div>
                        ))}
                      </div>
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs surface-muted">
                        <span>
                          {t('availableChannels.catalog.quoteCount', { count: model.quotes.length })}
                          {model.quotes.some((q) => q.group.is_exclusive) && (
                            <span
                              className="whitespace-nowrap font-medium text-purple-700 dark:text-purple-300"
                              title={t('availableChannels.catalog.exclusiveQuote')}
                            >
                              {' ' + t('availableChannels.exclusive')}
                            </span>
                          )}
                        </span>
                        {model.quotes.some((q) => rateSource(q.group, q.offer) === 'personal') && (
                          <span className="text-emerald-700 dark:text-emerald-300">
                            {t('availableChannels.catalog.personalRate')}
                          </span>
                        )}
                        {model.tiered !== 'none' && (
                          <span className="text-blue-700 dark:text-blue-300">
                            {t(model.tiered === 'all' ? 'availableChannels.pricing.intervals' : 'availableChannels.catalog.someTiered')}
                          </span>
                        )}
                        {model.dynamic !== 'none' && (
                          <span className="text-amber-700 dark:text-amber-300">
                            {t(model.dynamic === 'all' ? 'availableChannels.catalog.dynamicBilling' : 'availableChannels.catalog.someDynamic')}
                          </span>
                        )}
                      </div>
                    </div>
                  </article>
                </li>
              ))}
            </ol>
          </div>
        </section>
      ))}
    </div>
  )
}

export default CatalogModelList
