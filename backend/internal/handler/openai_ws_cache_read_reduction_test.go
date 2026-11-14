package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSCacheReadRuleRefreshAppliesAtTurnBoundary(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(0.15)
			repo.group.CacheReadReductionRules = []service.CacheReadReductionRule{{Model: "gpt-5.6-sol", Percent: 5}}
			keys := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
				apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
				upstreamUsage: `{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":960}}`,
				afterFirstUpstreamRequest: func(*service.ChannelService) error {
					return repo.adminUpdate(keys, func(_ *service.APIKey, group *service.Group) {
						group.CacheReadReductionRules = []service.CacheReadReductionRule{{Model: "gpt-5.6-sol", Percent: 0}}
					})
				},
			})
			for i, cached := range []int{912, 960} {
				require.EqualValues(t, cached, gjson.GetBytes(got.clientEvents[i], "response.usage.input_tokens_details.cached_tokens").Int())
				require.Equal(t, cached, got.logs[i].CacheReadTokens)
			}
		})
	}
}

func TestOpenAIWSCacheReadReductionMatchesBilledUsage(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		for _, dynamic := range []bool{false, true} {
			name := mode + "/fixed"
			if dynamic {
				name = mode + "/dynamic"
			}
			t.Run(name, func(t *testing.T) {
				group := wsAllowlistGroup(false)
				group.CacheReadReductionPercent = 50
				rule := service.CacheReadReductionRule{Model: "gpt-6.1-sol", Percent: 5}
				wantCached := 912
				if dynamic {
					start := 80.0
					rule.StartHitRatePercent = &start
					wantCached = 922
				}
				group.CacheReadReductionRules = []service.CacheReadReductionRule{rule, {Model: "gpt-5.6-sol", Percent: 0}}
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					group: group, ingressMode: mode,
					firstPayload:  `{"type":"response.create","model":"gpt-6.1-sol"}`,
					secondPayload: `{"type":"response.create","model":"gpt-5.6-sol"}`,
					upstreamUsage: `{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":960}}`,
				})
				require.Len(t, got.logs, 2)
				for i, want := range []int{wantCached, 960} {
					usage := gjson.GetBytes(got.clientEvents[i], "response.usage")
					require.EqualValues(t, want, usage.Get("input_tokens_details.cached_tokens").Int())
					require.EqualValues(t, 1000, usage.Get("input_tokens").Int())
					require.EqualValues(t, 20, usage.Get("output_tokens").Int())
					require.Equal(t, want, got.logs[i].CacheReadTokens)
					require.Equal(t, 1000-want, got.logs[i].InputTokens)
					require.Equal(t, 20, got.logs[i].OutputTokens)
					require.Greater(t, got.logs[i].TotalCost, 0.0)
				}
			})
		}
	}
}
