import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Select } from '@/components/ui'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import userChannelsAPI, { type ChannelCatalog } from '@/api/channels'
import { getLocale } from '@/i18n'
import {
  availableChannelCatalogGeneration,
  cacheAvailableChannelCatalog,
  invalidateAvailableChannelCatalog,
  readAvailableChannelCatalog
} from '@/utils/availableChannelCatalogCache'
import { extractApiErrorMessage } from '@/utils/apiError'
import { Icon } from './icons'
import { PlatformIcon } from './PlatformIcon'
import { CatalogModelCard } from './CatalogModelCard'
import { CatalogModelList } from './CatalogModelList'
import { CatalogPriceDetails } from './CatalogPriceDetails'
import {
  catalogModels,
  defaultCatalogFilters,
  normalizeCatalogFilters,
  visibleModels,
  type CatalogFilters,
  type CatalogModel
} from './catalog'
import { useUserView } from './useUserView'

const emptyCatalog = (): ChannelCatalog => ({ groups: [], user_rate_status: 'not_requested' })

export default function AvailableChannelsView() {
  const { t } = useTranslation()
  const auth = useAuthStore()
  const app = useAppStore()
  const userView = useUserView()

  const [catalog, setCatalog] = useState<ChannelCatalog>(emptyCatalog())
  const [category, setCategory] = useState<'group' | 'composite'>('group')
  const [categoryFilters, setCategoryFilters] = useState<{
    group: CatalogFilters
    composite: CatalogFilters
  }>({ group: defaultCatalogFilters(), composite: defaultCatalogFilters() })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const refreshButton = useRef<HTMLButtonElement>(null)

  const requestId = useRef(0)
  const controller = useRef<AbortController | undefined>(undefined)

  const catalogIdentity = (() => {
    const actorId = auth.user?.id
    const userId = userView.user?.id
    if (!auth.isAuthenticated() || actorId == null || userId == null) return null
    return JSON.stringify([availableChannelCatalogGeneration(), actorId])
  })()
  const catalogKey = catalogIdentity === null ? null : JSON.stringify([catalogIdentity, getLocale()])

  const filters = categoryFilters[category]
  const allModels = catalogModels(catalog.groups)
  const categories = (['group', 'composite'] as const).filter((item) => allModels.some((model) => model.category === item))
  const models = allModels.filter((model) => model.category === category)
  const platforms = [...new Set(models.map((m) => m.platform))].sort()
  const visible = visibleModels(models, filters)
  const selected = models.find((m) => m.key === selectedKey) || null

  const identityRef = useRef<string | null>(null)
  identityRef.current = catalogIdentity
  const keyRef = useRef<string | null>(null)
  keyRef.current = catalogKey
  const selectedKeyRef = useRef<string | null>(null)
  selectedKeyRef.current = selectedKey
  const selectedRef = useRef<CatalogModel | null>(null)
  selectedRef.current = selected

  // An empty category falls back to the first one that has models.
  useEffect(() => {
    if (!categories.includes(category) && categories[0]) setCategory(categories[0])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [categories.join(',')])

  // Each category owns its filters, so every one must be normalized against its
  // own model list. Normalizing only the active category leaves a platform
  // selected that the inactive category no longer offers, and that category then
  // renders an empty list until it is reset by hand.
  function normalizeAllCategoryFilters(nextCatalog: ChannelCatalog) {
    const nextAll = catalogModels(nextCatalog.groups)
    setCategoryFilters((previous) => {
      const next = { ...previous }
      for (const item of ['group', 'composite'] as const) {
        const scoped = { ...previous[item] }
        normalizeCatalogFilters(
          nextAll.filter((model) => model.category === item),
          scoped
        )
        next[item] = scoped
      }
      return next
    })
  }

  function selectPlatform(platform: string) {
    setCategoryFilters((previous) => {
      const scoped = { ...previous[category], platform }
      normalizeCatalogFilters(models, scoped)
      return { ...previous, [category]: scoped }
    })
  }

  function reset() {
    setCategoryFilters((previous) => ({ ...previous, [category]: defaultCatalogFilters() }))
  }

  function openDetails(model: CatalogModel) {
    setSelectedKey(model.key)
  }

  const loadCatalog = async (force = false) => {
    controller.current?.abort()
    const id = ++requestId.current
    const key = keyRef.current
    if (key === null) return
    setError('')
    if (force) invalidateAvailableChannelCatalog(key)
    const cached = force ? undefined : readAvailableChannelCatalog(key)
    const currentController = new AbortController()
    controller.current = currentController
    setLoading(!cached)
    try {
      const result = cached ?? (await userChannelsAPI.getCatalog({ signal: currentController.signal }))
      if (id !== requestId.current || keyRef.current !== key || currentController.signal.aborted) return
      setCatalog(result)
      if (!cached) cacheAvailableChannelCatalog(key, result)
      normalizeAllCategoryFilters(result)
    } catch (err: unknown) {
      if (id !== requestId.current || keyRef.current !== key || currentController.signal.aborted) return
      invalidateAvailableChannelCatalog(key)
      setCatalog(emptyCatalog())
      setError(extractApiErrorMessage(err, t('availableChannels.catalog.loadFailed')))
    } finally {
      if (id === requestId.current) {
        setLoading(false)
        if (selectedKeyRef.current && !selectedRef.current) {
          setSelectedKey(null)
          refreshButton.current?.focus()
        }
      }
    }
  }

  const previousIdentity = useRef<string | null | undefined>(undefined)

  // Replaces the Vue `watch(catalogKey, …, { immediate: true, flush: 'sync' })`.
  useEffect(() => {
    requestId.current += 1
    controller.current?.abort()
    if (identityRef.current !== previousIdentity.current) {
      setCatalog(emptyCatalog())
      setSelectedKey(null)
      setCategoryFilters({ group: defaultCatalogFilters(), composite: defaultCatalogFilters() })
      setCategory('group')
    }
    previousIdentity.current = identityRef.current
    setLoading(false)
    setError('')
    void loadCatalog()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalogKey])

  useEffect(() => {
    return () => {
      requestId.current += 1
      controller.current?.abort()
    }
  }, [])

  const rateUnavailable = catalog.user_rate_status === 'unavailable'
  const platformCount = (platform: string) => models.filter((m) => m.platform === platform).length

  return (
    <div className="catalog-page workspace-page min-w-0 space-y-5">
      <div className="card workspace-fixed flex items-start justify-between gap-3 p-4" data-testid="catalog-heading">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">{t('availableChannels.title')}</h1>
          <p className="mt-1 text-sm surface-muted">{t('availableChannels.description')}</p>
        </div>
        <button
          ref={refreshButton}
          className="btn btn-secondary"
          disabled={loading}
          aria-label={t('common.refresh')}
          onClick={() => void loadCatalog(true)}
        >
          <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
        </button>
      </div>
      <div
        className="surface-card workspace-fixed space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
        data-testid="catalog-filters"
      >
        {categories.length > 0 && (
          <div className="segmented-control" role="tablist" aria-label={t('availableChannels.catalog.category')}>
            {categories.map((item) => (
              <button
                key={item}
                type="button"
                className="segmented-option"
                role="tab"
                aria-selected={category === item}
                onClick={() => {
                  setCategory(item)
                  setSelectedKey(null)
                }}
              >
                {t(`availableChannels.catalog.${item}Platforms`)}
              </button>
            ))}
          </div>
        )}
        <div className="catalog-platforms flex flex-wrap items-center gap-2">
          <span className="mr-2 text-xs surface-muted">{t('modelPlaza.filters.platformLabel')}</span>
          <div className="segmented-control" role="group" aria-label={t('modelPlaza.filters.platformLabel')}>
            <button
              type="button"
              className="segmented-option"
              aria-pressed={!filters.platform}
              onClick={() => selectPlatform('')}
            >
              {t('modelPlaza.filters.all')} <span className="opacity-60">{models.length}</span>
            </button>
            {platforms.map((platform) => (
              <button
                key={platform}
                type="button"
                className="segmented-option"
                aria-pressed={filters.platform === platform}
                onClick={() => selectPlatform(platform)}
              >
                <PlatformIcon platform={platform} size="sm" />
                {platform} <span className="opacity-60">{platformCount(platform)}</span>
              </button>
            ))}
          </div>
        </div>
        <div className="filter-bar border-t border-gray-100 pt-3 dark:border-dark-800">
          <label className="filter-field filter-field--search relative">
            <span className="sr-only">{t('availableChannels.catalog.searchPlaceholder')}</span>
            <Icon name="search" size="md" className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              value={filters.search}
              onChange={(event) =>
                setCategoryFilters((previous) => ({ ...previous, [category]: { ...previous[category], search: event.target.value } }))
              }
              className="input w-full pl-10 pr-10"
              type="search"
              placeholder={t('availableChannels.catalog.searchPlaceholder')}
            />
          </label>
          <label className="filter-field relative">
            <span className="sr-only">{t('availableChannels.catalog.sort')}</span>
            <Select
              value={filters.sort}
              searchable={false}
              options={[
                { value: 'name', label: t('availableChannels.catalog.sortName') },
                { value: 'price', label: t('availableChannels.catalog.sortPrice') }
              ]}
              onValueChange={(value) =>
                setCategoryFilters((previous) => ({
                  ...previous,
                  [category]: { ...previous[category], sort: value === 'price' ? 'price' : 'name' }
                }))
              }
            />
          </label>
          <button className="btn btn-secondary" onClick={reset}>
            {t('availableChannels.catalog.reset')}
          </button>
        </div>
      </div>
      {rateUnavailable && (
        <div
          role="status"
          className="workspace-fixed flex flex-wrap items-center gap-3 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
        >
          {t('availableChannels.catalog.ratesUnavailable')}
          <button className="underline surface-control" onClick={() => void loadCatalog(true)}>
            {t('availableChannels.catalog.retry')}
          </button>
        </div>
      )}
      {!loading && !error && (
        <p className="workspace-fixed text-xs surface-muted" aria-live="polite">
          {t('availableChannels.catalog.modelCount', { count: visible.length })}
        </p>
      )}
      <section
        className="workspace-shell workspace-scroll"
        aria-label={t('availableChannels.title')}
        data-testid="catalog-results"
      >
        {loading ? (
          <div
            className={app.availableChannelsLayout() === 'list' ? 'space-y-3' : 'catalog-grid'}
            aria-busy="true"
            aria-label={t('common.loading')}
          >
            {Array.from({ length: 12 }, (_, i) => (
              <div
                key={i}
                className={`glass-surface animate-pulse ${app.availableChannelsLayout() === 'list' ? 'h-16' : 'h-52'}`}
              />
            ))}
          </div>
        ) : error ? (
          <div role="alert" className="card py-12 text-center">
            <p className="text-red-600 dark:text-red-400">{error}</p>
            <button className="btn btn-secondary mt-4" onClick={() => void loadCatalog(true)}>
              {t('availableChannels.catalog.retry')}
            </button>
          </div>
        ) : !visible.length ? (
          <div className="card py-12 text-center surface-muted">
            <p>{models.length ? t('availableChannels.catalog.noMatches') : t('availableChannels.noModels')}</p>
            {models.length > 0 && (
              <button className="btn btn-secondary mt-4" onClick={reset}>
                {t('availableChannels.catalog.reset')}
              </button>
            )}
          </div>
        ) : app.availableChannelsLayout() === 'list' ? (
          <CatalogModelList models={visible} rateUnavailable={rateUnavailable} onDetails={openDetails} />
        ) : (
          <div className="catalog-grid">
            {visible.map((model) => (
              <CatalogModelCard key={model.key} model={model} rateUnavailable={rateUnavailable} onDetails={openDetails} />
            ))}
          </div>
        )}
      </section>
      <CatalogPriceDetails model={selected} rateUnavailable={rateUnavailable} onClose={() => setSelectedKey(null)} />
    </div>
  )
}
