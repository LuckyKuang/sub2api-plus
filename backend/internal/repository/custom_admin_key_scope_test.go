//go:build unit

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	_ "github.com/LuckyKuang/sub2api-plus/ent/runtime"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/adminaccess"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScopedAdminAccountListScopesCountAndPageInSQL(t *testing.T) {
	var countScope string
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if !strings.Contains(actual, `"type"`) || !strings.Contains(actual, `"account_groups"`) || !strings.Contains(actual, " OR ") {
			return fmt.Errorf("查询缺少完整规则: %s", actual)
		}
		where := actual[strings.Index(actual, " WHERE "):]
		if i := strings.Index(where, " ORDER BY "); i >= 0 {
			where = where[:i]
		}
		if i := strings.Index(where, " LIMIT "); i >= 0 {
			where = where[:i]
		}
		if expected == "count" {
			countScope = where
		} else {
			if where != countScope {
				return fmt.Errorf("分页和统计范围不同")
			}
			// 允许模型映射子字段投影，禁止读取完整 credentials。
			projected := strings.ReplaceAll(actual, `"accounts"."credentials"->'model_mapping' AS model_mapping`, "model_mapping")
			if strings.Contains(projected, `"credentials"`) || strings.Contains(projected, `"extra"`) {
				return fmt.Errorf("读取了敏感字段")
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	repo := newAccountRepositoryWithSQL(client, db, nil)
	mock.ExpectQuery("count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("page").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	principal := &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{
		{Permissions: []string{"accounts.read"}, GroupIDs: []int64{1}, AccountTypes: []string{"oauth"}},
		{Permissions: []string{"accounts.read"}, GroupIDs: []int64{2}, AccountTypes: []string{"apikey"}},
	}}}
	_, total, err := repo.ScopedAdminList(adminaccess.WithPrincipal(context.Background(), principal), pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatal(total)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScopedAdminCodexCandidatesQueryIsBounded(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		where := actual[strings.Index(actual, " WHERE "):]
		if !strings.Contains(where, "credentials") || !strings.Contains(actual, " LIMIT ") {
			return fmt.Errorf("导入候选未按身份筛选并限制数量: %s", actual)
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	repo := newAccountRepositoryWithSQL(client, db, nil)
	ctx := adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.credentials"}, AccountTypes: []string{"oauth"}, AllGroups: true, IncludeUngrouped: true}}}})
	mock.ExpectQuery("候选身份").WillReturnRows(sqlmock.NewRows([]string{"id", "credentials"}))
	_, err = repo.ScopedAdminCodexCandidates(ctx, service.ScopedCodexIdentity{AccessToken: "inside", AccessTokenOnly: true})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
