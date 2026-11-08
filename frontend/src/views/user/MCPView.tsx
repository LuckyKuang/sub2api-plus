import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { mcpAPI, type MCPGrant } from '@/api/mcp'
import { Button, Modal, toast } from '@/components/ui'
import { formatDate } from '@/utils/format'
import { Icon } from './icons'
import { useClipboard } from './useClipboard'

export default function MCPView() {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [endpoint, setEndpoint] = useState('')
  const [connectionLoading, setConnectionLoading] = useState(false)
  const [connectionError, setConnectionError] = useState('')
  const [grants, setGrants] = useState<MCPGrant[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [revokeTarget, setRevokeTarget] = useState<MCPGrant | null>(null)
  const [revoking, setRevoking] = useState(false)
  const [revokeError, setRevokeError] = useState('')

  useEffect(() => {
    let unmounted = false

    async function bootstrap(): Promise<void> {
      setConnectionLoading(true)
      setConnectionError('')
      setEndpoint('')
      try {
        const result = await mcpAPI.getConnectionConfig()
        if (!unmounted) setEndpoint(result.resource)
      } catch (error) {
        if (!unmounted) setConnectionError(errorMessage(error, t('mcp.connectionLoadFailed')))
      } finally {
        if (!unmounted) setConnectionLoading(false)
      }

      setLoading(true)
      setLoadError('')
      try {
        const result = await mcpAPI.listGrants()
        if (!unmounted) setGrants(result)
      } catch (error) {
        if (!unmounted) setLoadError(errorMessage(error, t('mcp.loadFailed')))
      } finally {
        if (!unmounted) setLoading(false)
      }
    }

    void bootstrap()
    return () => {
      unmounted = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function errorMessage(error: unknown, fallback: string): string {
    const message = (error as { message?: unknown } | null)?.message
    return typeof message === 'string' && message ? message : fallback
  }

  async function loadConnection(): Promise<void> {
    if (connectionLoading) return
    setConnectionLoading(true)
    setConnectionError('')
    setEndpoint('')
    try {
      const result = await mcpAPI.getConnectionConfig()
      setEndpoint(result.resource)
    } catch (error) {
      setConnectionError(errorMessage(error, t('mcp.connectionLoadFailed')))
    } finally {
      setConnectionLoading(false)
    }
  }

  function isActive(grant: MCPGrant): boolean {
    return !grant.revoked_at && !grant.invalidated && new Date(grant.expires_at).getTime() > Date.now()
  }

  function grantStatus(grant: MCPGrant): string {
    if (grant.revoked_at) return t('mcp.revoked')
    if (grant.invalidated) return t('mcp.invalidated')
    return isActive(grant) ? t('mcp.active') : t('mcp.expired')
  }

  async function loadGrants(): Promise<void> {
    if (loading || revoking) return
    setLoading(true)
    setLoadError('')
    try {
      const result = await mcpAPI.listGrants()
      setGrants(result)
    } catch (error) {
      setLoadError(errorMessage(error, t('mcp.loadFailed')))
    } finally {
      setLoading(false)
    }
  }

  function closeRevoke(): void {
    if (revoking) return
    setRevokeTarget(null)
    setRevokeError('')
  }

  async function revokeGrant(): Promise<void> {
    if (!revokeTarget || revoking || loading) return
    setRevoking(true)
    setRevokeError('')
    const id = revokeTarget.id
    try {
      await mcpAPI.revokeGrant(id)
      setGrants(grants.map((grant) => (grant.id === id ? { ...grant, revoked_at: new Date().toISOString() } : grant)))
      setRevokeTarget(null)
      toast.success(t('mcp.revokeSuccess'))
    } catch (error) {
      setRevokeError(errorMessage(error, t('mcp.revokeFailed')))
    } finally {
      setRevoking(false)
    }
  }

  useEffect(() => {
    void loadConnection()
    void loadGrants()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <section className="card p-5 sm:p-6" aria-labelledby="mcp-connection-title" aria-busy={connectionLoading}>
        <div className="flex items-start gap-3">
          <div className="rounded-xl bg-primary-50 p-3 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
            <Icon name="mcp" size="lg" />
          </div>
          <div>
            <h2 id="mcp-connection-title" className="text-lg font-semibold text-gray-900 dark:text-white">
              {t('mcp.connectionTitle')}
            </h2>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{t('mcp.connectionDescription')}</p>
          </div>
        </div>
        <div className="mt-5">
          <label htmlFor="mcp-endpoint" className="input-label">
            {t('mcp.endpoint')}
          </label>
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              id="mcp-endpoint"
              value={endpoint}
              placeholder={connectionLoading ? t('mcp.connectionLoading') : ''}
              className="input min-w-0 flex-1 font-mono text-sm"
              readOnly
            />
            <button
              data-testid="copy-endpoint"
              className="btn btn-secondary shrink-0"
              type="button"
              disabled={!endpoint || connectionLoading}
              onClick={() => void copyToClipboard(endpoint)}
            >
              {t('mcp.copyEndpoint')}
            </button>
          </div>
          {connectionError && (
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <p role="alert" className="text-sm text-red-600 dark:text-red-400">
                {connectionError}
              </p>
              <button
                data-testid="retry-connection"
                className="btn btn-secondary btn-sm"
                type="button"
                disabled={connectionLoading}
                onClick={() => void loadConnection()}
              >
                {t('common.refresh')}
              </button>
            </div>
          )}
          <ol className="mt-5 list-decimal space-y-2 pl-5 text-sm text-gray-600 dark:text-gray-300">
            <li>{t('mcp.connectStep1')}</li>
            <li>{t('mcp.connectStep2')}</li>
            <li>{t('mcp.connectStep3')}</li>
          </ol>
        </div>
        <p className="mt-5 border-t border-gray-100 pt-4 text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
          {t('mcp.capabilities')}
        </p>
      </section>

      <section className="card" aria-labelledby="mcp-grants-title">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 p-5 dark:border-dark-700 sm:p-6">
          <div className="max-w-xl">
            <h2 id="mcp-grants-title" className="text-lg font-semibold text-gray-900 dark:text-white">
              {t('mcp.grantsTitle')}
            </h2>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{t('mcp.grantsDescription')}</p>
          </div>
          <button
            data-testid="refresh-grants"
            type="button"
            className="btn btn-secondary"
            disabled={loading || revoking}
            onClick={() => void loadGrants()}
          >
            {t('common.refresh')}
          </button>
        </div>
        <div className="p-5 sm:p-6" aria-busy={loading}>
          {loadError ? (
            <p role="alert" className="text-sm text-red-600 dark:text-red-400">
              {loadError}
            </p>
          ) : loading ? (
            <p role="status" className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
              {t('mcp.loading')}
            </p>
          ) : grants.length === 0 ? (
            <p className="rounded-xl border border-dashed border-gray-200 px-4 py-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
              {t('mcp.empty')}
            </p>
          ) : (
            <ul className="divide-y divide-gray-100 dark:divide-dark-700">
              {grants.map((grant) => (
                <li
                  key={grant.id}
                  className="flex items-start justify-between gap-4 py-5 first:pt-0 last:pb-0"
                  data-grant-id={grant.id}
                >
                  <div className="min-w-0 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="break-all font-medium text-gray-900 dark:text-white">{grant.client_name}</span>
                      <span className="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                        {grant.scope === 'admin' ? t('mcp.adminScope') : t('mcp.userScope')}
                      </span>
                      <span className="rounded-md bg-primary-50 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">
                        {grant.read_only ? t('mcp.readOnly') : t('mcp.readWrite')}
                      </span>
                      <span
                        className={
                          isActive(grant) ? 'text-xs text-emerald-600 dark:text-emerald-400' : 'text-xs text-gray-500 dark:text-gray-400'
                        }
                      >
                        {grantStatus(grant)}
                      </span>
                    </div>
                    <p className="truncate font-mono text-xs text-gray-500 dark:text-gray-400" title={grant.client_id}>
                      {grant.client_id}
                    </p>
                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                      <span>{t('mcp.createdAt', { date: formatDate(grant.created_at) })}</span>
                      <span>{t('mcp.expiresAt', { date: formatDate(grant.expires_at) })}</span>
                      <span>
                        {grant.last_used_at ? t('mcp.lastUsedAt', { date: formatDate(grant.last_used_at) }) : t('mcp.neverUsed')}
                      </span>
                    </div>
                  </div>
                  {!grant.revoked_at && (
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm shrink-0 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/20"
                      aria-label={`${t('mcp.revoke')} ${grant.client_name}`}
                      onClick={() => setRevokeTarget(grant)}
                    >
                      {t('mcp.revoke')}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      <Modal
        open={!!revokeTarget}
        onClose={closeRevoke}
        title={t('mcp.revokeTitle')}
        size="sm"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" disabled={revoking} onClick={closeRevoke}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="danger"
              disabled={revoking || loading}
              onClick={() => void revokeGrant()}
            >
              {revoking ? t('common.processing') : t('mcp.revoke')}
            </Button>
          </div>
        }
      >
        <p className="text-sm text-gray-600 dark:text-gray-300">
          {t('mcp.revokeConfirm', { name: revokeTarget?.client_name || '' })}
        </p>
        {revokeError && (
          <p role="alert" className="mt-3 text-sm text-red-600 dark:text-red-400">
            {revokeError}
          </p>
        )}
      </Modal>
    </div>
  )
}
