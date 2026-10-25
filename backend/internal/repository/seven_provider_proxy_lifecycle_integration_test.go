//go:build integration

package repository

import (
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// Saved binding changes, not network errors, authorize route changes. Exercise
// the real repository APIs and hydrated reads for both account types on every
// supported provider, including equal-URL but distinct proxy records.
func (s *ProxyRepoSuite) TestSevenProviderProxyBindingLifecycle() {
	first := s.mustCreateProxy(&service.Proxy{Name: "lifecycle-first", Protocol: "http", Host: "same-route.invalid", Port: 8080, Status: service.StatusActive})
	second := s.mustCreateProxy(&service.Proxy{Name: "lifecycle-second", Protocol: "http", Host: "same-route.invalid", Port: 8080, Status: service.StatusActive})
	accounts := newAccountRepositoryWithSQL(s.tx.Client(), s.tx, nil)
	var ids []int64
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
			account := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{
				Name: "proxy-lifecycle-" + platform + kind, Platform: platform, Type: kind,
				Status: service.StatusActive, Schedulable: true,
				Credentials: map[string]any{"access_token": "lifecycle-oauth", "api_key": "lifecycle-api-key"},
				Extra:       map[string]any{"unrelated": "preserved"},
			})
			ids = append(ids, account.ID)
			loaded, err := accounts.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			s.Require().Nil(loaded.ProxyID)
			s.Require().Nil(loaded.Proxy)
			loaded.ProxyID = &first.ID
			s.Require().NoError(accounts.Update(s.ctx, loaded), "first proxy binding is a saved account mutation")
			bound, err := accounts.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			s.Require().Equal(&first.ID, bound.ProxyID)
			s.Require().NotNil(bound.Proxy)
			s.Require().Equal(first.ID, bound.Proxy.ID)
			s.Require().Equal(loaded.Credentials, bound.Credentials)
		}
	}
	untouched := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "lifecycle-unrelated", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, ProxyID: &first.ID})
	changed, err := accounts.BulkUpdate(s.ctx, ids, service.AccountBulkUpdate{ProxyID: &second.ID})
	s.Require().NoError(err)
	s.Require().EqualValues(len(ids), changed)
	for _, id := range ids {
		loaded, err := accounts.GetByID(s.ctx, id)
		s.Require().NoError(err)
		s.Require().Equal(&second.ID, loaded.ProxyID, "same URL never erases proxy record ownership")
		s.Require().Equal(second.ID, loaded.Proxy.ID)
		s.Require().Equal("preserved", loaded.Extra["unrelated"])
	}
	// A proxy edited while idle must be reflected in the next hydrated operation.
	second.Host, second.Port, second.Username, second.Password = "edited-route.invalid", 9080, "edited-user", "edited-password"
	s.Require().NoError(s.repo.Update(s.ctx, second))
	for _, id := range ids {
		loaded, err := accounts.GetByID(s.ctx, id)
		s.Require().NoError(err)
		s.Require().Equal("http://edited-user:edited-password@edited-route.invalid:9080", loaded.Proxy.URL())
	}
	zero := int64(0)
	changed, err = accounts.BulkUpdate(s.ctx, ids, service.AccountBulkUpdate{ProxyID: &zero})
	s.Require().NoError(err)
	s.Require().EqualValues(len(ids), changed)
	for _, id := range ids {
		loaded, err := accounts.GetByID(s.ctx, id)
		s.Require().NoError(err)
		s.Require().Nil(loaded.ProxyID, "only an explicit saved unbinding authorizes direct egress")
		s.Require().Nil(loaded.Proxy)
		s.Require().Equal("preserved", loaded.Extra["unrelated"])
	}
	other, err := accounts.GetByID(s.ctx, untouched.ID)
	s.Require().NoError(err)
	s.Require().Equal(&first.ID, other.ProxyID, "bulk proxy edits must not touch an unrelated account")
	s.Require().Equal("same-route.invalid", other.Proxy.Host)
}
