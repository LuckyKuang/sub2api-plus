import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Select, type SelectOption } from '@/components/ui'
import { Icon } from '@/components/auth/Icon'
import { TotpStepUpDialog } from '@/components/auth/TotpStepUpDialog'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/components/auth/useStepUp'
import { mcpAPI, type MCPAccessScope, type MCPAuthorization, type MCPConsent } from '@/api/mcp'
import { useAuthStore } from '@/stores/auth'
import '@/views/authViews.css'

export default function MCPOAuthAuthorizeView() {
  const { t } = useTranslation()
  const location = useLocation()
  const authStore = useAuthStore()
  const stepUp = useStepUp()

  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [submitError, setSubmitError] = useState('')
  const [consent, setConsent] = useState<MCPConsent | null>(null)
  const [scope, setScope] = useState<MCPAccessScope>('user')
  const [readOnly, setReadOnly] = useState(true)

  const isAdmin = authStore.user?.role === 'admin'
  const authorizationRef = useRef<MCPAuthorization | null>(null)
  const generationRef = useRef(0)

  const scopeOptions = useMemo<SelectOption[]>(
    () => [
      { value: 'user', label: t('mcp.userScope') },
      ...(isAdmin ? [{ value: 'admin', label: t('mcp.adminScope') }] : [])
    ],
    [isAdmin, t]
  )

  const redirectOrigin = useMemo(() => {
    if (!consent) return ''
    try {
      return new URL(consent.redirect_uri).origin
    } catch {
      return ''
    }
  }, [consent])

  const errorMessage = useCallback((error: unknown, fallback: string): string => {
    const message = (error as { message?: unknown } | null)?.message
    return typeof message === 'string' && message ? message : fallback
  }, [])

  const parseAuthorization = useCallback((): MCPAuthorization => {
    const required = [
      'response_type',
      'client_id',
      'redirect_uri',
      'code_challenge',
      'code_challenge_method',
      'resource'
    ] as const
    const allowed = [...required, 'scope', 'state']
    const values: Record<string, string> = {}
    const params = new URLSearchParams(location.search)
    for (const [key, value] of params.entries()) {
      if (!allowed.includes(key as (typeof allowed)[number]) || typeof value !== 'string' || value.length > 4096) {
        throw new Error(t('mcp.invalidRequest'))
      }
      values[key] = value
    }
    if (
      required.some((key) => !values[key]) ||
      values.response_type !== 'code' ||
      values.code_challenge_method !== 'S256' ||
      !/^[A-Za-z0-9_-]{43}$/.test(values.code_challenge) ||
      (values.scope !== undefined && values.scope !== '' && values.scope !== 'mcp')
    ) {
      throw new Error(t('mcp.invalidRequest'))
    }
    return values as unknown as MCPAuthorization
  }, [location.search, t])

  const validatePreview = useCallback((result: MCPConsent, request: MCPAuthorization): void => {
    if (
      result.client_id !== request.client_id ||
      result.redirect_uri !== request.redirect_uri ||
      result.resource !== request.resource ||
      result.scope !== 'mcp' ||
      typeof result.client_name !== 'string'
    ) {
      throw new Error(t('mcp.invalidRequest'))
    }
    const redirect = new URL(result.redirect_uri)
    if (!['https:', 'http:'].includes(redirect.protocol) || redirect.username || redirect.password || redirect.hash) {
      throw new Error(t('mcp.invalidRequest'))
    }
  }, [t])

  const loadConsent = useCallback(async () => {
    const current = ++generationRef.current
    stepUp.onCancel()
    setLoading(true)
    setSubmitting(false)
    setPreviewError('')
    setSubmitError('')
    setConsent(null)
    authorizationRef.current = null
    setScope('user')
    setReadOnly(true)
    try {
      const request = parseAuthorization()
      const result = await mcpAPI.previewAuthorization({ ...request })
      if (current !== generationRef.current) return
      validatePreview(result, request)
      authorizationRef.current = Object.freeze({ ...request })
      setConsent(result)
    } catch (error) {
      if (current === generationRef.current) {
        setPreviewError(errorMessage(error, t('mcp.consentLoadFailed')))
      }
    } finally {
      if (current === generationRef.current) setLoading(false)
    }
  }, [errorMessage, parseAuthorization, stepUp, t, validatePreview])

  useEffect(() => {
    void loadConsent()
  }, [loadConsent])

  // A changed route or unmounted page must not retry a previous consent.
  useEffect(
    () => () => {
      generationRef.current += 1
      authorizationRef.current = null
      stepUp.onCancel()
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    []
  )

  const decide = useCallback(
    async (approved: boolean) => {
      if (loading || submitting || !consent || !authorizationRef.current) return
      const current = generationRef.current
      const expectedRedirect = new URL(consent.redirect_uri)
      const input = {
        authorization: { ...authorizationRef.current },
        approved,
        scope: approved && isAdmin ? scope : ('user' as const),
        read_only: approved ? readOnly : true
      }
      setSubmitting(true)
      setSubmitError('')
      try {
        const action = async () => {
          if (current !== generationRef.current) throw new Error(t('mcp.invalidRequest'))
          try {
            return await mcpAPI.authorize(input)
          } catch (error) {
            // Do not open a step-up prompt for a request that is no longer shown.
            if (current !== generationRef.current) throw new Error(t('mcp.invalidRequest'))
            throw error
          }
        }
        const result = approved && input.scope === 'admin' ? await stepUp.run(action) : await action()
        if (current !== generationRef.current) return
        const destination = new URL(result.redirect_url)
        if (
          destination.origin !== expectedRedirect.origin ||
          destination.pathname !== expectedRedirect.pathname ||
          destination.username ||
          destination.password ||
          destination.hash
        ) {
          throw new Error(t('mcp.invalidRedirect'))
        }
        window.location.assign(result.redirect_url)
      } catch (error) {
        if (current !== generationRef.current) return
        if (!isStepUpCancelled(error)) {
          setSubmitError(
            isStepUpBlocked(error)
              ? stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
                ? t('stepUp.adminApiKeyForbidden')
                : t('stepUp.notEnabled')
              : errorMessage(error, t('mcp.consentSubmitFailed'))
          )
        }
        setSubmitting(false)
      }
    },
    [consent, errorMessage, isAdmin, loading, readOnly, scope, stepUp, submitting, t]
  )

  return (
    <div className="min-h-screen surface-inset px-4 py-10">
      <div className="mx-auto max-w-2xl">
        <div
          className="card p-6"
          aria-labelledby="mcp-consent-title"
          aria-busy={loading || submitting}
        >
          <div className="mb-6 flex items-start gap-3">
            <div className="rounded-xl bg-primary-50 p-3 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
              <Icon name="mcp" size="lg" />
            </div>
            <div>
              <h1 id="mcp-consent-title" className="text-xl font-semibold text-gray-900 dark:text-white">
                {t('mcp.consentTitle')}
              </h1>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{t('mcp.consentDescription')}</p>
            </div>
          </div>

          {loading && (
            <p role="status" className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
              {t('mcp.consentLoading')}
            </p>
          )}

          {!loading && previewError && (
            <div className="space-y-4">
              <p role="alert" className="text-sm text-red-600 dark:text-red-400">
                {previewError}
              </p>
              <p className="text-sm text-gray-500 dark:text-gray-400">{t('mcp.consentRetryHint')}</p>
              <Link to="/settings/mcp" className="btn btn-secondary">
                {t('mcp.backToConnections')}
              </Link>
            </div>
          )}

          {!loading && !previewError && consent && (
            <form
              className="space-y-6"
              onSubmit={(event) => {
                event.preventDefault()
                void decide(true)
              }}
            >
              <div className="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
                <dl className="space-y-4 text-sm">
                  <div>
                    <dt className="text-gray-500 dark:text-gray-400">{t('mcp.applicationName')}</dt>
                    <dd data-testid="client-name" className="mt-1 break-all text-lg font-semibold text-gray-900 dark:text-white">
                      {consent.client_name}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-gray-500 dark:text-gray-400">{t('mcp.clientId')}</dt>
                    <dd className="mt-1 truncate font-mono text-xs text-gray-700 dark:text-gray-200" title={consent.client_id}>
                      {consent.client_id}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-gray-500 dark:text-gray-400">{t('mcp.returnAddress')}</dt>
                    <dd data-testid="redirect-origin" className="mt-1 break-all font-semibold text-gray-900 dark:text-white">
                      {redirectOrigin}
                    </dd>
                    <dd data-testid="redirect-uri" className="mt-1 break-all font-mono text-xs text-gray-600 dark:text-gray-300">
                      {consent.redirect_uri}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-gray-500 dark:text-gray-400">{t('mcp.requestedAccess')}</dt>
                    <dd className="mt-1 text-gray-700 dark:text-gray-200">{t('mcp.requestedAccessDescription')}</dd>
                    <dd className="mt-1 break-all font-mono text-xs text-gray-600 dark:text-gray-300">{consent.resource}</dd>
                  </div>
                </dl>
                <p className="mt-4 border-t border-gray-200 pt-4 text-sm text-amber-800 dark:border-dark-700 dark:text-amber-300">
                  {t('mcp.untrustedApplicationHint')}
                </p>
              </div>

              {authStore.user?.email && (
                <p className="break-all text-sm text-gray-600 dark:text-gray-300">
                  {t('mcp.signedInAs', { email: authStore.user.email })}
                </p>
              )}

              <div>
                <label htmlFor="mcp-consent-scope" className="input-label">
                  {t('mcp.scope')}
                </label>
                <Select
                  id="mcp-consent-scope"
                  value={scope}
                  options={scopeOptions}
                  searchable={false}
                  disabled={submitting}
                  ariaDescribedby="mcp-scope-hint"
                  onValueChange={(value) => setScope(value === 'admin' ? 'admin' : 'user')}
                />
                <p id="mcp-scope-hint" className="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {scope === 'admin' ? t('mcp.adminScopeHint') : t('mcp.userScopeHint')}
                </p>
              </div>

              <div>
                <label htmlFor="mcp-consent-permissions" className="input-label">
                  {t('mcp.permissions')}
                </label>
                <Select
                  id="mcp-consent-permissions"
                  value={readOnly}
                  options={[
                    { value: true, label: t('mcp.readOnly') },
                    { value: false, label: t('mcp.readWrite') }
                  ]}
                  searchable={false}
                  disabled={submitting}
                  ariaDescribedby="mcp-permissions-hint"
                  onValueChange={(value) => setReadOnly(value !== false)}
                />
                <p
                  id="mcp-permissions-hint"
                  className={`mt-2 text-xs ${readOnly ? 'text-gray-500 dark:text-gray-400' : 'text-amber-700 dark:text-amber-400'}`}
                >
                  {readOnly ? t('mcp.readOnlyHint') : t('mcp.readWriteHint')}
                </p>
                {scope === 'admin' && !readOnly && (
                  <p data-testid="admin-write-hint" className="mt-1 text-xs text-amber-700 dark:text-amber-400">
                    {t('mcp.adminWriteHint')}
                  </p>
                )}
              </div>

              {submitError && (
                <p role="alert" className="text-sm text-red-600 dark:text-red-400">
                  {submitError}
                </p>
              )}

              <div className="flex justify-end gap-3 border-t border-gray-100 pt-5 dark:border-dark-700">
                <button
                  data-testid="deny"
                  type="button"
                  className="btn btn-secondary"
                  disabled={submitting}
                  onClick={() => void decide(false)}
                >
                  {t('mcp.deny')}
                </button>
                <button data-testid="approve" type="submit" className="btn btn-primary" disabled={submitting}>
                  {submitting ? t('common.processing') : t('mcp.approve')}
                </button>
              </div>
            </form>
          )}
        </div>

        <TotpStepUpDialog controller={stepUp} />
      </div>
    </div>
  )
}
