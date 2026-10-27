package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const excelBPSModelPermissionFilterReason = "excel_bps_model_not_allowed"
const excelBPSRateLimitedFilterReason = "excel_bps_rate_limited"
const excelBPSModelPermissionMaxEntries = 4096
const excelBPSModelRecoveryScanInterval = 15 * time.Second

type excelBPSModelKey struct {
	accountID int64
	model     string
}
type excelBPSModelCooldown struct {
	nextProbeAt time.Time
	probing     bool
	rateLimited bool
}
type excelBPSModelCooldowns struct {
	mu      sync.Mutex
	entries map[excelBPSModelKey]*excelBPSModelCooldown
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

func isExcelBPSModelPermissionError(status int, code string) bool {
	switch code {
	case "basispoints_model_access_changed", "model_not_allowed":
		return status == 403
	case "model_not_found", "model_not_found_error":
		return status == 400 || status == 404
	default:
		return false
	}
}

// Expiry makes a model due for background testing, not eligible for user traffic.
func (s *OpenAIGatewayService) coolDownExcelBPSModel(ctx context.Context, account *Account, upstreamModel string) {
	if s == nil {
		return
	}
	s.scheduleExcelBPSModelRecovery(ctx, account, upstreamModel, time.Now().Add(s.settingService.GetExcelBPSModelRateLimitCooldown(ctx)), false)
}

func (s *OpenAIGatewayService) scheduleExcelBPSModelRecovery(ctx context.Context, account *Account, upstreamModel string, next time.Time, rateLimited bool) {
	if s == nil || account == nil || !account.IsOpenAI() || strings.TrimSpace(upstreamModel) == "" ||
		!account.isExcelBPSUpstreamModelEnabled(upstreamModel) || isQualityObservation(ctx) ||
		isControlledExperiment(ctx) || !s.excelBPSRecoveryEnabled(ctx) {
		return
	}
	key := excelBPSModelKey{account.ID, upstreamModel}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[excelBPSModelKey]*excelBPSModelCooldown)
	}
	previous := cache.entries[key]
	if previous == nil && len(cache.entries) >= excelBPSModelPermissionMaxEntries {
		cache.mu.Unlock()
		// Never evict an isolated model just to admit a new one.
		logger.FromContext(ctx).Warn("excel_bps.model_recovery_capacity_reached", zap.Int64("account_id", account.ID))
		return
	}
	if previous != nil && previous.nextProbeAt.After(next) {
		next = previous.nextProbeAt
	}
	// Replacing the entry invalidates any older in-flight probe result.
	cache.entries[key] = &excelBPSModelCooldown{nextProbeAt: next, rateLimited: rateLimited}
	cache.mu.Unlock()
	logger.FromContext(ctx).Info("excel_bps.model_recovery_scheduled", zap.Int64("account_id", account.ID), zap.String("model", upstreamModel), zap.Time("next_probe_at", next), zap.Bool("rate_limited", rateLimited))
}

func (s *OpenAIGatewayService) isExcelBPSModelCoolingDown(ctx context.Context, account *Account, requestedModel string) bool {
	if s == nil || account == nil || !account.IsExcelBPSEnabledForModel(requestedModel) {
		return false
	}
	if bps, known := ctx.Value(excelBPSSchedulingKey{}).(bool); known && !bps {
		return false
	}
	key := excelBPSModelKey{account.ID, account.GetMappedModel(requestedModel)}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	entry := cache.entries[key]
	cooling := entry != nil
	cache.mu.Unlock()
	// Healthy candidates need no additional settings/database read.
	return cooling && s.excelBPSRecoveryEnabled(ctx)
}

func (s *OpenAIGatewayService) isExcelBPSRateLimitRecoveryPending(accountID int64, upstreamModel string) bool {
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[excelBPSModelKey{accountID, upstreamModel}]
	return entry != nil && entry.rateLimited
}

func (s *OpenAIGatewayService) StartBPSModelRecovery() {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return
	}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.stopped || cache.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cache.cancel, cache.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(excelBPSModelRecoveryScanInterval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			// Credential waits and model isolation share cancellation and joining,
			// but neither scan blocks the other from starting.
			var scans sync.WaitGroup
			scans.Go(func() { s.syncExcelBPSCredentialRecovery(ctx) })
			scans.Go(func() { s.syncExcelBPSModelRecovery(ctx) })
			scans.Wait()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *OpenAIGatewayService) StopBPSModelRecovery() {
	if s == nil {
		return
	}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	cache.stopped = true
	cancel, done := cache.cancel, cache.done
	cache.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *OpenAIGatewayService) excelBPSModelProbingEnabled(ctx context.Context) bool {
	if !s.excelBPSRecoveryEnabled(ctx) {
		return false
	}
	enabled, err := s.settingService.GetProtocolFeatureEnabled(ctx, SettingKeyExcelBPSEnabled)
	return err == nil && enabled
}

