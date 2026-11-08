import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { Icon } from '@/components/auth/Icon'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { useAppStore } from '@/stores/app'
import { getLocale } from '@/i18n'
import { sanitizeUrl } from '@/utils/url'
import type { LoginAgreementDocument } from '@/types'
import zhAdminCompliance from '../../../../docs/legal/admin-compliance.zh.md?raw'
import enAdminCompliance from '../../../../docs/legal/admin-compliance.en.md?raw'
import '@/views/authViews.css'

marked.setOptions({
  breaks: true,
  gfm: true
})

type LegalDocumentIcon = 'document' | 'shield' | 'globe' | 'cog'

export default function LegalDocumentView() {
  const { t } = useTranslation()
  const { documentId } = useParams<{ documentId: string }>()
  const appStore = useAppStore()

  useProductAppearance()

  const settings = appStore.cachedPublicSettings
  const [loading, setLoading] = useState(!settings)
  const [loadError, setLoadError] = useState(false)

  const isAdminComplianceDocument = documentId === 'admin-compliance'
  const documents = settings?.login_agreement_documents ?? []
  const siteName = settings?.site_name || 'Sub2API Plus'
  const siteLogo = sanitizeUrl(settings?.site_logo || '', {
    allowRelative: true,
    allowDataUrl: true
  })
  const updatedAt = isAdminComplianceDocument ? '' : settings?.login_agreement_updated_at || ''
  const documentTypeLabel = isAdminComplianceDocument ? t('legal.adminCompliance') : t('legal.loginAgreement')

  const currentDocument = useMemo<LoginAgreementDocument | null>(() => {
    if (isAdminComplianceDocument) {
      return {
        id: 'admin-compliance',
        title: t('adminCompliance.title'),
        content_md: getLocale() === 'zh' ? zhAdminCompliance : enAdminCompliance
      }
    }
    const id = documentId || ''
    if (!id) {
      return null
    }
    return documents.find((doc) => doc.id === id) ?? null
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [documentId, isAdminComplianceDocument, settings, t])

  const hasContent = Boolean(currentDocument?.content_md?.trim())

  const renderedHtml = useMemo(() => {
    const content = currentDocument?.content_md?.trim() || ''
    if (!content) {
      return ''
    }
    const html = marked.parse(content) as string
    return DOMPurify.sanitize(html)
  }, [currentDocument])

  const documentIcon = useMemo<LegalDocumentIcon>(() => {
    const title = currentDocument?.title || ''
    if (title.includes('政策') || title.includes('隐私')) {
      return 'shield'
    }
    if (title.includes('国家') || title.includes('地区')) {
      return 'globe'
    }
    if (title.includes('特定')) {
      return 'cog'
    }
    return 'document'
  }, [currentDocument])

  useEffect(() => {
    let cancelled = false
    setLoadError(false)
    void (async () => {
      const loadedSettings = await appStore.fetchPublicSettings()
      if (cancelled) return
      if (!loadedSettings) {
        setLoadError(true)
      }
      setLoading(false)
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="flex min-h-screen flex-col gap-[var(--shell-gap)] bg-canvas p-[var(--shell-gutter)] text-gray-900 dark:surface-inset dark:text-white">
      <header className="shell-surface">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
          <Link to="/home" className="flex min-w-0 items-center gap-3">
            {settings ? (
              <>
                <span className="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl surface-inset shadow-sm ring-1 ring-gray-200 dark:ring-dark-700">
                  <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
                </span>
                <span className="truncate text-base font-semibold text-gray-950 dark:text-white">{siteName}</span>
              </>
            ) : (
              <>
                <span className="h-10 w-10 flex-shrink-0 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" aria-hidden="true" />
                <span className="h-5 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700" aria-hidden="true" />
              </>
            )}
          </Link>
          <Link
            to="/login"
            className="inline-flex flex-shrink-0 items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
          >
            {t('home.login')}
          </Link>
        </div>
      </header>

      <main className="mx-auto w-full min-w-0 max-w-4xl">
        {loading && (
          <div className="card flex min-h-[320px] items-center justify-center">
            <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
          </div>
        )}

        {!loading && loadError && (
          <section className="card border-red-200 p-6 text-red-700 dark:border-red-500/30 dark:text-red-200">
            <h1 className="text-lg font-semibold">{t('legal.loadFailed')}</h1>
            <p className="mt-2 text-sm">{t('legal.retryLater')}</p>
          </section>
        )}

        {!loading && !loadError && !currentDocument && (
          <section className="card p-6">
            <div className="flex items-start gap-3">
              <span className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md surface-inset surface-muted">
                <Icon name="document" size="sm" />
              </span>
              <div>
                <h1 className="text-lg font-semibold text-gray-900 dark:text-white">{t('legal.notFound')}</h1>
                <p className="mt-2 text-sm leading-6 surface-muted">{t('legal.notFoundDescription')}</p>
              </div>
            </div>
          </section>
        )}

        {!loading && !loadError && currentDocument && (
          <article className="card p-5 sm:p-8">
            <div className="mb-8 border-b border-gray-200 pb-6 dark:border-dark-700">
              <div className="flex items-start gap-4">
                <span className="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-md bg-primary-50/[0.15] text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">
                  <Icon name={documentIcon} size="md" />
                </span>
                <div className="min-w-0">
                  <p className="text-sm font-medium text-primary-700 dark:text-primary-300">{documentTypeLabel}</p>
                  <h1 className="mt-2 break-words text-2xl font-bold tracking-normal text-gray-950 dark:text-white sm:text-3xl">
                    {currentDocument.title}
                  </h1>
                  {updatedAt && (
                    <p className="mt-3 text-sm surface-muted">{t('legal.updatedAt', { date: updatedAt })}</p>
                  )}
                </div>
              </div>
            </div>

            {hasContent ? (
              <div className="legal-document-content" dangerouslySetInnerHTML={{ __html: renderedHtml }} />
            ) : (
              <div className="rounded-lg border border-dashed border-gray-300 surface-inset px-6 py-14 text-center text-sm dark:border-dark-700 surface-muted">
                {t('legal.empty')}
              </div>
            )}
          </article>
        )}
      </main>
    </div>
  )
}
