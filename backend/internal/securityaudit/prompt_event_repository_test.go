package securityaudit

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestScanEventIncludesObservabilityMetadataAndFullPrompt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Unix(100, 0).UTC()
	columns := make([]string, 42)
	for index := range columns {
		columns[index] = "column"
	}
	rows := sqlmock.NewRows(columns).AddRow(
		int64(1), int64(2), "request-1", int64(3), "alice", "alice@example.test", int64(4), "key-1",
		int64(5), "group-1", "openai", "/v1/responses", "openai_responses", "gpt-test", "hash", "red***", "http",
		"critical", "critical", "Block", `["jailbreak"]`, `["jailbreak"]`, `{"jailbreak":1}`, `{"jailbreak":"Jailbreak"}`,
		"qwen3guard-openai", "test", "guard-1", "priority", 1, int64(9), 4, 27004, createdAt,
		"203.0.113.42", 395959, 1, "blocking", 0, 100000, 3, false, "complete prompt",
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	event, err := scanEvent(db.QueryRow("SELECT event"), true)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.42", event.Snapshot.ClientIP)
	require.Equal(t, 395959, event.Snapshot.PromptLength)
	require.Equal(t, ModeBlocking, event.ExecutionMode)
	require.Equal(t, 0, *event.QueueDelayMS)
	require.Equal(t, 100000, *event.InputLimit)
	require.Equal(t, 3, *event.MatchedChunkIndex)
	require.False(t, event.Snapshot.FullPromptTruncated)
	require.Equal(t, "complete prompt", event.Snapshot.FullPrompt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildEventWhereFiltersExactClientIP(t *testing.T) {
	where, args := buildEventWhere(EventFilter{ClientIP: "2001:0db8::1"}, 1)
	require.Contains(t, where, "e.client_ip=$1")
	require.Equal(t, []any{"2001:db8::1"}, args)
}
