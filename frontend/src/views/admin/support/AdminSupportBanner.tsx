import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import type { AdminSupportUser } from '@/api/admin/supportView'
import { Icon } from '@/views/admin/AccountIcon'

export interface AdminSupportBannerProps {
  target: AdminSupportUser
}

/**
 * Read-only banner shown while an administrator is assisting a user. Mirrors the
 * Vue AdminSupportBanner; the read-only boundary itself lives in
 * `utils/adminSupportContext`.
 */
export function AdminSupportBanner({ target }: AdminSupportBannerProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  function exitSupport() {
    setAdminSupportContext(null)
    void navigate('/admin/users')
  }

  const targetLabel = target.username?.trim() || target.email

  return (
    <section
      className="rounded-xl border border-amber-200 bg-amber-50/90 p-4 shadow-sm dark:border-amber-800/70 dark:bg-amber-950/30"
      role="status"
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <span className="mt-0.5 flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/50 dark:text-amber-300">
            <Icon name="eye" size="md" />
          </span>
          <div className="min-w-0">
            <p className="font-semibold text-amber-950 dark:text-amber-100">{t('admin.support.bannerTitle')}</p>
            <p className="mt-0.5 break-words text-sm text-amber-800 dark:text-amber-200">
              {t('admin.support.bannerDescription', { account: targetLabel, id: target.id })}
            </p>
          </div>
        </div>
        <span className="inline-flex flex-shrink-0 items-center rounded-full border border-amber-300 surface-inset px-3 py-1 text-xs font-semibold text-amber-800 dark:border-amber-700 dark:text-amber-200">
          {t('admin.support.readOnly')}
        </span>
        <button type="button" className="btn btn-secondary btn-sm" onClick={exitSupport}>
          {t('admin.support.exit')}
        </button>
      </div>
    </section>
  )
}

export default AdminSupportBanner
