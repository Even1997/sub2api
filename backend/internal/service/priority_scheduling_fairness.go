package service

import (
	"context"
	"math"
	"slices"
	"sort"
	"time"
)

const priorityExplorationShare = 0.10

// Ten-percentage-point cohorts prefer genuinely idle capacity while preserving
// weighted diversity among similarly loaded accounts. The atomic slot race
// below still rejects full accounts; the band describes the observed load and
// must not promote every empty low-concurrency account just because it may take
// the next request.
func priorityCapacityBand(item openAIAccountCandidateScore) int {
	if !item.loadKnown || item.loadInfo == nil || item.account.Concurrency <= 0 {
		return 10
	}
	load := float64(max(0, item.loadInfo.CurrentConcurrency)) / float64(item.account.Concurrency)
	if item.rpmEnabled && item.rpmLimit > 0 {
		load = math.Max(load, float64(max(0, item.rpmCurrent))/float64(item.rpmLimit))
	}
	band := int(math.Floor(load * 10))
	if item.loadInfo.WaitingCount > 0 {
		return 20 + max(band, item.loadInfo.WaitingCount)
	}
	return band
}

func (s *OpenAIGatewayService) balancesPriorityProtocols(req OpenAIAccountScheduleRequest) bool {
	if s == nil || req.Platform != PlatformOpenAI || !req.UseUpstreamTokenCost || req.RequiredImageCapability != "" {
		return false
	}
	cfg := s.prioritySchedulingRuntimeConfig()
	return cfg.BalanceProtocols && priorityApplies(cfg, req)
}

// Count distinct bindings, not group price/name or an inferred account class.
// AccountGroups supports older snapshots that do not populate GroupIDs.
func priorityAccountGroupCount(account *Account) int {
	if account == nil {
		return 1
	}
	groups := make(map[int64]struct{}, len(account.GroupIDs)+len(account.AccountGroups))
	for _, id := range account.GroupIDs {
		if id > 0 {
			groups[id] = struct{}{}
		}
	}
	for _, binding := range account.AccountGroups {
		if binding.GroupID > 0 {
			groups[binding.GroupID] = struct{}{}
		}
	}
	return max(1, len(groups))
}

// prioritySelectionWeight bounds the score preference so every usable account
// in a tier/priority cohort can receive traffic. Capacity uses real slots, not
// load_factor, and quota headroom is a soft preference rather than a new gate.
func prioritySelectionWeight(item openAIAccountCandidateScore, now time.Time) float64 {
	remaining := 1.0
	if item.loadKnown && item.loadInfo != nil {
		remaining = math.Max(0.05, float64(max(1, item.account.Concurrency)-max(0, item.loadInfo.CurrentConcurrency))) /
			(1 + float64(max(0, item.loadInfo.WaitingCount)))
	}
	score := math.Mod(item.score, 200)
	if math.IsNaN(score) || math.IsInf(score, 0) {
		score = 0
	}
	preference := 0.5 + clamp01(score/100)
	quota := 0.5 + 0.5*openAIQuotaHeadroomFactor(item.account, now)
	// OAuth capacity is shared by every bound group. Prefer spare capacity
	// exposed to fewer groups without excluding broader accounts or reserving
	// idle slots when there is work they can serve. API-key routing weights keep
	// their existing model because upstream capacity may not be subscription-bound.
	groups := 1
	if item.account.IsOpenAIOAuth() {
		groups = priorityAccountGroupCount(item.account)
	}
	latency := item.priorityLatencyFactor
	if latency <= 0 || math.IsNaN(latency) || math.IsInf(latency, 0) {
		latency = 1
	}
	return remaining * preference * quota * latency / float64(groups)
}

