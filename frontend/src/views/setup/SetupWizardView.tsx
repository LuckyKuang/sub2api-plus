import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Select, Toggle } from '@/components/ui'
import { testDatabase, testRedis, install, type InstallRequest } from '@/api/setup'
import { buildGatewayUrl } from '@/api/url'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { resolveCompletedSetupRedirectPath } from '@/router/setupRedirect'
import { Icon } from '@/views/admin/AccountIcon'

export default function SetupWizardView() {
  const { t } = useTranslation()
  useProductAppearance()

  const [currentStep, setCurrentStep] = useState(0)
  const [errorMessage, setErrorMessage] = useState('')
  const [installSuccess, setInstallSuccess] = useState(false)

  const [testingDb, setTestingDb] = useState(false)
  const [testingRedis, setTestingRedis] = useState(false)
  const [dbConnected, setDbConnected] = useState(false)
  const [redisConnected, setRedisConnected] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [confirmPassword, setConfirmPassword] = useState('')
  const [serviceReady, setServiceReady] = useState(false)

  // Default server port
  const getCurrentPort = (): number => {
    const port = window.location.port
    if (port) {
      return parseInt(port, 10)
    }

    return window.location.protocol === 'https:' ? 443 : 80
  }

  const [formData, setFormData] = useState<InstallRequest>({
    database: {
      host: 'localhost',
      port: 5432,
      user: 'postgres',
      password: '',
      dbname: 'sub2api',
      sslmode: 'disable'
    },
    redis: {
      host: 'localhost',
      port: 6379,
      username: '',
      password: '',
      db: 0,
      enable_tls: false
    },
    admin: {
      email: '',
      password: ''
    },
    server: {
      host: '0.0.0.0',
      port: getCurrentPort(),
      mode: 'release'
    }
  })

  const steps = useMemo(
    () => [
      { id: 'database', title: t('setup.database.title') },
      { id: 'redis', title: t('setup.redis.title') },
      { id: 'admin', title: t('setup.admin.title') },
      { id: 'complete', title: t('setup.ready.title') }
    ],
    [t]
  )

  const adminPasswordValid = useMemo(() => {
    const byteLength = new TextEncoder().encode(formData.admin.password).length
    return byteLength >= 8 && byteLength <= 72
  }, [formData.admin.password])

  const canProceed = useMemo(() => {
    switch (currentStep) {
      case 0:
        return dbConnected
      case 1:
        return redisConnected
      case 2:
        return Boolean(
          formData.admin.email.trim() && adminPasswordValid && formData.admin.password === confirmPassword
        )
      default:
        return true
    }
  }, [currentStep, dbConnected, redisConnected, formData.admin.email, formData.admin.password, adminPasswordValid, confirmPassword])

  async function testDatabaseConnection() {
    setTestingDb(true)
    setErrorMessage('')
    setDbConnected(false)

    try {
      await testDatabase(formData.database)
      setDbConnected(true)
    } catch (error: unknown) {
      const err = error as { response?: { data?: { detail?: string; message?: string } }; message?: string }
      setErrorMessage(err.response?.data?.detail || err.response?.data?.message || err.message || 'Connection failed')
    } finally {
      setTestingDb(false)
    }
  }

  async function testRedisConnection() {
    setTestingRedis(true)
    setErrorMessage('')
    setRedisConnected(false)

    try {
      await testRedis(formData.redis)
      setRedisConnected(true)
    } catch (error: unknown) {
      const err = error as { response?: { data?: { detail?: string; message?: string } }; message?: string }
      setErrorMessage(err.response?.data?.detail || err.response?.data?.message || err.message || 'Connection failed')
    } finally {
      setTestingRedis(false)
    }
  }

  function nextStep() {
    if (canProceed) {
      setErrorMessage('')
      setCurrentStep(currentStep + 1)
    }
  }

  async function performInstall() {
    setInstalling(true)
    setErrorMessage('')

    try {
      await install(formData)
      setInstallSuccess(true)
      // Start polling for service restart
      void waitForServiceRestart()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { detail?: string; message?: string } }; message?: string }
      setErrorMessage(err.response?.data?.detail || err.response?.data?.message || err.message || 'Installation failed')
    } finally {
      setInstalling(false)
    }
  }

  // Wait for service to restart and become available
  async function waitForServiceRestart() {
    const maxAttempts = 60 // Increase to 60 attempts, ~60 seconds max
    const interval = 1000 // 1 second between attempts

    // Wait a moment for the service to start restarting
    await new Promise((resolve) => setTimeout(resolve, 3000))

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      try {
        // Use setup status endpoint as it tells us the real mode
        // Service might return 404 or connection refused while restarting
        const response = await fetch(buildGatewayUrl('/setup/status'), {
          method: 'GET',
          cache: 'no-store'
        })

        if (response.ok) {
          const data = await response.json()
          // If needs_setup is false, service has restarted in normal mode
          if (data.data && !data.data.needs_setup) {
            setServiceReady(true)
            // Redirect to the dashboard (or login when the session is gone) after a short delay
            setTimeout(() => {
              const token = localStorage.getItem('auth_token')
              window.location.href = resolveCompletedSetupRedirectPath(Boolean(token), false)
            }, 1500)
            return
          }
        }
      } catch {
        // Service not ready or network error during restart, continue polling
      }

      await new Promise((resolve) => setTimeout(resolve, interval))
    }

    // If we reach here, service didn't restart in time
    // Show a message to refresh manually
    setErrorMessage(t('setup.status.timeout'))
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-canvas p-[var(--shell-gutter)] dark:surface-inset">
      <div className="flex w-full min-w-0 max-w-2xl flex-col gap-[var(--shell-gap)]">
        <div className="shell-surface px-5 py-6">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white sm:text-3xl">{t('setup.title')}</h1>
          <p className="mt-2 surface-muted">{t('setup.description')}</p>
        </div>

        <div className="card px-3 py-5 sm:px-6">
          <div className="flex items-center justify-center">
            {steps.map((step, index) => (
              <div key={step.id} className="flex items-center">
                <div className="flex items-center">
                  <div
                    className={`flex h-10 w-10 items-center justify-center rounded-full text-sm font-semibold transition-all ${
                      currentStep > index
                        ? 'bg-primary-500 text-white'
                        : currentStep === index
                          ? 'bg-primary-500 text-white ring-4 ring-primary-100 dark:ring-primary-900'
                          : 'surface-inset text-gray-500 dark:surface-inset dark:text-dark-400'
                    }`}
                  >
                    {currentStep > index ? (
                      <Icon name="check" size="md" strokeWidth={2} />
                    ) : (
                      <span>{index + 1}</span>
                    )}
                  </div>
                  <span
                    className={`ml-2 hidden text-sm font-medium sm:inline ${
                      currentStep >= index ? 'text-gray-900 dark:text-white' : 'surface-muted'
                    }`}
                  >
                    {step.title}
                  </span>
                </div>
                {index < steps.length - 1 && (
                  <div
                    className={`mx-2 h-0.5 w-6 sm:mx-3 sm:w-12 ${currentStep > index ? 'bg-primary-500' : 'bg-gray-200 dark:bg-dark-700'}`}
                  />
                )}
              </div>
            ))}
          </div>
        </div>

        <div className="card p-5 sm:p-8">
          {currentStep === 0 && (
            <div className="space-y-6">
              <div className="mb-6 text-center">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">{t('setup.database.title')}</h2>
                <p className="mt-1 text-sm surface-muted">{t('setup.database.description')}</p>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="input-label">{t('setup.database.host')}</label>
                  <input
                    value={formData.database.host}
                    type="text"
                    className="input"
                    placeholder="localhost"
                    onChange={(event) =>
                      setFormData({ ...formData, database: { ...formData.database, host: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.database.port')}</label>
                  <input
                    value={formData.database.port}
                    type="number"
                    className="input"
                    placeholder="5432"
                    onChange={(event) =>
                      setFormData({
                        ...formData,
                        database: { ...formData.database, port: Number(event.target.value) }
                      })
                    }
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="input-label">{t('setup.database.username')}</label>
                  <input
                    value={formData.database.user}
                    type="text"
                    className="input"
                    placeholder="postgres"
                    onChange={(event) =>
                      setFormData({ ...formData, database: { ...formData.database, user: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.database.password')}</label>
                  <input
                    value={formData.database.password}
                    type="password"
                    className="input"
                    placeholder={t('setup.database.passwordPlaceholder')}
                    onChange={(event) =>
                      setFormData({ ...formData, database: { ...formData.database, password: event.target.value } })
                    }
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="input-label">{t('setup.database.databaseName')}</label>
                  <input
                    value={formData.database.dbname}
                    type="text"
                    className="input"
                    placeholder="sub2api"
                    onChange={(event) =>
                      setFormData({ ...formData, database: { ...formData.database, dbname: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.database.sslMode')}</label>
                  <Select
                    value={formData.database.sslmode}
                    searchable={false}
                    options={[
                      { value: 'disable', label: t('setup.database.ssl.disable') },
                      { value: 'require', label: t('setup.database.ssl.require') },
                      { value: 'verify-ca', label: t('setup.database.ssl.verifyCa') },
                      { value: 'verify-full', label: t('setup.database.ssl.verifyFull') }
                    ]}
                    onValueChange={(next) =>
                      setFormData({ ...formData, database: { ...formData.database, sslmode: String(next ?? 'disable') } })
                    }
                  />
                </div>
              </div>

              <button type="button" onClick={() => void testDatabaseConnection()} disabled={testingDb} className="btn btn-secondary w-full">
                {testingDb ? (
                  <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                ) : (
                  dbConnected && (
                    <Icon name="check" size="md" className="mr-2 text-green-500" strokeWidth={2} />
                  )
                )}
                {testingDb
                  ? t('setup.status.testing')
                  : dbConnected
                    ? t('setup.status.success')
                    : t('setup.status.testConnection')}
              </button>
            </div>
          )}

          {currentStep === 1 && (
            <div className="space-y-6">
              <div className="mb-6 text-center">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">{t('setup.redis.title')}</h2>
                <p className="mt-1 text-sm surface-muted">{t('setup.redis.description')}</p>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="input-label">{t('setup.redis.host')}</label>
                  <input
                    value={formData.redis.host}
                    type="text"
                    className="input"
                    placeholder="localhost"
                    onChange={(event) =>
                      setFormData({ ...formData, redis: { ...formData.redis, host: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.redis.port')}</label>
                  <input
                    value={formData.redis.port}
                    type="number"
                    className="input"
                    placeholder="6379"
                    onChange={(event) =>
                      setFormData({ ...formData, redis: { ...formData.redis, port: Number(event.target.value) } })
                    }
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="input-label">{t('setup.redis.username')}</label>
                  <input
                    value={formData.redis.username}
                    type="text"
                    className="input"
                    placeholder={t('setup.redis.usernamePlaceholder')}
                    onChange={(event) =>
                      setFormData({ ...formData, redis: { ...formData.redis, username: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.redis.password')}</label>
                  <input
                    value={formData.redis.password}
                    type="password"
                    className="input"
                    placeholder={t('setup.redis.passwordPlaceholder')}
                    onChange={(event) =>
                      setFormData({ ...formData, redis: { ...formData.redis, password: event.target.value } })
                    }
                  />
                </div>
                <div>
                  <label className="input-label">{t('setup.redis.database')}</label>
                  <input
                    value={formData.redis.db}
                    type="number"
                    className="input"
                    placeholder="0"
                    onChange={(event) =>
                      setFormData({ ...formData, redis: { ...formData.redis, db: Number(event.target.value) } })
                    }
                  />
                </div>
              </div>

              <div className="flex items-center justify-between rounded-xl border border-gray-200 p-3 dark:border-dark-700">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('setup.redis.enableTls')}</p>
                  <p className="text-xs surface-muted">{t('setup.redis.enableTlsHint')}</p>
                </div>
                <Toggle
                  checked={formData.redis.enable_tls}
                  onCheckedChange={(next) =>
                    setFormData({ ...formData, redis: { ...formData.redis, enable_tls: next } })
                  }
                />
              </div>

              <button type="button" onClick={() => void testRedisConnection()} disabled={testingRedis} className="btn btn-secondary w-full">
                {testingRedis ? (
                  <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                ) : (
                  redisConnected && (
                    <Icon name="check" size="md" className="mr-2 text-green-500" strokeWidth={2} />
                  )
                )}
                {testingRedis
                  ? t('setup.status.testing')
                  : redisConnected
                    ? t('setup.status.success')
                    : t('setup.status.testConnection')}
              </button>
            </div>
          )}

          {currentStep === 2 && (
            <div className="space-y-6">
              <div className="mb-6 text-center">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">{t('setup.admin.title')}</h2>
                <p className="mt-1 text-sm surface-muted">{t('setup.admin.description')}</p>
              </div>

              <div>
                <label htmlFor="setup-admin-email" className="input-label">
                  {t('setup.admin.email')}
                </label>
                <input
                  id="setup-admin-email"
                  value={formData.admin.email}
                  type="email"
                  className="input"
                  placeholder="you@example.com"
                  onChange={(event) =>
                    setFormData({ ...formData, admin: { ...formData.admin, email: event.target.value } })
                  }
                />
              </div>

              <div>
                <label htmlFor="setup-admin-password" className="input-label">
                  {t('setup.admin.password')}
                </label>
                <input
                  id="setup-admin-password"
                  value={formData.admin.password}
                  type="password"
                  className="input"
                  placeholder={t('setup.admin.passwordPlaceholder')}
                  aria-invalid={Boolean(formData.admin.password) && !adminPasswordValid}
                  aria-describedby={
                    formData.admin.password && !adminPasswordValid ? 'setup-password-error' : undefined
                  }
                  onChange={(event) =>
                    setFormData({ ...formData, admin: { ...formData.admin, password: event.target.value } })
                  }
                />
                {formData.admin.password && !adminPasswordValid && (
                  <p id="setup-password-error" role="alert" className="input-error-text">
                    {t('setup.admin.passwordLengthError')}
                  </p>
                )}
              </div>

              <div>
                <label htmlFor="setup-admin-confirm-password" className="input-label">
                  {t('setup.admin.confirmPassword')}
                </label>
                <input
                  id="setup-admin-confirm-password"
                  value={confirmPassword}
                  type="password"
                  className="input"
                  placeholder={t('setup.admin.confirmPasswordPlaceholder')}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                />
                {confirmPassword && formData.admin.password !== confirmPassword && (
                  <p className="input-error-text">{t('setup.admin.passwordMismatch')}</p>
                )}
              </div>
            </div>
          )}

          {currentStep === 3 && (
            <div className="space-y-6">
              <div className="mb-6 text-center">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">{t('setup.ready.title')}</h2>
                <p className="mt-1 text-sm surface-muted">{t('setup.ready.description')}</p>
              </div>

              <div className="space-y-4">
                <div className="rounded-xl surface-inset p-4">
                  <h3 className="mb-2 text-sm font-medium surface-muted">{t('setup.ready.database')}</h3>
                  <p className="text-gray-900 dark:text-white">
                    {formData.database.user}@{formData.database.host}:{formData.database.port}/{formData.database.dbname}
                  </p>
                </div>

                <div className="rounded-xl surface-inset p-4">
                  <h3 className="mb-2 text-sm font-medium surface-muted">{t('setup.ready.redis')}</h3>
                  <p className="text-gray-900 dark:text-white">
                    {formData.redis.host}:{formData.redis.port}
                  </p>
                </div>

                <div className="rounded-xl surface-inset p-4">
                  <h3 className="mb-2 text-sm font-medium surface-muted">{t('setup.ready.adminEmail')}</h3>
                  <p className="text-gray-900 dark:text-white">{formData.admin.email}</p>
                </div>
              </div>
            </div>
          )}

          {errorMessage && (
            <div className="mt-6 rounded-xl border border-red-200 bg-red-50/[0.15] p-4 dark:border-red-800/50 dark:bg-red-900/20">
              <div className="flex items-start gap-3">
                <Icon name="exclamationCircle" size="md" className="flex-shrink-0 text-red-500" />
                <p className="text-sm text-red-700 dark:text-red-400">{errorMessage}</p>
              </div>
            </div>
          )}

          {installSuccess && (
            <div className="mt-6 rounded-xl border border-green-200 bg-green-50/[0.15] p-4 dark:border-green-800/50 dark:bg-green-900/20">
              <div className="flex items-start gap-3">
                {!serviceReady ? (
                  <svg className="h-5 w-5 flex-shrink-0 animate-spin text-green-500" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                ) : (
                  <Icon name="checkCircle" size="md" className="flex-shrink-0 text-green-500" />
                )}
                <div>
                  <p className="text-sm font-medium text-green-700 dark:text-green-400">
                    {t('setup.status.completed')}
                  </p>
                  <p className="mt-1 text-sm text-green-600 dark:text-green-500">
                    {serviceReady ? t('setup.status.redirecting') : t('setup.status.restarting')}
                  </p>
                </div>
              </div>
            </div>
          )}

          <div className="mt-8 flex justify-between">
            {currentStep > 0 && !installSuccess ? (
              <button type="button" onClick={() => setCurrentStep(currentStep - 1)} className="btn btn-secondary">
                <Icon name="chevronLeft" size="sm" className="mr-2" strokeWidth={2} />
                {t('common.back')}
              </button>
            ) : (
              <div />
            )}

            {currentStep < 3 && (
              <button type="button" onClick={nextStep} disabled={!canProceed} className="btn btn-primary">
                {t('common.next')}
                <Icon name="chevronRight" size="sm" className="ml-2" strokeWidth={2} />
              </button>
            )}

            {currentStep >= 3 && !installSuccess && (
              <button type="button" onClick={() => void performInstall()} disabled={installing} className="btn btn-primary">
                {installing && (
                  <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                )}
                {installing ? t('setup.status.installing') : t('setup.status.completeInstallation')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
