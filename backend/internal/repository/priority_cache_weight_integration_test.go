//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPriorityCacheSignalsKeepScopeAndValidEvidence(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{Email: suffix + "@test.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: suffix, Name: "cache"})
	account := mustCreateAccount(t, client, &service.Account{Name: "cache", Platform: service.PlatformOpenAI})
	group := mustCreateGroup(t, client, &service.Group{Name: suffix, Platform: service.PlatformOpenAI})
	now := time.Now().UTC()
	effort := "high"
	add := func(mutate func(*service.UsageLog, map[string]any), traces int) {
		t.Helper()
		row := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "cache-model", InputTokens: 50, CacheReadTokens: 50, OutputTokens: 1, Stream: true, CreatedAt: now.Add(-time.Minute), ReasoningEffort: &effort}
		detail := map[string]any{"version": 1, "outcome": "success", "status": 200, "body_complete": true, "terminal": "completed", "canceled": false, "truncated": false, "client_disconnect": false, "downstream_error": false}
		if mutate != nil {
			mutate(row, detail)
		}
		_, err := repo.Create(ctx, row)
		require.NoError(t, err)
		raw, err := json.Marshal(detail)
		require.NoError(t, err)
		for i := 0; i < traces; i++ {
			_, err = tx.ExecContext(ctx, `INSERT INTO request_timing_details(usage_log_id,trace_id,detail) VALUES($1,$2,$3::jsonb)`, row.ID, uuid.NewString(), string(raw))
			require.NoError(t, err)
		}
	}
	add(nil, 1)
	add(func(r *service.UsageLog, _ map[string]any) { r.InputTokens = 100; r.CacheReadTokens = 900 }, 1)
	for _, mutation := range []func(*service.UsageLog, map[string]any){
		func(r *service.UsageLog, _ map[string]any) { r.Model = "other" },
		func(r *service.UsageLog, _ map[string]any) { r.GroupID = &group.ID },
		func(r *service.UsageLog, _ map[string]any) { other := "low"; r.ReasoningEffort = &other },
		func(r *service.UsageLog, _ map[string]any) { r.CreatedAt = now.Add(-2 * time.Hour) },
		func(r *service.UsageLog, _ map[string]any) { r.CreatedAt = now.Add(time.Hour) },
		func(r *service.UsageLog, _ map[string]any) { r.InputTokens = -1 },
		func(r *service.UsageLog, _ map[string]any) { r.CacheReadTokens = -1 },
		func(r *service.UsageLog, _ map[string]any) { r.CacheCreationTokens = -1 },
		func(r *service.UsageLog, _ map[string]any) { r.InputTokens = 0; r.CacheReadTokens = 0 },
		func(r *service.UsageLog, _ map[string]any) { r.ImageCount = 1 },
		func(r *service.UsageLog, _ map[string]any) { mode := "video"; r.BillingMode = &mode },
		func(_ *service.UsageLog, d map[string]any) { d["outcome"] = "failed" },
		func(_ *service.UsageLog, d map[string]any) { d["terminal"] = "incomplete" },
		func(_ *service.UsageLog, d map[string]any) { d["body_complete"] = false },
		func(_ *service.UsageLog, d map[string]any) { d["canceled"] = true },
	} {
		add(mutation, 1)
	}
	add(nil, 0)
	add(nil, 2)
	query := service.PrioritySchedulingQuery{AccountIDs: []int64{account.ID}, Model: "cache-model", UsageSince: now.Add(-time.Hour), UsageUntil: now, QualitySince: now.Add(-time.Hour), ObservationEffort: &effort}
	got, err := repo.ReadPrioritySchedulingSignals(ctx, query)
	require.NoError(t, err)
	require.Equal(t, 2, got[account.ID].CacheSamples)
	require.NotNil(t, got[account.ID].AvgCachePercent)
	require.InDelta(t, 70, *got[account.ID].AvgCachePercent, .00001, "arithmetic request mean, not pooled tokens")
	add(func(r *service.UsageLog, _ map[string]any) { r.Model = "zero-cache"; r.CacheReadTokens = 0 }, 1)
	query.Model = "zero-cache"
	got, err = repo.ReadPrioritySchedulingSignals(ctx, query)
	require.NoError(t, err)
	require.Equal(t, 1, got[account.ID].CacheSamples)
	require.NotNil(t, got[account.ID].AvgCachePercent)
	require.Zero(t, *got[account.ID].AvgCachePercent)
	query.Model = "missing"
	got, err = repo.ReadPrioritySchedulingSignals(ctx, query)
	require.NoError(t, err)
	require.Zero(t, got[account.ID].CacheSamples)
	require.Nil(t, got[account.ID].AvgCachePercent)
}
