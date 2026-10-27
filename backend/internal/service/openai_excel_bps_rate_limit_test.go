package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bpsRateLimitState(t *testing.T, s *OpenAIGatewayService, a *Account, model string) excelBPSRateLimitState {
	t.Helper()
	v, ok := s.excelBPSModelRateLimits.Load(excelBPSRateLimitKey{a.ID, model})
	require.True(t, ok)
	return v.(excelBPSRateLimitState)
}

func TestBPSModelRateLimitIsolationAndSwitch(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6.1-sol"}
	on := context.WithValue(context.Background(), excelBPSRecoveryKey{}, true)
	off := context.WithValue(context.Background(), excelBPSRecoveryKey{}, false)
	s.coolDownExcelBPSAttempt(on, a, "gpt-6.1-sol", nil, nil, nil, 429)
	require.True(t, s.isExcelBPSCoolingDown(on, a, "alias"))
	require.False(t, s.isExcelBPSCoolingDown(on, a, "gpt-6-astra"))
	other := *a
	other.ID++
	require.False(t, s.isExcelBPSCoolingDown(on, &other, "alias"))
	require.False(t, s.isExcelBPSCoolingDown(off, a, "alias"), "turning recovery off ignores its configured model restriction")
	require.True(t, s.isExcelBPSCoolingDown(on, a, "alias"), "turning recovery back on respects the unexpired restriction")
	s.coolDownExcelBPSAttempt(off, &other, "gpt-6.1-sol", nil, nil, nil, 429)
	_, exists := s.excelBPSModelRateLimits.Load(excelBPSRateLimitKey{other.ID, "gpt-6.1-sol"})
	require.False(t, exists, "switch-off requests must not create configured model restrictions")
	native := context.WithValue(on, excelBPSSchedulingKey{}, false)
	require.False(t, s.isExcelBPSCoolingDown(native, a, "alias"))
	require.False(t, s.isOpenAIAccountRuntimeBlocked(a))
	require.True(t, a.IsSchedulable())
	a.Extra["openai_excel_bps"] = false
	require.False(t, s.isExcelBPSCoolingDown(on, a, "alias"))
}

func TestBPSModelRateLimitDefaultCooldownAndSuccess(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelAccount()
	ctx := context.WithValue(context.Background(), excelBPSRecoveryKey{}, true)
	model := "gpt-6.1-sol"
	for n := 0; n < 6; n++ {
		before := time.Now()
		s.coolDownExcelBPSAttempt(ctx, a, model, nil, nil, nil, 429)
		state := bpsRateLimitState(t, s, a, model)
		require.Equal(t, n+1, state.failures)
		require.WithinDuration(t, before.Add(10*time.Minute), state.until, time.Second)
	}
	original := bpsRateLimitState(t, s, a, model)
	for _, started := range []time.Time{time.Now().Add(-time.Hour), time.Now()} {
		s.resetExcelBPSModelRateLimit(a, model, started)
		require.Equal(t, original, bpsRateLimitState(t, s, a, model), "success must not lift an active configured restriction")
	}
	key := excelBPSRateLimitKey{a.ID, model}
	expired := original
	expired.until = time.Now().Add(-time.Second)
	s.excelBPSModelRateLimits.Store(key, expired)
	require.True(t, s.isExcelBPSCoolingDown(ctx, a, model), "expiry permits a probe, not unverified user traffic")
	s.resetExcelBPSModelRateLimit(a, model, time.Now())
	_, exists := s.excelBPSModelRateLimits.Load(key)
	require.False(t, exists, "a post-expiry success clears the old streak")
	s.coolDownExcelBPSAttempt(ctx, a, model, nil, nil, nil, 429)
	require.Equal(t, 1, bpsRateLimitState(t, s, a, model).failures)
	old := bpsRateLimitState(t, s, a, model)
	old.until = time.Now().Add(-excelBPSRateLimitStreakTTL - time.Minute)
	s.excelBPSModelRateLimits.Store(key, old)
	s.coolDownExcelBPSAttempt(ctx, a, model, nil, nil, nil, 429)
	require.Equal(t, 1, bpsRateLimitState(t, s, a, model).failures)
}

