package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type providerFilesRepository struct{ db *sql.DB }

func NewProviderFilesRepository(db *sql.DB) service.ProviderFilesRepository {
	return &providerFilesRepository{db: db}
}

const providerFileColumns = `public_id,user_id,api_key_id,group_id,platform,account_id,principal,remote_id,state,filename,mime_type,purpose,bytes,content_digest,idempotency_digest,parameter_digest,created_at,expires_at,COALESCE(mutation_token,''),request_snapshot_digest`
const providerFileInsertColumns = `public_id,user_id,api_key_id,group_id,platform,account_id,principal,remote_id,state,filename,mime_type,purpose,bytes,content_digest,idempotency_digest,parameter_digest,created_at,expires_at,mutation_token,request_snapshot_digest`

func scanProviderFile(row interface{ Scan(...any) error }) (*service.ProviderFile, error) {
	f := new(service.ProviderFile)
	var expiry sql.NullTime
	err := row.Scan(&f.PublicID, &f.UserID, &f.APIKeyID, &f.GroupID, &f.Platform, &f.AccountID, &f.Principal, &f.RemoteID, &f.State, &f.Filename, &f.MIMEType, &f.Purpose, &f.Bytes, &f.ContentDigest, &f.IdempotencyDigest, &f.ParameterDigest, &f.CreatedAt, &expiry, &f.MutationToken, &f.RequestSnapshotDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrProviderFileNotFound
	}
	if err != nil {
		return nil, err
	}
	if expiry.Valid {
		f.ExpiresAt = expiry.Time
	}
	return f, nil
}

// Locking the account and its saved proxy closes the read/publish race against
// credential replacement, proxy edits and deletions. No secrets are persisted in
// provider_files. Account row creation also fences deletion/recreation.
func providerFileCheckOwner(ctx context.Context, tx *sql.Tx, expected *service.Account) error {
	if expected == nil {
		return service.ErrProviderFileOwnerChanged
	}
	if _, err := expected.StrictProxyURL(); err != nil {
		return service.ErrProviderFileOwnerChanged
	}
	current := new(service.Account)
	// Proxy editors and rebinding workflows acquire the proxy first. Preserve
	// that order so an uploader waiting for their proxy cannot retain the
	// account lock the editor needs to commit its invalidation.
	if expected.ProxyID != nil {
		p := new(service.Proxy)
		err := tx.QueryRowContext(ctx, `SELECT id,protocol,host,port,COALESCE(username,''),COALESCE(password,''),status,expires_at FROM proxies WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *expected.ProxyID).Scan(&p.ID, &p.Protocol, &p.Host, &p.Port, &p.Username, &p.Password, &p.Status, &p.ExpiresAt)
		if err != nil {
			return service.ErrProviderFileOwnerChanged
		}
		current.Proxy = p
	}
	var credentials []byte
	var proxyID sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id,platform,type,credentials,proxy_id,created_at,status FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, expected.ID).Scan(&current.ID, &current.Platform, &current.Type, &credentials, &proxyID, &current.CreatedAt, &current.Status)
	if err != nil {
		return service.ErrProviderFileOwnerChanged
	}
	if json.Unmarshal(credentials, &current.Credentials) != nil {
		return service.ErrProviderFileOwnerChanged
	}
	if proxyID.Valid {
		current.ProxyID = &proxyID.Int64
	}
	if !service.ProviderFileSnapshotMatches(expected, current) {
		return service.ErrProviderFileOwnerChanged
	}
	return nil
}

func (r *providerFilesRepository) Reserve(ctx context.Context, f *service.ProviderFile, expected *service.Account) (*service.ProviderFile, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize per user, including uploads from another key/group/instance.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('sub2api:provider-files:' || $1::text,0))`, f.UserID); err != nil {
		return nil, false, err
	}
	old, err := scanProviderFile(tx.QueryRowContext(ctx, `SELECT `+providerFileColumns+` FROM provider_files WHERE idempotency_digest=$1`, f.IdempotencyDigest))
	if err == nil {
		if err = providerFileCheckOwner(ctx, tx, expected); err != nil {
			return nil, false, err
		}
		return old, false, tx.Commit()
	}
	if !errors.Is(err, service.ErrProviderFileNotFound) {
		return nil, false, err
	}
	if err = providerFileCheckOwner(ctx, tx, expected); err != nil {
		return nil, false, err
	}
	var count, total int64
	err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(bytes),0) FROM provider_files WHERE user_id=$1 AND state<>'deleted' AND (expires_at IS NULL OR expires_at>NOW())`, f.UserID).Scan(&count, &total)
	if err != nil {
		return nil, false, err
	}
	if count >= 10000 || total+f.Bytes > 25<<30 {
		return nil, false, service.ErrProviderFileQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_files (`+providerFileInsertColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,NULL,$19)`, f.PublicID, f.UserID, f.APIKeyID, f.GroupID, f.Platform, f.AccountID, f.Principal, f.RemoteID, f.State, f.Filename, f.MIMEType, f.Purpose, f.Bytes, f.ContentDigest, f.IdempotencyDigest, f.ParameterDigest, f.CreatedAt, nullableProviderFileTime(f.ExpiresAt), f.RequestSnapshotDigest)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return f, true, nil
}

func nullableProviderFileTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (r *providerFilesRepository) Get(ctx context.Context, id string) (*service.ProviderFile, error) {
	return scanProviderFile(r.db.QueryRowContext(ctx, `SELECT `+providerFileColumns+` FROM provider_files WHERE public_id=$1`, id))
}

func (r *providerFilesRepository) GetByIdempotency(ctx context.Context, digest string) (*service.ProviderFile, error) {
	return scanProviderFile(r.db.QueryRowContext(ctx, `SELECT `+providerFileColumns+` FROM provider_files WHERE idempotency_digest=$1`, digest))
}

func (r *providerFilesRepository) List(ctx context.Context, scope service.ProviderFileScope, after, before string, limit int) ([]*service.ProviderFile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+providerFileColumns+` FROM provider_files f
 WHERE user_id=$1 AND api_key_id=$2 AND group_id=$3 AND platform=$4 AND state='ready' AND (expires_at IS NULL OR expires_at>NOW())
 AND ($5='' OR (created_at,public_id)>(SELECT created_at,public_id FROM provider_files WHERE public_id=$5 AND user_id=$1 AND api_key_id=$2 AND group_id=$3 AND platform=$4 AND state='ready' AND (expires_at IS NULL OR expires_at>NOW())))
 AND ($6='' OR (created_at,public_id)<(SELECT created_at,public_id FROM provider_files WHERE public_id=$6 AND user_id=$1 AND api_key_id=$2 AND group_id=$3 AND platform=$4 AND state='ready' AND (expires_at IS NULL OR expires_at>NOW())))
 ORDER BY created_at,public_id LIMIT $7`, scope.UserID, scope.APIKeyID, scope.GroupID, scope.Platform, after, before, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	files := make([]*service.ProviderFile, 0)
	for rows.Next() {
		f, err := scanProviderFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *providerFilesRepository) Publish(ctx context.Context, f *service.ProviderFile, expected *service.Account) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = providerFileCheckOwner(ctx, tx, expected); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE provider_files SET remote_id=$2,state='ready',updated_at=NOW() WHERE public_id=$1 AND state='uploading' AND account_id=$3 AND principal=$4`, f.PublicID, f.RemoteID, f.AccountID, f.Principal)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrProviderFileConflict
	}
	return tx.Commit()
}

