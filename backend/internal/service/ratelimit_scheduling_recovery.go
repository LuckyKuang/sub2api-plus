package service

import (
	"context"
	"errors"
	"log/slog"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
)

func (s *RateLimitService) prepareSchedulingRecoveryPublication(accountID int64) func(*Account) {
	if s.runtimeBlocker == nil {
		return nil
	}
	if preparer, ok := s.runtimeBlocker.(RuntimeRecoveryGenerationClearer); ok {
		return preparer.PrepareRuntimeRecoveryGenerationClear(accountID)
	}
	return func(*Account) { s.runtimeBlocker.ClearAccountSchedulingBlock(accountID) }
}

func publishSchedulingRecoveryAfterCommit(ctx context.Context, publish func()) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		tx.OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(commitCtx context.Context, tx *dbent.Tx) error {
				if err := next.Commit(commitCtx, tx); err != nil {
					return err
				}
				publish()
				return nil
			})
		})
		return
	}
	publish()
}

func (s *RateLimitService) publishSchedulingRecovery(ctx context.Context, account *Account, clearTempCache, clearCounters, invalidateToken bool, runtime func(*Account)) {
	publishSchedulingRecoveryAfterCommit(ctx, func() {
		if clearTempCache && s.tempUnschedCache != nil {
			if err := s.tempUnschedCache.DeleteTempUnsched(ctx, account.ID); err != nil {
				slog.Warn("temp_unsched_cache_delete_failed", "account_id", account.ID, "error", err)
			}
		}
		if clearCounters {
			s.ResetOpenAI403Counter(ctx, account.ID)
		}
		if runtime != nil {
			runtime(account)
		}
		if invalidateToken && s.tokenCacheInvalidator != nil && account.IsOAuth() {
			if err := s.tokenCacheInvalidator.InvalidateToken(ctx, account); err != nil {
				slog.Warn("recover_account_state_invalidate_token_failed", "account_id", account.ID, "error", err)
			}
		}
	})
}

func (s *RateLimitService) clearAccountSchedulingRecovery(ctx context.Context, accountID int64, mode SchedulingRecoveryMode) error {
	writer, ok := s.accountRepo.(OpenAIOAuthPolicyTransactionRepository)
	if !ok {
		return errors.New("scheduling recovery requires an atomic account transaction")
	}
	runtime := s.prepareSchedulingRecoveryPublication(accountID)
	var updated *Account
	err := writer.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repo AccountRepository) error {
		recovery, ok := repo.(AtomicSchedulingRecoveryRepository)
		if !ok {
			return errors.New("atomic scheduling recovery is unavailable")
		}
		if err := recovery.ClearAccountSchedulingRecoveryState(writeCtx, accountID, mode); err != nil {
			return err
		}
		var err error
		updated, err = repo.GetByID(writeCtx, accountID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishSchedulingRecovery(ctx, updated, true, mode == SchedulingRecoveryRates, false, runtime)
	return nil
}

func (s *RateLimitService) recoverAccountStateAtomically(ctx context.Context, accountID int64, options AccountRecoveryOptions) (*SuccessfulTestRecoveryResult, error) {
	writer, ok := s.accountRepo.(OpenAIOAuthPolicyTransactionRepository)
	if !ok {
		return nil, errors.New("account recovery requires an atomic account transaction")
	}
	runtime := s.prepareSchedulingRecoveryPublication(accountID)
	result := &SuccessfulTestRecoveryResult{}
	var updated *Account
	err := writer.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repo AccountRepository) error {
		locker, ok := repo.(AccountCredentialFamilyLocker)
		if !ok {
			return errors.New("account recovery requires saved-state locking")
		}
		if err := locker.LockAccountCredentialFamily(writeCtx, accountID); err != nil {
			return err
		}
		account, err := repo.GetByID(writeCtx, accountID)
		if err != nil {
			return err
		}
		result.ClearedError = account.Status == StatusError
		result.ClearedRateLimit = hasRecoverableRuntimeState(account)
		if !result.ClearedError && !result.ClearedRateLimit {
			return nil
		}
		if result.ClearedError {
			recovery, ok := repo.(AccountRecoveryRepository)
			if !ok {
				return errors.New("atomic account recovery is unavailable")
			}
			if err := recovery.ClearAccountRecoveryState(writeCtx, accountID); err != nil {
				return err
			}
		} else {
			recovery, ok := repo.(AtomicSchedulingRecoveryRepository)
			if !ok {
				return errors.New("atomic scheduling recovery is unavailable")
			}
			if err := recovery.ClearAccountSchedulingRecoveryState(writeCtx, accountID, SchedulingRecoveryRates); err != nil {
				return err
			}
		}
		updated, err = repo.GetByID(writeCtx, accountID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if updated != nil {
		s.publishSchedulingRecovery(ctx, updated, result.ClearedRateLimit, true, options.InvalidateToken && result.ClearedError, runtime)
	}
	return result, nil
}
