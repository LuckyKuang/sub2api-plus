package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestCreateManualIPAccessRuleUsesIndependentTypedArguments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	actorID := int64(7)
	rule := &service.IPAccessRule{
		IPOrCIDR:        "192.0.2.0/24",
		RuleKind:        service.IPAccessRuleKindManualBlock,
		Reason:          "test",
		CreatedByUserID: &actorID,
	}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE ip_access_rules")).
		WithArgs(rule.IPOrCIDR, string(rule.RuleKind)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ip_access_rules")).
		WithArgs(rule.IPOrCIDR, string(rule.RuleKind), rule.Reason, sqlmock.AnyArg(), nil, actorID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ip_or_cidr", "rule_kind", "status", "reason", "failure_count",
			"first_failed_at", "last_failed_at", "blocked_at", "expires_at", "last_seen_at", "hit_count",
			"created_by_user_id", "released_by_user_id", "released_at", "created_at", "updated_at",
		}).AddRow(
			int64(1), rule.IPOrCIDR, string(rule.RuleKind), string(service.IPAccessRuleStatusActive), rule.Reason, 0,
			nil, nil, now, nil, nil, int64(0),
			actorID, nil, nil, now, now,
		))
	mock.ExpectCommit()

	repo := NewIPAccessControlRepository(db)
	created, err := repo.CreateManualIPAccessRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("create manual rule: %v", err)
	}
	if created == nil || created.IPOrCIDR != rule.IPOrCIDR || created.BlockedAt == nil {
		t.Fatalf("unexpected created rule: %#v", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestListActiveIPAccessRulesDoesNotWriteOnRequestPath(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + ipAccessRuleColumns + " FROM ip_access_rules")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ip_or_cidr", "rule_kind", "status", "reason", "failure_count",
			"first_failed_at", "last_failed_at", "blocked_at", "expires_at", "last_seen_at", "hit_count",
			"created_by_user_id", "released_by_user_id", "released_at", "created_at", "updated_at",
		}))

	repo := NewIPAccessControlRepository(db)
	rules, err := repo.ListActiveIPAccessRules(context.Background())
	if err != nil {
		t.Fatalf("list active rules: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules, got %#v", rules)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("hot path performed an unexpected database operation: %v", err)
	}
}

