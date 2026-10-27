package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const excelBPSCredentialCooldown = 30 * time.Second
const excelBPSCredentialMaxEntries = 4096
const excelBPSCredentialFilterReason = "excel_bps_credentials_unavailable"

type excelBPSCredentialCooldowns struct {
	mu        sync.Mutex
	entries   map[int64]time.Time
	lastSweep time.Time
}

// Reasons are an allowlist: never put provider messages, credentials or storage
// errors into diagnostics. Unknown probe failures are not credential failures.
func excelBPSCredentialErrorKind(err error) (reason string, permanent bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	switch infraerrors.Reason(err) {
	case "OPENAI_EXCEL_AUTH_PENDING":
		return "authorization_pending", false
	case "OPENAI_EXCEL_AUTH_UNAVAILABLE":
		return "authorization_unavailable", false
	case "OPENAI_EXCEL_AUTH_REQUIRED":
		return "authorization_required", true
	case "OPENAI_EXCEL_AUTH_CONFIG_REQUIRED":
		return "authorization_config_required", true
	case "OPENAI_EXCEL_AUTH_FAILED":
		return "authorization_failed", true
	case "OPENAI_EXCEL_AUTH_VERIFICATION_REQUIRED":
		return "verification_required", true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"access_token not found", "api_key not found", "invalid saved excel", "identity does not match the account", "refresh token is invalid", "invalid_grant", "token_revoked", "token_invalidated"} {
		if strings.Contains(message, marker) {
			return "invalid_credentials", true
		}
	}
	return "", false
}

// This entry point also receives background probe errors. Only recognized
// credential errors may change scheduling; a model/network failure must not.
func (s *OpenAIGatewayService) pauseExcelBPSCredentialError(ctx context.Context, account *Account, err error) {
	reason, permanent := excelBPSCredentialErrorKind(err)
	if reason != "" {
		s.applyExcelBPSCredentialFailure(ctx, account, reason, permanent)
	}
}

// The caller has just failed to obtain a token, before sending the business
// request. Unknown infrastructure failures here can safely use a short backoff.
func (s *OpenAIGatewayService) handleExcelBPSCredentialReadError(ctx context.Context, account *Account, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	reason, permanent := excelBPSCredentialErrorKind(err)
	if reason == "" {
		reason = "credential_read_failed"
	}
	s.applyExcelBPSCredentialFailure(ctx, account, reason, permanent)
}

func (s *OpenAIGatewayService) applyExcelBPSCredentialFailure(ctx context.Context, account *Account, reason string, permanent bool) {
	if s == nil || account == nil || !account.IsExcelBPSEnabled() || isQualityObservation(ctx) ||
		isControlledExperiment(ctx) || !s.excelBPSRecoveryEnabled(ctx) {
		return
	}
	fields := []zap.Field{zap.String("component", "service.openai_excel_bps"), zap.Int64("account_id", account.ID), zap.String("reason", reason)}
	if permanent {
		logger.FromContext(ctx).Warn("openai.excel_bps_credential_failure", append(fields, zap.String("action", "pause_account"))...)
		s.pauseExcelBPSCredentials(ctx, account)
		return
	}
	if ctx.Err() != nil {
		return
	}
	now := time.Now()
	until := now.Add(excelBPSCredentialCooldown)
	cache := &s.excelBPSCredentialCooldowns
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[int64]time.Time)
	}
	if now.Sub(cache.lastSweep) >= excelBPSCredentialCooldown || len(cache.entries) >= excelBPSCredentialMaxEntries {
		for id, deadline := range cache.entries {
			if !now.Before(deadline) {
				delete(cache.entries, id)
			}
		}
		cache.lastSweep = now
	}
	previous, exists := cache.entries[account.ID]
	if !exists && len(cache.entries) >= excelBPSCredentialMaxEntries {
		cache.mu.Unlock()
		logger.FromContext(ctx).Warn("openai.excel_bps_credential_cooldown_capacity_reached", fields...)
		return
	}
	if previous.After(until) {
		until = previous
	}
	cache.entries[account.ID] = until
	cache.mu.Unlock()
	logger.FromContext(ctx).Warn("openai.excel_bps_credential_failure", append(fields,
		zap.String("action", "cooldown_bps"), zap.String("scope", "bps_account"),
		zap.Time("retry_at", until), zap.Int64("cooldown_seconds", int64(excelBPSCredentialCooldown/time.Second)))...)
}

func (s *OpenAIGatewayService) isExcelBPSCredentialCoolingDown(ctx context.Context, account *Account, model string) bool {
	if s == nil || account == nil || !account.IsExcelBPSEnabledForModel(model) || isQualityObservation(ctx) || isControlledExperiment(ctx) {
		return false
	}
	if bps, known := ctx.Value(excelBPSSchedulingKey{}).(bool); known && !bps {
		return false
	}
	cache := &s.excelBPSCredentialCooldowns
	cache.mu.Lock()
	until, exists := cache.entries[account.ID]
	cooling := exists && time.Now().Before(until)
	if exists && !cooling {
		delete(cache.entries, account.ID)
	}
	cache.mu.Unlock()
	// No extra settings/database read for healthy candidates. Turning recovery
	// off bypasses this transient BPS state; it never resumes a paused account.
	return cooling && s.excelBPSRecoveryEnabled(ctx)
}
