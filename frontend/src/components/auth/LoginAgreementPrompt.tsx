import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from './Icon'
import type { LoginAgreementDocument } from '@/types'

export type LoginAgreementMode = 'modal' | 'checkbox' | string

export interface LoginAgreementPromptProps {
  accepted: boolean
  documents: LoginAgreementDocument[]
  mode: LoginAgreementMode
  updatedAt?: string
  visible: boolean
  onAccept?: () => void
  onReject?: () => void
  onOpen?: () => void
}

function documentRoute(doc: LoginAgreementDocument): string {
  return `/legal/${doc.id || doc.title}`
}

function documentIcon(index: number, title: string): 'document' | 'shield' | 'globe' | 'cog' {
  const normalizedTitle = title.toLowerCase()
  if (
    normalizedTitle.includes('policy') ||
    normalizedTitle.includes('privacy') ||
    title.includes('政策') ||
    title.includes('隐私')
  ) {
    return 'shield'
  }
  if (
    normalizedTitle.includes('country') ||
    normalizedTitle.includes('region') ||
    title.includes('国家') ||
    title.includes('地区')
  ) {
    return 'globe'
  }
  if (index === 3) {
    return 'cog'
  }
  return 'document'
}

export function LoginAgreementPrompt({
  accepted,
  documents,
  mode,
  updatedAt = '',
  visible,
  onAccept,
  onReject,
  onOpen
}: LoginAgreementPromptProps) {
  const { t } = useTranslation()

  const visibleDocuments = documents.filter((doc) => doc.title.trim())
  const isCheckboxMode = mode === 'checkbox'
  const dialogVisible = visible && visibleDocuments.length > 0

  if (visibleDocuments.length === 0) {
    return null
  }

  const handleCheckboxChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      onAccept?.()
    } else {
      onReject?.()
    }
  }

  return (
    <>
      {isCheckboxMode ? (
        <div className="px-0.5">
          <div className="flex items-start gap-2">
            <input
              id="login-agreement-consent"
              type="checkbox"
              checked={accepted}
              className="mt-[2px] h-4 w-4 flex-shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-900"
              onChange={handleCheckboxChange}
            />
            <div className="min-w-0 flex-1">
              <p className="text-[13px] leading-5 surface-muted">
                <label htmlFor="login-agreement-consent" className="cursor-pointer surface-muted">
                  {t('legal.loginAgreementPrompt.checkboxPrefix')}
                </label>
                {visibleDocuments.map((doc, index) => (
                  <span key={doc.id || doc.title}>
                    <Link
                      to={documentRoute(doc)}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="font-medium text-primary-600 underline-offset-4 transition hover:text-primary-700 hover:underline dark:text-primary-300 dark:hover:text-primary-200"
                    >
                      {doc.title}
                    </Link>
                    {index < visibleDocuments.length - 1 && t('legal.loginAgreementPrompt.documentSeparator')}
                  </span>
                ))}
              </p>
            </div>
          </div>
        </div>
      ) : (
        !accepted && (
          <div className="rounded-lg border border-primary-100 bg-primary-50/70 p-3 text-sm text-primary-900 dark:border-primary-500/20 dark:bg-primary-500/10 dark:text-primary-100">
            <div className="flex items-start gap-3">
              <Icon name="shield" size="sm" className="mt-0.5 flex-shrink-0 text-primary-600 dark:text-primary-300" />
              <div className="min-w-0 flex-1">
                <p className="font-medium">{t('legal.loginAgreementPrompt.noticeTitle')}</p>
                <p className="mt-1 text-primary-700 dark:text-primary-200/80">
                  {t('legal.loginAgreementPrompt.noticeDescription')}
                </p>
              </div>
              <button
                type="button"
                className="flex-shrink-0 rounded-md bg-primary-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-primary-700"
                onClick={() => onOpen?.()}
              >
                {t('legal.loginAgreementPrompt.viewTerms')}
              </button>
            </div>
          </div>
        )
      )}

      {dialogVisible &&
        createPortal(
          <div className="fixed inset-0 z-[140] flex items-center justify-center overflow-y-auto surface-overlay p-4">
            <div className="glass-surface w-full max-w-[600px] overflow-hidden ring-1 ring-black/10 dark:ring-white/10">
              <div className="border-b border-gray-100 surface-inset px-6 py-6 dark:border-dark-800">
                <div className="flex items-start gap-4">
                  <span className="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-primary-50/[0.15] text-primary-700 ring-1 ring-primary-100 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-500/20">
                    <Icon name="shield" size="md" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="text-xl font-bold tracking-normal text-gray-950 dark:text-white">
                        {t('legal.loginAgreementPrompt.dialogTitle')}
                      </h2>
                      {updatedAt && (
                        <span className="rounded-full surface-inset px-2.5 py-1 text-xs font-medium surface-muted">
                          {updatedAt}
                        </span>
                      )}
                    </div>
                    <p className="mt-2 text-sm leading-6 surface-muted">
                      {t('legal.loginAgreementPrompt.dialogDescription', {
                        date: updatedAt || t('legal.loginAgreementPrompt.recently')
                      })}
                    </p>
                  </div>
                </div>
              </div>

              <div className="max-h-[58vh] overflow-y-auto px-6 py-5">
                <div className="mb-3 flex items-center justify-between gap-3">
                  <p className="text-sm font-semibold text-gray-900 dark:text-white">
                    {t('legal.loginAgreementPrompt.relatedDocuments')}
                  </p>
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {visibleDocuments.map((doc, index) => (
                    <Link
                      key={doc.id || doc.title}
                      to={documentRoute(doc)}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="group flex min-h-[72px] w-full items-center gap-3 rounded-xl border border-gray-200 bg-gray-50/70 px-4 py-3 text-left transition hover:-translate-y-0.5 hover:border-primary-200 hover:bg-white hover:shadow-sm dark:border-dark-700 dark:bg-dark-800/70 dark:hover:border-primary-500/30 dark:hover:bg-dark-800"
                    >
                      <span className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg surface-inset ring-1 ring-gray-200 transition group-hover:bg-primary-50/25 group-hover:text-primary-700 group-hover:ring-primary-100 dark:ring-dark-700 dark:group-hover:bg-primary-500/10 dark:group-hover:text-primary-200 dark:group-hover:ring-primary-500/20 surface-muted">
                        <Icon name={documentIcon(index, doc.title)} size="sm" />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-semibold text-gray-950 dark:text-white">
                          {doc.title}
                        </span>
                      </span>
                      <span className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full transition group-hover:bg-primary-50/25 group-hover:text-primary-600 dark:group-hover:bg-primary-500/10 dark:group-hover:text-primary-300 surface-muted">
                        <Icon name="externalLink" size="sm" />
                      </span>
                    </Link>
                  ))}
                </div>
              </div>

              <div className="border-t border-gray-100 surface-inset px-6 py-4 dark:border-dark-800">
                <div className="grid grid-cols-2 gap-3">
                  <button
                    type="button"
                    className="rounded-xl border border-gray-200 px-4 py-3 text-sm font-semibold transition hover:surface-hover dark:border-dark-700 surface-control surface-muted"
                    onClick={() => onReject?.()}
                  >
                    {t('legal.loginAgreementPrompt.reject')}
                  </button>
                  <button
                    type="button"
                    className="rounded-xl bg-primary-600 px-4 py-3 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
                    onClick={() => onAccept?.()}
                  >
                    {t('legal.loginAgreementPrompt.accept')}
                  </button>
                </div>
              </div>
            </div>
          </div>,
          document.body
        )}
    </>
  )
}

export default LoginAgreementPrompt
