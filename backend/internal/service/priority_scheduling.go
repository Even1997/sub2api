package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

const prioritySchedulingSettingKey = "priority_scheduling_v1"

const (
	PriorityLatencyDeprioritize = "deprioritize"
	PriorityLatencyStop         = "stop"
)

// PrioritySchedulingConfig controls OpenAI text routing and optional scoped
// new-request stops. A stopped owner is rejected rather than reassigned.
// Account eligibility, continuity, profit gates and concurrency acquisition stay authoritative.
type PrioritySchedulingConfig struct {
	Teams                      PriorityTeamsConfig `json:"teams"`
	ErrorRateWeight            float64             `json:"error_rate_weight"`
	ErrorRateWindowMinutes     int                 `json:"error_rate_window_minutes"`
	ErrorRatePriorSamples      int                 `json:"error_rate_prior_samples"`
	OAuthQuotaPriority         bool                `json:"oauth_quota_priority"`
	OAuthQuotaThreshold        int                 `json:"oauth_quota_threshold"`
	BalanceProtocols           bool                `json:"balance_protocols"`
	Enabled                    bool                `json:"enabled"`
	Mode                       string              `json:"mode"`
	GroupIDs                   []int64             `json:"group_ids"`
	Models                     []string            `json:"models"`
	WindowMinutes              int                 `json:"window_minutes"`
	MinSamples                 int                 `json:"min_samples"`
	TargetTTFTMs               int                 `json:"target_ttft_ms"`
	LatencyPercentile          int                 `json:"latency_percentile"`
	LatencyMeasurement         string              `json:"latency_measurement"`
	RecoveryExplorationPercent int                 `json:"recovery_exploration_percent"`
	LatencyPenaltyMode         string              `json:"latency_penalty_mode"`
	LatencyPenaltyMinutes      int                 `json:"latency_penalty_minutes"`
	MaxLoadPercent             int                 `json:"max_load_percent"`
	MinQualityPercent          int                 `json:"min_quality_percent"`
	QualityMaxAgeHours         int                 `json:"quality_max_age_hours"`
	QualityWeight              float64             `json:"quality_weight"`
	LatencyWeight              float64             `json:"latency_weight"`
	LoadWeight                 float64             `json:"load_weight"`
	CostWeight                 float64             `json:"cost_weight"`
}

func DefaultPrioritySchedulingConfig() PrioritySchedulingConfig {
	return PrioritySchedulingConfig{Teams: PriorityTeamsConfig{Enabled: false, CostCNY: 50, WindowHours: 4, CNYPerBillingUnit: 1, WindowSource: "first_usage"}, ErrorRateWeight: 25, ErrorRateWindowMinutes: 10, ErrorRatePriorSamples: 5, OAuthQuotaThreshold: 90, BalanceProtocols: true, Mode: "balanced", GroupIDs: []int64{}, Models: []string{}, WindowMinutes: 60, MinSamples: 5, TargetTTFTMs: 3000, LatencyPercentile: 90, LatencyMeasurement: "legacy_forward", RecoveryExplorationPercent: 0, LatencyPenaltyMode: PriorityLatencyDeprioritize, LatencyPenaltyMinutes: 10, MaxLoadPercent: 80, MinQualityPercent: 90, QualityMaxAgeHours: 24, QualityWeight: 30, LatencyWeight: 25, LoadWeight: 25, CostWeight: 20}
}

