import { useTranslation } from 'react-i18next'
import { Icon } from './AccountIcon'

export interface AccountTableActionsProps {
  loading?: boolean
  onRefresh: () => void
  onCreate?: () => void
  /** Rendered before the refresh button. */
  before?: React.ReactNode
  /** Rendered after the refresh button (auto-refresh + more tools). */
  after?: React.ReactNode
  beforeCreate?: React.ReactNode
  afterCreate?: React.ReactNode
}

export function AccountTableActions({
  loading = false,
  onRefresh,
  onCreate,
  before,
  after,
  beforeCreate,
  afterCreate
}: AccountTableActionsProps) {
  const { t } = useTranslation()
  return (
    <div className="filter-actions">
      {before}
      <button onClick={onRefresh} disabled={loading} className="btn btn-secondary">
        <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
      </button>
      {after}
      {beforeCreate}
      {onCreate && (
        <button onClick={onCreate} className="btn btn-primary">{t('admin.accounts.createAccount')}</button>
      )}
      {afterCreate}
    </div>
  )
}

export default AccountTableActions
