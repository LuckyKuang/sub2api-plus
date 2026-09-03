package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func newSessionIDUsageLog(sessionID *string) *service.UsageLog {
	return &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-session-id",
		Model:        "gpt-5.4",
		InputTokens:  10,
		OutputTokens: 20,
		TotalCost:    1.0,
		ActualCost:   1.0,
		SessionID:    sessionID,
		CreatedAt:    time.Now().UTC(),
	}
}

// TestPrepareUsageLogInsert_SessionIDArgWiring pins the session_id column to the
// arg slice / arg-type table so the INSERT column lists stay in sync. session_id
// precedes completion_status, usage_source, native_compaction_v2, and created_at.
func TestPrepareUsageLogInsert_SessionIDArgWiring(t *testing.T) {
	require.Len(t, usageLogInsertArgTypes, 68, "arg-type table must include completion metadata, native compaction and requested reasoning effort")

	sessionID := "sess-persisted-123"
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(&sessionID))

	require.Len(t, prepared.args, len(usageLogInsertArgTypes),
		"prepared args must match the arg-type table length")

	// created_at is last; native_compaction_v2 precedes it; completion_status/usage_source
	// sit between session_id and native_compaction_v2.
	sessionArg := prepared.args[len(prepared.args)-5]
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.True(t, ns.Valid)
	require.Equal(t, sessionID, ns.String)

	require.Equal(t, "text", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-5],
		"session_id arg type must be text")
	require.Equal(t, service.UsageCompletionUnknown, prepared.args[len(prepared.args)-4])
	require.Equal(t, service.UsageSourceUnknown, prepared.args[len(prepared.args)-3])
	require.Equal(t, "boolean", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-2],
		"native_compaction_v2 arg type must be boolean")
}

// TestPrepareUsageLogInsert_SessionIDNullWhenAbsent proves an absent session id is
// persisted as SQL NULL rather than an empty string.
func TestPrepareUsageLogInsert_SessionIDNullWhenAbsent(t *testing.T) {
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(nil))
	sessionArg := prepared.args[len(prepared.args)-5]
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.False(t, ns.Valid, "absent session id must be NULL, not empty string")

	empty := ""
	preparedEmpty := prepareUsageLogInsert(newSessionIDUsageLog(&empty))
	emptySessionArg := preparedEmpty.args[len(preparedEmpty.args)-5]
	nsEmpty, ok := emptySessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", emptySessionArg)
	require.False(t, nsEmpty.Valid, "empty session id must also be NULL")
}

func TestPrepareUsageLogInsert_RequestedReasoningEffortArgWiring(t *testing.T) {
	requested := "max"
	forwarded := "xhigh"
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:                   1,
		APIKeyID:                 2,
		AccountID:                3,
		RequestID:                "req-requested-effort",
		Model:                    "gpt-5.4",
		ReasoningEffort:          &forwarded,
		RequestedReasoningEffort: &requested,
		CreatedAt:                time.Now().UTC(),
	})

	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
	require.Equal(t, "text", usageLogInsertArgTypes[52], "reasoning_effort arg type must stay text")
	require.Equal(t, "text", usageLogInsertArgTypes[53], "requested_reasoning_effort must follow reasoning_effort")

	forwardedArg, ok := prepared.args[52].(sql.NullString)
	require.True(t, ok)
	require.True(t, forwardedArg.Valid)
	require.Equal(t, forwarded, forwardedArg.String)

	requestedArg, ok := prepared.args[53].(sql.NullString)
	require.True(t, ok)
	require.True(t, requestedArg.Valid)
	require.Equal(t, requested, requestedArg.String)
}

// TestUsageLogInsertQueries_IncludeSessionID guards that every generated INSERT path
// and the SELECT column list reference session_id.
func TestUsageLogInsertQueries_IncludeSessionID(t *testing.T) {
	require.Contains(t, usageLogSelectColumns, "requested_reasoning_effort",
		"SELECT column list must include requested_reasoning_effort")
	require.Contains(t, usageLogSelectColumns, "session_id",
		"SELECT column list must include session_id")
	require.Contains(t, usageLogSelectColumns, "completion_status",
		"SELECT column list must include completion_status")
	require.Contains(t, usageLogSelectColumns, "usage_source",
		"SELECT column list must include usage_source")
	require.Contains(t, usageLogSelectColumns, "native_compaction_v2",
		"SELECT column list must include native_compaction_v2")

	sessionID := "sess-in-query"
	log := newSessionIDUsageLog(&sessionID)
	prepared := prepareUsageLogInsert(log)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)

	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key},
		map[string]usageLogInsertPrepared{key: prepared})
	require.Contains(t, batchQuery, "session_id")
	require.Contains(t, batchQuery, "requested_reasoning_effort")
	require.Contains(t, batchQuery, "completion_status")
	require.Contains(t, batchQuery, "native_compaction_v2")
	// Two column references (INSERT column list + SELECT ... FROM input) plus the CTE def.
	require.GreaterOrEqual(t, strings.Count(batchQuery, "session_id"), 3)
	require.Len(t, batchArgs, len(prepared.args)+1,
		"batch args include the synthetic input_index before usage-log values")
}