func ValidatePrioritySchedulingConfig(c PrioritySchedulingConfig) error {
	if math.IsNaN(c.ErrorRateWeight) || math.IsInf(c.ErrorRateWeight, 0) || c.ErrorRateWeight < 0 || c.ErrorRateWeight > 100 || c.ErrorRateWindowMinutes < 1 || c.ErrorRateWindowMinutes > 60 || c.ErrorRatePriorSamples < 1 || c.ErrorRatePriorSamples > 1000 {
		return errors.New("error rate parameters outside allowed range")
	}
	if !slices.Contains([]int{50, 70, 90, 95}, c.LatencyPercentile) {
		return errors.New("latency percentile must be 50, 70, 90 or 95")
	}
	if c.LatencyMeasurement != "legacy_forward" && c.LatencyMeasurement != "attributed_v1" {
		return errors.New("invalid latency measurement")
	}
	if c.RecoveryExplorationPercent < 0 || c.RecoveryExplorationPercent > 10 {
		return errors.New("recovery exploration must be between 0 and 10 percent")
	}
	if c.LatencyPenaltyMode != "" && c.LatencyPenaltyMode != PriorityLatencyDeprioritize && c.LatencyPenaltyMode != PriorityLatencyStop {
		return errors.New("invalid latency penalty mode")
	}
	if c.Teams.Enabled {
		if c.Teams.CostCNY <= 0 || c.Teams.CostCNY > 1000000 || math.IsNaN(c.Teams.CostCNY) || math.IsInf(c.Teams.CostCNY, 0) || c.Teams.CNYPerBillingUnit <= 0 || c.Teams.CNYPerBillingUnit > 10000 || math.IsNaN(c.Teams.CNYPerBillingUnit) || math.IsInf(c.Teams.CNYPerBillingUnit, 0) || c.Teams.WindowHours < 1 || c.Teams.WindowHours > 168 || !slices.Contains([]string{"explicit", "expiry", "first_usage"}, c.Teams.WindowSource) {
			return errors.New("invalid Teams recovery configuration")
		}
	}
	if c.OAuthQuotaThreshold < 1 || c.OAuthQuotaThreshold > 100 {
		return errors.New("OAuth quota threshold must be between 1 and 100")
	}
	if !slices.Contains([]string{"experience", "balanced", "profit", "custom"}, c.Mode) {
		return errors.New("invalid scheduling mode")
	}
	if c.WindowMinutes < 5 || c.WindowMinutes > 1440 || c.MinSamples < 1 || c.MinSamples > 1000 || c.TargetTTFTMs < 100 || c.TargetTTFTMs > 120000 || c.LatencyPenaltyMinutes < 0 || c.LatencyPenaltyMinutes > 1440 || c.MaxLoadPercent < 10 || c.MaxLoadPercent > 100 || c.MinQualityPercent < 0 || c.MinQualityPercent > 100 || c.QualityMaxAgeHours < 1 || c.QualityMaxAgeHours > 168 {
		return errors.New("scheduling thresholds outside allowed range")
	}
	if len(c.GroupIDs) > 100 || len(c.Models) > 100 {
		return errors.New("at most 100 groups or models allowed")
	}
	for _, id := range c.GroupIDs {
		if id <= 0 {
			return errors.New("group IDs must be positive")
		}
	}
	for _, model := range c.Models {
		if len(model) == 0 || len(model) > 200 {
			return errors.New("model names must contain 1–200 bytes")
		}
	}
	sum := 0.0
	for _, w := range []float64{c.QualityWeight, c.LatencyWeight, c.LoadWeight, c.CostWeight} {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 || w > 100 {
			return errors.New("weights must be between 0 and 100")
		}
		sum += w
	}
	if sum <= 0 {
		return errors.New("at least one weight must be positive")
	}
	return nil
}

func (c PrioritySchedulingConfig) applies(group *int64, model string) bool {
	return c.Enabled && (len(c.GroupIDs) == 0 || group != nil && slices.Contains(c.GroupIDs, *group)) && (len(c.Models) == 0 || slices.Contains(c.Models, model))
}

func (c PrioritySchedulingConfig) weights() [4]float64 {
	switch c.Mode {
	case "experience":
		return [4]float64{40, 35, 20, 5}
	case "profit":
		return [4]float64{20, 15, 20, 45}
	case "balanced":
		return [4]float64{30, 25, 25, 20}
	default:
		return [4]float64{c.QualityWeight, c.LatencyWeight, c.LoadWeight, c.CostWeight}
	}
}

type priorityConfigCache struct {
	saveMu     sync.Mutex
	mu         sync.Mutex
	config     PrioritySchedulingConfig
	expires    time.Time
	refreshing bool
	revision   uint64
}

