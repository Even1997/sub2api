package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestMonthlyBillPeriodsAndDecimalReconciliation(t *testing.T) {
	now := time.Date(2026, 10, 27, 15, 30, 0, 0, billZone)
	for _, tc := range []struct {
		month string
		days  int
	}{{"2024-02", 29}, {"2025-02", 28}, {"2026-09", 30}, {"2026-08", 31}} {
		start, end, partial, err := billPeriod(tc.month, now)
		require.NoError(t, err)
		require.False(t, partial)
		require.Equal(t, tc.days, int(end.Sub(start).Hours()/24))
		require.Equal(t, 16, start.UTC().Hour())
	}
	_, end, partial, err := billPeriod("2026-10", now)
	require.NoError(t, err)
	require.True(t, partial)
	require.Equal(t, now, end)
	for _, month := range []string{"2026-11", "2026-2", "2026-13", "garbage", "1999-01"} {
		_, _, _, err := billPeriod(month, now)
		require.Error(t, err)
	}
	m := BillModel{Model: "same-model", Requests: "1", Tokens: "9007199254740993", Images: "0", VideoSeconds: "0", Videos: "0", Cost: "0.1000000001"}
	b := &MonthlyBill{Models: []BillModel{m, m}, Days: []BillDay{{Models: []BillModel{m, m}, Transfers: []BillTransfer{{Amount: "0.3000000001"}}}}}
	require.NoError(t, reconcileBill(b))
	require.Equal(t, "0.2000000002", b.Consumption)
	require.Equal(t, "-0.0999999999", b.Net)
	require.Equal(t, b.Net, b.CNY)
	require.Len(t, b.Models, 1)
	require.Equal(t, "18014398509481986", b.Models[0].Tokens)
	b.Days = nil
	require.ErrorContains(t, reconcileBill(b), "reconciliation")
}

