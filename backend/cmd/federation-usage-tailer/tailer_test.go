//go:build unit || !integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/enttest"
	"github.com/LuckyKuang/sub2api-plus/internal/federation"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newTailerTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(10)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func newTestTailer(t *testing.T, client *dbent.Client, mainlandBaseURL string) *tailer {
	t.Helper()
	cfg := tailerConfig{
		MainlandBaseURL: mainlandBaseURL,
		AdminEmail:      "admin@example.com",
		AdminPassword:   "test-password",
		BatchSize:       10,
		HTTPTimeout:     5 * time.Second,
		CursorScope:     "overseas_to_mainland",
	}
	// Model a tailer that was already running before the test's usage rows
	// were written: a brand-new cursor would start at the usage_log head and
	// skip them (see TestTailer_NewCursorStartsAtUsageLogHead).
	_, err := client.FederationUsageCursor.Create().
		SetScope(cfg.CursorScope).
		SetLastDeliveredID(0).
		Save(context.Background())
	require.NoError(t, err)
	return newTestTailerNoCursor(client, cfg)
}

func newTestTailerNoCursor(client *dbent.Client, cfg tailerConfig) *tailer {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &tailer{
		db:       client,
		mainland: newMainlandClient(cfg),
		cfg:      cfg,
		logger:   logger,
	}
}

func insertLocalUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	u, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		Save(ctx)
	require.NoError(t, err)
	return u
}

// insertUsageFixtures creates the Account/APIKey rows usage_log's required
// edges point at (see (UsageLog) Edges() in ent/schema/usage_log.go) so
// SQLite's foreign_keys=ON doesn't reject the insert.
func insertUsageFixtures(t *testing.T, ctx context.Context, client *dbent.Client, userID int64) (accountID, apiKeyID int64) {
	t.Helper()
	a, err := client.Account.Create().
		SetName(fmt.Sprintf("tailer-test-account-%d", userID)).
		SetPlatform("openai").
		SetType("apikey").
		SetStatus("active").
		SetCredentials(map[string]any{"api_key": "sk-test"}).
		Save(ctx)
	require.NoError(t, err)

	k, err := client.APIKey.Create().
		SetUserID(userID).
		SetKey(fmt.Sprintf("sk-tailer-test-%d-%d", userID, time.Now().UnixNano())).
		SetName("tailer-test-key").
		Save(ctx)
	require.NoError(t, err)

	return a.ID, k.ID
}

func insertUsageLog(t *testing.T, ctx context.Context, client *dbent.Client, userID, accountID, apiKeyID int64, cost float64) *dbent.UsageLog {
	t.Helper()
	row, err := client.UsageLog.Create().
		SetUserID(userID).
		SetAPIKeyID(apiKeyID).
		SetAccountID(accountID).
		SetRequestID(fmt.Sprintf("req-%d-%d", userID, time.Now().UnixNano())).
		SetModel("test-model").
		SetActualCost(cost).
		Save(ctx)
	require.NoError(t, err)
	return row
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
}

func mainlandMux(t *testing.T, findEmail string, mainlandID int64, onBalance func(w http.ResponseWriter, r *http.Request)) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]string{"access_token": "test-token"})
	})
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"items": []federation.RemoteUser{{ID: mainlandID, Email: findEmail, Status: "active"}}})
	})
	mux.HandleFunc(fmt.Sprintf("/api/v1/admin/users/%d/balance", mainlandID), onBalance)
	return mux
}

func TestTailer_DeliversBatchAndAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-user@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	r1 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1.5)
	r2 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 2.5)

	var balanceCalls int
	mux := mainlandMux(t, "tailer-user@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		balanceCalls++
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.Equal(t, 2, balanceCalls)
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, r2.ID, cursor.LastDeliveredID)
	require.Equal(t, 0, cursor.Attempts)
	require.Nil(t, cursor.LastError)

	_ = r1 // referenced for readability of test intent
}

func TestTailer_MidBatchFailureStopsAtFailingRow(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-fail@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	r1 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1.0)
	r2 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 2.0) // this one will fail (insufficient balance)
	r3 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 3.0) // must never be attempted

	calls := 0
	mux := mainlandMux(t, "tailer-fail@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			jsonOK(w, map[string]any{"message": "ok"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "message": "insufficient balance"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.Equal(t, 2, calls, "must attempt r1 (succeeds) then r2 (fails), never reach r3")

	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, r1.ID, cursor.LastDeliveredID, "cursor must stop at the last successfully delivered row")
	require.Equal(t, 1, cursor.Attempts)
	require.NotNil(t, cursor.LastError)
	require.NotNil(t, cursor.NextRetryAt)

	_ = r2
	_ = r3
}

func TestTailer_RespectsBackoffBeforeRetryingBlockedRow(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-backoff@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1.0)

	calls := 0
	mux := mainlandMux(t, "tailer-backoff@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "message": "boom"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)

	require.NoError(t, tl.runOnce(ctx))
	require.Equal(t, 1, calls)

	// Immediately re-running must not retry yet: next_retry_at is in the future.
	require.NoError(t, tl.runOnce(ctx))
	require.Equal(t, 1, calls, "must not retry before next_retry_at elapses")

	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	_, err = client.FederationUsageCursor.UpdateOneID(cursor.ID).
		SetNextRetryAt(time.Now().Add(-time.Second)).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, tl.runOnce(ctx))
	require.Equal(t, 2, calls, "must retry once next_retry_at has elapsed")
}

func TestTailer_SkipsZeroCostRowsWithoutNetworkCall(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-zero@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	r1 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 0)

	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		calls++
		jsonOK(w, map[string]string{"access_token": "test-token"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.Equal(t, 0, calls, "a zero-cost row must not even trigger a login/lookup call")
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, r1.ID, cursor.LastDeliveredID)
}

