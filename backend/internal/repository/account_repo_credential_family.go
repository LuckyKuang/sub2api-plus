package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.AccountCredentialFamilyLocker = (*accountRepository)(nil)

// A soft-deleted parent still satisfies the SQL foreign key. Its live row must
// remain locked until child creation commits, so a concurrent family deletion
// either sees the new child or prevents the creation after its own commit.
func lockLiveAccountCredentialParent(ctx context.Context, exec sqlExecutor, parentID int64) error {
	rows, err := exec.QueryContext(ctx, `/* account_live_credential_parent_lock */
		SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, parentID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrAccountNotFound
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return err
	}
	return rows.Err()
}

func (r *accountRepository) LockAccountCredentialFamily(ctx context.Context, accountID int64) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return errors.New("credential family locking requires an account transaction")
	}
	return lockLiveAccountCredentialParent(ctx, tx.Client(), accountID)
}

func lockLiveSparkCredentialParent(ctx context.Context, exec sqlExecutor, parentID int64) error {
	if err := lockLiveAccountCredentialParent(ctx, exec, parentID); err != nil {
		return err
	}
	// Read after acquiring the live parent lock. A type conversion committed
	// while this creation waited must be visible before the child is inserted.
	rows, err := exec.QueryContext(ctx, `SELECT platform,type,parent_account_id FROM accounts WHERE id=$1 AND deleted_at IS NULL`, parentID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrAccountNotFound
	}
	var platform, accountType string
	var credentialParent sql.NullInt64
	if err := rows.Scan(&platform, &accountType, &credentialParent); err != nil {
		return err
	}
	if platform != service.PlatformOpenAI || accountType != service.AccountTypeOAuth || credentialParent.Valid {
		return infraerrors.BadRequest("SPARK_SHADOW_INVALID_PARENT", "spark shadow requires a real OpenAI OAuth credential-owning parent account")
	}
	return rows.Err()
}