func (s *OpenAIGatewayService) syncExcelBPSModelRecovery(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, excelBPS403RecoveryTimeout)
	defer cancel()
	if ctx.Err() != nil || s.accountRepo == nil || !s.excelBPSModelProbingEnabled(ctx) {
		return
	}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	claimed := make(map[excelBPSModelKey]*excelBPSModelCooldown)
	now := time.Now()
	var due []excelBPSModelKey
	for key, entry := range cache.entries {
		if !entry.probing && !now.Before(entry.nextProbeAt) {
			due = append(due, key)
		}
	}
	// Oldest deadlines first: a busy pool must not starve older isolated models.
	sort.Slice(due, func(i, j int) bool {
		a, b := cache.entries[due[i]], cache.entries[due[j]]
		if !a.nextProbeAt.Equal(b.nextProbeAt) {
			return a.nextProbeAt.Before(b.nextProbeAt)
		}
		if due[i].accountID != due[j].accountID {
			return due[i].accountID < due[j].accountID
		}
		return due[i].model < due[j].model
	})
	for _, key := range due[:min(len(due), excelBPS403RecoveryConcurrency)] {
		entry := cache.entries[key]
		entry.probing = true
		claimed[key] = entry
	}
	cache.mu.Unlock()
	var wg sync.WaitGroup
	for key, entry := range claimed {
		wg.Add(1)
		go func() { defer wg.Done(); s.recoverExcelBPSModel(ctx, key, entry) }()
	}
	wg.Wait()
}

func (s *OpenAIGatewayService) recoverExcelBPSModel(ctx context.Context, key excelBPSModelKey, entry *excelBPSModelCooldown) {
	restored, removed := false, false
	defer func() {
		if recover() != nil {
			restored, removed = false, false
			logger.FromContext(ctx).Error("excel_bps.model_recovery_probe_panicked", zap.Int64("account_id", key.accountID))
		}
		cache := &s.excelBPSModelCooldowns
		next := time.Now().Add(s.settingService.GetExcelBPSModelRateLimitCooldown(ctx))
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.entries[key] != entry {
			return
		}
		if removed || (restored && ctx.Err() == nil) {
			delete(cache.entries, key)
			if restored {
				logger.FromContext(ctx).Info("excel_bps.model_recovery_restored", zap.Int64("account_id", key.accountID), zap.String("model", key.model))
			}
		} else {
			entry.probing, entry.nextProbeAt = false, next
		}
	}()
	probeCtx, cancel := context.WithTimeout(ctx, excelBPS403RecoveryTimeout)
	defer cancel()
	if !s.excelBPSModelProbingEnabled(probeCtx) {
		return
	}
	account, err := s.accountRepo.GetByID(probeCtx, key.accountID)
	if errors.Is(err, ErrAccountNotFound) {
		removed = true
		return
	}
	if err != nil || account == nil || !account.IsOpenAI() || !account.IsSchedulable() ||
		!account.isExcelBPSUpstreamModelEnabled(key.model) || s.isOpenAIAccountRuntimeBlocked(account) {
		return
	}
	// Respect rate limits using the already resolved model, without remapping aliases twice.
	if until, ok := s.excelBPSRateLimitedUntil.Load(account.ID); ok && time.Now().Before(until.(time.Time)) {
		return
	}
	if state, ok := s.excelBPSModelRateLimits.Load(excelBPSRateLimitKey{account.ID, key.model}); ok && time.Now().Before(state.(excelBPSRateLimitState).until) {
		return
	}
	if !s.excelBPSModelProbingEnabled(probeCtx) || probeCtx.Err() != nil {
		return
	}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	current := cache.entries[key] == entry
	cache.mu.Unlock()
	if !current {
		return
	}
	err = s.probeExcelBPSRecoveryModel(probeCtx, account, key.model, "bps-model-recovery")
	if err != nil {
		// Classifiable credential failures still pause the entire account. Never log response bodies.
		s.pauseExcelBPSCredentialError(probeCtx, account, err)
		logger.FromContext(ctx).Info("excel_bps.model_recovery_probe_failed", zap.Int64("account_id", key.accountID), zap.String("model", key.model))
		return
	}
	restored = probeCtx.Err() == nil && s.excelBPSModelProbingEnabled(probeCtx)
}
