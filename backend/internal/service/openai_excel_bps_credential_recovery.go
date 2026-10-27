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

// This scan shares the model recovery worker's start/stop lifecycle. Checks
// only read/validate/refresh credentials; they never replay a user's request.
func (s *OpenAIGatewayService) syncExcelBPSCredentialRecovery(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, excelBPS403RecoveryTimeout)
	defer cancel()
	if ctx.Err() != nil || s.accountRepo == nil || !s.excelBPSModelProbingEnabled(ctx) {
		return
	}
	cache := &s.excelBPSCredentialCooldowns
	cache.mu.Lock()
	claimed := make(map[int64]*excelBPSCredentialWait)
	now := time.Now()
	for id, entry := range cache.entries {
		if !entry.checking && !now.Before(entry.nextCheckAt) {
			entry.checking = true
			claimed[id] = entry
			if len(claimed) == excelBPS403RecoveryConcurrency {
				break
			}
		}
	}
	cache.mu.Unlock()
	var wg sync.WaitGroup
	for id, entry := range claimed {
		wg.Go(func() { s.recoverExcelBPSCredentials(ctx, id, entry) })
	}
	wg.Wait()
}

func (s *OpenAIGatewayService) recoverExcelBPSCredentials(ctx context.Context, id int64, entry *excelBPSCredentialWait) {
	restored, removed := false, false
	defer func() {
		if recover() != nil {
			restored, removed = false, false
			logger.FromContext(ctx).Error("excel_bps.credential_recovery_check_panicked", zap.Int64("account_id", id))
		}
		cache := &s.excelBPSCredentialCooldowns
		cache.mu.Lock()
		if cache.entries[id] != entry {
			cache.mu.Unlock()
			return
		}
		restored = restored && ctx.Err() == nil
		if removed || restored {
			delete(cache.entries, id)
		} else {
			entry.checking = false
			entry.nextCheckAt = time.Now().Add(excelBPSCredentialCheckInterval)
		}
		cache.mu.Unlock()
		if restored {
			logger.FromContext(ctx).Info("excel_bps.credential_recovery_restored", zap.Int64("account_id", id))
		}
	}()
	if ctx.Err() != nil || !s.excelBPSModelProbingEnabled(ctx) {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, id)
	if errors.Is(err, ErrAccountNotFound) {
		removed = true
		return
	}
	if err != nil || account == nil || !account.IsSchedulable() || s.isOpenAIAccountRuntimeBlocked(account) {
		return
	}
	// The requested BPS switch may still be waiting for the authorization
	// worker. Check that grant without enabling or mutating production routes.
	route := *account
	route.Extra = cloneExcelBPSRecoveryExtra(account.Extra)
	delete(route.Extra, ExcelBPSAuthorizationPendingKey)
	if !route.IsExcelBPSEnabled() {
		return
	}
	cache := &s.excelBPSCredentialCooldowns
	cache.mu.Lock()
	current := cache.entries[id] == entry
	cache.mu.Unlock()
	if !current || ctx.Err() != nil || !s.excelBPSModelProbingEnabled(ctx) {
		return
	}
	err = s.checkExcelBPSCredentialReadiness(ctx, account)
	cache.mu.Lock()
	current = cache.entries[id] == entry
	cache.mu.Unlock()
	if !current || ctx.Err() != nil || !s.excelBPSModelProbingEnabled(ctx) {
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			reason, permanent := excelBPSCredentialErrorKind(err)
			if reason == "" {
				reason = "credential_read_failed"
			}
			if permanent {
				s.applyExcelBPSCredentialFailure(ctx, &route, reason, true)
			}
			logger.FromContext(ctx).Info("excel_bps.credential_recovery_check_failed", zap.Int64("account_id", id), zap.String("reason", reason))
		}
		return
	}
	restored = ctx.Err() == nil && account.IsExcelBPSEnabled() && s.excelBPSModelProbingEnabled(ctx)
}

func (s *OpenAIGatewayService) checkExcelBPSCredentialReadiness(ctx context.Context, account *Account) error {
	if s.excelOAuthReauth != nil {
		if repo, ok := s.excelOAuthReauth.repo.(OpenAIExcelOAuthRepository); ok {
			task, err := repo.GetLatestExcelTask(ctx, account.ID)
			if err != nil {
				return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_UNAVAILABLE", "Excel authorization status is unavailable")
			}
			if task != nil {
				switch task.Status {
				case "queued", "running", "callback_processing":
					return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_PENDING", "Excel authorization is in progress")
				}
			}
		}
	}
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("access_token not found in credentials")
	}
	return nil
}
