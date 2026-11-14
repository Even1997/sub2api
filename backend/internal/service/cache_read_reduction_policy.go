package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type CacheReadReductionRule = domain.CacheReadReductionRule

func normalizeCacheReadReductionRules(rules []CacheReadReductionRule) ([]CacheReadReductionRule, error) {
	if len(rules) > 200 {
		return nil, fmt.Errorf("at most 200 cache read reduction rules are allowed")
	}
	out := make([]CacheReadReductionRule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		rule.Model = strings.ToLower(strings.TrimSpace(rule.Model))
		if rule.Model == "" || len(rule.Model) > 200 || strings.ContainsAny(rule.Model, "*?\r\n\t ") {
			return nil, fmt.Errorf("cache read reduction requires an exact model name")
		}
		if seen[rule.Model] {
			return nil, fmt.Errorf("duplicate cache read reduction model: %s", rule.Model)
		}
		seen[rule.Model] = true
		if err := ValidateCacheReadReductionPercent(rule.Percent); err != nil {
			return nil, err
		}
		if rule.StartHitRatePercent != nil {
			start := *rule.StartHitRatePercent
			if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start >= 100 {
				return nil, fmt.Errorf("start_hit_rate_percent must be at least 0 and less than 100")
			}
			rule.StartHitRatePercent = &start
		}
		out = append(out, rule)
	}
	return out, nil
}

func (g *Group) cacheReadReductionPolicy(model string) CacheReadReductionRule {
	if g == nil {
		return CacheReadReductionRule{}
	}
	model = strings.TrimSpace(model)
	for _, rule := range g.CacheReadReductionRules {
		if strings.EqualFold(strings.TrimSpace(rule.Model), model) {
			return rule
		}
	}
	return CacheReadReductionRule{Percent: g.CacheReadReductionPercent}
}

func cacheReadReductionPercent(rule CacheReadReductionRule, totalInput, cached int) float64 {
	if totalInput <= 0 || cached <= 0 || rule.Percent <= 0 || math.IsNaN(rule.Percent) {
		return 0
	}
	percent := min(rule.Percent, 100)
	if rule.StartHitRatePercent == nil {
		return percent
	}
	start := *rule.StartHitRatePercent
	if start < 0 || start >= 100 || math.IsNaN(start) {
		return 0
	}
	hit := 100 * float64(min(cached, totalInput)) / float64(totalInput)
	return percent * math.Max(0, (hit-start)/(100-start))
}

func cacheReadReductionModel(original, fallback string) string {
	if strings.TrimSpace(original) != "" {
		return original
	}
	return fallback
}
