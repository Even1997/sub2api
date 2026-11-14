package service

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type openAIQuotaCostRepoStub struct {
	UsageLogRepository
	stats   map[int64]*usagestats.AccountStats
	calls   int
	samples []map[int64]OpenAIQuotaCostSample
}

func (s *openAIQuotaCostRepoStub) GetOpenAIQuotaWindowStatsBatch(_ context.Context, samples map[int64]OpenAIQuotaCostSample) (map[int64]*usagestats.AccountStats, error) {
	s.calls++
	s.samples = append(s.samples, samples)
	result := make(map[int64]*usagestats.AccountStats, len(samples))
	for accountID := range samples {
		result[accountID] = s.stats[accountID]
	}
	return result, nil
}

func TestGetOpenAIQuotaCostSummary(t *testing.T) {
	now := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	repo := &openAIQuotaCostRepoStub{stats: map[int64]*usagestats.AccountStats{
		1: {Cost: 4},
		2: {Cost: 3},
	}}
	svc := &AccountUsageService{usageLogRepo: repo}
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
			"codex_5h_used_percent":  50.0,
			"codex_5h_reset_at":      now.Add(2 * time.Hour).Format(time.RFC3339),
			"codex_7d_used_percent":  25.0,
			"codex_7d_reset_at":      now.Add(4 * 24 * time.Hour).Format(time.RFC3339),
		}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-2 * time.Minute).Format(time.RFC3339),
			"codex_5h_used_percent":  75.0,
			"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
			"codex_7d_used_percent":  50.0,
			"codex_7d_reset_at":      now.Add(2 * 24 * time.Hour).Format(time.RFC3339),
		}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}

	summary, err := svc.GetOpenAIQuotaCostSummary(context.Background(), accounts, now)
	require.NoError(t, err)
	require.Equal(t, 2, repo.calls)
	require.NotNil(t, summary.FiveHour.RemainingCost)
	require.NotNil(t, summary.FiveHour.EstimatedCost)
	require.InDelta(t, 5, *summary.FiveHour.RemainingCost, 1e-9)
	require.InDelta(t, 12, *summary.FiveHour.EstimatedCost, 1e-9)
	require.NotNil(t, summary.SevenDay.RemainingCost)
	require.NotNil(t, summary.SevenDay.EstimatedCost)
	require.InDelta(t, 15, *summary.SevenDay.RemainingCost, 1e-9)
	require.InDelta(t, 22, *summary.SevenDay.EstimatedCost, 1e-9)
	require.Equal(t, 2, summary.FiveHour.EstimatedAccounts)
	require.Equal(t, 2, summary.FiveHour.TotalAccounts)
	require.Equal(t, now.Add(-3*time.Hour).Unix(), repo.samples[0][1].StartAt)
	require.Equal(t, now.Add(-time.Minute).Unix(), repo.samples[0][1].SampledAt)
	var remainingByAccount, totalByAccount float64
	for _, account := range accounts {
		individual, err := svc.GetOpenAIQuotaCostSummary(context.Background(), []Account{account}, now)
		require.NoError(t, err)
		if individual.FiveHour.EstimatedCost != nil {
			remainingByAccount += *individual.FiveHour.RemainingCost
			totalByAccount += *individual.FiveHour.EstimatedCost
		}
	}
	require.InDelta(t, *summary.FiveHour.RemainingCost, remainingByAccount, 1e-9)
	require.InDelta(t, *summary.FiveHour.EstimatedCost, totalByAccount, 1e-9)
}

func TestGetOpenAIQuotaCostSummaryUnknownIsNotZero(t *testing.T) {
	now := time.Now().UTC()
	repo := &openAIQuotaCostRepoStub{}
	svc := &AccountUsageService{usageLogRepo: repo}
	summary, err := svc.GetOpenAIQuotaCostSummary(context.Background(), []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
			"codex_5h_used_percent":  0.0,
			"codex_usage_updated_at": now.Format(time.RFC3339),
		}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}, now)
	require.NoError(t, err)
	require.Nil(t, summary.FiveHour.EstimatedCost)
	require.Nil(t, summary.FiveHour.RemainingCost)
	require.Equal(t, 2, summary.FiveHour.TotalAccounts)
	require.Zero(t, summary.FiveHour.EstimatedAccounts)
	require.Equal(t, 2, summary.SevenDay.TotalAccounts)
}