func (s *SettingService) GetPrioritySchedulingConfig(ctx context.Context) (PrioritySchedulingConfig, error) {
	c := DefaultPrioritySchedulingConfig()
	raw, err := s.settingRepo.GetValue(ctx, prioritySchedulingSettingKey)
	if errors.Is(err, ErrSettingNotFound) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	// Existing stored configurations opt in explicitly; fresh installations use 25.
	c.ErrorRateWeight = 0
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	return c, ValidatePrioritySchedulingConfig(c)
}
func (s *SettingService) SavePrioritySchedulingConfig(ctx context.Context, c PrioritySchedulingConfig) error {
	if c.LatencyPenaltyMode == "" {
		c.LatencyPenaltyMode = PriorityLatencyDeprioritize
	}
	if err := ValidatePrioritySchedulingConfig(c); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	cache := &s.prioritySchedulingConfig
	cache.saveMu.Lock()
	defer cache.saveMu.Unlock()
	if err = s.settingRepo.Set(ctx, prioritySchedulingSettingKey, string(raw)); err != nil {
		return err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.revision++
	cache.config = c
	cache.expires = time.Now().Add(5 * time.Second)
	return nil
}

// No database access on the request goroutine, including a cold start. A failed
// refresh disables the feature; other processes poll settings on a 5s interval.
func (s *SettingService) prioritySchedulingRuntimeConfig() PrioritySchedulingConfig {
	if s == nil || s.settingRepo == nil {
		return DefaultPrioritySchedulingConfig()
	}
	cache := &s.prioritySchedulingConfig
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	if !cache.refreshing && now.After(cache.expires) {
		cache.refreshing = true
		revision := cache.revision
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c, err := s.GetPrioritySchedulingConfig(ctx)
			cache.mu.Lock()
			defer cache.mu.Unlock()
			cache.refreshing = false
			if cache.revision != revision {
				return
			}
			if err != nil {
				c = DefaultPrioritySchedulingConfig()
			}
			cache.config = c
			cache.expires = time.Now().Add(5 * time.Second)
		}()
	}
	if now.After(cache.expires.Add(5 * time.Second)) {
		return DefaultPrioritySchedulingConfig()
	}
	return cache.config
}

func (s *OpenAIGatewayService) prioritySchedulingRuntimeConfig() PrioritySchedulingConfig {
	if s == nil {
		return DefaultPrioritySchedulingConfig()
	}
	return s.settingService.prioritySchedulingRuntimeConfig()
}

type PrioritySchedulingSignal struct {
	TeamsRevenue           float64    `json:"-"`
	TeamsWindowStart       *time.Time `json:"-"`
	TeamsWindowEnd         *time.Time `json:"-"`
	SelectedTTFTMs         *float64   `json:"selected_ttft_ms"`
	P50TTFTMs              float64    `json:"p50_ttft_ms"`
	P70TTFTMs              float64    `json:"p70_ttft_ms"`
	P95TTFTMs              float64    `json:"p95_ttft_ms"`
	LatencyPercentile      int        `json:"latency_percentile"`
	LatencyMeasurement     string     `json:"latency_measurement"`
	LatencyEvidenceVersion int        `json:"latency_evidence_version"`
	LatencyUnknownSamples  int        `json:"latency_unknown_samples"`
	ExperienceSamples      int        `json:"experience_samples"`
	ExperienceP90Ms        float64    `json:"experience_p90_ms"`
	LatestBadLatencyAt     *time.Time `json:"latest_bad_latency_at,omitempty"`
	LatestQualityPassed    *bool      `json:"latest_quality_passed,omitempty"`
	Revenue                float64    `json:"revenue"`
	BaseCost               float64    `json:"-"`
	ProfitSamples          int        `json:"profit_samples"`
	Samples                int        `json:"samples"`
	P90TTFTMs              float64    `json:"p90_ttft_ms"`
	LatestLatencyAt        *time.Time `json:"latest_latency_at,omitempty"`
	QualityPassed          int        `json:"quality_passed"`
	QualitySamples         int        `json:"quality_samples"`
}

type PrioritySchedulingQuery struct {
	LatencyPercentile  int
	LatencyMeasurement string
	TargetTTFTMs       int
	AccountIDs         []int64
	Model              string // public requested model in persisted usage
	QualityModel       string // routing model used by account quality probes; defaults to Model
	GroupID            *int64
	UsageUntil         time.Time
	UsageSince         time.Time
	QualitySince       time.Time
	TeamsWindows       []PriorityTeamsWindow
	TeamsWindowHours   int
}
type PrioritySchedulingSignalReader interface {
	ReadPrioritySchedulingSignals(context.Context, PrioritySchedulingQuery) (map[int64]PrioritySchedulingSignal, error)
}