// Opt-in PostgreSQL verification in a unique schema, never production tables.
func TestMonthlyBillPostgres(t *testing.T) {
	dsn := os.Getenv("BILL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BILL_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	schema := fmt.Sprintf("bill_test_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, e := admin.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, e)
	}()
	connector, err := pq.NewConnector(dsn + " search_path=" + schema)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(8)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,email TEXT,created_at TIMESTAMPTZ,balance NUMERIC DEFAULT 100);
    CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,user_id BIGINT,model TEXT,billing_type SMALLINT DEFAULT 0,created_at TIMESTAMPTZ,
    input_tokens INTEGER DEFAULT 0,output_tokens INTEGER DEFAULT 0,cache_creation_tokens INTEGER DEFAULT 0,cache_read_tokens INTEGER DEFAULT 0,
    image_input_tokens INTEGER DEFAULT 0,image_output_tokens INTEGER DEFAULT 0,image_count INTEGER DEFAULT 0,video_count INTEGER DEFAULT 0,video_duration_seconds INTEGER,
    actual_cost NUMERIC(20,10),rate_multiplier NUMERIC DEFAULT 1);
    CREATE INDEX usage_user_time ON usage_logs(user_id,created_at);
    CREATE TABLE user_affiliate_ledger(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,amount NUMERIC(20,10),created_at TIMESTAMPTZ);
    CREATE TABLE usage_billing_events(observed_at TIMESTAMPTZ,state TEXT,payload JSONB);`)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "318_user_monthly_bills.sql"))
	require.NoError(t, err)
	exec(string(migration))
	month := time.Now().In(billZone).AddDate(0, -1, 0).Format("2006-01")
	start, end, _, err := billPeriod(month, time.Now())
	require.NoError(t, err)
	exec(`INSERT INTO users(id,email,created_at) VALUES(1,'billing@example.test',$1),(2,'other@example.test',$1)`, start.AddDate(-1, 0, 0))
	s := NewMonthlyBillService(db)
	// Pre-migration history is not silently assumed complete.
	_, err = s.Generate(ctx, 1, month, 0)
	require.ErrorIs(t, err, errBillIncomplete)
	exec(`UPDATE monthly_bill_coverage SET complete_from=$1`, start)
	exec(`INSERT INTO usage_logs(user_id,model,created_at,input_tokens,actual_cost,rate_multiplier) VALUES
    (1,'text-model',$1,100,0.1000000001,0.5),(1,'text-model',$1,200,0.2,2),
    (1,'text-model',$2,900,99,1),(1,'text-model',$3,900,99,1),(2,'private-model',$1,900,99,1)`, start, start.Add(-time.Microsecond), end)
	exec(`UPDATE usage_logs SET image_input_tokens=10,image_output_tokens=20 WHERE user_id=1 AND created_at=$1`, start)
	exec(`INSERT INTO usage_logs(user_id,model,created_at,billing_type,actual_cost,image_count,video_count,video_duration_seconds) VALUES
    (1,'image-model',$1,0,0.3,2,0,NULL),(1,'video-model',$1,0,0.4,0,2,5),(1,'subscription-model',$1,1,5,0,0,NULL)`, start.Add(24*time.Hour))
	exec(`INSERT INTO user_affiliate_ledger(user_id,action,amount,created_at) VALUES
    (1,'transfer',2,$1),(1,'accrue',100,$1),(1,'unfreeze',100,$1),(1,'transfer',-5,$1),(2,'transfer',100,$1),
    (1,'transfer',100,$2),(1,'transfer',100,$3)`, start.Add(24*time.Hour), start.Add(-time.Microsecond), end)
	exec(`INSERT INTO usage_billing_events VALUES($1,'pending','{"command":{"UserID":1}}')`, start)
	_, err = s.Generate(ctx, 1, month, 0)
	require.ErrorContains(t, err, "BILL_UNSETTLED")
	exec(`DELETE FROM usage_billing_events`)
	var wg sync.WaitGroup
	bills := make(chan *MonthlyBill, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); b, e := s.Generate(ctx, 1, month, 0); bills <- b; errs <- e }()
	}
	wg.Wait()
	close(bills)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var b *MonthlyBill
	for result := range bills {
		b = result
		require.Equal(t, 1, b.Version)
		require.Equal(t, "1.0000000001", b.Consumption)
		require.Equal(t, "2", b.Rebate)
		require.Equal(t, "-0.9999999999", b.CNY)
	}
	require.Equal(t, "5", b.Subscription)
	require.Len(t, b.Models, 3)
	require.Len(t, b.SubscriptionModels, 1)
	require.Equal(t, "300", b.Models[1].Tokens)
	require.Equal(t, "10", b.Models[2].VideoSeconds)
	require.Equal(t, int(end.Sub(start).Hours()/24), len(b.Days))
	require.Equal(t, "0", b.Days[len(b.Days)-1].Consumption)
	list, err := s.List(ctx, 1, month)
	require.NoError(t, err)
	require.Len(t, list, 1)
	other, err := s.List(ctx, 2, month)
	require.NoError(t, err)
	require.Empty(t, other)
	_, err = s.Get(ctx, 2, month, 1)
	require.ErrorContains(t, err, "BILL_NOT_FOUND")
	var balance string
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=1`).Scan(&balance))
	require.Equal(t, "100", balance)
	// A consistent snapshot cannot mix new rebate transfers into older usage.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	require.NoError(t, err)
	snap := &MonthlyBill{UserID: 1, Start: start, End: end}
	require.NoError(t, readBillUsage(ctx, tx, snap))
	exec(`INSERT INTO user_affiliate_ledger(user_id,action,amount,created_at) VALUES(1,'transfer',1,$1)`, start)
	require.NoError(t, readBillTransfers(ctx, tx, snap))
	require.NoError(t, reconcileBill(snap))
	require.Equal(t, "2", snap.Rebate)
	require.NoError(t, tx.Rollback())
	v2, err := s.Generate(ctx, 1, month, 1)
	require.NoError(t, err)
	require.Equal(t, 2, v2.Version)
	require.Equal(t, "3", v2.Rebate)
	saved, err := s.Get(ctx, 1, month, 1)
	require.NoError(t, err)
	require.Equal(t, "2", saved.Rebate)
	_, err = s.Generate(ctx, 1, month, 9)
	require.ErrorContains(t, err, "BILL_VERSION_STALE")
	// Deleted source history blocks new versions but never erases old snapshots.
	exec(`DELETE FROM usage_logs WHERE user_id=1 AND created_at=$1`, start)
	_, err = s.Generate(ctx, 1, month, 2)
	require.ErrorIs(t, err, errBillIncomplete)
	saved, err = s.Generate(ctx, 1, month, 0)
	require.NoError(t, err)
	require.Equal(t, "1.0000000001", saved.Consumption)
	// A user with no activity still gets every elapsed day, never future days.
	current := time.Now().In(billZone).Format("2006-01")
	empty, err := s.Generate(ctx, 2, current, 0)
	require.NoError(t, err)
	require.True(t, empty.Partial)
	require.Equal(t, "0", empty.CNY)
	require.Equal(t, empty.GeneratedAt.In(billZone).Day(), len(empty.Days))
	// Database aggregation, not individual-request snapshots, keeps large bills small.
	exec(`INSERT INTO users(id,email,created_at) VALUES(3,'load@example.test',$1)`, start)
	exec(`INSERT INTO usage_logs(user_id,model,created_at,input_tokens,actual_cost) SELECT 3,'load-model',$1,1000,0.0001 FROM generate_series(1,100000)`, start)
	began := time.Now()
	large, err := s.Generate(ctx, 3, month, 0)
	require.NoError(t, err)
	elapsed := time.Since(began)
	require.Equal(t, "10", large.Consumption)
	require.Len(t, large.Models, 1)
	require.Equal(t, "100000", large.Models[0].Requests)
	raw, err := json.Marshal(large)
	require.NoError(t, err)
	require.Less(t, len(raw), 20000)
	t.Logf("100,000 usage records: %s generation, %d-byte snapshot", elapsed, len(raw))
	exec(`TRUNCATE usage_logs`)
	_, err = s.Generate(ctx, 2, current, 1)
	require.ErrorIs(t, err, errBillIncomplete)
	_, err = s.Get(ctx, 2, current, 1)
	require.NoError(t, err)
	// Store a sanitized fixture only when the local visual QA harness requests it.
	if path := os.Getenv("BILL_QA_FIXTURE"); path != "" {
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
	require.False(t, strings.Contains(string(raw), "private-model"))
}
