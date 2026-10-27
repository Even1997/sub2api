package usagestats

import "time"

// Exact decimal strings preserve money and quantities through the public API.
type HourlyUsageValues struct {
	Requests     string `json:"requests"`
	Tokens       string `json:"tokens"`
	Images       string `json:"images"`
	Videos       string `json:"videos"`
	VideoSeconds string `json:"video_seconds"`
	Cost         string `json:"cost"`
}

type HourlyUsageItem struct {
	HourlyUsageValues
	Hour        time.Time `json:"hour"`
	Model       string    `json:"model"`
	BillingType int       `json:"billing_type"`
}

type HourlyUsageReport struct {
	Items            []HourlyUsageItem `json:"items"`
	Trend            []TrendDataPoint  `json:"trend"`
	Totals           HourlyUsageValues `json:"totals"`
	BalanceCost      string            `json:"balance_cost"`
	SubscriptionCost string            `json:"subscription_cost"`
	CompleteFrom     time.Time         `json:"complete_from"`
	MissingMonths    []string          `json:"missing_months"`
	BackfillPending  bool              `json:"backfill_pending"`
	GeneratedAt      time.Time         `json:"generated_at"`
	Timezone         string            `json:"timezone"`
}
