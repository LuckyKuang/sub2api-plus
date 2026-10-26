package service

import (
	"context"
)

type grokTokenOwnerKey struct{}

// A request may advance its token only through an issuer rotation of this
// captured grant, or an access-token-only rotation of the same refresh grant.
type grokTokenOwner struct {
	account      *Account
	requestOwner *Account
}

func captureGrokTokenOwner(ctx context.Context, account *Account) (context.Context, *Account, *grokTokenOwner, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if owner := grokTokenOwnerFromContext(ctx); owner != nil {
		if err := owner.validate(account); err != nil {
			return ctx, nil, nil, err
		}
		return ctx, owner.account, owner, nil
	}
	captured, err := CaptureOAuthCredentialOwner(account)
	if err != nil {
		return ctx, nil, nil, err
	}
	if _, err := captured.StrictProxyURL(); err != nil {
		return ctx, nil, nil, NewProviderCredentialFailure("grok_oauth_proxy_preparation_unavailable", err)
	}
	owner := &grokTokenOwner{account: captured, requestOwner: cnControlRequestOwner(ctx, nil)}
	return context.WithValue(ctx, grokTokenOwnerKey{}, owner), captured, owner, nil
}

func grokTokenOwnerFromContext(ctx context.Context) *grokTokenOwner {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(grokTokenOwnerKey{}).(*grokTokenOwner)
	return owner
}

func (o *grokTokenOwner) validate(account *Account) error {
	expected, expectedErr := grokRequestOwnerFingerprint(o.account)
	actual, actualErr := grokRequestOwnerFingerprint(account)
	if expectedErr != nil || actualErr != nil || expected != actual {
		return errOAuthRefreshAccountStateChanged
	}
	return nil
}

func grokTokenRotationFingerprint(account *Account, issuerRotation bool) (string, error) {
	owner, err := CaptureOAuthCredentialOwner(account)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"access_token", "expires_at", "_token_version", "id_token", "subscription_tier", "entitlement_status", "token_type"} {
		delete(owner.Credentials, key)
	}
	if issuerRotation {
		for _, key := range []string{"refresh_token", "scope", "email"} {
			delete(owner.Credentials, key)
		}
	}
	return grokRequestOwnerFingerprint(owner)
}

func (o *grokTokenOwner) validateRotation(account *Account, issuerRotation bool) error {
	expected, expectedErr := grokTokenRotationFingerprint(o.account, issuerRotation)
	actual, actualErr := grokTokenRotationFingerprint(account, issuerRotation)
	if expectedErr != nil || actualErr != nil || expected != actual {
		return errOAuthRefreshAccountStateChanged
	}
	return nil
}

func (o *grokTokenOwner) adopt(account *Account, issuerRotation bool) error {
	if err := o.validate(account); err != nil {
		if err := o.validateRotation(account, issuerRotation); err != nil {
			return err
		}
		if !issuerRotation && account.GetCredentialAsInt64("_token_version") <= o.account.GetCredentialAsInt64("_token_version") {
			return errOAuthRefreshAccountStateChanged
		}
	}
	owner, err := CaptureOAuthCredentialOwner(account)
	if err != nil {
		return errOAuthRefreshAccountStateChanged
	}
	o.account = owner
	// Only a sender-owned snapshot is advanced. A live repository/cache object
	// passed to the token provider is never mutated as an adoption side effect.
	if o.requestOwner != nil {
		*o.requestOwner = *owner
	}
	return nil
}
