import { useTranslation } from 'react-i18next'
import { SearchInput, Select } from '@/components/ui'
import { CONCRETE_PLATFORM_OPTIONS } from './platformCatalog'
import type { Group } from '@/types'

export interface AccountTableFiltersProps {
  searchQuery: string
  filters: Record<string, unknown>
  groups?: Group[]
  onUpdateSearchQuery: (value: string) => void
  onUpdateFilters: (filters: Record<string, unknown>) => void
  onChange: () => void
}

export function AccountTableFilters({
  searchQuery,
  filters,
  groups,
  onUpdateSearchQuery,
  onUpdateFilters,
  onChange
}: AccountTableFiltersProps) {
  const { t } = useTranslation()

  const updatePlatform = (value: string | number | boolean | null) => {
    onUpdateFilters({ ...filters, platform: value })
  }
  const updateType = (value: string | number | boolean | null) => {
    onUpdateFilters({ ...filters, type: value })
  }
  const updateStatus = (value: string | number | boolean | null) => {
    onUpdateFilters({ ...filters, status: value })
  }
  const updatePrivacyMode = (value: string | number | boolean | null) => {
    onUpdateFilters({ ...filters, privacy_mode: value })
  }
  const updateGroup = (value: string | number | boolean | null) => {
    onUpdateFilters({ ...filters, group: value })
  }

  const pOpts = [
    { value: '', label: t('admin.accounts.allPlatforms') },
    ...CONCRETE_PLATFORM_OPTIONS
  ]
  const tOpts = [
    { value: '', label: t('admin.accounts.allTypes') },
    { value: 'oauth', label: t('admin.accounts.oauthType') },
    { value: 'setup-token', label: t('admin.accounts.setupToken') },
    { value: 'apikey', label: t('admin.accounts.apiKey') },
    { value: 'bedrock', label: 'AWS Bedrock' }
  ]
  const sOpts = [
    { value: '', label: t('admin.accounts.allStatus') },
    { value: 'active', label: t('admin.accounts.status.active') },
    { value: 'inactive', label: t('admin.accounts.status.inactive') },
    { value: 'error', label: t('admin.accounts.status.error') },
    { value: 'rate_limited', label: t('admin.accounts.status.rateLimited') },
    { value: 'temp_unschedulable', label: t('admin.accounts.status.tempUnschedulable') },
    { value: 'unschedulable', label: t('admin.accounts.status.unschedulable') }
  ]
  const privacyOpts = [
    { value: '', label: t('admin.accounts.allPrivacyModes') },
    { value: '__unset__', label: t('admin.accounts.privacyUnset') },
    { value: 'training_off', label: 'Privacy' },
    { value: 'training_set_cf_blocked', label: 'CF' },
    { value: 'training_set_failed', label: 'Fail' }
  ]
  const gOpts = [
    { value: '', label: t('admin.accounts.allGroups') },
    { value: 'ungrouped', label: t('admin.accounts.ungroupedGroup') },
    ...(groups || []).map((g) => ({ value: String(g.id), label: g.name }))
  ]

  return (
    <div className="filter-bar filter-bar--inline">
      <div className="filter-field filter-field--search">
        <SearchInput
          value={searchQuery}
          placeholder={t('admin.accounts.searchAccounts')}
          onValueChange={onUpdateSearchQuery}
          onSearch={onChange}
        />
      </div>
      <div className="filter-field">
        <Select
          value={(filters.platform as string) ?? ''}
          options={pOpts}
          onValueChange={updatePlatform}
          onChange={onChange}
        />
      </div>
      <div className="filter-field">
        <Select
          value={(filters.type as string) ?? ''}
          options={tOpts}
          onValueChange={updateType}
          onChange={onChange}
        />
      </div>
      <div className="filter-field">
        <Select
          value={(filters.status as string) ?? ''}
          options={sOpts}
          onValueChange={updateStatus}
          onChange={onChange}
        />
      </div>
      <div className="filter-field">
        <Select
          value={(filters.privacy_mode as string) ?? ''}
          options={privacyOpts}
          onValueChange={updatePrivacyMode}
          onChange={onChange}
        />
      </div>
      <div className="filter-field">
        <Select
          value={(filters.group as string) ?? ''}
          options={gOpts}
          onValueChange={updateGroup}
          onChange={onChange}
        />
      </div>
    </div>
  )
}

export default AccountTableFilters
