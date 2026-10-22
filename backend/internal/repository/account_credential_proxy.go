package repository

import (
	"errors"
	"fmt"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func validateCredentialMutationOwner(owner *service.Account) error {
	if owner == nil || owner.ID <= 0 || owner.Platform == "" || owner.Type == "" || owner.IsShadow() || owner.CredentialGeneration != "" || owner.CNQuotaGeneration != "" {
		return errors.New("credential mutation requires a hydrated owner snapshot")
	}
	return nil
}

func validateGrokOAuthCredentialMutationOwner(owner *service.Account) error {
	if err := validateCredentialMutationOwner(owner); err != nil {
		return err
	}
	if owner.Platform != service.PlatformGrok || owner.Type != service.AccountTypeOAuth {
		return errors.New("grok credential mutation requires a Grok OAuth owner")
	}
	return nil
}

// Lock the exact observed proxy row until the account mutation commits. A plain
// EXISTS can keep seeing an old statement snapshot while a repaired proxy wins
// a concurrent update. Missing hydration is not evidence of an absent proxy.
func credentialMutationProxyPredicate(proxyID *int64, proxy *service.Proxy, proxyParameter int, args []any) (string, []any, error) {
	if proxyID == nil {
		if proxy != nil {
			return "", nil, errors.New("credential proxy snapshot does not match its binding")
		}
		return "TRUE", args, nil
	}
	if proxy == nil || proxy.ID != *proxyID {
		return "", nil, errors.New("credential mutation requires the observed hydrated proxy")
	}
	first := len(args) + 1
	args = append(args, proxy.Protocol, proxy.Host, proxy.Port, proxy.Username, proxy.Password, proxy.Status)
	predicate := fmt.Sprintf("EXISTS (SELECT p.id FROM proxies p WHERE p.id = $%d AND p.deleted_at IS NULL AND p.protocol = $%d AND p.host = $%d AND p.port = $%d AND COALESCE(p.username, '') = $%d AND COALESCE(p.password, '') = $%d AND p.status = $%d FOR SHARE)", proxyParameter, first, first+1, first+2, first+3, first+4, first+5)
	return predicate, args, nil
}

func grokCredentialMutationProxyPredicate(snapshot service.GrokCredentialMutationSnapshot, proxyParameter int, args []any, missingProxyFailure bool) (string, []any, error) {
	if snapshot.ProxyMissing {
		if !missingProxyFailure || snapshot.ProxyID == nil || snapshot.Proxy != nil {
			return "", nil, errors.New("credential mutation requires explicit missing-proxy failure evidence")
		}
		// A configured missing proxy is a soft-deleted row (the account FK
		// clears bindings on physical deletion). Lock that invalid state so a
		// simultaneous restoration cannot be overwritten by stale quarantine.
		return fmt.Sprintf("EXISTS (SELECT p.id FROM proxies p WHERE p.id = $%d AND p.deleted_at IS NOT NULL FOR SHARE)", proxyParameter), args, nil
	}
	return credentialMutationProxyPredicate(snapshot.ProxyID, snapshot.Proxy, proxyParameter, args)
}