type prioritySignalEntry struct {
	windowEnd                              time.Time
	signals                                map[int64]PrioritySchedulingSignal
	accounts                               map[int64]time.Time
	expires, observed, lastUsed, attempted time.Time
	refreshing                             bool
	failure                                string
}

type prioritySchedulingState struct {
	mu          sync.Mutex
	entries     map[string]*prioritySignalEntry
	launches    [2]time.Time
	launchIndex int
	active      int
	latest      *PrioritySchedulingSnapshot
	latestInput *prioritySnapshotInput
}

type PrioritySchedulingScore struct {
	PriorityErrorEvidence
	ErrorPenalty        float64                `json:"error_penalty"`
	BaseScore           float64                `json:"base_score"`
	TeamsRecovery       *PriorityTeamsRecovery `json:"teams_recovery"`
	OAuthQuotaRole      string                 `json:"oauth_quota_role,omitempty"`
	HistoryStatus       string                 `json:"history_status"`
	CapacityBand        int                    `json:"capacity_band"`
	BoundGroups         int                    `json:"bound_groups"`
	SelectionWeight     float64                `json:"selection_weight"`
	ExplorationEligible bool                   `json:"exploration_eligible"`
	TheoreticalCost     float64                `json:"theoretical_cost"`
	Profit              *float64               `json:"profit"`
	Margin              *float64               `json:"margin"`
	EconomicsSource     string                 `json:"economics_source"`
	Priority            int                    `json:"priority"`
	Concurrency         int                    `json:"concurrency"`
	LoadFactor          int                    `json:"load_factor"`
	AccountID           int64                  `json:"account_id"`
	AccountName         string                 `json:"account_name"`
	Score               float64                `json:"score"`
	Tier                string                 `json:"tier"`
	Reasons             []string               `json:"reasons"`
	Rate                *float64               `json:"rate"`
	LoadPercent         *int                   `json:"load_percent"`
	Waiting             int                    `json:"waiting"`
	LatencyPenaltyUntil *time.Time             `json:"latency_penalty_until,omitempty"`
	LatencyStopped      bool                   `json:"latency_stopped"`
	LatencyRecovery     bool                   `json:"latency_recovery,omitempty"`
	PrioritySchedulingSignal
}
type PrioritySchedulingSnapshot struct {
	SelectionPolicy    string                    `json:"selection_policy"`
	At                 time.Time                 `json:"at"`
	Model              string                    `json:"model"`
	GroupID            *int64                    `json:"group_id"`
	Mode               string                    `json:"mode"`
	HistoryReady       bool                      `json:"history_ready"`
	LatencyPercentile  int                       `json:"latency_percentile"`
	LatencyMeasurement string                    `json:"latency_measurement"`
	MinSamples         int                       `json:"min_samples"`
	WindowMinutes      int                       `json:"window_minutes"`
	HistoryWindowEnd   *time.Time                `json:"history_window_end,omitempty"`
	ConfigRevision     string                    `json:"config_revision"`
	Candidates         []PrioritySchedulingScore `json:"candidates"`
}

func (s *OpenAIGatewayService) PrioritySchedulingSnapshot() *PrioritySchedulingSnapshot {
	if s == nil {
		return nil
	}
	s.priorityScheduling.mu.Lock()
	input := s.priorityScheduling.latestInput
	latest := s.priorityScheduling.latest
	s.priorityScheduling.mu.Unlock()
	if input != nil {
		return s.prioritySnapshotFromInput(input)
	}
	if !s.prioritySchedulingRuntimeConfig().Enabled {
		return nil
	}
	return latest
}

