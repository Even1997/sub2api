package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Real PostgreSQL exercises trigger/transaction behavior that SQL mocks cannot.
func TestPermanentHourlyUsage(t *testing.T) {
	dsn := os.Getenv("BILL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BILL_TEST_POSTGRES_DSN not set")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	schema := fmt.Sprintf("hourly_test_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, err := admin.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
		require.NoError(t, err)
	}()
	connector, err := pq.NewConnector(dsn + " search_path=" + schema)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(8)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,email TEXT,created_at TIMESTAMPTZ);
    CREATE TABLE usage_logs(id BIGSERIAL,user_id BIGINT,api_key_id BIGINT,group_id BIGINT,model TEXT,requested_model TEXT,
    billing_type SMALLINT DEFAULT 0,billing_mode TEXT,request_type SMALLINT DEFAULT 0,stream BOOLEAN DEFAULT FALSE,openai_ws_mode BOOLEAN DEFAULT FALSE,native_compaction_v2 BOOLEAN DEFAULT FALSE,
    input_tokens INTEGER DEFAULT 0,output_tokens INTEGER DEFAULT 0,cache_creation_tokens INTEGER DEFAULT 0,cache_read_tokens INTEGER DEFAULT 0,
    image_count INTEGER DEFAULT 0,video_count INTEGER DEFAULT 0,video_duration_seconds INTEGER,total_cost NUMERIC DEFAULT 0,actual_cost NUMERIC(20,10),created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(id,created_at)) PARTITION BY RANGE(created_at);
    CREATE TABLE usage_logs_202601 PARTITION OF usage_logs FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
    CREATE TABLE usage_logs_202602 PARTITION OF usage_logs FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
    CREATE TABLE usage_billing_events(observed_at TIMESTAMPTZ,state TEXT,payload JSONB);
    INSERT INTO users VALUES(1,'one@example.test','2025-01-01'),(2,'two@example.test','2025-01-01');
    INSERT INTO usage_logs(user_id,model,input_tokens,actual_cost,created_at) VALUES(1,'gpt-legacy',10,0.1,'2026-01-01T00:15:00Z'),(1,'gpt-legacy',20,0.2,'2026-01-01T00:20:00Z'),(2,'private-model',30,99,'2026-01-01T00:20:00Z');`)
	for _, name := range []string{"318_user_monthly_bills.sql", "321_permanent_hourly_usage.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		exec(string(raw))
	}
	ctx := context.Background()
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, zone)
	end := start.AddDate(0, 0, 1)
	r := &usageLogRepository{sql: db, db: db}
	report, err := r.GetHourlyUsage(ctx, start, end, "hour", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.True(t, report.BackfillPending)
	_, err = db.Exec(`TRUNCATE usage_logs`)
	require.ErrorContains(t, err, "backfill pending")
	_, err = db.Exec(`TRUNCATE usage_logs_202601`)
	require.ErrorContains(t, err, "backfill pending")
	require.ErrorContains(t, dropUsageLogsPartitionWithRollupInvalidation(ctx, db, "usage_logs_202601", start), "backfill pending")
	// Deletion before backfill archives once, instead of losing legacy usage.
	exec(`DELETE FROM usage_logs WHERE id=1`)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := db.Exec(`SELECT backfill_user_usage_hourly(1)`); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	exec(`SELECT backfill_user_usage_hourly(1000)`)
	report, err = r.GetHourlyUsage(ctx, start, end, "hour", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.False(t, report.BackfillPending)
	require.Equal(t, "0.3", report.Totals.Cost)
	require.Equal(t, "30", report.Totals.Tokens)
	require.Len(t, report.Items, 1)
	require.True(t, report.CompleteFrom.After(start)) // Never manufacture historical completeness.
	// Repeated insert and ON CONFLICT UPDATE do not count twice.
	exec(`INSERT INTO usage_logs(id,user_id,model,input_tokens,actual_cost,created_at) VALUES(2,1,'gpt-legacy',100,2,'2026-01-01T00:20:00Z') ON CONFLICT DO NOTHING`)
	exec(`INSERT INTO usage_logs(id,user_id,model,input_tokens,actual_cost,created_at) VALUES(2,1,'gpt-legacy',100,2,'2026-01-01T00:20:00Z') ON CONFLICT(id,created_at) DO UPDATE SET actual_cost=EXCLUDED.actual_cost,input_tokens=EXCLUDED.input_tokens`)
	// Unrelated updates and attempted marker resets must not duplicate counts.
	exec(`UPDATE usage_logs SET hourly_summary_recorded=FALSE WHERE id=2`)
	_, err = db.Exec(`UPDATE usage_logs SET created_at='2026-02-01' WHERE id=2`)
	require.ErrorContains(t, err, "timestamp is immutable")
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO usage_logs(user_id,model,actual_cost,created_at) VALUES(1,'rollback',999,'2026-01-01T01:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	exec(`INSERT INTO usage_logs(user_id,api_key_id,group_id,model,requested_model,actual_cost,image_count,video_count,video_duration_seconds,billing_type,stream,created_at) VALUES
    (1,7,8,'routed',' gpt-image ',0.3,2,0,NULL,0,TRUE,'2026-01-01T01:01:00Z'),
    (1,7,8,'sora',NULL,0.4,0,2,5,1,FALSE,'2026-01-01T01:02:00Z')`)
	report, err = r.GetHourlyUsage(ctx, start, end, "hour", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "2.8", report.Totals.Cost)
	require.Equal(t, "2.4", report.BalanceCost)
	require.Equal(t, "0.4", report.SubscriptionCost)
	require.Equal(t, "110", report.Totals.Tokens)
	require.Equal(t, "2", report.Totals.Images)
	require.Equal(t, "10", report.Totals.VideoSeconds)
	filter := usagestats.UsageLogFilters{UserID: 1, APIKeyID: 7, GroupID: 8, Model: "gpt-image", ModelProvider: "openai", BillingMode: "image"}
	filtered, err := r.GetHourlyUsage(ctx, start, end, "hour", filter)
	require.NoError(t, err)
	require.Len(t, filtered.Items, 1)
	require.Equal(t, "0.3", filtered.Totals.Cost)
	falseValue := false
	filter.Stream = &falseValue
	filtered, err = r.GetHourlyUsage(ctx, start, end, "hour", filter)
	require.NoError(t, err)
	require.Empty(t, filtered.Items)
	_, err = r.GetHourlyUsage(ctx, start, end, "hour", usagestats.UsageLogFilters{})
	require.Error(t, err)
	// Concurrent committed charges, including direct partition inserts, stay atomic.
	errs = make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, e := db.Exec(`INSERT INTO usage_logs_202601(user_id,model,actual_cost,created_at) VALUES(1,'parallel',0.0000000001,'2026-01-01T02:01:00Z')`)
				if e != nil {
					errs <- e
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	before, err := r.GetHourlyUsage(ctx, start, end, "day", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "2.800000008", before.Totals.Cost)
	exec(`DELETE FROM usage_logs WHERE user_id=1`)
	after, err := r.GetHourlyUsage(ctx, start, end, "day", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, before.Totals, after.Totals)
	require.Equal(t, before.Trend, after.Trend)
	exec(`TRUNCATE usage_logs`)
	after, err = r.GetHourlyUsage(ctx, start, end, "day", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, before.Totals, after.Totals)
	exec(`CREATE TABLE usage_group_rollup_state(id SMALLINT,closed_before DATE,updated_at TIMESTAMPTZ);
    INSERT INTO usage_group_rollup_state VALUES(1,'2026-03-01',NOW())`)
	require.NoError(t, dropUsageLogsPartitionWithRollupInvalidation(ctx, db, "usage_logs_202601", start))
	after, err = r.GetHourlyUsage(ctx, start, end, "day", usagestats.UsageLogFilters{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, before.Totals, after.Totals)
}
