package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// Keep the sample, not a second usage counter. Costs are read in two batched
// queries, so collecting samples adds no usage-log query to the request path.
type OpenAIQuotaCostSample struct {
	StartAt        int64   `json:"start_at"`
	SampledAt      int64   `json:"sampled_at"`
	UsedPercent    float64 `json:"used_percent"`
	PlanType       string  `json:"plan_type"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

func openAIQuotaWindowDuration(window string) time.Duration {
	if window == OpenAIQuotaWindow5h {
		return 5 * time.Hour
	}
	return 7 * 24 * time.Hour
}

func openAIQuotaUsedPercent(extra map[string]any, window string) (float64, bool) {
	used, err := strconv.ParseFloat(fmt.Sprint(extra["codex_"+window+"_used_percent"]), 64)
	return used, err == nil && !math.IsNaN(used) && !math.IsInf(used, 0) && used >= 0 && used <= 100
}

// Only fields from one upstream observation may be used to build a sample.
// A 1% rounded reading is too noisy to replace an established capacity.
func NewOpenAIQuotaCostSample(extra map[string]any, window string) *OpenAIQuotaCostSample {
	if minutes, exists := extra["codex_"+window+"_window_minutes"]; exists && parseExtraInt(minutes) != int(openAIQuotaWindowDuration(window).Minutes()) {
		return nil
	}
	if raw, versioned := extra[CodexUsageFieldVersionsKey]; versioned {
		versions, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		prefix := "codex_" + window + "_"
		// A coalesced packet can contain multiple observations. Capacity needs
		// a percentage and reset from the same observation, not its global time.
		for _, suffix := range []string{"reset_at", "reset_after_seconds", "window_minutes"} {
			if _, exists := extra[prefix+suffix]; exists && fmt.Sprint(versions[prefix+suffix]) != fmt.Sprint(versions[prefix+"used_percent"]) {
				return nil
			}
		}
		extra = qualityQuotaWindowObservation(extra, window, time.Now())
	}
	used, ok := openAIQuotaUsedPercent(extra, window)
	if !ok || used < 5 {
		return nil
	}
	observedAt, err := parseTime(fmt.Sprint(extra["codex_usage_updated_at"]))
	if err != nil {
		return nil
	}
	progress := buildCodexUsageProgressFromExtra(extra, window, observedAt)
	if progress == nil || progress.ResetsAt == nil {
		return nil
	}
	duration := openAIQuotaWindowDuration(window)
	start := progress.ResetsAt.Add(-duration)
	if !start.Before(observedAt) || !observedAt.Before(*progress.ResetsAt) {
		return nil
	}
	return &OpenAIQuotaCostSample{StartAt: start.Unix(), SampledAt: observedAt.Unix(), UsedPercent: used}
}

func openAIQuotaCostSampleForAccount(account *Account, window string, now time.Time) *OpenAIQuotaCostSample {
	var sample *OpenAIQuotaCostSample
	plan := strings.ToLower(strings.TrimSpace(account.GetCredential("plan_type")))
	if stored, exists := account.Extra["codex_"+window+"_cost_sample"]; exists {
		data, err := json.Marshal(stored)
		if err != nil || json.Unmarshal(data, &sample) != nil || sample == nil ||
			sample.PlanType != plan || sample.RateMultiplier != account.BillingRateMultiplier() {
			return nil
		}
	} else {
		// Bootstrap existing accounts without writing from the summary GET.
		sample = NewOpenAIQuotaCostSample(account.Extra, window)
		if sample == nil {
			return nil
		}
		sample.PlanType, sample.RateMultiplier = plan, account.BillingRateMultiplier()
	}
	if sample.StartAt <= 0 || sample.SampledAt <= sample.StartAt || sample.SampledAt > now.Unix() ||
		sample.SampledAt-sample.StartAt >= int64(openAIQuotaWindowDuration(window).Seconds()) ||
		!(sample.UsedPercent >= 5 && sample.UsedPercent <= 100) ||
		math.IsNaN(sample.RateMultiplier) || math.IsInf(sample.RateMultiplier, 0) || sample.RateMultiplier < 0 {
		return nil
	}
	return sample
}

func openAIQuotaCurrentUsage(extra map[string]any, window string, now time.Time) (float64, bool) {
	if _, versioned := extra[CodexUsageFieldVersionsKey]; versioned {
		extra = qualityQuotaWindowObservation(extra, window, now)
	}
	used, ok := openAIQuotaUsedPercent(extra, window)
	if !ok {
		return 0, false
	}
	stamp, exists := extra["codex_"+window+"_observed_at"]
	if !exists {
		stamp = extra["codex_usage_updated_at"]
	}
	observedAt, err := parseTime(fmt.Sprint(stamp))
	if err != nil || observedAt.After(now) || now.Sub(observedAt) > openAIQuotaWindowDuration(window) {
		return 0, false
	}
	progress := buildCodexUsageProgressFromExtra(extra, window, observedAt)
	if progress != nil && progress.ResetsAt != nil && now.Before(*progress.ResetsAt) &&
		progress.ResetsAt.Sub(observedAt) <= openAIQuotaWindowDuration(window) {
		return used, true
	}
	// An expired cached window is not proof of a fresh, unused window. Only an
	// explicit recent zero (including reset-credit recovery) can restore it.
	return used, used == 0 && now.Sub(observedAt) <= openAIProbeCacheTTL &&
		(progress == nil || progress.ResetsAt == nil || !observedAt.Before(*progress.ResetsAt))
}

func estimateOpenAIQuotaCost(result *OpenAIQuotaCostEstimate, samples map[int64]OpenAIQuotaCostSample, usage map[int64]float64, stats map[int64]*usagestats.AccountStats) {
	var remaining, total float64
	for id, sample := range samples {
		stat := stats[id]
		if stat == nil || stat.Cost <= 0 {
			continue
		}
		estimated := stat.Cost * 100 / sample.UsedPercent
		if math.IsNaN(estimated) || math.IsInf(estimated, 0) || estimated <= 0 {
			continue
		}
		total += estimated
		remaining += estimated * (1 - usage[id]/100)
		result.EstimatedAccounts++
	}
	if result.EstimatedAccounts > 0 {
		result.EstimatedCost, result.RemainingCost = &total, &remaining
	}
}
