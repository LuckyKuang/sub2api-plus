import { useTranslation } from 'react-i18next'
import type { User, UserAuthBindingStatus, UserAuthProvider, UserProfileSourceContext } from '@/types'
import { Icon } from '../icons'
import { ProfileAvatarCard } from './ProfileAvatarCard'
import { ProfileEditForm } from './ProfileEditForm'
import { ProfileIdentityBindingsSection } from './ProfileIdentityBindingsSection'

export interface ProfileInfoCardProps {
  user: User | null
  linuxdoEnabled?: boolean
  dingtalkEnabled?: boolean
  oidcEnabled?: boolean
  oidcProviderName?: string
  wechatEnabled?: boolean
  wechatOpenEnabled?: boolean
  wechatMpEnabled?: boolean
}

function normalizeBindingStatus(binding: boolean | UserAuthBindingStatus | undefined): boolean | null {
  if (typeof binding === 'boolean') return binding
  if (!binding) return null
  if (typeof binding.bound === 'boolean') return binding.bound
  return Boolean(binding.provider_subject || binding.issuer || binding.provider_key)
}

function isEmailBound(user: User | null | undefined): boolean {
  if (typeof user?.email_bound === 'boolean') return user.email_bound
  const nested = user?.auth_bindings?.email ?? user?.identity_bindings?.email
  const normalized = normalizeBindingStatus(nested)
  return normalized ?? false
}