func (s *OpenAIGatewayService) prioritySnapshotFromInput(input *prioritySnapshotInput) *PrioritySchedulingSnapshot {
	h := priorityHistoryResult{status: "limited"}
	if !input.limited {
		h = s.priorityHistory(input.request, input.config, input.candidates)
	}
	now := time.Now()
	snapshot := &PrioritySchedulingSnapshot{At: input.at, Model: priorityHistoryModel(input.request), GroupID: input.request.GroupID, Mode: input.config.Mode, SelectionPolicy: "capacity_first", HistoryReady: h.ready(), LatencyPercentile: input.config.LatencyPercentile, LatencyMeasurement: input.config.LatencyMeasurement, MinSamples: input.config.MinSamples, WindowMinutes: input.config.WindowMinutes, HistoryWindowEnd: h.windowEnd, ConfigRevision: priorityConfigRevision(input.config), Candidates: make([]PrioritySchedulingScore, 0, len(input.candidates))}
	for _, item := range input.candidates {
		if s.openaiAccountStats != nil {
			item.priorityErrors = s.openaiAccountStats.attempts.snapshot(priorityRequestAttemptKey(input.request, item.account.ID), now, input.config.ErrorRateWindowMinutes, input.config.ErrorRatePriorSamples)
		}
		signal := h.signals[item.account.ID]
		snapshot.Candidates = append(snapshot.Candidates, applyPriorityCandidate(input.config, &item, signal, now))
	}
	return snapshot
}

func (s *OpenAIGatewayService) prioritySignals(req OpenAIAccountScheduleRequest, c PrioritySchedulingConfig, accounts []openAIAccountCandidateScore) (map[int64]PrioritySchedulingSignal, bool) {
	h := s.priorityHistory(req, c, accounts)
	return h.signals, h.ready()
}

func scorePriorityCandidate(c PrioritySchedulingConfig, item openAIAccountCandidateScore, signal PrioritySchedulingSignal, now time.Time) PrioritySchedulingScore {
	return scorePriorityCandidateWithOAuthRate(c, item, signal, now, defaultOpenAIOAuthSchedulingRateMultiplier)
}

