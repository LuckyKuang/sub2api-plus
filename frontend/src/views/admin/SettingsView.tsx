import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { SystemSettings } from '@/api/admin/settings'
import type { CustomEndpoint, CustomMenuItem } from '@/types'
import { Button, Select, Toggle } from '@/components/ui'
import { Icon } from './AccountIcon'

type SettingsTab =
  | 'general'
  | 'agreement'
  | 'features'
  | 'security'
  | 'users'
  | 'gateway'
  | 'identity'
  | 'payment'
  | 'email'
  | 'backup'

const SETTINGS_TABS: Array<{ key: SettingsTab; icon: string }> = [
  { key: 'general', icon: 'home' },
  { key: 'agreement', icon: 'document' },
  { key: 'features', icon: 'bolt' },
  { key: 'security', icon: 'shield' },
  { key: 'users', icon: 'user' },
  { key: 'gateway', icon: 'server' },
  { key: 'identity', icon: 'shield' },
  { key: 'payment', icon: 'creditCard' },
  { key: 'email', icon: 'mail' },
  { key: 'backup', icon: 'database' }
]

const SETTINGS_TAB_KEYBOARD_ACTIONS: Record<string, number | 'first' | 'last'> = {
  ArrowLeft: -1,
  ArrowUp: -1,
  ArrowRight: 1,
  ArrowDown: 1,
  Home: 'first',
  End: 'last'
}

const TABLE_PAGE_SIZE_MIN = 5
const TABLE_PAGE_SIZE_MAX = 1000
const TABLE_PAGE_SIZE_DEFAULT = 20

const COMPLIANCE_ACK_CODE = 'ADMIN_COMPLIANCE_ACK_REQUIRED'

const parseTablePageSizeOptionsInput = (raw: string): number[] | null => {
  const parts = raw
    .split(',')
    .map((part) => part.trim())
    .filter((part) => part.length > 0)
  if (parts.length === 0) return null
  const values: number[] = []
  for (const part of parts) {
    const value = Number(part)
    if (!Number.isInteger(value) || value < TABLE_PAGE_SIZE_MIN || value > TABLE_PAGE_SIZE_MAX) {
      return null
    }
    values.push(value)
  }
  return values
}

const formatTablePageSizeOptions = (values: number[]): string => values.join(', ')

const sanitizeUrlLike = (value: string): string => value.trim()

