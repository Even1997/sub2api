package service

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIssue715FlexibleAffinityFallsBackWithinAdmission(t *testing.T) {
	for _, state := range []string{"healthy", "rate_limited", "overloaded", "disabled", "auth_cooldown", "expired", "excluded", "deleted", "other_group", "model_removed"} {
		t.Run(state, func(t *testing.T) {
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 100, GroupIDs: []int64{715}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 0, GroupIDs: []int64{715}},
			}
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"), concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: true, 2: true}})}
			issue715LearnRoute(t, svc, &accounts[0], issue715OpaqueBody)
			future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
			var excluded map[int64]struct{}
			switch state {
			case "rate_limited":
				accounts[0].RateLimitResetAt = &future
			case "overloaded":
				accounts[0].OverloadUntil = &future
			case "disabled":
				accounts[0].Schedulable = false
			case "auth_cooldown":
				accounts[0].TempUnschedulableUntil = &future
			case "expired":
				accounts[0].AutoPauseOnExpired, accounts[0].ExpiresAt = true, &past
			case "excluded":
				excluded = map[int64]struct{}{1: {}}
			case "deleted":
				accounts = accounts[1:]
			case "other_group":
				accounts[0].GroupIDs = []int64{716}
			case "model_removed":
				accounts[0].Credentials = map[string]any{"model_mapping": map[string]any{"other": "other"}}
			}
			svc.accountRepo = schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}}
			c, _ := newTurnStateTestContext(t, 715, "fixture-session")
			c.Set("api_key", &APIKey{ID: 715, GroupID: int64Ptr(715)})
			ctx := svc.WithOpenAIAgentMessageRecovery(context.Background(), c, []byte(issue715OpaqueBody), "fixture-session", "gpt-6-astra")
			selection, _, err := svc.SelectAccountWithScheduler(ctx, int64Ptr(715), "", "fixture-session", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			want := int64(2)
			if state == "healthy" {
				want = 1
			}
			require.Equal(t, want, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			// Once normal sticky has moved, the producer hint must not pull it back.
			cache.sessionBindings["openai:fixture-session"] = 2
			selection, _, err = svc.SelectAccountWithScheduler(ctx, int64Ptr(715), "", "fixture-session", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestIssue715FlexibleAffinityPreservesMixedProducerRecords(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "shared"}[shared], func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			if shared {
				svc.cache = &issue715SharedOwners{}
			}
			a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			b := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			issue715LearnRoute(t, svc, a, issue715OpaqueBody)
			// Consumption on B keeps A's producer observation intact.
			issue715LearnRoute(t, svc, b, issue715OpaqueBody)
			second := strings.ReplaceAll(issue715OpaqueBody, "fixture-opaque-reference", "second-task")
			issue715LearnRoute(t, svc, b, second)
			mixed := strings.TrimSuffix(issue715OpaqueBody, "]}") + "," + strings.SplitN(second, `"input":[`, 2)[1]
			c, _ := newTurnStateTestContext(t, 715, "fixture-session")
			c.Set("api_key", &APIKey{ID: 715, GroupID: int64Ptr(715)})
			ctx := svc.WithOpenAIAgentMessageRecovery(context.Background(), c, []byte(mixed), "fixture-session", "gpt-6-astra")
			r := openAIAgentRecoveryFromContext(ctx)
			require.False(t, r.conflict)
			require.Len(t, r.known, 2)
			req, err := http.NewRequest(http.MethodPost, "https://native.example/v1/responses", bytes.NewBufferString(mixed))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer fixture")
			finish, err := svc.prepareOpenAIAgentRecoveryAttempt(ctx, a, req)
			require.NoError(t, err)
			finish(true)
			for i, key := range r.keys {
				var cache OpenAIAgentRecoveryCache
				if shared {
					cache = svc.cache.(OpenAIAgentRecoveryCache)
				}
				binding, found, err := svc.awaitOpenAIAgentRecovery(ctx, cache, &svc.agentMessageRecovery, key)
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, int64(i+1), binding.accountID)
			}
		})
	}
}