func TestGetOpenAIQuotaCostSummaryRetainsCapacity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sample := OpenAIQuotaCostSample{
		StartAt: now.Add(-7 * time.Hour).Unix(), SampledAt: now.Add(-3 * time.Hour).Unix(),
		UsedPercent: 50, PlanType: "plus", RateMultiplier: 1,
	}
	for _, tt := range []struct {
		name    string
		used    any
		plan    string
		rate    float64
		expired bool
		want    *float64
	}{
		{name: "confirmed reset", used: 0.0, plan: "plus", rate: 1, want: ptrQuotaCost(40)},
		{name: "tiny new sample", used: 1.0, plan: "plus", rate: 1, want: ptrQuotaCost(39.6)},
		{name: "fully consumed", used: 100.0, plan: "plus", rate: 1, want: ptrQuotaCost(0)},
		{name: "expired is not confirmed reset", used: 50.0, plan: "plus", rate: 1, expired: true},
		{name: "missing utilization", used: nil, plan: "plus", rate: 1},
		{name: "invalid utilization", used: "bad", plan: "plus", rate: 1},
		{name: "nonfinite utilization", used: math.NaN(), plan: "plus", rate: 1},
		{name: "plan changed", used: 0.0, plan: "pro", rate: 1},
		{name: "rate changed", used: 0.0, plan: "plus", rate: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reset := now.Add(4 * time.Hour)
			if tt.expired {
				reset = now.Add(-time.Second)
			}
			account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"plan_type": tt.plan}, RateMultiplier: &tt.rate,
				Extra: map[string]any{
					"codex_5h_cost_sample": sample, "codex_5h_used_percent": tt.used,
					"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
					"codex_5h_reset_at":      reset.Format(time.RFC3339),
				}}
			repo := &openAIQuotaCostRepoStub{stats: map[int64]*usagestats.AccountStats{1: {Cost: 20}}}
			svc := &AccountUsageService{usageLogRepo: repo}
			summary, err := svc.GetOpenAIQuotaCostSummary(context.Background(), []Account{account}, now)
			require.NoError(t, err)
			require.Equal(t, 1, summary.FiveHour.TotalAccounts)
			if tt.want == nil {
				require.Nil(t, summary.FiveHour.RemainingCost)
				require.Zero(t, summary.FiveHour.EstimatedAccounts)
			} else {
				require.NotNil(t, summary.FiveHour.RemainingCost)
				require.InDelta(t, *tt.want, *summary.FiveHour.RemainingCost, 1e-9)
				require.InDelta(t, 40, *summary.FiveHour.EstimatedCost, 1e-9)
				require.Equal(t, 1, summary.FiveHour.EstimatedAccounts)
				require.Equal(t, sample, repo.samples[0][1])
			}
		})
	}
}

func ptrQuotaCost(value float64) *float64 { return &value }

