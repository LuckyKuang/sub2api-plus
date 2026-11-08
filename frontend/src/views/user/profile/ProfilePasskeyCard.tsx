import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { passkeyAPI, type PasskeyCredentialSummary } from '@/api/passkey'
import { Modal, toast } from '@/components/ui'
import { Icon } from '../icons'

export interface ProfilePasskeyCardProps {
  enabled: boolean
  className?: string
}

export function ProfilePasskeyCard({ enabled, className }: ProfilePasskeyCardProps) {
  const { t } = useTranslation()
  const supported = passkeyAPI.isSupported()
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [showAddForm, setShowAddForm] = useState(false)
  const [newName, setNewName] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<PasskeyCredentialSummary | null>(null)
  const [deletePassword, setDeletePassword] = useState('')
  const [credentials, setCredentials] = useState<PasskeyCredentialSummary[]>([])

  // apiClient 拦截器把错误规范化为 { code, reason, message }；
  // 透出后端消息（如密码错误），否则回退到通用文案。
  function extractErrorMessage(error: unknown, fallback: string): string {
    const message = (error as { message?: string }).message
    return typeof message === 'string' && message.length > 0 ? message : fallback
  }

  async function loadCredentials(): Promise<void> {
    if (!enabled) {
      setCredentials([])
      return
    }
    setLoading(true)
    try {
      setCredentials(await passkeyAPI.list())
    } catch (error) {
      // 字符串错误码在 reason 字段（code 是数字状态码）；
      // 设置变更竞态下后端仍可能返回 PASSKEY_DISABLED，静默处理
      const reason = (error as { reason?: string }).reason
      if (reason !== 'PASSKEY_DISABLED') {
        toast.error(t('profile.passkey.loadFailed'))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadCredentials()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled])

  async function addPasskey(): Promise<void> {
    if (newPassword.length === 0) return
    setBusy(true)
    try {
      await passkeyAPI.register(newName.trim(), newPassword)
      toast.success(t('profile.passkey.added'))
      cancelAdd()
      await loadCredentials()
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'NotAllowedError')) {
        toast.error(extractErrorMessage(error, t('profile.passkey.addFailed')))
      }
    } finally {
      setBusy(false)
    }
  }

  function cancelAdd(): void {
    setShowAddForm(false)
    setNewName('')
    setNewPassword('')
  }

  async function renamePasskey(credential: PasskeyCredentialSummary): Promise<void> {
    const name = window.prompt(t('profile.passkey.renamePrompt'), credential.name)?.trim()
    if (!name || name === credential.name) return
    setBusy(true)
    try {
      await passkeyAPI.rename(credential.id, name)
      setCredentials(credentials.map((item) => (item.id === credential.id ? { ...item, name } : item)))
      toast.success(t('profile.passkey.renamed'))
    } catch {
      toast.error(t('profile.passkey.renameFailed'))
    } finally {
      setBusy(false)
    }
  }

  function deletePasskey(credential: PasskeyCredentialSummary): void {
    setDeleteTarget(credential)
    setDeletePassword('')
  }

  function closeDeleteDialog(): void {
    setDeleteTarget(null)
    setDeletePassword('')
  }

  async function confirmDelete(): Promise<void> {
    const credential = deleteTarget
    if (!credential || deletePassword.length === 0) return
    setBusy(true)
    try {
      await passkeyAPI.remove(credential.id, deletePassword)
      setCredentials(credentials.filter((item) => item.id !== credential.id))
      toast.success(t('profile.passkey.deleted'))
      closeDeleteDialog()
    } catch (error) {
      // 密码错误等失败保持对话框打开，允许重试
      toast.error(extractErrorMessage(error, t('profile.passkey.deleteFailed')))
    } finally {
      setBusy(false)
    }
  }

  function formatDate(value: string): string {
    return new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' }).format(
      new Date(value)
    )
  }

  return (
    <div className={`card ${className ?? ''}`}>
      <div className="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-lg font-medium text-gray-900 dark:text-white">{t('profile.passkey.title')}</h2>
          <p className="mt-1 text-sm surface-muted">{t('profile.passkey.description')}</p>
        </div>
        {enabled && supported && !showAddForm && (
          <button
            type="button"
            className="btn btn-primary w-full shrink-0 sm:w-auto"
            disabled={busy}
            onClick={() => setShowAddForm(true)}
          >
            {t('profile.passkey.add')}
          </button>
        )}
      </div>

      <div className="px-6 py-6">
        {!enabled && <div className="mb-5 text-sm surface-muted">{t('profile.passkey.featureDisabled')}</div>}
        {enabled && !supported && (
          <div className="mb-5 text-sm text-amber-600 dark:text-amber-400">{t('profile.passkey.unsupported')}</div>
        )}
        <div>
          {enabled && supported && showAddForm && (
            <form
              onSubmit={(event) => {
                event.preventDefault()
                void addPasskey()
              }}
              className="mb-5 flex flex-col gap-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700"
            >
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <label htmlFor="passkey-name" className="input-label">
                    {t('profile.passkey.name')}
                  </label>
                  <input
                    id="passkey-name"
                    value={newName}
                    onChange={(event) => setNewName(event.target.value)}
                    className="input"
                    maxLength={100}
                    placeholder={t('profile.passkey.namePlaceholder')}
                    autoFocus
                  />
                </div>
                <div>
                  <label htmlFor="passkey-add-password" className="input-label">
                    {t('profile.currentPassword')}
                  </label>
                  <input
                    id="passkey-add-password"
                    value={newPassword}
                    onChange={(event) => setNewPassword(event.target.value)}
                    type="password"
                    autoComplete="current-password"
                    className="input"
                    placeholder={t('profile.passkey.passwordPlaceholder')}
                  />
                </div>
              </div>
              <div className="flex justify-end gap-2">
                <button type="button" className="btn btn-secondary" disabled={busy} onClick={cancelAdd}>
                  {t('common.cancel')}
                </button>
                <button type="submit" className="btn btn-primary" disabled={busy || newPassword.length === 0}>
                  {busy ? t('common.processing') : t('profile.passkey.continue')}
                </button>
              </div>
            </form>
          )}

          {loading ? (
            <div className="flex justify-center py-6">
              <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-500" />
            </div>
          ) : credentials.length === 0 ? (
            <div className="surface-muted rounded-lg border border-dashed border-gray-200 px-4 py-8 text-center text-sm dark:border-dark-700">
              {t('profile.passkey.empty')}
            </div>
          ) : (
            <div className="divide-y divide-gray-100 dark:divide-dark-700">
              {credentials.map((credential) => (
                <div
                  key={credential.id}
                  className="flex flex-col gap-4 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <Icon name="key" size="md" className="shrink-0 text-primary-500" />
                      <p className="min-w-0 flex-1 truncate font-medium text-gray-900 dark:text-white">
                        {credential.name}
                      </p>
                      {credential.backup && (
                        <span className="shrink-0 whitespace-nowrap rounded-full bg-green-50/[0.15] px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/30 dark:text-green-300">
                          {t('profile.passkey.synced')}
                        </span>
                      )}
                    </div>
                    <p className="mt-1 text-xs surface-muted">
                      {t('profile.passkey.createdAt', { date: formatDate(credential.created_at) })}
                      {credential.last_used_at &&
                        ` · ${t('profile.passkey.lastUsed', { date: formatDate(credential.last_used_at) })}`}
                    </p>
                  </div>
                  <div className="flex shrink-0 justify-end gap-2">
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      disabled={busy}
                      onClick={() => void renamePasskey(credential)}
                    >
                      {t('common.edit')}
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm text-red-600 hover:bg-red-50/25 dark:text-red-300 dark:hover:bg-red-950/30"
                      disabled={busy}
                      onClick={() => deletePasskey(credential)}
                    >
                      {t('common.delete')}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 删除确认：吊销凭据需验证当前密码，防止被窃会话静默移除 Passkey */}
      <Modal
        open={!!deleteTarget}
        onClose={() => {
          if (!busy) closeDeleteDialog()
        }}
        title={t('profile.passkey.deleteTitle')}
        size="sm"
      >
        <p className="break-words text-sm surface-muted">
          {t('profile.passkey.deleteConfirm', { name: deleteTarget?.name ?? '' })}
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void confirmDelete()
          }}
          className="mt-4 space-y-4"
        >
          <div>
            <label htmlFor="passkey-delete-password" className="input-label">
              {t('profile.currentPassword')}
            </label>
            <input
              id="passkey-delete-password"
              value={deletePassword}
              onChange={(event) => setDeletePassword(event.target.value)}
              type="password"
              autoComplete="current-password"
              className="input"
              placeholder={t('profile.passkey.passwordPlaceholder')}
              autoFocus
            />
          </div>
          <div className="flex justify-end gap-3">
            <button type="button" className="btn btn-secondary" disabled={busy} onClick={closeDeleteDialog}>
              {t('common.cancel')}
            </button>
            <button type="submit" className="btn btn-danger" disabled={busy || deletePassword.length === 0}>
              {busy ? t('common.processing') : t('common.delete')}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}

export default ProfilePasskeyCard