// Rebalance one movable turn only when another compatible account has both
// materially lower utilization and substantially more capacity per bound group.
// The durable session binding is preserved by the caller. Hard response/task
// ownership never enters this path, and failed load reads retain the binding.
func (s *defaultOpenAIAccountScheduler) shouldRebalancePrioritySticky(ctx context.Context, req OpenAIAccountScheduleRequest, sticky *Account) bool {
	if s == nil || s.service == nil || s.service.concurrencyService == nil || s.stats == nil || sticky == nil ||
		req.DisableStickyEscape || req.PreserveStickyBinding || req.PreviousResponseID != "" ||
		req.Platform != PlatformOpenAI || !req.UseUpstreamTokenCost || req.RequiredImageCapability != "" {
		return false
	}
	cfg := s.service.prioritySchedulingRuntimeConfig()
	if !priorityApplies(cfg, req) {
		return false
	}
	quotaSticky := cfg.OAuthQuotaPriority && sticky.IsOpenAIApiKey()
	// Optional balancing must not hold a healthy sticky turn behind slow dependencies.
	ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	accounts, err := s.service.listSchedulableAccountsForRequest(ctx, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.ExcludedIDs)
	if err != nil || len(accounts) < 2 {
		return false
	}
	loads := make([]AccountWithConcurrency, 0, len(accounts)+1)
	loads = append(loads, AccountWithConcurrency{ID: sticky.ID, MaxConcurrency: max(1, sticky.Concurrency)})
	eligible := make([]*Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.ID == sticky.ID || !account.IsSchedulable() || (!quotaSticky && account.Priority > sticky.Priority) ||
			!s.service.openAIAccountMatchesSchedulingGroup(account, req.GroupID) ||
			!s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(account, req.RequiredTransport, req.RequestedModel) ||
			s.service.isExcelBPSCoolingDown(ctx, account, req.RequestedModel) {
			continue
		}
		if _, excluded := req.ExcludedIDs[account.ID]; excluded {
			continue
		}
		if !s.service.balancesPriorityProtocols(req) && sticky.IsExcelBPSEnabledForModel(req.RequestedModel) && !account.IsExcelBPSEnabledForModel(req.RequestedModel) {
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(account) < openAICompactSupportTier(sticky) {
			continue
		}
		if req.SubscriptionPriority && sticky.IsOpenAIChatGPTSubscription() && !account.IsOpenAIChatGPTSubscription() {
			continue
		}
		errorRate, _, _ := s.stats.snapshot(account.ID)
		if errorRate > 0.2 {
			continue
		}
		eligible = append(eligible, account)
		loads = append(loads, AccountWithConcurrency{ID: account.ID, MaxConcurrency: max(1, account.Concurrency)})
	}
	if len(eligible) == 0 {
		return false
	}
	loadMap, err := s.service.concurrencyService.GetAccountsLoadBatch(ctx, loads)
	if err != nil || loadMap[sticky.ID] == nil {
		return false
	}
	if quotaSticky {
		historyPool := make([]openAIAccountCandidateScore, 0, len(eligible)+1)
		for _, account := range eligible {
			historyPool = append(historyPool, openAIAccountCandidateScore{account: account})
		}
		historyPool = append(historyPool, openAIAccountCandidateScore{account: sticky})
		signals, _ := s.service.prioritySignals(req, cfg, historyPool)
		now := time.Now()
		for i := range historyPool {
			item := &historyPool[i]
			item.loadInfo = loadMap[item.account.ID]
			item.loadKnown = item.loadInfo != nil
			if s.stats != nil {
				item.errorRate, _, _ = s.stats.snapshot(item.account.ID)
			}
			applyPriorityCandidate(cfg, item, signals[item.account.ID], now)
		}
		for _, item := range buildPrioritySelectionOrder(historyPool, req) {
			if item.account.ID == sticky.ID && item.priorityAPIStandby {
				return true
			}
		}
	}
	current := loadMap[sticky.ID]
	utilization := float64(current.CurrentConcurrency) / float64(max(1, sticky.Concurrency))
	now := time.Now()
	weight := prioritySelectionWeight(openAIAccountCandidateScore{account: sticky, loadInfo: current, loadKnown: true, score: 50}, now)
	signalPool := make([]openAIAccountCandidateScore, 0, len(eligible))
	for _, account := range eligible {
		signalPool = append(signalPool, openAIAccountCandidateScore{account: account})
	}
	rebalanceSignals, _ := s.service.prioritySignals(req, cfg, signalPool)
	for _, account := range eligible {
		load := loadMap[account.ID]
		if load == nil || load.WaitingCount > 0 || load.CurrentConcurrency >= account.Concurrency {
			continue
		}
		candidate := openAIAccountCandidateScore{account: account, loadKnown: true, loadInfo: load}
		score := applyPriorityCandidate(cfg, &candidate, rebalanceSignals[account.ID], now)
		if candidate.priorityUnhealthy || slices.Contains(score.Reasons, "latency_above_target") {
			continue
		}
		otherUtilization := float64(load.CurrentConcurrency) / float64(max(1, account.Concurrency))
		otherWeight := prioritySelectionWeight(openAIAccountCandidateScore{account: account, loadInfo: load, loadKnown: true, score: 50}, now)
		if utilization-otherUtilization >= 0.20 && otherWeight > 2*weight {
			return true
		}
	}
	return false
}

// Weighted sampling without replacement avoids a deterministic best-account
// monopoly and keeps all overflow candidates. Exponential races are O(n log n)
// and require no cross-instance counters or per-session scheduler state.
func buildPrioritySelectionOrder(pool []openAIAccountCandidateScore, req OpenAIAccountScheduleRequest) []openAIAccountCandidateScore {
	type choice struct {
		candidate openAIAccountCandidateScore
		key       float64
	}
	now := time.Now()
	rng := newOpenAISelectionRNG(deriveOpenAISelectionSeed(req))
	choices := make([]choice, len(pool))
	for i, candidate := range pool {
		choices[i] = choice{candidate: candidate, key: -math.Log1p(-rng.nextFloat64()) / prioritySelectionWeight(candidate, now)}
	}
	sort.Slice(choices, func(i, j int) bool {
		a, b := choices[i].candidate, choices[j].candidate
		if a.priorityUnhealthy != b.priorityUnhealthy {
			return !a.priorityUnhealthy
		}
		if aBand, bBand := priorityCapacityBand(a), priorityCapacityBand(b); aBand != bBand {
			return aBand < bBand
		}
		if int(a.score/200) != int(b.score/200) {
			return a.score > b.score
		}
		if a.priorityRecovery != b.priorityRecovery {
			return a.priorityRecovery
		}
		if a.priorityRecovery && a.priorityRecoveryPressure != b.priorityRecoveryPressure {
			return a.priorityRecoveryPressure > b.priorityRecoveryPressure
		}
		if a.account.Priority != b.account.Priority {
			return a.account.Priority < b.account.Priority
		}
		if choices[i].key != choices[j].key {
			return choices[i].key < choices[j].key
		}
		return a.account.ID < b.account.ID
	})

	// Cold OAuth accounts otherwise never gain enough evidence when an
	// established eligible cohort exists. Reserve a 10% exploration probability
	// for safe unknown accounts at that same explicit priority. Known degraded
	// accounts, busy accounts, protocol partitions and hard bindings cannot use
	// this lane; the ordinary eligibility/profit/slot checks still follow.
	explorationRoll := rng.nextFloat64()
	if len(choices) > 1 && !choices[0].candidate.priorityRecovery && int(choices[0].candidate.score/200) == 2 && explorationRoll < priorityExplorationShare {
		best := -1
		for i := 1; i < len(choices); i++ {
			candidate := choices[i].candidate
			if !candidate.priorityRecovery && int(candidate.score/200) == 1 && candidate.priorityExploration &&
				(!candidate.priorityLatencyRecovery || explorationRoll < float64(candidate.priorityRecoveryExplorationPercent)/100) &&
				!candidate.priorityUnhealthy && priorityCapacityBand(candidate) <= priorityCapacityBand(choices[0].candidate) &&
				candidate.account.Priority == choices[0].candidate.account.Priority &&
				(best < 0 || choices[i].key < choices[best].key) {
				best = i
			}
		}
		if best > 0 {
			selected := choices[best]
			copy(choices[1:best+1], choices[:best])
			choices[0] = selected
		}
	}
	order := make([]openAIAccountCandidateScore, len(choices))
	for i := range choices {
		order[i] = choices[i].candidate
	}
	return applyPriorityOAuthStandby(order)
}
