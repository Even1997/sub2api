package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// Personal summaries intentionally exclude request IDs, credentials and upstream
// account details. Filter dimensions survive deletion of keys and groups.
func (r *usageLogRepository) GetHourlyUsage(ctx context.Context, start, end time.Time, granularity string, f usagestats.UsageLogFilters) (*usagestats.HourlyUsageReport, error) {
	if f.UserID <= 0 || !end.After(start) || end.Sub(start) > 400*24*time.Hour || (granularity != "day" && granularity != "hour") || (granularity == "hour" && end.Sub(start) > 31*24*time.Hour) {
		return nil, fmt.Errorf("invalid personal hourly usage range")
	}
	result := &usagestats.HourlyUsageReport{Items: []usagestats.HourlyUsageItem{}, Trend: []usagestats.TrendDataPoint{}, MissingMonths: []string{}, Timezone: "Asia/Shanghai"}
	if err := scanSingleRow(ctx, r.sql, `SELECT c.complete_from, transaction_timestamp(),
        ARRAY(SELECT month FROM monthly_bill_missing_months WHERE user_id=$1 ORDER BY month),
        EXISTS(SELECT 1 FROM usage_logs WHERE user_id=$1 AND created_at >= $2 AND created_at < $3 AND NOT hourly_summary_recorded)
        FROM monthly_bill_coverage c WHERE singleton=TRUE`, []any{f.UserID, start, end}, &result.CompleteFrom, &result.GeneratedAt, pq.Array(&result.MissingMonths), &result.BackfillPending); err != nil {
		return nil, err
	}
	args := []any{f.UserID, start, end}
	where := " WHERE user_id=$1 AND hour >= $2 AND hour < $3"
	for _, field := range []struct {
		name  string
		value int64
	}{{"api_key_id", f.APIKeyID}, {"group_id", f.GroupID}} {
		if field.value > 0 {
			args = append(args, field.value)
			where += fmt.Sprintf(" AND %s=$%d", field.name, len(args))
		}
	}
	where, args = appendUsageLogModelQueryFilter(where, args, f.Model, usagestats.ModelSourceRequested, f.ModelProvider)
	where, args = appendRequestTypeOrStreamQueryFilter(where, args, f.RequestType, f.Stream)
	where, args = appendNativeCompactionV2QueryFilter(where, args, f.NativeCompactionV2, "")
	where, args = appendUsageLogBillingModeQueryFilter(where, args, f.BillingMode, "")
	if f.BillingType != nil {
		args = append(args, *f.BillingType)
		where += fmt.Sprintf(" AND billing_type=$%d", len(args))
	}
	bucket, model := "hour", "requested_model"
	if granularity == "day" {
		bucket = "date_trunc('day',hour AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'"
		model = "''::text"
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT `+bucket+`, `+model+`, billing_type,
        SUM(requests)::text, SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens)::text,
        SUM(image_count)::text, SUM(video_count)::text, SUM(video_seconds)::text, SUM(actual_cost)::text
        FROM user_usage_hourly`+where+` GROUP BY 1,2,3 HAVING SUM(requests)>0 ORDER BY 1 DESC,2,3`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := make([]decimal.Decimal, 6)
	balance, subscription := decimal.Zero, decimal.Zero
	days := map[string]*usagestats.TrendDataPoint{}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for rows.Next() {
		var item usagestats.HourlyUsageItem
		v := &item.HourlyUsageValues
		if err := rows.Scan(&item.Hour, &item.Model, &item.BillingType, &v.Requests, &v.Tokens, &v.Images, &v.Videos, &v.VideoSeconds, &v.Cost); err != nil {
			return nil, err
		}
		values := []string{v.Requests, v.Tokens, v.Images, v.Videos, v.VideoSeconds, v.Cost}
		for i, raw := range values {
			n, err := decimal.NewFromString(raw)
			if err != nil {
				return nil, err
			}
			totals[i] = totals[i].Add(n)
			if i == 5 {
				if item.BillingType == 0 {
					balance = balance.Add(n)
				} else if item.BillingType == 1 {
					subscription = subscription.Add(n)
				}
			}
		}
		if granularity == "hour" {
			result.Items = append(result.Items, item)
		}
		date := item.Hour.In(zone).Format("2006-01-02")
		if days[date] == nil {
			days[date] = &usagestats.TrendDataPoint{Date: date}
		}
		requests, err := strconv.ParseInt(v.Requests, 10, 64)
		if err != nil {
			return nil, err
		}
		tokens, err := strconv.ParseInt(v.Tokens, 10, 64)
		if err != nil {
			return nil, err
		}
		cost, err := strconv.ParseFloat(v.Cost, 64)
		if err != nil {
			return nil, err
		}
		days[date].Requests += requests
		days[date].TotalTokens += tokens
		days[date].ActualCost += cost
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for day := start.In(zone); day.Before(end); day = day.AddDate(0, 0, 1) {
		if p := days[day.Format("2006-01-02")]; p != nil {
			result.Trend = append(result.Trend, *p)
		}
	}
	result.Totals = usagestats.HourlyUsageValues{Requests: totals[0].String(), Tokens: totals[1].String(), Images: totals[2].String(), Videos: totals[3].String(), VideoSeconds: totals[4].String(), Cost: totals[5].String()}
	result.BalanceCost = balance.String()
	result.SubscriptionCost = subscription.String()
	return result, nil
}
