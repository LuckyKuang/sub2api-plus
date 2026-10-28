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
	"github.com/stretchr/testify/require"
)

func TestProxySampleSQLRandomizesWholePoolBeforeLimit(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if !strings.Contains(actual, `"status"`) || !strings.Contains(actual, `"expires_at"`) {
			return fmt.Errorf("缺少可用池过滤: %s", actual)
		}
		if strings.Contains(actual, `"username"`) || strings.Contains(actual, `"password"`) {
			return fmt.Errorf("抽样不应查询认证字段: %s", actual)
		}
		if expected == "sample" {
			if !strings.Contains(actual, "ORDER BY RANDOM() LIMIT 50") || strings.Contains(actual, "OFFSET") || strings.Contains(actual, "ORDER BY \"proxies\".\"id\"") {
				return fmt.Errorf("未在全池随机后限制50: %s", actual)
			}
			if strings.Count(actual, "SELECT") != 1 {
				return fmt.Errorf("不得从预先截断的子查询抽样: %s", actual)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectQuery("count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20000))
	mock.ExpectQuery("sample").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "port", "status"}).AddRow(1234, "候选", "192.0.2.1", 8080, "active"))
	items, total, err := newProxyRepositoryWithSQL(client, db).SampleOptions(context.Background(), "region")
	require.NoError(t, err)
	require.EqualValues(t, 20000, total)
	require.Len(t, items, 1)
	require.EqualValues(t, 1234, items[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