export function ProfileInfoCard({
  user,
  linuxdoEnabled = false,
  dingtalkEnabled = false,
  oidcEnabled = false,
  oidcProviderName = 'OIDC',
  wechatEnabled = false,
  wechatOpenEnabled,
  wechatMpEnabled
}: ProfileInfoCardProps) {
  const { t } = useTranslation()

  const avatarUrl = user?.avatar_url?.trim() || ''
  const displayName = user?.username?.trim() || user?.email?.trim() || t('profile.user')
  const primaryEmailDisplay = (() => {
    const email = user?.email?.trim() || ''
    if (!email) return ''
    if (email.endsWith('.invalid') && !isEmailBound(user)) return ''
    return email
  })()
  const avatarInitial = displayName.charAt(0).toUpperCase() || 'U'
  const memberSinceLabel = (() => {
    const raw = user?.created_at?.trim()
    if (!raw) return '-'
    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return '-'
    return new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short' }).format(date)
  })()

  const providerLabels: Record<UserAuthProvider, string> = {
    email: t('profile.authBindings.providers.email'),
    linuxdo: t('profile.authBindings.providers.linuxdo'),
    dingtalk: t('profile.authBindings.providers.dingtalk'),
    oidc: t('profile.authBindings.providers.oidc', { providerName: oidcProviderName }),
    wechat: t('profile.authBindings.providers.wechat'),
    github: 'GitHub',
    google: 'Google'
  }

  function formatCurrency(value: number): string {
    return `$${value.toFixed(2)}`
  }

  function normalizeProvider(value: string): UserAuthProvider | null {
    const normalized = value.trim().toLowerCase()
    if (
      normalized === 'email' ||
      normalized === 'linuxdo' ||
      normalized === 'wechat' ||
      normalized === 'github' ||
      normalized === 'google'
    ) {
      return normalized
    }
    if (normalized === 'oidc' || normalized.startsWith('oidc:') || normalized.startsWith('oidc/')) {
      return 'oidc'
    }
    return null
  }

  function readObjectString(source: Record<string, unknown>, ...keys: string[]): string {
    for (const key of keys) {
      const value = source[key]
      if (typeof value === 'string' && value.trim()) return value.trim()
    }
    return ''
  }

  function resolveThirdPartySource(
    rawSource: string | UserProfileSourceContext | null | undefined
  ): { provider: UserAuthProvider; label: string } | null {
    if (!rawSource) return null

    if (typeof rawSource === 'string') {
      const provider = normalizeProvider(rawSource)
      if (!provider || provider === 'email') return null
      return { provider, label: providerLabels[provider] }
    }

    const sourceRecord = rawSource as Record<string, unknown>
    const provider = normalizeProvider(
      readObjectString(sourceRecord, 'provider', 'source', 'provider_type', 'auth_provider')
    )
    if (!provider || provider === 'email') return null

    const explicitLabel = readObjectString(
      sourceRecord,
      'provider_label',
      'label',
      'provider_name',
      'providerName'
    )

    return { provider, label: explicitLabel || providerLabels[provider] }
  }

  const sourceHints: Array<{ key: string; text: string }> = []
  if (user) {
    const avatarSource = resolveThirdPartySource(user.profile_sources?.avatar ?? user.avatar_source)
    const usernameSource = resolveThirdPartySource(
      user.profile_sources?.username ??
        user.profile_sources?.display_name ??
        user.profile_sources?.nickname ??
        user.display_name_source ??
        user.username_source ??
        user.nickname_source
    )

    if (avatarSource) {
      sourceHints.push({
        key: 'avatar',
        text: t('profile.authBindings.source.avatar', { providerName: avatarSource.label })
      })
    }
    if (usernameSource) {
      sourceHints.push({
        key: 'username',
        text: t('profile.authBindings.source.username', { providerName: usernameSource.label })
      })
    }
  }

  return (
    <div className="space-y-6">
      <section data-testid="profile-overview-hero" className="card workspace-shell overflow-hidden">
        <div className="px-6 py-6 md:px-8">
          <div className="flex flex-col gap-6 lg:flex-row lg:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-[1.75rem] bg-gradient-to-br from-primary-500 to-primary-600 text-2xl font-bold text-white shadow-lg shadow-primary-500/20">
              {avatarUrl ? (
                <img src={avatarUrl} alt={displayName} className="h-full w-full object-cover" />
              ) : (
                <span>{avatarInitial}</span>
              )}
            </div>

            <div className="min-w-0 flex-1 space-y-5">
              <div className="space-y-3">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="truncate text-2xl font-semibold text-gray-900 dark:text-white">{displayName}</h2>
                  <span className={`badge ${user?.role === 'admin' ? 'badge-primary' : 'badge-gray'}`}>
                    {user?.role === 'admin' ? t('profile.administrator') : t('profile.user')}
                  </span>
                  <span className={`badge ${user?.status === 'active' ? 'badge-success' : 'badge-danger'}`}>
                    {user?.status === 'active' ? t('common.active') : t('common.disabled')}
                  </span>
                </div>

                <div className="space-y-1">
                  <p className="truncate text-sm surface-muted">{primaryEmailDisplay}</p>
                  {sourceHints.length > 0 && (
                    <div className="flex flex-wrap gap-2 text-xs surface-muted">
                      {sourceHints.map((hint) => (
                        <span
                          key={hint.key}
                          className="inline-flex items-center gap-1 rounded-full surface-inset px-3 py-1 ring-1 ring-primary-100 dark:ring-primary-900/40"
                        >
                          <Icon name="link" size="sm" />
                          {hint.text}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-3">
                <div
                  data-testid="profile-overview-metric-balance"
                  className="glass-surface px-4 py-3 ring-1 ring-white/70 dark:ring-dark-700"
                >
                  <p className="text-xs font-medium uppercase tracking-[0.16em] surface-muted">
                    {t('profile.accountBalance')}
                  </p>
                  <p className="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
                    {formatCurrency(user?.balance || 0)}
                  </p>
                </div>
                <div
                  data-testid="profile-overview-metric-concurrency"
                  className="glass-surface px-4 py-3 ring-1 ring-white/70 dark:ring-dark-700"
                >
                  <p className="text-xs font-medium uppercase tracking-[0.16em] surface-muted">
                    {t('profile.concurrencyLimit')}
                  </p>
                  <p className="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{user?.concurrency || 0}</p>
                </div>
                <div
                  data-testid="profile-overview-metric-member-since"
                  className="glass-surface px-4 py-3 ring-1 ring-white/70 dark:ring-dark-700"
                >
                  <p className="text-xs font-medium uppercase tracking-[0.16em] surface-muted">
                    {t('profile.memberSince')}
                  </p>
                  <p className="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{memberSinceLabel}</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <div className="space-y-6">
        <div data-testid="profile-main-column" className="space-y-6">
          <section data-testid="profile-basics-panel" className="card workspace-shell p-6">
            <div className="mb-5 flex items-start justify-between gap-4">
              <div>
                <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('profile.basicsTitle')}</h3>
                <p className="mt-1 text-sm surface-muted">{t('profile.basicsDescription')}</p>
              </div>
            </div>

            <div className="grid gap-6 sm:grid-cols-1 md:grid-cols-2">
              <div className="glass-surface border p-5">
                <ProfileAvatarCard user={user} embedded />
              </div>

              <div className="glass-surface border p-5">
                <ProfileEditForm initialUsername={user?.username || ''} embedded />
              </div>
            </div>
          </section>

          <section data-testid="profile-auth-bindings-panel" className="card workspace-shell p-6">
            <ProfileIdentityBindingsSection
              user={user}
              linuxdoEnabled={linuxdoEnabled}
              dingtalkEnabled={dingtalkEnabled}
              oidcEnabled={oidcEnabled}
              oidcProviderName={oidcProviderName}
              wechatEnabled={wechatEnabled}
              wechatOpenEnabled={wechatOpenEnabled}
              wechatMpEnabled={wechatMpEnabled}
              embedded
              compact
            />
          </section>
        </div>

        <div data-testid="profile-side-column" className="space-y-6">
          {sourceHints.length > 0 && (
            <section className="card workspace-shell p-6">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                {t('profile.linkedProfileSources')}
              </h3>
              <p className="mt-1 text-sm surface-muted">{t('profile.linkedProfileSourcesDescription')}</p>

              <div className="mt-5 grid gap-3">
                {sourceHints.map((hint) => (
                  <div
                    key={hint.key}
                    className="glass-surface flex items-start gap-3 border px-4 py-3 text-sm surface-muted"
                  >
                    <Icon name="link" size="sm" className="mt-0.5 surface-muted" />
                    <span>{hint.text}</span>
                  </div>
                ))}
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}

export default ProfileInfoCard