func TestListIPLoginFailureStatesReturnsCurrentWindowWithBlockStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	window := 15 * time.Minute
	windowSeconds := int64(window.Seconds())
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM ip_login_failure_states WHERE")).
		WithArgs(windowSeconds, "%203.0.113%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT\ns.normalized_ip,")).
		WithArgs(windowSeconds, "%203.0.113%", 25, 25).
		WillReturnRows(sqlmock.NewRows([]string{
			"normalized_ip", "failure_count", "window_started_at", "last_failed_at",
			"window_expires_at", "currently_blocked", "auto_block_rule_id",
		}).AddRow(
			"203.0.113.8", 4, now.Add(-5*time.Minute), now.Add(-time.Minute),
			now.Add(10*time.Minute), true, int64(42),
		))

	repo := NewIPAccessControlRepository(db)
	list, err := repo.ListIPLoginFailureStates(context.Background(), service.IPLoginFailureStateFilter{
		Page: 2, PageSize: 25, Query: "203.0.113",
	}, window)
	if err != nil {
		t.Fatalf("list failure states: %v", err)
	}
	if list.Total != 1 || list.Page != 2 || list.PageSize != 25 || len(list.Items) != 1 {
		t.Fatalf("unexpected list metadata: %#v", list)
	}
	state := list.Items[0]
	if state.NormalizedIP != "203.0.113.8" || state.FailureCount != 4 ||
		!state.CurrentlyBlocked || state.AutoBlockRuleID == nil || *state.AutoBlockRuleID != 42 {
		t.Fatalf("unexpected failure state: %#v", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestRecordFailedLoginDoesNotCreateAutoBlockForMatchingAllowRule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	const normalizedIP = "203.0.113.8"
	window := 15 * time.Minute
	blockFor := time.Hour
	windowStartedAt := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE ip_access_rules")).
		WithArgs(normalizedIP).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM ip_login_failure_states")).
		WithArgs(normalizedIP, int64(window.Seconds())).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ip_login_failure_states")).
		WithArgs(normalizedIP, int64(window.Seconds())).
		WillReturnRows(sqlmock.NewRows([]string{"failure_count", "window_started_at"}).AddRow(3, windowStartedAt))
	// The conditional INSERT returns no row when a matching active allow rule
	// exists. A missing result must not be treated as a database failure.
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ip_access_rules")).
		WithArgs(normalizedIP, 3, int64(blockFor.Seconds()), windowStartedAt).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ip_or_cidr", "rule_kind", "status", "reason", "failure_count",
			"first_failed_at", "last_failed_at", "blocked_at", "expires_at", "last_seen_at", "hit_count",
			"created_by_user_id", "released_by_user_id", "released_at", "created_at", "updated_at",
		}))
	mock.ExpectCommit()

	repo := NewIPAccessControlRepository(db)
	result, err := repo.RecordFailedLogin(context.Background(), normalizedIP, 3, window, blockFor)
	if err != nil {
		t.Fatalf("record failed login: %v", err)
	}
	if result == nil || result.FailureCount != 3 || result.Blocked || result.Rule != nil {
		t.Fatalf("matching allow rule must suppress automatic block: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestRecordFailedLoginCreatesAutoBlockWithoutAllowRule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	const normalizedIP = "203.0.113.8"
	window := 15 * time.Minute
	blockFor := time.Hour
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE ip_access_rules")).
		WithArgs(normalizedIP).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM ip_login_failure_states")).
		WithArgs(normalizedIP, int64(window.Seconds())).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ip_login_failure_states")).
		WithArgs(normalizedIP, int64(window.Seconds())).
		WillReturnRows(sqlmock.NewRows([]string{"failure_count", "window_started_at"}).AddRow(3, now))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ip_access_rules")).
		WithArgs(normalizedIP, 3, int64(blockFor.Seconds()), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ip_or_cidr", "rule_kind", "status", "reason", "failure_count",
			"first_failed_at", "last_failed_at", "blocked_at", "expires_at", "last_seen_at", "hit_count",
			"created_by_user_id", "released_by_user_id", "released_at", "created_at", "updated_at",
		}).AddRow(
			int64(1), normalizedIP, string(service.IPAccessRuleKindAutoBlock), string(service.IPAccessRuleStatusActive), "automatic login failure threshold reached", 3,
			now, now, now, now.Add(blockFor), nil, int64(0),
			nil, nil, nil, now, now,
		))
	mock.ExpectCommit()

	repo := NewIPAccessControlRepository(db)
	result, err := repo.RecordFailedLogin(context.Background(), normalizedIP, 3, window, blockFor)
	if err != nil {
		t.Fatalf("record failed login: %v", err)
	}
	if result == nil || !result.Blocked || result.Rule == nil || result.Rule.RuleKind != service.IPAccessRuleKindAutoBlock {
		t.Fatalf("non-allow IP must receive an automatic block: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestRecordIPAccessRuleHitUpdatesMatchingActiveBlockRules(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE ip_access_rules\nSET last_seen_at = NOW(), hit_count = hit_count + 1")).
		WithArgs("203.0.113.8").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewIPAccessControlRepository(db)
	if err := repo.RecordIPAccessRuleHit(context.Background(), "203.0.113.8"); err != nil {
		t.Fatalf("record IP access rule hit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestReleaseIPAccessRuleResetsFailureStateOnlyForBlockRules(t *testing.T) {
	tests := []struct {
		name        string
		ipOrCIDR    string
		ruleKind    service.IPAccessRuleKind
		deleteQuery string
	}{
		{
			name:     "allow rule preserves failure state",
			ipOrCIDR: "192.0.2.8",
			ruleKind: service.IPAccessRuleKindAllow,
		},
		{
			name:        "single IP block resets failure state",
			ipOrCIDR:    "192.0.2.8",
			ruleKind:    service.IPAccessRuleKindManualBlock,
			deleteQuery: "DELETE FROM ip_login_failure_states WHERE normalized_ip = $1",
		},
		{
			name:        "CIDR block resets matching failure states",
			ipOrCIDR:    "192.0.2.0/24",
			ruleKind:    service.IPAccessRuleKindManualBlock,
			deleteQuery: "DELETE FROM ip_login_failure_states\nWHERE normalized_ip::inet <<= $1::cidr",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("new sqlmock: %v", err)
			}
			defer db.Close()

			now := time.Now().UTC()
			ruleID := int64(12)
			actorID := int64(7)
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("UPDATE ip_access_rules")).
				WithArgs(ruleID, actorID).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "ip_or_cidr", "rule_kind", "status", "reason", "failure_count",
					"first_failed_at", "last_failed_at", "blocked_at", "expires_at", "last_seen_at", "hit_count",
					"created_by_user_id", "released_by_user_id", "released_at", "created_at", "updated_at",
				}).AddRow(
					ruleID, test.ipOrCIDR, string(test.ruleKind), string(service.IPAccessRuleStatusReleased), "test", 4,
					now.Add(-time.Hour), now.Add(-time.Minute), now.Add(-time.Minute), nil, nil, int64(0),
					actorID, actorID, now, now.Add(-time.Hour), now,
				))
			if test.deleteQuery != "" {
				mock.ExpectExec(regexp.QuoteMeta(test.deleteQuery)).
					WithArgs(test.ipOrCIDR).
					WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()

			repo := NewIPAccessControlRepository(db)
			released, err := repo.ReleaseIPAccessRuleAndReset(context.Background(), ruleID, actorID)
			if err != nil {
				t.Fatalf("release rule: %v", err)
			}
			if released == nil || released.RuleKind != test.ruleKind || released.Status != service.IPAccessRuleStatusReleased {
				t.Fatalf("unexpected released rule: %#v", released)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestCleanupExpiredIPLoginFailureStatesUsesBoundedBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM ip_login_failure_states\nWHERE normalized_ip IN (")).
		WithArgs(sqlmock.AnyArg(), 1000).
		WillReturnResult(sqlmock.NewResult(0, 4))

	repo := NewIPAccessControlRepository(db)
	cleanupRepo, ok := repo.(service.IPAccessFailureStateCleanupRepository)
	if !ok {
		t.Fatal("repository must implement failure-state cleanup")
	}
	deleted, err := cleanupRepo.CleanupExpiredIPLoginFailureStates(context.Background(), time.Now().UTC(), 0)
	if err != nil || deleted != 4 {
		t.Fatalf("unexpected cleanup result: deleted=%d err=%v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