func scorePriorityCandidateWithOAuthRate(c PrioritySchedulingConfig, item openAIAccountCandidateScore, signal PrioritySchedulingSignal, now time.Time, _ float64) PrioritySchedulingScore {
	// Only unversioned legacy P90 inputs may use the old field. Attributed or
	// non-P90 evidence must never silently fall back to a different population.
	if signal.LatencyEvidenceVersion == 0 && c.LatencyMeasurement == "legacy_forward" && c.LatencyPercentile == 90 {
		if signal.Samples > 0 && signal.P90TTFTMs > 0 {
			value := signal.P90TTFTMs
			signal.SelectedTTFTMs = &value
		}
		signal.LatencyPercentile = 90
		signal.LatencyMeasurement = "legacy_forward"
	} else if signal.LatencyPercentile != c.LatencyPercentile || signal.LatencyMeasurement != c.LatencyMeasurement {
		signal.Samples = 0
		signal.SelectedTTFTMs = nil
		signal.LatencyPercentile = c.LatencyPercentile
		signal.LatencyMeasurement = c.LatencyMeasurement
	}
	selectedLatency := 0.0
	if signal.SelectedTTFTMs != nil {
		selectedLatency = *signal.SelectedTTFTMs
	}
	rate := item.account.CostMultiplier()
	theoreticalCost := signal.BaseCost * rate
	waiting := 0
	if item.loadInfo != nil {
		waiting = item.loadInfo.WaitingCount
	}
	out := PrioritySchedulingScore{Rate: &rate, TheoreticalCost: theoreticalCost, AccountID: item.account.ID, AccountName: item.account.Name, Priority: openAIAccountSchedulingPriority(item.account), Concurrency: item.account.Concurrency, LoadFactor: item.account.EffectiveLoadFactor(), Tier: "eligible", Reasons: []string{}, PrioritySchedulingSignal: signal, Waiting: waiting}
	quality, latency, load, cost := 0.5, 0.5, 0.5, 0.5
	degraded, unknown := false, false
	if signal.QualitySamples > 0 {
		quality = float64(signal.QualityPassed) / float64(signal.QualitySamples)
		if quality*100 < float64(c.MinQualityPercent) {
			degraded = true
			out.Reasons = append(out.Reasons, "quality_below_target")
		}
	} else {
		unknown = true
		out.Reasons = append(out.Reasons, "quality_unknown")
	}
	if signal.Samples >= c.MinSamples && selectedLatency > 0 {
		latency = 1 / (1 + selectedLatency/float64(c.TargetTTFTMs))
		if selectedLatency > float64(c.TargetTTFTMs) {
			// Renew only on a genuinely bad sample, never arbitrary new traffic.
			// Missing timestamps retain the penalty rather than invent recovery.
			badAt := signal.LatestBadLatencyAt
			if c.LatencyPenaltyMinutes > 0 && badAt != nil {
				until := badAt.Add(time.Duration(c.LatencyPenaltyMinutes) * time.Minute)
				out.LatencyPenaltyUntil = &until
				out.LatencyRecovery = !now.Before(until)
			}
			if out.LatencyRecovery {
				unknown = true
				out.Reasons = append(out.Reasons, "latency_recovery")
			} else {
				degraded = true
				out.Reasons = append(out.Reasons, "latency_above_target")
				out.LatencyStopped = c.LatencyPenaltyMode == PriorityLatencyStop
			}
		}
	} else {
		unknown = true
		out.Reasons = append(out.Reasons, "latency_insufficient")
	}
	if item.loadKnown {
		percent := 0
		if item.account.Concurrency > 0 {
			percent = int(100 * float64(item.loadInfo.CurrentConcurrency) / float64(item.account.Concurrency))
		}
		out.LoadPercent = &percent
		// A configured load factor may express a routing preference, but must
		// not make a nearly full account look idle to this capacity policy.
		load = (1 - clamp01(float64(item.loadInfo.CurrentConcurrency)/float64(max(1, item.account.Concurrency)))) / (1 + float64(max(0, item.loadInfo.WaitingCount)))
		if percent >= c.MaxLoadPercent || item.loadInfo.WaitingCount > 0 {
			degraded = true
			out.Reasons = append(out.Reasons, "busy")
		}
	} else {
		unknown = true
		out.Reasons = append(out.Reasons, "load_unknown")
	}

	if c.Teams.Enabled && isPriorityTeamsAccount(item.account) {
		out.TeamsRecovery = scorePriorityTeams(c.Teams, signal, now)
		if recovery := out.TeamsRecovery; recovery != nil {
			out.EconomicsSource = "teams_window"
			out.Profit = &recovery.ProfitCNY
			if recovery.RevenueCNY > 0 {
				margin := recovery.ProfitCNY / recovery.RevenueCNY
				out.Margin = &margin
			}
			cost = math.Max(0, recovery.RevenueCNY) / (math.Max(0, recovery.RevenueCNY) + recovery.CostCNY)
			// A paid window that has not broken even is a recovery target,
			// not evidence of bad account quality. Do not penalize its tier.
			if recovery.NeedsRecovery {
				out.Reasons = append(out.Reasons, "teams_recovery")
			}
		} else {
			unknown = true
			out.EconomicsSource = "unknown"
			out.Reasons = append(out.Reasons, "teams_window_unavailable")
		}
	} else if signal.ProfitSamples >= c.MinSamples && signal.Revenue >= 0 && theoreticalCost >= 0 && signal.Revenue+theoreticalCost > 0 {
		profit := signal.Revenue - theoreticalCost
		out.Profit = &profit
		if signal.Revenue > 0 {
			margin := profit / signal.Revenue
			out.Margin = &margin
		}
		// Revenue/(revenue+cost) maps break-even to 0.5 and zero-cost
		// revenue to 1, retaining loss information without unstable division
		// by a near-zero margin. Use sums, not a mean of per-request margins.
		cost = signal.Revenue / (signal.Revenue + theoreticalCost)
		out.EconomicsSource = "usage"
		if profit < 0 {
			degraded = true
			out.Reasons = append(out.Reasons, "historical_loss")
		}
	} else if item.account.IsOpenAIApiKey() && out.Rate != nil {
		cost = 1 / (1 + *out.Rate)
		out.EconomicsSource = "rate"
	} else {
		unknown = true
		out.EconomicsSource = "unknown"
		out.Reasons = append(out.Reasons, "profit_insufficient")
	}
	// Realtime failures are an additional penalty, never inferred from zero-cost logs.
	if c.ErrorRateWeight == 0 && item.errorRate > 0.2 {
		degraded = true
		out.Reasons = append(out.Reasons, "recent_errors")
	}
	w := c.weights()
	sum := w[0] + w[1] + w[2] + w[3]
	out.BaseScore = 100 * (w[0]*quality + w[1]*latency + w[2]*load + w[3]*cost) / sum
	out.PriorityErrorEvidence = item.priorityErrors
	out.ErrorPenalty = 1 - clamp01(item.errorRate)
	if c.ErrorRateWeight > 0 {
		out.ErrorPenalty = priorityErrorPenalty(c, item.priorityErrors)
	}
	out.Score = out.BaseScore * out.ErrorPenalty
	if degraded {
		out.Tier = "degraded"
	} else if unknown {
		out.Tier = "insufficient"
	}
	return out
}

