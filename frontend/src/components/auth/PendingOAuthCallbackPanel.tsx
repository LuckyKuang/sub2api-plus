import { useTranslation } from 'react-i18next'
import { PendingOAuthCreateAccountForm, type PendingOAuthCreateAccountPayload } from './PendingOAuthCreateAccountForm'

export interface PendingOAuthCallbackPanelProps {
  providerName: string
  needsInvitation: boolean
  needsAdoptionConfirmation: boolean
  needsChooser: boolean
  needsCreateAccount: boolean
  needsBindLogin: boolean
  needsTotpChallenge: boolean
  isSubmitting: boolean

  adoptionRequired: boolean
  suggestedDisplayName: string
  suggestedAvatarUrl: string
  adoptDisplayName: boolean
  adoptAvatar: boolean
  onAdoptDisplayNameChange: (value: boolean) => void
  onAdoptAvatarChange: (value: boolean) => void

  invitationCode: string
  onInvitationCodeChange: (value: string) => void
  onSubmitInvitation: () => void
  invitationRequiredText: string
  invitationCompletingText: string
  invitationCompleteText: string

  onContinueLogin: () => void

  pendingAccountEmail: string
  onSwitchToBindLogin: (email?: string) => void
  onSwitchToCreateAccount: () => void

  onCreateAccount: (payload: PendingOAuthCreateAccountPayload) => void
  accountActionError: string
  createAccountTestIdPrefix: string
  canReturnToCreateAccount: boolean

  bindLoginEmail: string
  bindLoginPassword: string
  onBindLoginEmailChange: (value: string) => void
  onBindLoginPasswordChange: (value: string) => void
  onBindLogin: () => void
  bindLoginTestIdPrefix: string
  bindLoginHintText: string

  totpCode: string
  onTotpCodeChange: (value: string) => void
  onSubmitTotpChallenge: () => void
  totpUserEmailMasked: string
  totpTestIdPrefix: string
}

/**
 * Presentation-only shell shared by the LinuxDo, OIDC and DingTalk callback
 * views. Every branch below mirrors the corresponding Vue template exactly.
 */
