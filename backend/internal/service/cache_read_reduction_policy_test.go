package service

import (
	"context"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCacheReadReductionModelRules(t *testing.T) {
	start := 80.0
	rules, err := normalizeCacheReadReductionRules([]CacheReadReductionRule{
		{Model: " GPT-6.1-SOL ", Percent: 5, StartHitRatePercent: &start},
		{Model: "gpt-5.6-sol", Percent: 0},
	})
	require.NoError(t, err)
	group := &Group{CacheReadReductionPercent: 7, CacheReadReductionRules: rules}
	for _, tc := range []struct {
		model  string
		cached int
		want   float64
	}{
		{"gpt-6.1-sol", 800, 0}, {"GPT-6.1-SOL", 900, 2.5}, {"gpt-6.1-sol", 960, 4},
		{"gpt-6.1-sol", 1000, 5}, {"gpt-5.6-sol", 960, 0}, {"other", 960, 7}, {"other", 0, 0},
	} {
		require.InDelta(t, tc.want, cacheReadReductionPercent(group.cacheReadReductionPolicy(tc.model), 1000, tc.cached), 1e-9)
	}
	for _, rule := range []CacheReadReductionRule{
		{Model: "", Percent: 5}, {Model: "gpt-*", Percent: 5}, {Model: "gpt", Percent: math.NaN()},
		{Model: "gpt", Percent: 101}, {Model: "gpt", Percent: 5, StartHitRatePercent: float64Ptr(100)},
		{Model: "gpt", Percent: 5, StartHitRatePercent: float64Ptr(math.Inf(1))},
	} {
		_, err := normalizeCacheReadReductionRules([]CacheReadReductionRule{rule})
		require.Error(t, err)
	}
	_, err = normalizeCacheReadReductionRules([]CacheReadReductionRule{{Model: "gpt", Percent: 1}, {Model: "GPT", Percent: 2}})
	require.Error(t, err)
}

func TestCacheReadReductionDynamicDownstreamMatchesBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	start := 80.0
	group := &Group{RateMultiplier: .15, CacheReadReductionPercent: 50, CacheReadReductionRules: []CacheReadReductionRule{{Model: "requested", Percent: 5, StartHitRatePercent: &start}}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(context.WithValue(context.Background(), ctxkey.Model, "requested"))
	key := &APIKey{ID: 1, GroupID: int64Ptr(17), Group: group}
	c.Set("api_key", key)
	policy := downstreamCacheReadReductionPolicy(c)
	body := []byte(`{"response":{"model":"mapped","usage":{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":960}}}}`)
	written := reduceCacheReadUsagePayloadWithPolicy(body, policy)
	require.EqualValues(t, 922, gjson.GetBytes(written, "response.usage.input_tokens_details.cached_tokens").Int())
	require.EqualValues(t, 960, gjson.GetBytes(body, "response.usage.input_tokens_details.cached_tokens").Int(), "retain raw upstream usage")
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:        &OpenAIForwardResult{RequestID: "cache-policy", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 1000, CacheReadInputTokens: 960, OutputTokens: 20}, Duration: time.Second},
		OriginalModel: "requested", APIKey: key, User: &User{ID: 2}, Account: &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 922, usageRepo.lastLog.CacheReadTokens)
	require.Equal(t, 78, usageRepo.lastLog.InputTokens)
	require.Equal(t, 20, usageRepo.lastLog.OutputTokens)
	pricing, err := svc.billingService.GetModelPricing("gpt-5.1")
	require.NoError(t, err)
	wantCost := 78*pricing.InputPricePerToken + 922*pricing.CacheReadPricePerToken + 20*pricing.OutputPricePerToken
	require.InDelta(t, wantCost, usageRepo.lastLog.TotalCost, 1e-12)
	wantActual := math.Round(wantCost*.15*1e8) / 1e8
	require.InDelta(t, wantActual, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, wantActual, billingRepo.lastCmd.BalanceCost, 1e-12)
	claude := []byte(`{"usage":{"input_tokens":40,"cache_read_input_tokens":960,"output_tokens":20,"cache_creation_input_tokens":0}}`)
	got := reduceCacheReadUsagePayloadWithPolicy(claude, policy)
	require.EqualValues(t, 78, gjson.GetBytes(got, "usage.input_tokens").Int())
	require.EqualValues(t, 922, gjson.GetBytes(got, "usage.cache_read_input_tokens").Int())
	frame := "event: response.completed\ndata: " + string(body) + "\n\n"
	require.Contains(t, reduceClientVisibleSSEBlockWithPolicy(frame, policy), `"cached_tokens":922`)
}

func TestCacheReadReductionDynamicAliases(t *testing.T) {
	policy := CacheReadReductionRule{Percent: 5, StartHitRatePercent: float64Ptr(80)}
	for _, body := range []string{
		`{"usage":{"prompt_tokens":1000,"completion_tokens":20,"prompt_cache_hit_tokens":960,"prompt_cache_miss_tokens":40}}`,
		`{"usage":{"input_tokens":1000,"output_tokens":20,"prompt_tokens":1000,"cache_read_input_tokens":960,"prompt_cache_hit_tokens":960,"prompt_cache_miss_tokens":40}}`,
	} {
		got := reduceCacheReadUsagePayloadWithPolicy([]byte(body), policy)
		require.EqualValues(t, 1000, gjson.GetBytes(got, "usage.prompt_tokens").Int())
		require.EqualValues(t, 922, gjson.GetBytes(got, "usage.prompt_cache_hit_tokens").Int())
		require.EqualValues(t, 78, gjson.GetBytes(got, "usage.prompt_cache_miss_tokens").Int())
		if gjson.Get(body, "usage.input_tokens").Exists() {
			require.EqualValues(t, 78, gjson.GetBytes(got, "usage.input_tokens").Int())
		}
	}
}

func TestCacheReadReductionRulesUpdateAndSnapshot(t *testing.T) {
	rules := []CacheReadReductionRule{{Model: "model", Percent: 5, StartHitRatePercent: float64Ptr(80)}}
	repo := &cacheReductionGroupRepoStub{group: &Group{ID: 1, Name: "group", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1}}
	svc := &adminServiceImpl{groupRepo: repo}
	updated, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CacheReadReductionRules: &rules})
	require.NoError(t, err)
	require.Equal(t, rules, updated.CacheReadReductionRules)
	updated, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{})
	require.NoError(t, err)
	require.Equal(t, rules, updated.CacheReadReductionRules, "old clients preserve model overrides")
	keys := &APIKeyService{}
	snapshot := keys.snapshotFromAPIKey(context.Background(), &APIKey{ID: 1, User: &User{ID: 1}, Group: updated, ContextTokenLimit: 32000})
	roundTrip := keys.snapshotToAPIKey("fixture", snapshot)
	require.Equal(t, rules, roundTrip.Group.CacheReadReductionRules)
	require.Equal(t, 32000, roundTrip.ContextTokenLimit)
	legacy := *snapshot
	legacy.Version = 36
	key, accepted, err := keys.applyAuthCacheEntry("fixture", &APIKeyAuthCacheEntry{Snapshot: &legacy})
	require.NoError(t, err)
	require.False(t, accepted, "reject snapshots that predate combined context and cache rules")
	require.Nil(t, key)
	empty := []CacheReadReductionRule{}
	updated, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CacheReadReductionRules: &empty})
	require.NoError(t, err)
	require.Empty(t, updated.CacheReadReductionRules)
}