func (s *defaultOpenAIAccountScheduler) applyPriorityScheduling(req OpenAIAccountScheduleRequest, plan *openAIAccountLoadPlan) bool {
	c := s.service.prioritySchedulingRuntimeConfig()
	if req.Platform != PlatformOpenAI || req.RequiredImageCapability != "" || !req.UseUpstreamTokenCost || !priorityApplies(c, req) {
		return false
	}
	signals, _ := s.service.prioritySignals(req, c, plan.candidates)
	now := time.Now()
	// Cold/failed history must not restore a tiny fixed Top-K and strand idle
	// accounts. Missing history stays unknown while live capacity still balances.
	for i := range plan.candidates {
		item := &plan.candidates[i]
		item.priorityErrors = s.priorityErrorEvidence(req, c, item.account.ID, now)
		applyPriorityCandidate(c, item, signals[item.account.ID], now)
	}
	plan.priorityScheduling = true
	plan.topK = len(plan.candidates)
	plan.includeOverflowFallback = true
	plan.selectionOrder = s.buildOpenAISelectionOrder(req, *plan)
	s.recordPrioritySnapshot(req, c, plan.selectionOrder, now)
	return true
}

func (s *defaultOpenAIAccountScheduler) recordPrioritySnapshot(req OpenAIAccountScheduleRequest, c PrioritySchedulingConfig, pool []openAIAccountCandidateScore, now time.Time) {
	signals, ready := s.service.prioritySignals(req, c, pool)
	snapshot := &PrioritySchedulingSnapshot{At: now, Model: priorityHistoryModel(req), GroupID: req.GroupID, Mode: c.Mode, SelectionPolicy: "capacity_first", HistoryReady: ready, LatencyPercentile: c.LatencyPercentile, LatencyMeasurement: c.LatencyMeasurement, MinSamples: c.MinSamples, WindowMinutes: c.WindowMinutes, HistoryWindowEnd: &now, ConfigRevision: priorityConfigRevision(c), Candidates: []PrioritySchedulingScore{}}
	if c.OAuthQuotaPriority {
		snapshot.SelectionPolicy = "oauth_quota_priority"
	}
	for _, candidate := range pool {
		role := ""
		if candidate.priorityOAuthSpare > 0 {
			role = "preferred"
		}
		if candidate.priorityAPIStandby {
			role = "standby"
		}
		item := candidate
		score := applyPriorityCandidate(c, &item, signals[item.account.ID], now)
		score.OAuthQuotaRole = role
		score.ExplorationEligible = item.priorityExploration
		score.BoundGroups = priorityAccountGroupCount(item.account)
		score.SelectionWeight = prioritySelectionWeight(item, now)
		score.CapacityBand = priorityCapacityBand(item)
		snapshot.Candidates = append(snapshot.Candidates, score)
	}
	slices.SortStableFunc(snapshot.Candidates, func(a, b PrioritySchedulingScore) int {
		unhealthy := func(v PrioritySchedulingScore) bool {
			return slices.Contains(v.Reasons, "quality_below_target") || slices.Contains(v.Reasons, "recent_errors") || slices.Contains(v.Reasons, "historical_loss")
		}
		if unhealthy(a) != unhealthy(b) {
			if unhealthy(a) {
				return 1
			}
			return -1
		}
		if a.CapacityBand != b.CapacityBand {
			return a.CapacityBand - b.CapacityBand
		}
		tier := func(t string) int {
			if t == "eligible" {
				return 2
			}
			if t == "insufficient" {
				return 1
			}
			return 0
		}
		if tier(a.Tier) != tier(b.Tier) {
			return tier(b.Tier) - tier(a.Tier)
		}
		aRecovery, bRecovery := a.TeamsRecovery != nil && a.TeamsRecovery.NeedsRecovery, b.TeamsRecovery != nil && b.TeamsRecovery.NeedsRecovery
		if aRecovery != bRecovery {
			if aRecovery {
				return -1
			}
			return 1
		}
		if aRecovery && a.TeamsRecovery.RequiredRevenuePerHourCNY != b.TeamsRecovery.RequiredRevenuePerHourCNY {
			if a.TeamsRecovery.RequiredRevenuePerHourCNY > b.TeamsRecovery.RequiredRevenuePerHourCNY {
				return -1
			}
			return 1
		}
		if a.Priority != b.Priority {
			if a.Priority < b.Priority {
				return -1
			}
			return 1
		}
		if a.SelectionWeight > b.SelectionWeight {
			return -1
		}
		if a.SelectionWeight < b.SelectionWeight {
			return 1
		}
		return 0
	})
	if len(snapshot.Candidates) > 100 {
		snapshot.Candidates = snapshot.Candidates[:100]
	}
	state := &s.service.priorityScheduling
	state.mu.Lock()
	state.latest = snapshot
	state.mu.Unlock()
}

