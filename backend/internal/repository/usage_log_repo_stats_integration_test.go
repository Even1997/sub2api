//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLog_UpstreamModelMismatchFilterAndPartialIndex(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "model-audit@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-model-audit", Name: "model-audit"})
	account := mustCreateAccount(t, client, &service.Account{Name: "model-audit-account"})
	now := time.Now().UTC()
	responseModel := "gpt-5.4"
	for _, mismatch := range []bool{true, false} {
		mismatchValue := mismatch
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			Model: "gpt-5.5", InputTokens: 1, OutputTokens: 1,
			UpstreamResponseModel: &responseModel, UpstreamModelMismatch: &mismatchValue,
			CreatedAt: now,
		})
		require.NoError(t, err)
	}

	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	trueValue := true
	stats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
		UserID: user.ID, StartTime: &start, EndTime: &end, UpstreamModelMismatch: &trueValue,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, []usagestats.EndpointStat{{
		Endpoint: "unknown", Requests: 1, TotalTokens: 2,
	}}, stats.Endpoints)
	require.Equal(t, []usagestats.EndpointStat{{
		Endpoint: "unknown", Requests: 1, TotalTokens: 2,
	}}, stats.UpstreamEndpoints)
	require.Equal(t, []usagestats.EndpointStat{{
		Endpoint: "unknown -> unknown", Requests: 1, TotalTokens: 2,
	}}, stats.EndpointPaths)

	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", usagestats.UsageLogFilters{
		UserID: user.ID, UpstreamModelMismatch: &trueValue,
	})
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, int64(1), trend[0].Requests)

	// With only the two matching rows, PostgreSQL can legitimately prefer the
	// older created_at index plus incremental sort. Give the expression indexes
	// a selective model predicate and current statistics so this checks the
	// intended access paths rather than a tiny-table cost tie. All fixture rows
	// remain in this test's rollback-only transaction; index assertions stay exact.
	seeded, err := tx.ExecContext(ctx, `
INSERT INTO usage_logs (user_id, api_key_id, account_id, model, requested_model, upstream_model, created_at)
SELECT $1, $2, $3, 'index-fixture-other', 'index-fixture-other', 'index-fixture-other',
       $4::timestamptz - (g % 3600) * INTERVAL '1 second'
FROM generate_series(1, 10000) AS g`, user.ID, apiKey.ID, account.ID, now)
	require.NoError(t, err)
	seedCount, err := seeded.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 10000, seedCount)
	_, err = tx.ExecContext(ctx, "ANALYZE usage_logs")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)
	assertPlanUsesIndex := func(query, indexName string, args ...any) {
		rows, queryErr := tx.QueryContext(ctx, query, args...)
		require.NoError(t, queryErr)
		var planLines []string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			planLines = append(planLines, line)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Contains(t, strings.Join(planLines, "\n"), indexName)
	}
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE upstream_model_mismatch IS TRUE
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsUpstreamModelMismatchIndex)
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE COALESCE(NULLIF(TRIM(requested_model), ''), model) = $1
  AND created_at >= $2 AND created_at < $3
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsEffectiveRequestedModelIndex, "gpt-5.5", start, end)
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE COALESCE(NULLIF(TRIM(upstream_model), ''), model) = $1
  AND created_at >= $2 AND created_at < $3
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsEffectiveUpstreamModelIndex, "gpt-5.5", start, end)
}

func TestUsageLog_PersonalTrendTimezoneAtDateBoundary(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "trend-timezone@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-trend-timezone", Name: "trend"})
	account := mustCreateAccount(t, client, &service.Account{Name: "trend-timezone"})
	_, err := repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
		Model: "gpt-5", InputTokens: 2, OutputTokens: 3, TotalCost: 0.5, ActualCost: 0.4,
		CreatedAt: time.Date(2026, 10, 12, 16, 30, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	for _, tc := range []struct{ zone, day, hour string }{
		{"Asia/Shanghai", "2026-10-13", "00:00"},
		{"America/Los_Angeles", "2026-10-12", "09:00"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			location, loadErr := time.LoadLocation(tc.zone)
			require.NoError(t, loadErr)
			start, parseErr := time.ParseInLocation("2006-01-02", tc.day, location)
			require.NoError(t, parseErr)
			for _, granularity := range []string{"hour", "day"} {
				trend, trendErr := repo.GetUsageTrendWithUsageFilters(ctx, start, start.AddDate(0, 0, 1), granularity, usagestats.UsageLogFilters{UserID: user.ID})
				require.NoError(t, trendErr)
				require.Len(t, trend, 1)
				expected := tc.day
				if granularity == "hour" {
					expected += " " + tc.hour
				}
				require.Equal(t, expected, trend[0].Date)
				require.Equal(t, int64(1), trend[0].Requests)
			}
		})
	}
}

