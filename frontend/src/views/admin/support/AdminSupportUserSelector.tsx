import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Select } from '@/components/ui'
import { useAuthStore } from '@/stores/auth'
import { useAdminSupportViewStore } from '@/stores/adminSupportView'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import { accountSelectionDestination, parseAdminSupportTargetId } from '@/utils/adminSupport'
import { Icon } from '@/views/admin/AccountIcon'

export interface AdminSupportUserSelectorProps {
  collapsed?: boolean
}

interface AccountOption {
  value: number
  label: string
  description: string
  email: string
  username: string
  role: 'admin' | 'user'
  status: 'active' | 'disabled'
}

/** Account switcher for admin assistance mode, ported from the Vue selector. */
export function AdminSupportUserSelector({ collapsed = false }: AdminSupportUserSelectorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const params = useParams()
  const user = useAuthStore((s) => s.user)
  const accounts = useAdminSupportViewStore((s) => s.accounts)
  const accountsLoading = useAdminSupportViewStore((s) => s.accountsLoading)
  const accountsError = useAdminSupportViewStore((s) => s.accountsError)
  const target = useAdminSupportViewStore((s) => s.target)
  const loadAccounts = useAdminSupportViewStore((s) => s.loadAccounts)

  const actorUserId = user?.id ?? 0

  const selectorEmptyText = accountsLoading
    ? t('common.loading')
    : accountsError
      ? t('admin.support.selectorLoadFailed')
      : t('admin.support.selectorEmpty')

  const selectedUserId = parseAdminSupportTargetId(params.user_id) ?? actorUserId

  const accountOptions = useMemo<AccountOption[]>(() => {
    const options = new Map<number, AccountOption>()
    const add = (candidate: {
      id: number
      email: string
      username?: string
      role: 'admin' | 'user'
      status: 'active' | 'disabled'
    }) => {
      const username = candidate.username?.trim() ?? ''
      options.set(candidate.id, {
        value: candidate.id,
        label: username || candidate.email,
        description: `${candidate.email} #${candidate.id}`,
        email: candidate.email,
        username,
        role: candidate.role,
        status: candidate.status
      })
    }

    if (user) add(user)
    for (const account of accounts) add(account)
    if (target) add(target)

    return [...options.values()].sort((a, b) => {
      if (a.value === actorUserId) return -1
      if (b.value === actorUserId) return 1
      return a.label.localeCompare(b.label)
    })
  }, [user, accounts, target, actorUserId])

  useEffect(() => {
    void loadAccounts().catch(() => undefined)
  }, [loadAccounts])

  function optionInitial(option?: AccountOption): string {
    const label = option?.username || option?.email || '?'
    return label.slice(0, 1).toUpperCase()
  }

  function selectAccount(value: string | number | boolean | null): void {
    const targetUserId = Number(value)
    if (!Number.isSafeInteger(targetUserId) || targetUserId <= 0 || actorUserId <= 0) return
    if (targetUserId === actorUserId) setAdminSupportContext(null)
    const destination = accountSelectionDestination(location.pathname, actorUserId, targetUserId)
    if (destination && destination !== location.pathname) void navigate(destination)
  }

  return (
    <Select
      value={selectedUserId}
      options={accountOptions}
      valueKey="value"
      labelKey="label"
      searchable
      searchPlaceholder={t('admin.support.selectorSearch')}
      emptyText={selectorEmptyText}
      ariaLabel={t('admin.support.selectorLabel')}
      triggerClass={collapsed ? 'admin-support-selector admin-support-selector-collapsed' : 'admin-support-selector'}
      onValueChange={selectAccount}
      renderSelected={(option) => {
        const account = option as unknown as AccountOption | undefined
        return (
          <span className="flex min-w-0 items-center gap-2">
            <span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-primary-100/[0.15] text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
              {optionInitial(account)}
            </span>
            {!collapsed && (
              <span className="min-w-0 text-left">
                <span className="block truncate text-xs font-semibold text-gray-800 dark:text-gray-100">
                  {account?.label ?? t('admin.support.selectorLabel')}
                </span>
                <span className="block truncate text-[10px] surface-muted">
                  {selectedUserId === actorUserId ? t('admin.support.selfAccount') : t('admin.support.readOnlyTarget')}
                </span>
              </span>
            )}
          </span>
        )
      }}
      renderOption={(option, state) => {
        const account = option as unknown as AccountOption
        return (
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <span className="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md surface-inset text-xs font-semibold surface-muted">
              {optionInitial(account)}
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-1.5">
                <span className="truncate text-sm font-medium">{account.label}</span>
                {account.value === actorUserId && (
                  <span className="rounded bg-primary-50/[0.15] px-1.5 py-0.5 text-[10px] text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                    {t('admin.support.self')}
                  </span>
                )}
              </span>
              <span className="block truncate text-xs surface-muted">
                {account.email} · #{account.value}
              </span>
            </span>
            {state.selected && <Icon name="check" size="sm" className="text-primary-500" />}
          </div>
        )
      }}
    />
  )
}

export default AdminSupportUserSelector