export function PendingOAuthCallbackPanel(props: PendingOAuthCallbackPanelProps) {
  const { t } = useTranslation()

  const showBody =
    props.needsInvitation ||
    props.needsAdoptionConfirmation ||
    props.needsChooser ||
    props.needsCreateAccount ||
    props.needsBindLogin ||
    props.needsTotpChallenge

  if (!showBody) return null

  return (
    <div className="space-y-4">
      {props.adoptionRequired && (props.suggestedDisplayName || props.suggestedAvatarUrl) && (
        <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
          <div className="space-y-3">
            <div className="space-y-1">
              <p className="text-sm font-medium text-gray-900 dark:text-white">
                {t('auth.oauthFlow.profileDetailsTitle', { providerName: props.providerName })}
              </p>
              <p className="text-xs surface-muted">
                {t('auth.oauthFlow.profileDetailsDescription', { providerName: props.providerName })}
              </p>
            </div>

            {props.suggestedDisplayName && (
              <label className="flex items-start gap-3 rounded-lg border border-gray-200 surface-inset p-3 text-sm dark:border-dark-600">
                <input
                  type="checkbox"
                  className="mt-1 h-4 w-4"
                  checked={props.adoptDisplayName}
                  onChange={(event) => props.onAdoptDisplayNameChange(event.target.checked)}
                />
                <span className="space-y-1">
                  <span className="block font-medium text-gray-900 dark:text-white">
                    {t('auth.oauthFlow.useDisplayName')}
                  </span>
                  <span className="block surface-muted">{props.suggestedDisplayName}</span>
                </span>
              </label>
            )}

            {props.suggestedAvatarUrl && (
              <label className="flex items-start gap-3 rounded-lg border border-gray-200 surface-inset p-3 text-sm dark:border-dark-600">
                <input
                  type="checkbox"
                  className="mt-1 h-4 w-4"
                  checked={props.adoptAvatar}
                  onChange={(event) => props.onAdoptAvatarChange(event.target.checked)}
                />
                <img
                  src={props.suggestedAvatarUrl}
                  alt={t('auth.oauthFlow.avatarAlt', { providerName: props.providerName })}
                  className="h-10 w-10 rounded-full border border-gray-200 object-cover dark:border-dark-600"
                />
                <span className="space-y-1">
                  <span className="block font-medium text-gray-900 dark:text-white">
                    {t('auth.oauthFlow.useAvatar')}
                  </span>
                  <span className="block break-all surface-muted">{props.suggestedAvatarUrl}</span>
                </span>
              </label>
            )}
          </div>
        </div>
      )}

      {props.needsInvitation && (
        <>
          <p className="text-sm surface-muted">{props.invitationRequiredText}</p>
          <div>
            <input
              value={props.invitationCode}
              type="text"
              className="input w-full"
              placeholder={t('auth.invitationCodePlaceholder')}
              disabled={props.isSubmitting}
              onChange={(event) => props.onInvitationCodeChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') props.onSubmitInvitation()
              }}
            />
          </div>
          <button
            className="btn btn-primary w-full"
            disabled={props.isSubmitting || !props.invitationCode.trim()}
            onClick={props.onSubmitInvitation}
          >
            {props.isSubmitting ? props.invitationCompletingText : props.invitationCompleteText}
          </button>
        </>
      )}

      {!props.needsInvitation && props.needsAdoptionConfirmation && (
        <>
          <p className="text-sm surface-muted">
            {t('auth.oauthFlow.reviewProfileBeforeContinue', { providerName: props.providerName })}
          </p>
          <button
            className="btn btn-primary w-full"
            disabled={props.isSubmitting}
            onClick={props.onContinueLogin}
          >
            {props.isSubmitting ? t('common.processing') : t('auth.continue')}
          </button>
        </>
      )}

      {!props.needsInvitation && !props.needsAdoptionConfirmation && props.needsChooser && (
        <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
          <div className="space-y-4">
            <div className="space-y-1">
              <p className="text-sm font-medium text-gray-900 dark:text-white">
                {t('auth.oauthFlow.chooseHowToContinue')}
              </p>
              <p className="text-xs surface-muted">
                {props.pendingAccountEmail
                  ? t('auth.oauthFlow.suggestedEmail', { email: props.pendingAccountEmail })
                  : t('auth.oauthFlow.chooseAccountActionHint')}
              </p>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <button
                className="btn btn-secondary w-full"
                disabled={props.isSubmitting}
                onClick={() => props.onSwitchToBindLogin()}
              >
                {t('auth.oauthFlow.bindExistingAccount')}
              </button>
              <button
                className="btn btn-primary w-full"
                disabled={props.isSubmitting}
                onClick={props.onSwitchToCreateAccount}
              >
                {t('auth.oauthFlow.createNewAccount')}
              </button>
            </div>
          </div>
        </div>
      )}

      {!props.needsInvitation && !props.needsAdoptionConfirmation && !props.needsChooser && props.needsCreateAccount && (
        <>
          <p className="text-sm surface-muted">{t('auth.oauthFlow.createAccountHint')}</p>
          <PendingOAuthCreateAccountForm
            testIdPrefix={props.createAccountTestIdPrefix}
            initialEmail={props.pendingAccountEmail}
            isSubmitting={props.isSubmitting}
            errorMessage={props.accountActionError}
            onSubmit={props.onCreateAccount}
            onSwitchToBind={props.onSwitchToBindLogin}
          />
        </>
      )}

      {!props.needsInvitation &&
        !props.needsAdoptionConfirmation &&
        !props.needsChooser &&
        !props.needsCreateAccount &&
        props.needsBindLogin && (
          <>
            <p className="text-sm surface-muted">{props.bindLoginHintText}</p>
            <div className="space-y-3">
              <input
                value={props.bindLoginEmail}
                data-testid={`${props.bindLoginTestIdPrefix}-bind-login-email`}
                type="email"
                className="input w-full"
                placeholder={t('auth.emailPlaceholder')}
                disabled={props.isSubmitting}
                onChange={(event) => props.onBindLoginEmailChange(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') props.onBindLogin()
                }}
              />
              <input
                value={props.bindLoginPassword}
                data-testid={`${props.bindLoginTestIdPrefix}-bind-login-password`}
                type="password"
                className="input w-full"
                placeholder={t('auth.passwordPlaceholder')}
                disabled={props.isSubmitting}
                onChange={(event) => props.onBindLoginPasswordChange(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') props.onBindLogin()
                }}
              />
              <button
                data-testid={`${props.bindLoginTestIdPrefix}-bind-login-submit`}
                className="btn btn-primary w-full"
                disabled={props.isSubmitting || !props.bindLoginEmail.trim() || !props.bindLoginPassword}
                onClick={props.onBindLogin}
              >
                {props.isSubmitting ? t('common.processing') : t('auth.oauthFlow.logInAndBind')}
              </button>
              {props.canReturnToCreateAccount && (
                <button
                  className="btn btn-secondary w-full"
                  disabled={props.isSubmitting}
                  onClick={props.onSwitchToCreateAccount}
                >
                  {t('auth.oauthFlow.useDifferentEmail')}
                </button>
              )}
            </div>
          </>
        )}

      {!props.needsInvitation &&
        !props.needsAdoptionConfirmation &&
        !props.needsChooser &&
        !props.needsCreateAccount &&
        !props.needsBindLogin &&
        props.needsTotpChallenge && (
          <>
            <p className="text-sm surface-muted">
              {t('auth.oauthFlow.totpHint', {
                providerName: props.providerName,
                account: props.totpUserEmailMasked || t('auth.oauthFlow.yourAccount')
              })}
            </p>
            <div className="space-y-3">
              <input
                value={props.totpCode}
                data-testid={`${props.totpTestIdPrefix}-bind-login-totp`}
                type="text"
                inputMode="numeric"
                maxLength={6}
                className="input w-full"
                placeholder="123456"
                disabled={props.isSubmitting}
                onChange={(event) => props.onTotpCodeChange(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') props.onSubmitTotpChallenge()
                }}
              />
              <button
                data-testid={`${props.totpTestIdPrefix}-bind-login-totp-submit`}
                className="btn btn-primary w-full"
                disabled={props.isSubmitting || props.totpCode.trim().length !== 6}
                onClick={props.onSubmitTotpChallenge}
              >
                {props.isSubmitting ? t('common.processing') : t('auth.oauthFlow.verifyAndContinue')}
              </button>
            </div>
          </>
        )}
    </div>
  )
}

export default PendingOAuthCallbackPanel