func TestOpenAIQuotaCostUsesFieldObservationTimes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fiveAt, sevenAt := now.Add(-time.Minute), now.Add(-6*time.Hour)
	extra := map[string]any{
		"codex_usage_updated_at":       now.Format(time.RFC3339),
		"codex_5h_observed_at":         now.Add(-24 * time.Hour).Format(time.RFC3339),
		"codex_5h_used_percent":        50.0,
		"codex_5h_reset_after_seconds": 3600,
		"codex_7d_used_percent":        25.0,
		"codex_7d_reset_after_seconds": 86400,
		CodexUsageFieldVersionsKey: map[string]any{
			"codex_5h_used_percent":        strconv.FormatInt(fiveAt.UnixNano(), 10),
			"codex_5h_reset_after_seconds": strconv.FormatInt(fiveAt.UnixNano(), 10),
			"codex_7d_used_percent":        strconv.FormatInt(sevenAt.UnixNano(), 10),
			"codex_7d_reset_after_seconds": strconv.FormatInt(sevenAt.UnixNano(), 10),
		},
	}
	for _, tt := range []struct {
		window          string
		at              time.Time
		used            float64
		duration, reset time.Duration
	}{
		{OpenAIQuotaWindow5h, fiveAt, 50, 5 * time.Hour, time.Hour},
		{OpenAIQuotaWindow7d, sevenAt, 25, 7 * 24 * time.Hour, 24 * time.Hour},
	} {
		t.Run(tt.window, func(t *testing.T) {
			used, known := openAIQuotaCurrentUsage(extra, tt.window, now)
			require.True(t, known, "current field versions supersede stale legacy observation times")
			require.Equal(t, tt.used, used)
			sample := NewOpenAIQuotaCostSample(extra, tt.window)
			require.NotNil(t, sample)
			require.Equal(t, tt.at.Unix(), sample.SampledAt)
			require.Equal(t, tt.at.Add(tt.reset-tt.duration).Unix(), sample.StartAt)
		})
	}
	versions := extra[CodexUsageFieldVersionsKey].(map[string]any)
	versions["codex_5h_reset_after_seconds"] = strconv.FormatInt(sevenAt.UnixNano(), 10)
	require.Nil(t, NewOpenAIQuotaCostSample(extra, OpenAIQuotaWindow5h), "mixed-generation fields cannot calibrate capacity")
}

func TestNewOpenAIQuotaCostSampleRejectsUnreliableSnapshots(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tt := range []struct {
		name  string
		key   string
		value any
	}{
		{"tiny", "codex_5h_used_percent", 1.0},
		{"reset", "codex_5h_used_percent", 0.0},
		{"missing time", "codex_usage_updated_at", nil},
		{"expired", "codex_5h_reset_at", now.Add(-time.Minute).Format(time.RFC3339)},
		{"wrong window", "codex_5h_window_minutes", 10080},
		{"above 100", "codex_5h_used_percent", 101.0},
		{"infinite", "codex_5h_used_percent", math.Inf(1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			extra := map[string]any{"codex_5h_used_percent": 50.0,
				"codex_usage_updated_at": now.Format(time.RFC3339),
				"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339)}
			require.NotNil(t, NewOpenAIQuotaCostSample(extra, OpenAIQuotaWindow5h))
			extra[tt.key] = tt.value
			require.Nil(t, NewOpenAIQuotaCostSample(extra, OpenAIQuotaWindow5h))
		})
	}
}

func TestOpenAIQuotaCurrentUsageDoesNotRefreshOtherWindow(t *testing.T) {
	now := time.Now().UTC()
	extra := map[string]any{
		"codex_5h_used_percent":  0.0,
		"codex_5h_observed_at":   now.Add(-6 * time.Hour).Format(time.RFC3339),
		"codex_usage_updated_at": now.Format(time.RFC3339),
	}
	_, known := openAIQuotaCurrentUsage(extra, OpenAIQuotaWindow5h, now)
	require.False(t, known)
}

func TestGetOpenAIQuotaCostSummaryWindowsAreIndependent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"codex_5h_used_percent": 0.0, "codex_7d_used_percent": 100.0,
		"codex_usage_updated_at": now.Format(time.RFC3339),
		"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
		"codex_7d_reset_at":      now.Add(24 * time.Hour).Format(time.RFC3339),
		"codex_5h_cost_sample":   OpenAIQuotaCostSample{StartAt: now.Add(-8 * time.Hour).Unix(), SampledAt: now.Add(-4 * time.Hour).Unix(), UsedPercent: 50, RateMultiplier: 1},
	}}
	repo := &openAIQuotaCostRepoStub{stats: map[int64]*usagestats.AccountStats{1: {Cost: 20}}}
	svc := &AccountUsageService{usageLogRepo: repo}
	summary, err := svc.GetOpenAIQuotaCostSummary(context.Background(), []Account{account}, now)
	require.NoError(t, err)
	require.Equal(t, 40.0, *summary.FiveHour.RemainingCost)
	require.Equal(t, 0.0, *summary.SevenDay.RemainingCost)
	require.Equal(t, 2, repo.calls)
}
