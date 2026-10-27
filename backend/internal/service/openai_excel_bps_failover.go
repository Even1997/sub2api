package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

type excelBPSFailoverRouteKey struct{}
type excelBPSRecoveryKey struct{}

// Snapshot the opt-in switch once per BPS request, including its repair sends.
// Unconfigured or unreadable settings preserve the pre-recovery behavior.
func (s *OpenAIGatewayService) excelBPSRecoveryEnabled(ctx context.Context) bool {
	if enabled, ok := ctx.Value(excelBPSRecoveryKey{}).(bool); ok {
		return enabled
	}
	if s == nil || s.settingService == nil {
		return false
	}
	read := func() bool {
		enabled, err := s.settingService.GetProtocolFeatureEnabled(ctx, SettingKeyExcelBPSRecoveryEnabled)
		return err == nil && enabled
	}
	if route, ok := ctx.Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute); ok {
		route.recoveryOnce.Do(func() { route.recoveryEnabled = read() })
		return route.recoveryEnabled
	}
	return read()
}

func (s *OpenAIGatewayService) withExcelBPSRecovery(ctx context.Context) context.Context {
	return context.WithValue(ctx, excelBPSRecoveryKey{}, s.excelBPSRecoveryEnabled(ctx))
}

// Shared by all attempts of one request, including detached upstream contexts.
// Recovering a BPS request must not silently change its protocol or mapped model.
type excelBPSFailoverRoute struct {
	mu                  sync.Mutex
	model               string
	recoveryOnce        sync.Once
	recoveryEnabled     bool
	rateLimitedAccounts map[int64]struct{}
}

func lockExcelBPSFailoverRoute(ctx context.Context, c *gin.Context, account *Account, model string) {
	if enabled, _ := ctx.Value(excelBPSRecoveryKey{}).(bool); !enabled {
		return
	}
	route, _ := ctx.Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute)
	if route == nil && c != nil && c.Request != nil {
		route, _ = c.Request.Context().Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute)
		if route == nil {
			route = &excelBPSFailoverRoute{}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), excelBPSFailoverRouteKey{}, route))
		}
	}
	if route != nil {
		route.mu.Lock()
		defer route.mu.Unlock()
		if route.model == "" {
			route.model = account.GetMappedModel(model)
		}
	}
}

func excelBPSFailoverRouteBlocked(ctx context.Context, account *Account, model string) bool {
	if route, ok := ctx.Value(excelBPSFailoverRouteKey{}).(*excelBPSFailoverRoute); ok {
		route.mu.Lock()
		defer route.mu.Unlock()
		return route.model != "" && (!account.IsExcelBPSEnabledForModel(model) || account.GetMappedModel(model) != route.model)
	}
	return false
}

func (s *OpenAIGatewayService) pauseExcelBPSCredentials(ctx context.Context, account *Account) {
	if account == nil || isQualityObservation(ctx) || !s.excelBPSRecoveryEnabled(ctx) {
		return
	}
	const reason = "Excel BPS credential is unavailable; repair credentials and resume this account"
	s.BlockAccountScheduling(account, time.Time{}, reason)
	if s.accountRepo != nil {
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		// SetError persists status=error and schedulable=false, publishes the
		// scheduler change, and requires an explicit resume after repair.
		if err := s.accountRepo.SetError(stateCtx, account.ID, reason); err != nil {
			logger.LegacyPrintf("service.openai_excel_bps", "credential account pause failed: account_id=%d", account.ID)
		}
	}
}

func excelBPSFailureCanRecover(failure *basispoints.UpstreamFailure) bool {
	if failure == nil {
		return false
	}
	switch failure.Code {
	case "cyber_policy", "content_policy_violation", "invalid_prompt":
		return false
	}
	switch failure.Status {
	case http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		return isExcelBPSModelPermissionError(failure.Status, failure.Code)
	case http.StatusBadGateway:
		// Unknown errors also normalize to 502. Require a recognized transient
		// identifier rather than replaying arbitrary policy/protocol failures.
		return failure.Code == "api_error"
	}
	return false
}

func (s *OpenAIGatewayService) newExcelBPSFailover(ctx context.Context, c *gin.Context, account *Account, model string, status int, headers http.Header, code string, retrySame bool) *UpstreamFailoverError {
	lockExcelBPSFailoverRoute(ctx, c, account, model)
	message := "Upstream service is temporarily unavailable, please retry later"
	body, _ := json.Marshal(gin.H{"error": gin.H{"type": excelBPSErrorTypeForStatus(status), "code": code, "message": message}})
	err := s.openAIUpstreamFailoverError(status, headers, body, message, retrySame)
	err.SuppressScheduleFailure = true
	err.SkipOAuth429AttemptLimit = true
	err.ClientStatusCode = http.StatusBadGateway
	err.ClientMessage = message
	if isQualityObservation(ctx) || isControlledExperiment(ctx) {
		err.NextAccountAction = NextAccountStop
		err.RetryableOnSameAccount = false
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		err.ClientStatusCode = status
	}
	if retrySame {
		err.SameAccountRetryMax = 1
		err.SameAccountRetryDelay = time.Second
		err.SameAccountRetryDeadline = time.Now().Add(10 * time.Second)
	}
	s.limitExcelBPS429Failover(ctx, c, account, model, err)
	if c != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: status, Kind: "failover", Message: "Excel BPS attempt failed: " + code,
		})
	}
	return err
}

// Retry only a rejected correction submission. The validated tool batch stays
// withheld, the original body is reused, and no delivered generation is replayed.
func (s *OpenAIGatewayService) doExcelBPSRepairRequest(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	response, err := s.doPriorityObservedHTTP(req, proxyURL, account)
	if err != nil || response == nil || response.StatusCode != http.StatusTooManyRequests || req.GetBody == nil || !s.excelBPSRecoveryEnabled(req.Context()) || isControlledExperiment(req.Context()) || isQualityObservation(req.Context()) {
		return response, err
	}
	delay := time.Second
	if reset := parseRetryAfterResetTime(response.Header, time.Now()); reset != nil {
		if requested := time.Until(*reset); requested > delay {
			delay = requested
		}
	}
	// A long Retry-After is handled by the caller's route cooldown instead of
	// keeping the already-running client response waiting indefinitely.
	if delay > 2*time.Second {
		return response, nil
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	logger.LegacyPrintf("service.openai_excel_bps", "retrying throttled tool correction once: account_id=%d delay=%s", account.ID, delay)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-timer.C:
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	retry.Body = body
	return s.doPriorityObservedHTTP(retry, proxyURL, account)
}