export default function SettingsView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const requireAcknowledgement = useAdminComplianceStore((state) => state.requireAcknowledgement)

  const [activeTab, setActiveTab] = useState<SettingsTab>('general')
  const [loading, setLoading] = useState(true)
  const [loadFailed, setLoadFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const settingsBodyRef = useRef<HTMLDivElement | null>(null)

  // Generic settings form: the admin settings payload is wide and open-ended, so
  // the ported tabs bind individual keys and save sends the whole payload back,
  // exactly like the Vue form did.
  const [form, setForm] = useState<Record<string, unknown>>({})
  const formRef = useRef(form)
  formRef.current = form

  const [tablePageSizeOptionsInput, setTablePageSizeOptionsInput] = useState('10, 20, 50, 100')

  const customMenuItems = useAdminSettingsStore((state) => state.customMenuItems)

  const patchForm = useCallback((patch: Record<string, unknown>) => {
    setForm((prev) => ({ ...prev, ...patch }))
  }, [])

  const loadSettings = useCallback(async () => {
    setLoading(true)
    setLoadFailed(false)
    try {
      const settings = (await adminAPI.settings.getSettings()) as unknown as Record<string, unknown>
      settings.payment_load_balance_strategy = settings.payment_load_balance_strategy || 'round-robin'
      // Only assign non-null values from backend (null means unconfigured, keep defaults)
      for (const [key, value] of Object.entries(settings)) {
        if (value !== null && value !== undefined) {
          formRef.current[key] = value
        }
      }
      setForm({ ...formRef.current })
      setTablePageSizeOptionsInput(
        formatTablePageSizeOptions(
          Array.isArray(settings.table_page_size_options)
            ? (settings.table_page_size_options as number[])
            : [10, 20, 50, 100]
        )
      )
    } catch (error) {
      console.error('Failed to load settings:', error)
      setLoadFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadSettings()
  }, [loadSettings])

  // Reset the tab scroll position when switching tabs.
  useEffect(() => {
    if (settingsBodyRef.current) settingsBodyRef.current.scrollTop = 0
  }, [activeTab])

  const focusSettingsTab = (tab: SettingsTab) => {
    window.requestAnimationFrame(() => {
      document.getElementById(`settings-tab-${tab}`)?.focus()
    })
  }

  const handleSettingsTabKeydown = (event: React.KeyboardEvent, tab: SettingsTab) => {
    const action = SETTINGS_TAB_KEYBOARD_ACTIONS[event.key]
    if (action === undefined) return

    event.preventDefault()
    const currentIndex = SETTINGS_TABS.findIndex((item) => item.key === tab)
    let nextIndex = currentIndex < 0 ? 0 : currentIndex

    if (action === 'first') {
      nextIndex = 0
    } else if (action === 'last') {
      nextIndex = SETTINGS_TABS.length - 1
    } else {
      nextIndex = (nextIndex + action + SETTINGS_TABS.length) % SETTINGS_TABS.length
    }

    const nextTab = SETTINGS_TABS[nextIndex]
    if (!nextTab) return
    setActiveTab(nextTab.key)
    focusSettingsTab(nextTab.key)
  }

  const saveSettings = async () => {
    const current = formRef.current
    const normalizedTableDefaultPageSize = Math.floor(Number(current.table_default_page_size))
    if (
      !Number.isInteger(normalizedTableDefaultPageSize) ||
      normalizedTableDefaultPageSize < TABLE_PAGE_SIZE_MIN ||
      normalizedTableDefaultPageSize > TABLE_PAGE_SIZE_MAX
    ) {
      showError(
        t('admin.settings.site.tableDefaultPageSizeRangeError', {
          min: TABLE_PAGE_SIZE_MIN,
          max: TABLE_PAGE_SIZE_MAX
        })
      )
      return
    }

    const normalizedTablePageSizeOptions = parseTablePageSizeOptionsInput(tablePageSizeOptionsInput)
    if (!normalizedTablePageSizeOptions) {
      showError(
        t('admin.settings.site.tablePageSizeOptionsFormatError', {
          min: TABLE_PAGE_SIZE_MIN,
          max: TABLE_PAGE_SIZE_MAX
        })
      )
      return
    }

    setSaving(true)
    try {
      const payload: Record<string, unknown> = {
        ...current,
        table_default_page_size: normalizedTableDefaultPageSize,
        table_page_size_options: normalizedTablePageSizeOptions,
        custom_menu_items: customMenuItems,
        custom_endpoints: (current.custom_endpoints as CustomEndpoint[] | undefined) ?? []
      }
      delete payload.pricing_fallback_models
      await adminAPI.settings.updateSettings(payload as unknown as SystemSettings)
      showSuccess(t('common.success'))
      await loadSettings()
    } catch (error) {
      // A 423 with ADMIN_COMPLIANCE_ACK_REQUIRED must surface the acknowledgement flow.
      const err = error as { status?: number; code?: string; metadata?: Record<string, unknown> }
      if (err?.status === 423 && err?.code === COMPLIANCE_ACK_CODE) {
        requireAcknowledgement(err.metadata ?? {})
        return
      }
      showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      setSaving(false)
    }
  }

  // ---- General tab: site settings ----
  const strField = (key: string, fallback = ''): string => {
    const value = form[key]
    return typeof value === 'string' ? value : typeof value === 'number' ? String(value) : fallback
  }
  const boolField = (key: string, fallback = false): boolean => {
    const value = form[key]
    return typeof value === 'boolean' ? value : fallback
  }

  const customEndpoints = (form.custom_endpoints as CustomEndpoint[] | undefined) ?? []

  const addEndpoint = () => {
    patchForm({ custom_endpoints: [...customEndpoints, { name: '', endpoint: '', description: '' }] })
  }

  const removeEndpoint = (index: number) => {
    patchForm({ custom_endpoints: customEndpoints.filter((_, i) => i !== index) })
  }

  const patchEndpoint = (index: number, patch: Partial<CustomEndpoint>) => {
    patchForm({
      custom_endpoints: customEndpoints.map((entry, i) => (i === index ? { ...entry, ...patch } : entry))
    })
  }

  const menuItems = (customMenuItems as CustomMenuItem[]) ?? []

  const patchMenuItem = (index: number, patch: Partial<CustomMenuItem>) => {
    const next = menuItems.map((item, i) => (i === index ? { ...item, ...patch } : item))
    useAdminSettingsStore.setState({ customMenuItems: next })
  }

  const removeMenuItem = (index: number) => {
    const next = menuItems
      .filter((_, i) => i !== index)
      .map((item, i) => ({ ...item, sort_order: i }))
    useAdminSettingsStore.setState({ customMenuItems: next })
  }

  const moveMenuItem = (index: number, direction: -1 | 1) => {
    const targetIndex = index + direction
    if (targetIndex < 0 || targetIndex >= menuItems.length) return
    const items = [...menuItems]
    const temp = items[index]
    items[index] = items[targetIndex]
    items[targetIndex] = temp
    useAdminSettingsStore.setState({
      customMenuItems: items.map((item, i) => ({ ...item, sort_order: i }))
    })
  }

  const generalTab = useMemo(
    () => (
      <div className="space-y-6">
        {/* Site Settings */}
        <div className="card workspace-shell">
          <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t('admin.settings.site.title')}</h2>
            <p className="mt-1 text-sm surface-muted">{t('admin.settings.site.description')}</p>
          </div>
          <div className="space-y-6 p-6">
            {/* Backend Mode */}
            <div className="flex items-center justify-between rounded-lg border border-amber-200 bg-amber-50/[0.15] p-4 dark:border-amber-800 dark:bg-amber-900/20">
              <div>
                <h3 className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.settings.site.backendMode')}</h3>
                <p className="mt-1 text-xs surface-muted">{t('admin.settings.site.backendModeDescription')}</p>
              </div>
              <Toggle
                checked={boolField('backend_mode_enabled')}
                onCheckedChange={(checked) => patchForm({ backend_mode_enabled: checked })}
              />
            </div>

            <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm font-medium surface-muted">{t('admin.settings.site.siteName')}</label>
                <input
                  value={strField('site_name')}
                  onChange={(event) => patchForm({ site_name: event.target.value })}
                  type="text"
                  className="input"
                  placeholder={t('admin.settings.site.siteNamePlaceholder')}
                />
                <p className="mt-1.5 text-xs surface-muted">{t('admin.settings.site.siteNameHint')}</p>
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium surface-muted">{t('admin.settings.site.siteSubtitle')}</label>
                <input
                  value={strField('site_subtitle')}
                  onChange={(event) => patchForm({ site_subtitle: event.target.value })}
                  type="text"
                  className="input"
                  placeholder={t('admin.settings.site.siteSubtitlePlaceholder')}
                />
                <p className="mt-1.5 text-xs surface-muted">{t('admin.settings.site.siteSubtitleHint')}</p>
              </div>
            </div>

            {/* API Base URL */}
            <div>
              <label className="mb-2 block text-sm font-medium surface-muted">{t('admin.settings.site.apiBaseUrl')}</label>
              <input
                value={strField('api_base_url')}
                onChange={(event) => patchForm({ api_base_url: event.target.value })}
                type="text"
                className="input font-mono text-sm"
                placeholder={t('admin.settings.site.apiBaseUrlPlaceholder')}
              />
              <p className="mt-1.5 text-xs surface-muted">{t('admin.settings.site.apiBaseUrlHint')}</p>
            </div>

            {/* Global Table Preferences */}
            <div className="border-t border-gray-100 pt-4 dark:border-dark-700">
              <h3 className="text-sm font-medium text-gray-900 dark:text-white">
                {t('admin.settings.site.tablePreferencesTitle')}
              </h3>
              <p className="mt-1 text-xs surface-muted">{t('admin.settings.site.tablePreferencesDescription')}</p>
              <div className="mt-4 grid grid-cols-1 gap-6 md:grid-cols-2">
                <div>
                  <label className="mb-2 block text-sm font-medium surface-muted">
                    {t('admin.settings.site.tableDefaultPageSize')}
                  </label>
                  <input
                    value={String(form.table_default_page_size ?? TABLE_PAGE_SIZE_DEFAULT)}
                    onChange={(event) => patchForm({ table_default_page_size: event.target.value })}
                    type="number"
                    min={TABLE_PAGE_SIZE_MIN}
                    max={TABLE_PAGE_SIZE_MAX}
                    step="1"
                    className="input w-40"
                  />
                  <p className="mt-1.5 text-xs surface-muted">{t('admin.settings.site.tableDefaultPageSizeHint')}</p>
                </div>
                <div>
                  <label className="mb-2 block text-sm font-medium surface-muted">
                    {t('admin.settings.site.tablePageSizeOptions')}
                  </label>
                  <input
                    value={tablePageSizeOptionsInput}
                    onChange={(event) => setTablePageSizeOptionsInput(event.target.value)}
                    type="text"
                    className="input font-mono text-sm"
                    placeholder={t('admin.settings.site.tablePageSizeOptionsPlaceholder')}
                  />
                  <p className="mt-1.5 text-xs surface-muted">{t('admin.settings.site.tablePageSizeOptionsHint')}</p>
                </div>
              </div>
            </div>

            {/* Custom Endpoints */}
            <div>
              <label className="mb-2 block text-sm font-medium surface-muted">
                {t('admin.settings.site.customEndpoints.title')}
              </label>
              <p className="mb-3 text-xs surface-muted">{t('admin.settings.site.customEndpoints.description')}</p>

              <div className="space-y-3">
                {customEndpoints.map((ep, index) => (
                  <div key={index} className="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
                    <div className="mb-3 flex items-center justify-between">
                      <span className="text-sm font-medium surface-muted">
                        {t('admin.settings.site.customEndpoints.itemLabel', { n: index + 1 })}
                      </span>
                      <button
                        type="button"
                        className="rounded p-1 text-red-400 hover:bg-red-50/25 hover:text-red-600 dark:hover:bg-red-900/20 surface-control"
                        onClick={() => removeEndpoint(index)}
                      >
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth="2"
                            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                          />
                        </svg>
                      </button>
                    </div>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div>
                        <label className="mb-1 block text-xs font-medium surface-muted">
                          {t('admin.settings.site.customEndpoints.name')}
                        </label>
                        <input
                          value={ep.name}
                          onChange={(event) => patchEndpoint(index, { name: event.target.value })}
                          type="text"
                          className="input text-sm"
                        />
                      </div>
                      <div>
                        <label className="mb-1 block text-xs font-medium surface-muted">
                          {t('admin.settings.site.customEndpoints.endpoint')}
                        </label>
                        <input
                          value={ep.endpoint}
                          onChange={(event) => patchEndpoint(index, { endpoint: event.target.value })}
                          type="text"
                          className="input font-mono text-sm"
                        />
                      </div>
                      <div className="sm:col-span-2">
                        <label className="mb-1 block text-xs font-medium surface-muted">
                          {t('admin.settings.site.customEndpoints.description')}
                        </label>
                        <input
                          value={ep.description}
                          onChange={(event) => patchEndpoint(index, { description: event.target.value })}
                          type="text"
                          className="input text-sm"
                        />
                      </div>
                    </div>
                  </div>
                ))}
                <button type="button" className="btn btn-secondary btn-sm" onClick={addEndpoint}>
                  <Icon name="plus" size="sm" className="mr-1" />
                  {t('admin.settings.site.customEndpoints.add')}
                </button>
              </div>
            </div>
          </div>
        </div>

        {/* Custom Menu Items */}
        <div className="card workspace-shell">
          <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
              {t('admin.settings.customMenu.title')}
            </h2>
            <p className="mt-1 text-sm surface-muted">{t('admin.settings.customMenu.description')}</p>
          </div>
          <div className="space-y-4 p-6">
            {menuItems.map((item, index) => (
              <div key={item.id || index} className="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
                <div className="mb-3 flex items-center justify-between">
                  <span className="text-sm font-medium surface-muted">
                    {t('admin.settings.customMenu.itemLabel', { n: index + 1 })}
                  </span>
                  <div className="flex items-center gap-2">
                    {index > 0 && (
                      <button
                        type="button"
                        className="rounded p-1 hover:surface-hover hover:text-gray-600 surface-control surface-muted"
                        title={t('admin.settings.customMenu.moveUp')}
                        onClick={() => moveMenuItem(index, -1)}
                      >
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                          <path strokeLinecap="round" strokeLinejoin="round" d="M5 15l7-7 7 7" />
                        </svg>
                      </button>
                    )}
                    {index < menuItems.length - 1 && (
                      <button
                        type="button"
                        className="rounded p-1 hover:surface-hover hover:text-gray-600 surface-control surface-muted"
                        title={t('admin.settings.customMenu.moveDown')}
                        onClick={() => moveMenuItem(index, 1)}
                      >
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                          <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
                        </svg>
                      </button>
                    )}
                    <button
                      type="button"
                      className="rounded p-1 text-red-400 hover:bg-red-50/25 hover:text-red-600 dark:hover:bg-red-900/20 surface-control"
                      title={t('admin.settings.customMenu.remove')}
                      onClick={() => removeMenuItem(index)}
                    >
                      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                        />
                      </svg>
                    </button>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.settings.customMenu.name')}</label>
                    <input
                      value={item.label}
                      onChange={(event) => patchMenuItem(index, { label: event.target.value })}
                      type="text"
                      className="input text-sm"
                      placeholder={t('admin.settings.customMenu.namePlaceholder')}
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs font-medium surface-muted">
                      {t('admin.settings.customMenu.visibility')}
                    </label>
                    <Select
                      value={item.visibility}
                      options={[
                        { value: 'user', label: t('admin.settings.customMenu.visibilityUser') },
                        { value: 'admin', label: t('admin.settings.customMenu.visibilityAdmin') }
                      ]}
                      onValueChange={(value) =>
                        patchMenuItem(index, { visibility: String(value) as CustomMenuItem['visibility'] })
                      }
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.settings.customMenu.url')}</label>
                    <input
                      value={item.url}
                      onChange={(event) => patchMenuItem(index, { url: sanitizeUrlLike(event.target.value) })}
                      type="text"
                      className="input font-mono text-sm"
                      placeholder={t('admin.settings.customMenu.urlPlaceholder')}
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs font-medium surface-muted">
                      {t('admin.settings.customMenu.pageSlug')}
                    </label>
                    <input
                      value={item.page_slug ?? ''}
                      onChange={(event) => patchMenuItem(index, { page_slug: event.target.value })}
                      type="text"
                      className="input font-mono text-sm"
                    />
                  </div>
                </div>
              </div>
            ))}
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() =>
                useAdminSettingsStore.setState({
                  customMenuItems: [
                    ...menuItems,
                    {
                      id: '',
                      label: '',
                      icon_svg: '',
                      url: '',
                      visibility: 'user' as const,
                      sort_order: menuItems.length
                    }
                  ]
                })
              }
            >
              <Icon name="plus" size="sm" className="mr-1" />
              {t('admin.settings.customMenu.add')}
            </button>
          </div>
        </div>
      </div>
    ),
    [
      t,
      form,
      customEndpoints,
      menuItems,
      patchForm,
      tablePageSizeOptionsInput
    ]
  )

  const pendingTab = (tab: SettingsTab) => (
    <div className="card p-6">
      <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t(`admin.settings.tabs.${tab}`)}</h2>
      <p className="mt-2 text-sm surface-muted">{t('admin.settings.tabPending')}</p>
    </div>
  )

  if (loading) {
    return (
      <div className="workspace-page">
        <div className="flex items-center justify-center py-12">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
        </div>
      </div>
    )
  }

  return (
    <div className="workspace-page">
      {loadFailed && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50/[0.15] p-4 text-sm text-red-700 dark:border-red-700 dark:bg-red-900/20 dark:text-red-300">
          {t('admin.settings.loadFailed')}
          <Button variant="secondary" size="sm" className="ml-3" onClick={() => void loadSettings()}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      <form
        className="workspace-body workspace-clip"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          void saveSettings()
        }}
      >
        {/* Tab Navigation */}
        <div className="settings-tabs-shell workspace-fixed">
          <nav className="settings-tabs-scroll" role="tablist" aria-label={t('admin.settings.title')}>
            <div className="settings-tabs">
              {SETTINGS_TABS.map((tab) => (
                <button
                  key={tab.key}
                  id={`settings-tab-${tab.key}`}
                  type="button"
                  role="tab"
                  aria-selected={activeTab === tab.key}
                  tabIndex={activeTab === tab.key ? 0 : -1}
                  className={`settings-tab ${activeTab === tab.key ? 'settings-tab-active' : ''}`}
                  onClick={() => setActiveTab(tab.key)}
                  onKeyDown={(event) => handleSettingsTabKeydown(event, tab.key)}
                >
                  <span className="settings-tab-icon">
                    <Icon name={tab.icon} size="sm" />
                  </span>
                  <span className="settings-tab-label">{t(`admin.settings.tabs.${tab.key}`)}</span>
                </button>
              ))}
            </div>
          </nav>
        </div>

        <div ref={settingsBodyRef} className="workspace-shell workspace-scroll workspace-stack space-y-6" data-test="settings-body-scroll">
          <div style={{ display: activeTab === 'general' ? undefined : 'none' }} className="space-y-6">
            {generalTab}
          </div>
          {SETTINGS_TABS.filter((tab) => tab.key !== 'general').map((tab) => (
            <div key={tab.key} style={{ display: activeTab === tab.key ? undefined : 'none' }} className="space-y-6">
              {activeTab === tab.key && pendingTab(tab.key)}
            </div>
          ))}

          {/* Save bar */}
          <div className="flex justify-end gap-3 py-2">
            <Button type="submit" variant="primary" loading={saving}>
              {saving ? t('common.saving') : t('common.save')}
            </Button>
          </div>
        </div>
      </form>
    </div>
  )
}
