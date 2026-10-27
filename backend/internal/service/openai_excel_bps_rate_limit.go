package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	excelBPSRateLimitStreakTTL   = 15 * time.Minute
	excelBPSRateLimitMaxAccounts = 3
)

type excelBPSRateLimitKey struct {
	accountID int64
	model     string
}
type excelBPSRateLimitState struct {
	until, lastFailure time.Time
	failures           int
	source             string
}

// Ordinary BPS 429s do not establish that the primary Codex grant is exhausted.
// Keep the opt-in configurable restriction scoped to the actual BPS model. Explicit HTTP account
// quota signals retain the existing account-wide handling.
func (s *OpenAIGatewayService) coolDownExcelBPSAttempt(ctx context.Context, account *Account, model string, headers http.Header, raw []byte, failure *basispoints.UpstreamFailure, httpStatus int) {
	if s == nil || account == nil || isQualityObservation(ctx) || isControlledExperiment(ctx) {
		return
	}
	if !s.excelBPSRecoveryEnabled(ctx) || strings.TrimSpace(model) == "" {
		s.coolDownExcelBPS(ctx, account, headers, raw)
		return
	}
	if failure == nil {
		accountQuota := gjson.GetBytes(raw, "error.type").String() == "usage_limit_reached"
		if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
			if normalized := snapshot.Normalize(); normalized != nil {
				accountQuota = accountQuota || (normalized.Used5hPercent != nil && *normalized.Used5hPercent >= 100) || (normalized.Used7dPercent != nil && *normalized.Used7dPercent >= 100)
			}
		}
		if accountQuota {
			s.coolDownExcelBPS(ctx, account, headers, raw)
			return
		}
	}
	cooldown := s.settingService.GetExcelBPSModelRateLimitCooldown(ctx)
	now := time.Now()
	hint := basispoints.ParseRateLimitHint(raw)
	code := "basispoints_rate_limited"
	switch upstreamCode := gjson.GetBytes(raw, "error.code").String(); upstreamCode {
	case "rate_limit_exceeded", "rate_limit_error", "insufficient_quota", "usage_limit_reached":
		code = upstreamCode
	}
	if failure != nil {
		hint, code = failure.RateLimit, failure.Code
	}
	key := excelBPSRateLimitKey{account.ID, model}
	var stored excelBPSRateLimitState
	for {
		value, loaded := s.excelBPSModelRateLimits.Load(key)
		previous := excelBPSRateLimitState{}
		if loaded {
			previous = value.(excelBPSRateLimitState)
		}
		failures := 1
		if loaded && now.Before(previous.until.Add(excelBPSRateLimitStreakTTL)) {
			failures = min(previous.failures+1, 16)
		}
		// A model limit blocks this account/model for the configured duration from the latest
		// failure. Upstream reset hints are diagnostic only for this opt-in policy.
		source := "model_limit_cooldown"
		until := now.Add(cooldown)
		if previous.until.After(until) {
			until, source = previous.until, previous.source
		}
		lastFailure := now
		if previous.lastFailure.After(lastFailure) {
			lastFailure = previous.lastFailure
		}
		stored = excelBPSRateLimitState{until: until, lastFailure: lastFailure, failures: failures, source: source}
		if !loaded {
			if _, exists := s.excelBPSModelRateLimits.LoadOrStore(key, stored); !exists {
				break
			}
		} else if s.excelBPSModelRateLimits.CompareAndSwap(key, value, stored) {
			break
		}
	}
	// A cooldown deadline permits a background probe, never unverified user
	// traffic. Reuse model recovery so permission and quota failures share the
	// configured interval, bounded worker and stale-result protection.
	s.scheduleExcelBPSModelRecovery(ctx, account, model, stored.until, true)
	// Bounded idle retention, including models no longer selected by the scheduler.
	lastSweep := s.excelBPSRateLimitSweepAt.Load()
	if now.Unix()-lastSweep >= 60 && s.excelBPSRateLimitSweepAt.CompareAndSwap(lastSweep, now.Unix()) {
		s.excelBPSModelRateLimits.Range(func(k, v any) bool {
			state := v.(excelBPSRateLimitState)
			if !now.Before(state.until.Add(excelBPSRateLimitStreakTTL)) {
				s.excelBPSModelRateLimits.CompareAndDelete(k, v)
			}
			return true
		})
	}
	fields := []zap.Field{
		zap.String("component", "service.openai_excel_bps"), zap.Int64("account_id", account.ID), zap.String("model", model),
		zap.String("scope", "bps_account_model"), zap.Int("upstream_http_status", httpStatus), zap.String("upstream_error_code", code),
		zap.String("cooldown_source", stored.source), zap.Time("cooldown_until", stored.until), zap.Int64("cooldown_seconds", int64((stored.until.Sub(now)+time.Second-1)/time.Second)), zap.Int("consecutive_failures", stored.failures),
	}
	if hint != nil {
		fields = append(fields, zap.Int64("upstream_reset_after_seconds", hint.ResetAfterSeconds), zap.Int64("upstream_reset_at_unix", hint.ResetAtUnix))
	}
	logger.FromContext(ctx).Warn("openai.excel_bps_model_rate_limited", fields...)
}

func (s *OpenAIGatewayService) resetExcelBPSModelRateLimit(account *Account, upstreamModel string, attemptStarted time.Time) {
	key := excelBPSRateLimitKey{account.ID, upstreamModel}
	if value, ok := s.excelBPSModelRateLimits.Load(key); ok {
		state := value.(excelBPSRateLimitState)
		// A successful response (including one already in flight) cannot lift an
		// active model restriction. Only a post-expiry attempt can clear it.
		if !state.until.After(attemptStarted) && !state.lastFailure.After(attemptStarted) {
			s.excelBPSModelRateLimits.CompareAndDelete(key, value)
		}
	}
}

func (s *OpenAIGatewayService) limitExcelBPS429Failover(ctx context.Context, c *gin.Context, account *Account, model string, failure *UpstreamFailoverError) {
	if failure == nil || account == nil || !account.IsExcelBPSEnabledForModel(model) || !s.excelBPSRecoveryEnabled(ctx) {
		return
	}
	if !isQualityObservation(ctx) && !isControlledExperiment(ctx) {
		failure.Reason = ExcelBPSRecoveryFailureReason
		failure.ClientStatusCode, failure.ClientMessage, _ = failure.ExcelBPSRecoveryClientResponse()
	}
	route, _ := ctx.Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute)
	if route == nil && c != nil && c.Request != nil {
		route, _ = c.Request.Context().Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute)
	}
	if route == nil {
		return
	}
	route.mu.Lock()
	defer route.mu.Unlock()
	if failure.StatusCode != http.StatusTooManyRequests {
		route.rateLimitedAccounts = nil
		return
	}
	if route.model == "" || route.model != account.GetMappedModel(model) {
		return
	}
	if route.rateLimitedAccounts == nil {
		route.rateLimitedAccounts = make(map[int64]struct{})
	}
	route.rateLimitedAccounts[account.ID] = struct{}{}
	if len(route.rateLimitedAccounts) >= excelBPSRateLimitMaxAccounts {
		failure.NextAccountAction = NextAccountStop
		failure.RetryableOnSameAccount = false
		logger.FromContext(ctx).Warn("openai.excel_bps_rate_limit_retry_exhausted", zap.String("component", "service.openai_excel_bps"), zap.String("model", route.model), zap.Int("limited_accounts", len(route.rateLimitedAccounts)), zap.Int("account_limit", excelBPSRateLimitMaxAccounts))
	}
}