func (r *providerFilesRepository) SetState(ctx context.Context, id, state, remote string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE provider_files SET state=$2,remote_id=CASE WHEN $3<>'' THEN $3 ELSE remote_id END,updated_at=NOW() WHERE public_id=$1 AND state IN ('uploading','uncertain')`, id, state, remote)
	return err
}

func (r *providerFilesRepository) BeginDelete(ctx context.Context, id, token, snapshot string) (*service.ProviderFile, bool, error) {
	f, err := scanProviderFile(r.db.QueryRowContext(ctx, `UPDATE provider_files SET state='delete_pending',mutation_token=$2,mutation_until=NOW()+INTERVAL '45 seconds',request_snapshot_digest=CASE WHEN $3<>'' THEN $3 ELSE request_snapshot_digest END,updated_at=NOW()
 WHERE public_id=$1 AND (state IN ('ready','delete_pending','revoked') OR (state='uncertain' AND remote_id<>'')) AND (mutation_token IS NULL OR mutation_until<NOW()) RETURNING `+providerFileColumns, id, token, snapshot))
	if errors.Is(err, service.ErrProviderFileNotFound) {
		return nil, false, nil
	}
	return f, err == nil, err
}

func (r *providerFilesRepository) ListCleanup(ctx context.Context, limit int) ([]*service.ProviderFile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+providerFileColumns+` FROM provider_files WHERE platform='deepseek' AND remote_id<>'' AND request_snapshot_digest<>'' AND state IN ('uncertain','delete_pending') AND updated_at<NOW()-INTERVAL '1 minute' AND (mutation_token IS NULL OR mutation_until<NOW()) ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	files := make([]*service.ProviderFile, 0)
	for rows.Next() {
		f, err := scanProviderFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *providerFilesRepository) FinishDelete(ctx context.Context, id, token string, success bool) error {
	state := service.ProviderFileDeletePending
	if success {
		state = service.ProviderFileDeleted
	}
	result, err := r.db.ExecContext(ctx, `UPDATE provider_files SET state=CASE WHEN platform='minimax' THEN 'revoked' ELSE $3 END,mutation_token=NULL,mutation_until=NULL,updated_at=NOW() WHERE public_id=$1 AND mutation_token=$2`, id, token, state)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrProviderFileConflict
	}
	return nil
}