func TestUsageLog_GetStatsWithFilters_AggregatesAndEndpoints(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "stats@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-stats-1", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "acc-stats"})

	now := time.Now().UTC()
	inboundEndpoint := "/v1/messages"
	upstreamEndpoint := "/v1/responses"
	for i := 0; i < 3; i++ {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			Model: "claude-3", InputTokens: 2, OutputTokens: 3,
			CacheCreationTokens: 4, CacheReadTokens: 5,
			TotalCost: 0.5, ActualCost: 0.4, CreatedAt: now,
			InboundEndpoint: &inboundEndpoint, UpstreamEndpoint: &upstreamEndpoint,
		})
		require.NoError(t, err)
	}

	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)
	// 按本测试创建的 user 维度过滤:集成库为共享实例,其它用 testEntClient 的兄弟测试会留下
	// 已提交的 usage_log 行(含零 token 的失败请求),不限定 user 会把它们计入 TotalRequests。
	stats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
	require.Equal(t, int64(6), stats.TotalInputTokens)
	require.Equal(t, int64(9), stats.TotalOutputTokens)
	require.Equal(t, int64(27), stats.TotalCacheTokens)
	require.Equal(t, int64(12), stats.TotalCacheCreationTokens)
	require.Equal(t, int64(15), stats.TotalCacheReadTokens)
	require.InDelta(t, 1.2, stats.TotalActualCost, 1e-9)
	require.NotEmpty(t, stats.Endpoints)
	require.NotEmpty(t, stats.UpstreamEndpoints)
	require.NotEmpty(t, stats.EndpointPaths)
}

func TestUsageLog_ModelProviderFiltersEveryPersonalQuery(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "provider@test.com"})
	other := mustCreateUser(t, client, &service.User{Email: "other-provider@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-provider", Name: "provider"})
	otherKey := mustCreateApiKey(t, client, &service.APIKey{UserID: other.ID, Key: "sk-other-provider", Name: "other"})
	account := mustCreateAccount(t, client, &service.Account{Name: "provider"})
	group := mustCreateGroup(t, client, &service.Group{Name: "OpenAI relay", Platform: "openai"})
	now := time.Now().UTC()
	for _, model := range []string{"gpt-5", "OpenAI/o3", "claude-sonnet-4", "anthropic/CLAUDE-opus-4", "xai/grok-4", "google/gemini-3-pro", "unknown"} {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, GroupID: &group.ID,
			Model: "gpt-upstream", RequestedModel: model, InputTokens: 2, OutputTokens: 3,
			TotalCost: 10, ActualCost: 0.4, RateMultiplier: 99, CreatedAt: now,
		})
		require.NoError(t, err)
	}
	_, err := repo.Create(ctx, &service.UsageLog{UserID: other.ID, APIKeyID: otherKey.ID, AccountID: account.ID, Model: "claude-sonnet-4", TotalCost: 100, ActualCost: 100, CreatedAt: now})
	require.NoError(t, err)
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	for provider, count := range map[string]int{"openai": 2, "anthropic": 2, "grok": 1, "gemini": 1} {
		t.Run(provider, func(t *testing.T) {
			filters := usagestats.UsageLogFilters{UserID: user.ID, GroupID: group.ID, ModelProvider: provider, StartTime: &start, EndTime: &end}
			logs, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
			require.NoError(t, err)
			require.Len(t, logs, count)
			require.Equal(t, int64(count), page.Total)
			stats, err := repo.GetStatsWithFilters(ctx, filters)
			require.NoError(t, err)
			require.Equal(t, int64(count), stats.TotalRequests)
			require.InDelta(t, float64(count)*0.4, stats.TotalActualCost, 1e-9)
			require.InDelta(t, float64(count)*10, stats.TotalCost, 1e-9)
			trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", filters)
			require.NoError(t, err)
			require.Len(t, trend, 1)
			require.Equal(t, int64(count), trend[0].Requests)
			models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
			require.NoError(t, err)
			require.Len(t, models, count)
			groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
			require.NoError(t, err)
			require.Len(t, groups, 1)
			require.Equal(t, int64(count), groups[0].Requests)
		})
	}
}

func TestUsageLog_ReportsMissingHistoryWithoutInventingCosts(t *testing.T) {
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { require.NoError(t, timezone.Init("UTC")) })
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "retention@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-retention", Name: "retention"})
	account := mustCreateAccount(t, client, &service.Account{Name: "retention"})
	_, err := tx.ExecContext(ctx, "DELETE FROM usage_logs")
	require.NoError(t, err)
	retained := time.Date(2026, 7, 15, 8, 58, 2, 0, time.UTC)
	_, err = repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "gpt-5", TotalCost: 10, ActualCost: 3.25, CreatedAt: retained})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "INSERT INTO usage_dashboard_daily_users (bucket_date, user_id) VALUES ('2026-05-23', $1), ('2026-07-15', $1)", user.ID)
	require.NoError(t, err)
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := retained.AddDate(0, 0, 1)
	filters := usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end}
	stats, err := repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.True(t, stats.HistoryIncomplete)
	require.True(t, retained.Equal(*stats.RecordsAvailableFrom))
	require.InDelta(t, 3.25, stats.TotalActualCost, 1e-9)
	start = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.False(t, stats.HistoryIncomplete, "the first retained day alone is not evidence of deletion")
	start = time.Date(2026, 5, 23, 16, 0, 0, 0, time.UTC)
	end = start.Add(time.Hour)
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.False(t, stats.HistoryIncomplete, "the selected range starts after the missing Shanghai day")
	start = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	end = retained.AddDate(0, 0, 1)
	_, err = tx.ExecContext(ctx, "DELETE FROM usage_logs WHERE user_id = $1", user.ID)
	require.NoError(t, err)
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.True(t, stats.HistoryIncomplete)
	require.Nil(t, stats.RecordsAvailableFrom)
	require.Zero(t, stats.TotalActualCost)
}
