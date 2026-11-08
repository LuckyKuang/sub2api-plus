import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { supportsAccountQuota } from '@/utils/accountQuota'
import type { Account } from '@/types'
import { Icon } from './AccountIcon'

export interface AccountActionMenuProps {
  show: boolean
  account: Account | null
  anchorRect: DOMRect | null
  onClose: () => void
  onTest: (account: Account) => void
  onStats: (account: Account) => void
  onSchedule: (account: Account) => void
  onUsageAlert: (account: Account) => void
  onDuplicate: (account: Account) => void
  onReauth: (account: Account) => void
  onRefreshToken: (account: Account) => void
  onRecoverState: (account: Account) => void
  onResetQuota: (account: Account) => void
  onSetPrivacy: (account: Account) => void
  onCreateSparkShadow: (account: Account) => void
}

const VIEWPORT_PADDING = 8

export function AccountActionMenu({
  show,
  account,
  anchorRect,
  onClose,
  onTest,
  onStats,
  onSchedule,
  onUsageAlert,
  onDuplicate,
  onReauth,
  onRefreshToken,
  onRecoverState,
  onResetQuota,
  onSetPrivacy,
  onCreateSparkShadow
}: AccountActionMenuProps) {
  const { t } = useTranslation()
  const menuRef = useRef<HTMLDivElement | null>(null)
  const [viewport, setViewport] = useState({ width: 0, height: 0 })
  const [position, setPosition] = useState({ top: VIEWPORT_PADDING, left: VIEWPORT_PADDING })

  useEffect(() => {
    const update = () => setViewport({ width: window.innerWidth, height: window.innerHeight })
    update()
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [])

  const menuStyle = useMemo(
    () => ({
      top: `${position.top}px`,
      left: `${position.left}px`,
      maxWidth: `${Math.max(0, viewport.width - VIEWPORT_PADDING * 2)}px`,
      maxHeight: `${Math.max(0, viewport.height - VIEWPORT_PADDING * 2)}px`
    }),
    [position, viewport]
  )

  // Measure after rendering; menu items and translated labels can change its size.
  useLayoutEffect(() => {
    if (!show) return
    const updatePosition = () => {
      if (!menuRef.current || !anchorRect) return
      const { width, height } = menuRef.current.getBoundingClientRect()
      const gap = 4
      const maxTop = viewport.height - height - VIEWPORT_PADDING
      const top = anchorRect.bottom + gap <= maxTop ? anchorRect.bottom + gap : anchorRect.top - height - gap
      const left =
        viewport.width < 768
          ? anchorRect.left + anchorRect.width / 2 - width / 2
          : anchorRect.right - width
      setPosition({
        top: Math.max(VIEWPORT_PADDING, Math.min(top, maxTop)),
        left: Math.max(VIEWPORT_PADDING, Math.min(left, viewport.width - width - VIEWPORT_PADDING))
      })
    }
    updatePosition()
    const observer = new ResizeObserver(updatePosition)
    if (menuRef.current) observer.observe(menuRef.current)
    return () => observer.disconnect()
  }, [show, anchorRect, viewport])

  useEffect(() => {
    if (!show) return
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [show, onClose])

  const isRateLimited = useMemo(() => {
    if (account?.rate_limit_reset_at && new Date(account.rate_limit_reset_at) > new Date()) {
      return true
    }
    const modelLimits = (account?.extra as Record<string, unknown> | undefined)?.model_rate_limits as
      | Record<string, { rate_limit_reset_at: string }>
      | undefined
    if (modelLimits) {
      const now = new Date()
      return Object.values(modelLimits).some((info) => new Date(info.rate_limit_reset_at) > now)
    }
    return false
  }, [account])

  const isOverloaded = Boolean(account?.overload_until && new Date(account.overload_until) > new Date())
  const isTempUnschedulable = Boolean(
    account?.temp_unschedulable_until && new Date(account.temp_unschedulable_until) > new Date()
  )
  const hasRecoverableState =
    account?.status === 'error' || Boolean(isRateLimited) || isOverloaded || isTempUnschedulable
  const isAntigravityOAuth = account?.platform === 'antigravity' && account?.type === 'oauth'
  const isOpenAIOAuth = account?.platform === 'openai' && account?.type === 'oauth'
  // 影子账号(链接型,持 parent_account_id)不持凭据、type 不可变,凭据/隐私类操作对其无效。
  const isShadow = account?.parent_account_id != null
  // A "parent" OpenAI OAuth account is one that is NOT itself a shadow (parent_account_id == null)
  const isOpenAIOAuthParent = isOpenAIOAuth && !isShadow
  const supportsPrivacy = (isAntigravityOAuth || isOpenAIOAuth) && !isShadow
  const hasQuotaLimit =
    supportsAccountQuota(account?.type) &&
    ((account?.quota_limit ?? 0) > 0 ||
      (account?.quota_daily_limit ?? 0) > 0 ||
      (account?.quota_weekly_limit ?? 0) > 0)
  const canDuplicate =
    !!account &&
    account.parent_account_id == null &&
    ['apikey', 'upstream', 'bedrock', 'service_account'].includes(account.type)

  if (!show || !anchorRect) return null

  const emit = (handler: (account: Account) => void) => {
    if (!account) return
    handler(account)
    onClose()
  }

  const itemClass =
    'flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control'

  return createPortal(
    <div>
      {/* Backdrop: click anywhere outside to close */}
      <div className="fixed inset-0 z-[9998]" onClick={onClose} />
      <div
        ref={menuRef}
        className="action-menu-content fixed z-[9999] w-52 overflow-y-auto overscroll-contain shadow-lg ring-1 ring-black/5 surface-panel"
        style={menuStyle}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="space-y-0.5 p-1.5">
          {account && (
            <>
              <button onClick={() => emit(onTest)} className={itemClass}>
                <Icon name="play" size="sm" className="text-green-500" strokeWidth={2} />
                {t('admin.accounts.testConnection')}
              </button>
              <button onClick={() => emit(onStats)} className={itemClass}>
                <Icon name="chart" size="sm" className="text-indigo-500" />
                {t('admin.accounts.viewStats')}
              </button>
              <button onClick={() => emit(onSchedule)} className={itemClass}>
                <Icon name="clock" size="sm" className="text-orange-500" />
                {t('admin.scheduledTests.schedule')}
              </button>
              <button onClick={() => emit(onUsageAlert)} className={itemClass}>
                <Icon name="bell" size="sm" className="text-green-600" />
                {t('admin.usageAlert.menu')}
              </button>
              {canDuplicate && (
                <button onClick={() => emit(onDuplicate)} className={itemClass}>
                  <Icon name="copy" size="sm" className="text-sky-500" />
                  {t('admin.accounts.duplicateAccount')}
                </button>
              )}
              {/* 影子账号不持凭据:重授权/刷新 token 对其无效(后端拒绝),故隐藏(外审 G4)。 */}
              {(account.type === 'oauth' || account.type === 'setup-token') && !isShadow && (
                <>
                  {account.platform !== 'zhipu' && (
                    <button onClick={() => emit(onReauth)} className={cn(itemClass, 'text-blue-600')}>
                      <Icon name="link" size="sm" />
                      {t('admin.accounts.reAuthorize')}
                    </button>
                  )}
                  {account.platform !== 'deepseek' && account.platform !== 'zhipu' && (
                    <button onClick={() => emit(onRefreshToken)} className={cn(itemClass, 'text-purple-600')}>
                      <Icon name="refresh" size="sm" />
                      {t('admin.accounts.refreshToken')}
                    </button>
                  )}
                </>
              )}
              {isOpenAIOAuthParent && (
                <button onClick={() => emit(onCreateSparkShadow)} className={cn(itemClass, 'text-amber-600')}>
                  <Icon name="sparkles" size="sm" />
                  {t('admin.accounts.createSparkShadow')}
                </button>
              )}
              {supportsPrivacy && (
                <button onClick={() => emit(onSetPrivacy)} className={cn(itemClass, 'text-emerald-600')}>
                  <Icon name="shield" size="sm" />
                  {t('admin.accounts.setPrivacy')}
                </button>
              )}
              {hasRecoverableState && <div className="my-1 border-t border-gray-100 dark:border-dark-700" />}
              {hasRecoverableState && (
                <button onClick={() => emit(onRecoverState)} className={cn(itemClass, 'text-emerald-600')}>
                  <Icon name="sync" size="sm" />
                  {t('admin.accounts.recoverState')}
                </button>
              )}
              {hasQuotaLimit && (
                <button onClick={() => emit(onResetQuota)} className={cn(itemClass, 'text-primary-600')}>
                  <Icon name="refresh" size="sm" />
                  {t('admin.accounts.resetQuota')}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>,
    document.body
  )
}

export default AccountActionMenu