func TestIssue715ToolResultFailoverRequiresExplicitRejection(t *testing.T) {
	r := &openAIAgentRecoveryRequest{owner: &openAIAgentRecoveryBinding{accountID: 1}, toolResult: true}
	ctx := context.WithValue(context.Background(), openAIAgentRecoveryContextKey{}, r)
	require.True(t, OpenAIAgentToolResultCanFailover(ctx, &UpstreamFailoverError{StatusCode: 429, RequestRejected: true}))
	for _, status := range []int{0, 401, 403, 429, 500, 520} {
		require.False(t, OpenAIAgentToolResultCanFailover(ctx, &UpstreamFailoverError{StatusCode: status}))
	}
	r.previousResponse = "resp_stored"
	require.False(t, OpenAIAgentToolResultCanFailover(ctx, &UpstreamFailoverError{StatusCode: 429, RequestRejected: true}))
}

func TestIssue715FlexibleAffinityStillRechecksKnownRecordsAtSend(t *testing.T) {
	for _, state := range []string{"lost", "expired", "failed", "pending", "conflict"} {
		t.Run(state, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			issue715LearnRoute(t, svc, account, issue715OpaqueBody)
			c, _ := newTurnStateTestContext(t, 715, "fixture-session")
			c.Set("api_key", &APIKey{ID: 715, GroupID: int64Ptr(715)})
			ctx := svc.WithOpenAIAgentMessageRecovery(context.Background(), c, []byte(issue715OpaqueBody), "fixture-session", "gpt-6-astra")
			for key, binding := range svc.agentMessageRecovery.bindings {
				switch state {
				case "lost":
					delete(svc.agentMessageRecovery.bindings, key)
					continue
				case "expired":
					binding.expiresAt = time.Now().Add(-time.Second)
				case "failed":
					binding.failed = true
				case "pending":
					binding.pending = "fixture-producer"
				case "conflict":
					binding.conflicted = true
				}
				svc.agentMessageRecovery.bindings[key] = binding
			}
			req, err := http.NewRequest(http.MethodPost, "https://native.example/v1/responses", bytes.NewBufferString(issue715OpaqueBody))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer fixture")
			_, err = svc.prepareOpenAIAgentRecoveryAttempt(ctx, &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, req)
			require.Error(t, err, "account flexibility must not bypass the final delivery fence")
		})
	}
}

func TestIssue715OrdinaryChildFollowsCurrentParentWithFallback(t *testing.T) {
	for _, state := range []string{"healthy", "rate_limited", "disabled", "moved"} {
		t.Run(state, func(t *testing.T) {
			const parentID = "11111111-1111-4111-8111-111111111111"
			parentHash := DeriveSessionHashFromSeed(parentID)
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 100, GroupIDs: []int64{715}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 0, GroupIDs: []int64{715}},
			}
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:" + parentHash: 1}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache, accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}}, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"), concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: true, 2: true}})}
			issue715LearnRoute(t, svc, &accounts[0], issue715OpaqueBody)
			switch state {
			case "rate_limited":
				future := time.Now().Add(time.Hour)
				accounts[0].RateLimitResetAt = &future
			case "disabled":
				accounts[0].Schedulable = false
			case "moved":
				cache.sessionBindings["openai:"+parentHash] = 2
			}
			c, _ := newTurnStateTestContext(t, 715, "fixture-session")
			c.Set("api_key", &APIKey{ID: 715, GroupID: int64Ptr(715)})
			c.Request.Header.Set(openAISubagentHeader, "collab_spawn")
			c.Request.Header.Set(codexParentThreadIDHeader, parentID)
			ctx := WithOpenAIGuardianParentAffinity(context.Background(), c, []byte(issue715OpaqueBody), "gpt-6-astra")
			ctx = svc.WithOpenAIAgentMessageRecovery(ctx, c, []byte(issue715OpaqueBody), "fixture-session", "gpt-6-astra")
			selection, _, err := svc.SelectAccountWithScheduler(ctx, int64Ptr(715), "", "fixture-session", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			want := int64(2)
			if state == "healthy" {
				want = 1
			}
			require.Equal(t, want, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Zero(t, cache.deletedSessions["openai:"+parentHash])
			require.Equal(t, map[bool]int64{false: 1, true: 2}[state == "moved"], cache.sessionBindings["openai:"+parentHash], "child selection must not rewrite parent affinity")
		})
	}
}
