//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestListCommercialModelMappingsUsesOneNarrowAuthorizedProjection(t *testing.T) {
	// The authorized/sale catalog contract in docs/COMPOSITE_GROUPS.md needs
	// configured public mappings only, with no account selection, credential
	// hydration or unrelated proxy/group association reads.
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newAccountRepositoryWithSQL(nil, db, nil)
	mock.ExpectQuery("commercial model mapping projection").
		WithArgs("{1,2}", service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "platform", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, `{"public":"private-target","invalid":42}`).
			AddRow(int64(2), service.PlatformOpenCodeGo, `{"public":"other-private-target"}`).
			AddRow(int64(2), service.PlatformOpenAI, `null`))
	out, err := repo.ListCommercialModelMappings(context.Background(), []int64{1, 2, 1})
	require.NoError(t, err)
	require.Equal(t, map[int64][]service.CommercialModelMapping{
		1: {{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"public": "private-target", "invalid": float64(42)}}},
		2: {{Platform: service.PlatformOpenCodeGo, ModelMapping: map[string]any{"public": "other-private-target"}}, {Platform: service.PlatformOpenAI}},
	}, out)
	require.NoError(t, mock.ExpectationsWereMet(), "non-empty mappings must not trigger account/proxy/group hydration")

	normalized := normalizeSQLWhitespace(capturedSQL)
	selectClause, _, found := strings.Cut(normalized, " FROM ")
	require.True(t, found)
	require.Contains(t, selectClause, "ag.group_id, a.platform")
	require.Contains(t, selectClause, "a.credentials->'model_mapping'")
	require.NotContains(t, selectClause, "a.credentials,")
	for _, unneeded := range []string{"a.*", "a.extra", "proxy_id", "access_token", "proxies", "JOIN groups"} {
		require.NotContains(t, normalized, unneeded)
	}
	require.Contains(t, normalized, "ag.group_id = ANY($1)")
	require.Contains(t, normalized, "a.deleted_at IS NULL")
	require.Contains(t, normalized, "a.status = $2")
	require.Contains(t, normalized, "a.schedulable = TRUE")
	for _, transient := range []string{"rate_limit_reset_at", "overload_until", "temp_unschedulable_until", "expires_at", "auto_pause_on_expired"} {
		require.NotContains(t, normalized, transient, "configured catalogs ignore transient state and expiry")
	}
}

func TestListCommercialModelMappingsEmptyAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
		err  error
	}{
		{name: "query", err: errors.New("database unavailable")},
		{name: "scan", rows: sqlmock.NewRows([]string{"group_id", "platform", "model_mapping"}).AddRow("not-an-id", service.PlatformOpenAI, `{}`)},
		{name: "decode", rows: sqlmock.NewRows([]string{"group_id", "platform", "model_mapping"}).AddRow(int64(1), service.PlatformOpenAI, `{`)},
		{name: "iteration", rows: sqlmock.NewRows([]string{"group_id", "platform", "model_mapping"}).AddRow(int64(1), service.PlatformOpenAI, `{}`).RowError(0, errors.New("read interrupted"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := newAccountRepositoryWithSQL(nil, db, nil)
			empty, err := repo.ListCommercialModelMappings(context.Background(), nil)
			require.NoError(t, err)
			require.Empty(t, empty)
			expectation := mock.ExpectQuery("SELECT ag.group_id, a.platform")
			if tc.err != nil {
				expectation.WillReturnError(tc.err)
			} else {
				expectation.WillReturnRows(tc.rows)
			}
			out, err := repo.ListCommercialModelMappings(context.Background(), []int64{1})
			require.Error(t, err)
			require.Nil(t, out, "a failed read must not return a partial directory")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
