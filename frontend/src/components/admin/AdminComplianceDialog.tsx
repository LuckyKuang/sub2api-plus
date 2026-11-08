import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { Modal } from '@/components/ui'
import { Input } from '@/components/ui'
import { Icon } from '@/components/Icon'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { getLocale } from '@/i18n'
import zhDocument from '../../../../docs/legal/admin-compliance.zh.md?raw'
import enDocument from '../../../../docs/legal/admin-compliance.en.md?raw'

marked.setOptions({ breaks: true, gfm: true })

/**
 * Blocking acknowledgement every administrator must complete before using the
 * console. Mirrors the Vue AdminComplianceDialog: it cannot be dismissed, so
 * the only ways out are accepting or logging out.
 */
export function AdminComplianceDialog() {
  const { t } = useTranslation()
  const complianceStore = useAdminComplianceStore()
  const appStore = useAppStore()
  const authStore = useAuthStore()

  const [typedPhrase, setTypedPhrase] = useState('')
  const [attemptedSubmit, setAttemptedSubmit] = useState(false)

  const status = complianceStore.status
  const visible = authStore.isAuthenticated() && authStore.isAdmin() && complianceStore.shouldShow()
  const expectedPhrase = complianceStore.expectedPhrase()
  const canSubmit = typedPhrase.trim() === expectedPhrase

  useEffect(() => {
    if (visible) {
      setTypedPhrase('')
      setAttemptedSubmit(false)
    }
  }, [visible])

  useEffect(() => {
    setTypedPhrase('')
    setAttemptedSubmit(false)
  }, [expectedPhrase])

  const currentDocument = getLocale() === 'zh' ? zhDocument : enDocument
  const renderedDocument = useMemo(
    () => DOMPurify.sanitize(marked.parse(currentDocument) as string),
    [currentDocument]
  )

  const documentUrl =
    getLocale() === 'zh'
      ? status?.document_url_zh ||
        'https://github.com/luckykuang/sub2api-plus/blob/main/docs/legal/admin-compliance.zh.md'
      : status?.document_url_en ||
        'https://github.com/luckykuang/sub2api-plus/blob/main/docs/legal/admin-compliance.en.md'

  const inputError = !attemptedSubmit || canSubmit ? '' : t('adminCompliance.inputMismatch')

  async function submit() {
    setAttemptedSubmit(true)
    if (!canSubmit) return
    try {
      const next = await complianceStore.accept(typedPhrase.trim())
      if (!next.required) {
        appStore.showSuccess(t('adminCompliance.accepted'))
        setTypedPhrase('')
        setAttemptedSubmit(false)
      }
    } catch (error) {
      const message = (error as { message?: string })?.message || t('adminCompliance.acceptFailed')
      appStore.showError(message)
    }
  }

  async function logout() {
    await authStore.logout()
    window.location.href = '/login'
  }

  return (
    <Modal
      open={visible}
      title={t('adminCompliance.title')}
      size="lg"
      showClose={false}
      onClose={() => {}}
      footer={
        <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
          <button type="button" className="btn btn-secondary" disabled={complianceStore.submitting} onClick={logout}>
            {t('adminCompliance.logout')}
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!canSubmit || complianceStore.submitting}
            onClick={submit}
          >
            {complianceStore.submitting ? t('common.submitting') : t('adminCompliance.accept')}
          </button>
        </div>
      }
    >
      <div className="space-y-5">
        <div className="rounded-lg border border-amber-200 bg-amber-50/[0.15] p-4 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100">
          <div className="flex gap-3">
            <Icon name="exclamationTriangle" size="md" className="mt-0.5 flex-shrink-0" />
            <div className="space-y-2">
              <p className="font-semibold">{t('adminCompliance.blockingNotice')}</p>
              <p className="leading-6">{t('adminCompliance.riskNotice')}</p>
            </div>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_240px]">
          <section className="min-h-[320px] max-h-[46vh] overflow-y-auto rounded-lg border border-gray-200 surface-inset p-5 dark:border-dark-700">
            {/* The document is a repository-authored legal text, sanitised before
                it reaches the DOM. */}
            <div className="legal-document-content" dangerouslySetInnerHTML={{ __html: renderedDocument }} />
          </section>

          <aside className="space-y-3 rounded-lg border border-gray-200 surface-inset p-4 text-sm dark:border-dark-700">
            <div>
              <p className="text-xs font-medium uppercase tracking-wide surface-muted">{t('adminCompliance.version')}</p>
              <p className="mt-1 break-all font-mono text-gray-900 dark:text-white">
                {status?.version || 'v2026.07.25-plus.1'}
              </p>
            </div>
            <a
              href={documentUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2 text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300 dark:hover:text-primary-200"
            >
              <Icon name="externalLink" size="sm" />
              {t('adminCompliance.openDocument')}
            </a>
            <p className="leading-6 surface-muted">{t('adminCompliance.documentSource')}</p>
          </aside>
        </div>

        <div className="space-y-3">
          <label htmlFor="admin-compliance-phrase" className="block text-sm font-semibold text-gray-900 dark:text-white">
            {t('adminCompliance.inputLabel')}
          </label>
          <div className="rounded-lg surface-inset px-3 py-2 font-mono text-sm text-gray-900 dark:text-dark-100">
            {expectedPhrase}
          </div>
          <Input
            id="admin-compliance-phrase"
            value={typedPhrase}
            onValueChange={setTypedPhrase}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void submit()
            }}
            placeholder={t('adminCompliance.inputPlaceholder')}
            autoComplete="off"
            disabled={complianceStore.submitting}
            error={inputError}
          />
        </div>

        <p className="text-xs leading-5 surface-muted">{t('adminCompliance.legalNote')}</p>
      </div>
    </Modal>
  )
}

export default AdminComplianceDialog