func TestBPSModelRateLimitHintsDoNotChangeConfiguredCooldown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hint    *basispoints.RateLimitHint
		headers http.Header
	}{
		{"no hint", nil, nil},
		{"body seconds", &basispoints.RateLimitHint{ResetAfterSeconds: 120}, nil},
		{"body absolute", &basispoints.RateLimitHint{ResetAtUnix: time.Now().Unix() + 180}, nil},
		{"retry after", nil, http.Header{"Retry-After": []string{"90"}}},
		{"expired", &basispoints.RateLimitHint{ResetAtUnix: 1}, nil},
		{"body longer than header", &basispoints.RateLimitHint{ResetAfterSeconds: 120}, http.Header{"Retry-After": []string{"90"}}},
		{"header longer than body", &basispoints.RateLimitHint{ResetAfterSeconds: 120}, http.Header{"Retry-After": []string{"180"}}},
		{"oversized body", &basispoints.RateLimitHint{ResetAfterSeconds: 9223372036854775807}, nil},
		{"oversized header", nil, http.Header{"Retry-After": []string{"9223372036854775807"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &OpenAIGatewayService{}
			a := excelAccount()
			ctx := context.WithValue(context.Background(), excelBPSRecoveryKey{}, true)
			before := time.Now()
			s.coolDownExcelBPSAttempt(ctx, a, "gpt-6.1-sol", tc.headers, nil, &basispoints.UpstreamFailure{Status: 429, Code: "rate_limit_exceeded", RateLimit: tc.hint}, 200)
			state := bpsRateLimitState(t, s, a, "gpt-6.1-sol")
			require.Equal(t, "model_limit_cooldown", state.source)
			require.WithinDuration(t, before.Add(10*time.Minute), state.until, 2*time.Second)
			s.coolDownExcelBPSAttempt(ctx, a, "gpt-6.1-sol", http.Header{"Retry-After": []string{"1"}}, nil, &basispoints.UpstreamFailure{Status: 429}, 200)
			require.False(t, bpsRateLimitState(t, s, a, "gpt-6.1-sol").until.Before(state.until))
		})
	}
}

func TestBPSModelRateLimitRepeatedFailureRestartsCooldown(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelAccount()
	ctx := context.WithValue(context.Background(), excelBPSRecoveryKey{}, true)
	key := excelBPSRateLimitKey{a.ID, "gpt-6.1-sol"}
	for _, remaining := range []time.Duration{time.Minute, 48 * time.Hour} {
		previous := excelBPSRateLimitState{until: time.Now().Add(remaining), lastFailure: time.Now().Add(-time.Hour), failures: 1, source: "model_limit_cooldown"}
		s.excelBPSModelRateLimits.Store(key, previous)
		before := time.Now()
		s.coolDownExcelBPSAttempt(ctx, a, key.model, nil, nil, nil, 429)
		state := bpsRateLimitState(t, s, a, key.model)
		require.False(t, state.until.Before(previous.until), "a concurrent failure cannot shorten an existing deadline")
		if remaining < 10*time.Minute {
			require.WithinDuration(t, before.Add(10*time.Minute), state.until, time.Second)
		} else {
			require.Equal(t, previous.until, state.until)
		}
	}
}

func TestBPSModelRateLimitConcurrentUpdates(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelAccount()
	ctx := context.WithValue(context.Background(), excelBPSRecoveryKey{}, true)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.coolDownExcelBPSAttempt(ctx, a, "gpt-6.1-sol", nil, nil, nil, 429) }()
	}
	wg.Wait()
	require.Equal(t, 12, bpsRateLimitState(t, s, a, "gpt-6.1-sol").failures)
}

func TestBPSRateLimitBudgetCountsDistinctAccounts(t *testing.T) {
	s := &OpenAIGatewayService{}
	route := &excelBPSFailoverRoute{model: "gpt-6.1-sol"}
	ctx := context.WithValue(context.WithValue(context.Background(), excelBPSRecoveryKey{}, true), excelBPSFailoverRouteKey{}, route)
	a := excelAccount()
	for _, id := range []int64{1, 1, 2, 3} {
		a.ID = id
		f := &UpstreamFailoverError{StatusCode: 429}
		s.limitExcelBPS429Failover(ctx, nil, a, "gpt-6.1-sol", f)
		require.Equal(t, id != 3, f.ShouldRetryNextAccount())
	}
	s.limitExcelBPS429Failover(ctx, nil, a, "gpt-6.1-sol", &UpstreamFailoverError{StatusCode: 503})
	f := &UpstreamFailoverError{StatusCode: 429}
	s.limitExcelBPS429Failover(ctx, nil, a, "gpt-6.1-sol", f)
	require.True(t, f.ShouldRetryNextAccount())
	off := context.WithValue(ctx, excelBPSRecoveryKey{}, false)
	for _, id := range []int64{4, 5, 6, 7} {
		a.ID = id
		f := &UpstreamFailoverError{StatusCode: 429}
		s.limitExcelBPS429Failover(off, nil, a, "gpt-6.1-sol", f)
		require.True(t, f.ShouldRetryNextAccount())
	}
}

func TestBPSRateLimitSSETimingReachesGateway(t *testing.T) {
	for _, stream := range []bool{true, false} {
		upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_limit\",\"status\":\"failed\",\"output\":[],\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"PRIVATE\",\"resets_in_seconds\":120}}}\n\n")}
		svc := openAIExcelBPSRecoveryTestService(upstream)
		a := excelAccount()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		body := []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":false}`)
		if stream {
			body = []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":true}`)
		}
		_, err := svc.Forward(context.Background(), c, a, body)
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.True(t, failure.ShouldRetryNextAccount())
		require.Empty(t, rec.Body.String())
		state := bpsRateLimitState(t, svc, a, "gpt-6.1-sol")
		require.Equal(t, "model_limit_cooldown", state.source)
		require.WithinDuration(t, time.Now().Add(10*time.Minute), state.until, 2*time.Second)
		require.False(t, svc.isExcelBPSRateLimited(a, "gpt-6-astra"))
		require.False(t, svc.isOpenAIAccountRuntimeBlocked(a))
	}
}