func TestTailer_ReportsWatermarkAfterSuccessfulSubtract(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-watermark@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	r1 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1.5)

	var sawWatermarkBody map[string]any
	mux := mainlandMux(t, "tailer-watermark@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"message": "ok"})
	})
	mux.HandleFunc("/api/v1/admin/users/42/federation-usage-watermark", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sawWatermarkBody)
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.InDelta(t, float64(r1.ID), sawWatermarkBody["usage_seq"], 0.0001)
}

func TestTailer_WatermarkFailureDoesNotBlockCursorAdvance(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-watermark-fail@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	r1 := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1.5)

	mux := mainlandMux(t, "tailer-watermark-fail@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"message": "ok"})
	})
	mux.HandleFunc("/api/v1/admin/users/42/federation-usage-watermark", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, r1.ID, cursor.LastDeliveredID, "the balance subtract succeeded, so the row is delivered regardless of the watermark call's outcome")
	require.Nil(t, cursor.LastError, "a watermark-report failure must not be recorded as a blocking cursor error")
}

// A tailer started for the first time must not replay pre-federation usage:
// that history was already paid from the local balance.
func TestTailer_NewCursorStartsAtUsageLogHead(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-head@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1)
	old := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 2)

	var balanceCalls int
	mux := mainlandMux(t, "tailer-head@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		balanceCalls++
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailerNoCursor(client, tailerConfig{
		MainlandBaseURL: server.URL,
		AdminEmail:      "admin@example.com",
		AdminPassword:   "test-password",
		BatchSize:       10,
		HTTPTimeout:     5 * time.Second,
		CursorScope:     "overseas_to_mainland",
	})
	require.NoError(t, tl.runOnce(ctx))
	require.Zero(t, balanceCalls, "pre-existing usage must not be billed to mainland")
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, old.ID, cursor.LastDeliveredID)

	fresh := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 3)
	require.NoError(t, tl.runOnce(ctx))
	require.Equal(t, 1, balanceCalls, "usage recorded after the cursor was created is billed")
	cursor, err = tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, fresh.ID, cursor.LastDeliveredID)
}

func TestTailer_NewCursorOnEmptyUsageLogStartsAtZero(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	tl := newTestTailerNoCursor(client, tailerConfig{CursorScope: "overseas_to_mainland", BatchSize: 10})
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Zero(t, cursor.LastDeliveredID)
}

// A user mainland has never heard of is local-only: its usage is skipped
// (already billed locally) and must not block federated users behind it.
func TestTailer_SkipsLocalOnlyUserWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	fed := insertLocalUser(t, ctx, client, "tailer-fed@example.com")
	native := insertLocalUser(t, ctx, client, "tailer-native@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, fed.ID)
	insertUsageLog(t, ctx, client, fed.ID, accountID, apiKeyID, 1)
	insertUsageLog(t, ctx, client, native.ID, accountID, apiKeyID, 5)
	last := insertUsageLog(t, ctx, client, fed.ID, accountID, apiKeyID, 2)

	var billed []float64
	mux := mainlandMux(t, "tailer-fed@example.com", 42, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		amount, _ := body["balance"].(float64)
		billed = append(billed, amount)
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.Equal(t, []float64{1, 2}, billed, "only the federated user's usage is billed to mainland")
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, last.ID, cursor.LastDeliveredID)
	require.Nil(t, cursor.LastError)
}

// Served usage must always be billed, even past zero: the subtract asks
// mainland to allow overdraft and uses the overdraft-specific key.
func TestTailer_SubtractAllowsOverdraft(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-overdraft@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	row := insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 7.21)

	var body map[string]any
	var key string
	mux := mainlandMux(t, "tailer-overdraft@example.com", 14, func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&body)
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	require.Equal(t, "subtract", body["operation"])
	require.Equal(t, true, body["allow_negative"])
	require.InDelta(t, 7.21, body["balance"], 1e-9)
	require.Equal(t, fmt.Sprintf("federation-usage-%d-od", row.ID), key)
	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, row.ID, cursor.LastDeliveredID)
}

// Admin balances are shared with mainland, so admin usage is billed like anyone's.
func TestTailer_BillsAdminUsage(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	admin := insertLocalUser(t, ctx, client, "tailer-admin@example.com")
	_, err := client.User.UpdateOneID(admin.ID).SetRole("admin").Save(ctx)
	require.NoError(t, err)
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, admin.ID)
	insertUsageLog(t, ctx, client, admin.ID, accountID, apiKeyID, 3)

	var billed []float64
	mux := mainlandMux(t, "tailer-admin@example.com", 7, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		amount, _ := body["balance"].(float64)
		billed = append(billed, amount)
		jsonOK(w, map[string]any{"message": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))
	require.Equal(t, []float64{3}, billed)
}

// A failed lookup is not a miss: it must block and retry, never skip.
func TestTailer_UserLookupFailureBlocksCursor(t *testing.T) {
	ctx := context.Background()
	client := newTailerTestClient(t)
	u := insertLocalUser(t, ctx, client, "tailer-lookup@example.com")
	accountID, apiKeyID := insertUsageFixtures(t, ctx, client, u.ID)
	insertUsageLog(t, ctx, client, u.ID, accountID, apiKeyID, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]string{"access_token": "test-token"})
	})
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"db down"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tl := newTestTailer(t, client, server.URL)
	require.NoError(t, tl.runOnce(ctx))

	cursor, err := tl.loadOrCreateCursor(ctx)
	require.NoError(t, err)
	require.Zero(t, cursor.LastDeliveredID, "a lookup failure must not advance the cursor")
	require.Equal(t, 1, cursor.Attempts)
	require.NotNil(t, cursor.LastError)
}
