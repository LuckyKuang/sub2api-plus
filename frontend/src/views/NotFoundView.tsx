import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { Icon } from '@/components/auth/Icon'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import '@/views/authViews.css'

export default function NotFoundView() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  useProductAppearance()

  function goBack(): void {
    navigate(-1)
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas p-[var(--shell-gutter)] dark:surface-inset">
      {/* Background Decoration */}
      <div className="pointer-events-none absolute inset-0 overflow-hidden">
        <div className="absolute -right-40 -top-40 h-80 w-80 rounded-full bg-primary-400/10 blur-3xl" />
        <div className="absolute -bottom-40 -left-40 h-80 w-80 rounded-full bg-primary-500/10 blur-3xl" />
      </div>

      <div className="card relative z-10 w-full max-w-md p-5 text-center sm:p-8">
        {/* 404 Display */}
        <div className="mb-8">
          <div className="relative inline-block">
            <span className="text-[9rem] font-bold leading-none text-gray-100 dark:text-dark-800 sm:text-[12rem]">
              404
            </span>
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="flex h-24 w-24 items-center justify-center rounded-2xl bg-gradient-to-br from-primary-500 to-primary-600 shadow-lg shadow-primary-500/30">
                <svg
                  className="h-12 w-12 text-white"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  strokeWidth="1.5"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
                  />
                </svg>
              </div>
            </div>
          </div>
        </div>

        {/* Text Content */}
        <div className="mb-8">
          <h1 className="mb-3 text-2xl font-bold text-gray-900 dark:text-white">{t('errors.pageNotFound')}</h1>
          <p className="surface-muted">The page you are looking for doesn&apos;t exist or has been moved.</p>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-col justify-center gap-3 sm:flex-row">
          <button onClick={goBack} className="btn btn-secondary">
            <Icon name="arrowLeft" size="md" className="mr-2" />
            Go Back
          </button>
          <Link to="/dashboard" className="btn btn-primary">
            <Icon name="home" size="md" className="mr-2" />
            Go to Dashboard
          </Link>
        </div>

        {/* Help Link */}
        <p className="mt-8 text-sm surface-muted">
          Need help?
          <a
            href="#"
            className="text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
          >
            Contact support
          </a>
        </p>
      </div>
    </div>
  )
}
