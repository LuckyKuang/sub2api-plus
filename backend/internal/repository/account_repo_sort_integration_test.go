//go:build integration

package repository

import (
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func (s *AccountRepoSuite) TestList_DefaultSortByNameAsc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "z-account"})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "a-account"})

	accounts, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10})
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("a-account", accounts[0].Name)
	s.Require().Equal("z-account", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByPriorityDesc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "low-priority", Priority: 10})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "high-priority", Priority: 90})

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "priority",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("high-priority", accounts[0].Name)
	s.Require().Equal("low-priority", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByRateMultiplier() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "high-rate", RateMultiplier: floatPtr(0.8)})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "low-rate", RateMultiplier: floatPtr(0.03)})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "missing-rate"})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "unsupported-with-retained-rate", RateMultiplier: floatPtr(0.01)})

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{order: "asc", want: []string{"unsupported-with-retained-rate", "low-rate", "high-rate", "missing-rate"}},
		{order: "desc", want: []string{"missing-rate", "high-rate", "low-rate", "unsupported-with-retained-rate"}},
	} {
		accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
			Page: 1, PageSize: 10, SortBy: "rate_multiplier", SortOrder: tc.order,
		}, "", "", "", "", 0, "")
		s.Require().NoError(err)
		s.Require().Len(accounts, 4)
		for i, name := range tc.want {
			s.Require().Equal(name, accounts[i].Name)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }
