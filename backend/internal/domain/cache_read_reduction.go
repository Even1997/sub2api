package domain

// CacheReadReductionRule overrides the group default for one requested model.
// A nil StartHitRatePercent means fixed reduction; otherwise Percent is the
// maximum reached at a 100% cache hit rate.
type CacheReadReductionRule struct {
	Model               string   `json:"model"`
	Percent             float64  `json:"percent"`
	StartHitRatePercent *float64 `json:"start_hit_rate_percent,omitempty"`
}
