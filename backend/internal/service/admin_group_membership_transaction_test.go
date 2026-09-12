//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/stretchr/testify/require"
)

type groupMembershipTransactionRepo struct {
	*groupRepoStubForAdmin
	t           *testing.T
	tx          *dbent.Tx
	bindFailure error
}

func (r *groupMembershipTransactionRepo) checkTransaction(ctx context.Context) {
	r.t.Helper()
	tx := dbent.TxFromContext(ctx)
	require.NotNil(r.t, tx, "all group and member writes must use a transaction")
	if r.tx == nil {
		r.tx = tx
	}
	require.Same(r.t, r.tx, tx)
}

func (r *groupMembershipTransactionRepo) Create(ctx context.Context, group *Group) error {
	r.checkTransaction(ctx)
	return r.groupRepoStubForAdmin.Create(ctx, group)
}

func (r *groupMembershipTransactionRepo) Update(ctx context.Context, group *Group) error {
	r.checkTransaction(ctx)
	return r.groupRepoStubForAdmin.Update(ctx, group)
}

func (r *groupMembershipTransactionRepo) DeleteAccountGroupsByGroupID(ctx context.Context, _ int64) (int64, error) {
	r.checkTransaction(ctx)
	return 1, nil
}

func (r *groupMembershipTransactionRepo) BindAccountsToGroup(ctx context.Context, _ int64, _ []int64) error {
	r.checkTransaction(ctx)
	return r.bindFailure
}

func TestAdminGroupMembershipWritesCommitOrRollbackTogether(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, fail := range []bool{false, true} {
			name := operation + "/commit"
			if fail {
				name = operation + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer func() { _ = db.Close() }()
				client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT .*accounts.*ORDER BY .*id.*FOR UPDATE`).WithArgs(int64(42)).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				if fail {
					mock.ExpectRollback()
				} else {
					mock.ExpectCommit()
				}
				source := &Account{ID: 42, Name: "OAuth source", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				existing := &Group{ID: 7, Name: "Follow reset", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1, QuotaResetSourceAccountID: &source.ID}
				repo := &groupMembershipTransactionRepo{t: t, groupRepoStubForAdmin: &groupRepoStubForAdmin{
					createID: 7, getByID: existing,
					getByIDByID:               map[int64]*Group{7: existing, 99: {ID: 99, Platform: PlatformOpenAI}},
					getAccountIDsByGroupIDsFn: func([]int64) ([]int64, error) { return []int64{42}, nil },
				}}
				if fail {
					repo.bindFailure = errors.New("copy failed")
				}
				svc := &adminServiceImpl{groupRepo: repo, accountRepo: &quotaResetAccountRepoStub{account: source}, entClient: client}
				if operation == "create" {
					_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Follow reset", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1, CopyAccountsFromGroupIDs: []int64{99}, QuotaResetSourceAccountID: &source.ID})
				} else {
					_, err = svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{CopyAccountsFromGroupIDs: []int64{99}})
				}
				if fail {
					require.ErrorIs(t, err, repo.bindFailure)
				} else {
					require.NoError(t, err)
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