func applyPriorityCandidate(c PrioritySchedulingConfig, item *openAIAccountCandidateScore, signal PrioritySchedulingSignal, now time.Time) PrioritySchedulingScore {
	score := scorePriorityCandidate(c, *item, signal, now)
	item.priorityUnhealthy = slices.Contains(score.Reasons, "quality_below_target") ||
		slices.Contains(score.Reasons, "recent_errors") || slices.Contains(score.Reasons, "historical_loss")
	item.priorityLatencyFactor = 1
	if score.Samples >= c.MinSamples && score.SelectedTTFTMs != nil && *score.SelectedTTFTMs > 0 {
		ratio := *score.SelectedTTFTMs / float64(c.TargetTTFTMs)
		// Experience constrains profit preference even when every account
		// misses the target. Keep a floor for recovery/overflow traffic.
		item.priorityLatencyFactor = math.Max(0.02, 1/(1+ratio*ratio))
	}
	// Experience tiers remain distinct within the same risk/capacity cohort;
	// current congestion is considered before historical score differences.
	offset := 0.0
	switch score.Tier {
	case "eligible":
		offset = 400
	case "insufficient":
		offset = 200
	}
	item.score = offset + score.Score
	item.priorityRecovery = false
	item.priorityRecoveryPressure = 0
	if score.TeamsRecovery != nil {
		item.priorityRecovery = score.TeamsRecovery.NeedsRecovery
		item.priorityRecoveryPressure = score.TeamsRecovery.RequiredRevenuePerHourCNY
	}
	qualityReady := score.QualitySamples > 0 &&
		float64(score.QualityPassed)/float64(score.QualitySamples)*100 >= float64(c.MinQualityPercent)
	item.priorityRecoveryExplorationPercent = c.RecoveryExplorationPercent
	item.priorityLatencyRecovery = score.LatencyRecovery
	item.priorityExploration = (!score.LatencyRecovery || c.RecoveryExplorationPercent > 0) && (item.account.IsOpenAIOAuth() || item.account.IsOpenAIApiKey()) &&
		score.Tier == "insufficient" && qualityReady && (c.ErrorRateWeight == 0 || item.priorityErrors.SmoothedRate <= .2) &&
		(!score.LatencyRecovery || (score.LatestQualityPassed != nil && *score.LatestQualityPassed)) &&
		(score.ProfitSamples < c.MinSamples || score.Samples < c.MinSamples || score.LatencyRecovery) &&
		item.loadKnown && score.LoadPercent != nil && *score.LoadPercent < c.MaxLoadPercent
	item.priorityOAuthSpare = priorityOAuthQuotaSpare(c, *item, score, now)
	item.priorityAPIStandby = false
	return score
}

func priorityHistoryKey(req OpenAIAccountScheduleRequest, c PrioritySchedulingConfig) string {
	// Switching the penalty action must keep the measured population.
	c.LatencyPenaltyMode = PriorityLatencyDeprioritize
	return fmt.Sprintf("%q/%q/%d/%t/%d/%d", priorityHistoryModel(req), strings.TrimSpace(req.RequestedModel), derefGroupID(req.GroupID), req.GroupID != nil, c.WindowMinutes, c.QualityMaxAgeHours) + "/" + priorityConfigRevision(c)
}

func priorityConfigRevision(c PrioritySchedulingConfig) string {
	b, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(b))[:16]
}
