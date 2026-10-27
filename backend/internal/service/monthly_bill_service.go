package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// Monetary values stay decimal strings all the way to the browser and export.
type BillModel struct {
	Model        string `json:"model"`
	Requests     string `json:"requests"`
	Tokens       string `json:"tokens"`
	Images       string `json:"images"`
	VideoSeconds string `json:"video_seconds"`
	Videos       string `json:"videos"`
	Cost         string `json:"cost"`
}
type BillTransfer struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Amount string    `json:"amount"`
}
type BillDay struct {
	Date        string         `json:"date"`
	Consumption string         `json:"consumption"`
	Rebate      string         `json:"rebate"`
	Net         string         `json:"net"`
	Models      []BillModel    `json:"models"`
	Transfers   []BillTransfer `json:"transfers"`
}
type MonthlyBill struct {
	Month              string      `json:"month"`
	Version            int         `json:"version"`
	UserID             int64       `json:"user_id"`
	Email              string      `json:"email"`
	GeneratedAt        time.Time   `json:"generated_at"`
	Start              time.Time   `json:"start"`
	End                time.Time   `json:"end"`
	Partial            bool        `json:"partial"`
	Timezone           string      `json:"timezone"`
	CNYPerCredit       string      `json:"cny_per_credit"`
	Policy             string      `json:"policy"`
	Consumption        string      `json:"consumption"`
	Rebate             string      `json:"rebate"`
	Net                string      `json:"net"`
	CNY                string      `json:"cny"`
	Subscription       string      `json:"subscription"`
	Models             []BillModel `json:"models"`
	SubscriptionModels []BillModel `json:"subscription_models"`
	Days               []BillDay   `json:"days"`
}
type MonthlyBillVersion struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
}
type MonthlyBillService struct{ db *sql.DB }

func NewMonthlyBillService(db *sql.DB) *MonthlyBillService { return &MonthlyBillService{db: db} }

var billZone = time.FixedZone("Asia/Shanghai", 8*60*60)
var errBillIncomplete = apperrors.Conflict("BILL_HISTORY_INCOMPLETE", "该月历史数据完整性无法确认，不能生成账单。已保存账单仍可查看；请联系管理员核验历史日志。")

func billPeriod(month string, now time.Time) (time.Time, time.Time, bool, error) {
	start, err := time.ParseInLocation("2006-01", month, billZone)
	if err != nil || len(month) != 7 || start.Year() < 2000 || start.After(now) {
		return time.Time{}, time.Time{}, false, apperrors.BadRequest("BILL_MONTH_INVALID", "请选择有效月份，不能选择未来月份")
	}
	end := start.AddDate(0, 1, 0)
	partial := end.After(now)
	if partial {
		end = now
	}
	return start, end, partial, nil
}

func (s *MonthlyBillService) List(ctx context.Context, userID int64, month string) ([]MonthlyBillVersion, error) {
	if _, _, _, err := billPeriod(month, time.Now()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version, generated_at FROM user_monthly_bills WHERE user_id=$1 AND month=$2 ORDER BY version DESC`, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MonthlyBillVersion{}
	for rows.Next() {
		var v MonthlyBillVersion
		if err := rows.Scan(&v.Version, &v.GeneratedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *MonthlyBillService) Get(ctx context.Context, userID int64, month string, version int) (*MonthlyBill, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT snapshot FROM user_monthly_bills WHERE user_id=$1 AND month=$2 AND version=$3`, userID, month, version).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NotFound("BILL_NOT_FOUND", "账单不存在")
	}
	if err != nil {
		return nil, err
	}
	var bill MonthlyBill
	if err := json.Unmarshal(raw, &bill); err != nil {
		return nil, err
	}
	return &bill, nil
}

// A generation is identified by (owner, month, base version + 1). Double clicks,
// retries and concurrent tabs return the same saved version. An explicit refresh
// must supply the latest version. This requires no separate idempotency store.
func (s *MonthlyBillService) Generate(ctx context.Context, userID int64, month string, base int) (*MonthlyBill, error) {
	if base < 0 || base > 1000000 {
		return nil, apperrors.BadRequest("BILL_VERSION_INVALID", "无效账单版本")
	}
	if _, _, _, err := billPeriod(month, time.Now()); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		bill, err := s.generate(ctx, userID, month, base)
		var pg *pq.Error
		if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "23505") {
			continue
		}
		return bill, err
	}
	return nil, apperrors.Conflict("BILL_RETRY", "账单正在生成，请重试")
}

func (s *MonthlyBillService) generate(ctx context.Context, userID int64, month string, base int) (*MonthlyBill, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Database time is taken with the same MVCC snapshot as the source records.
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	start, end, partial, err := billPeriod(month, now)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT snapshot FROM user_monthly_bills WHERE user_id=$1 AND month=$2 AND version=$3`, userID, month, base+1).Scan(&raw)
	if err == nil {
		var b MonthlyBill
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, err
		}
		return &b, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM user_monthly_bills WHERE user_id=$1 AND month=$2`, userID, month).Scan(&latest); err != nil {
		return nil, err
	}
	if base != latest {
		return nil, apperrors.Conflict("BILL_VERSION_STALE", "请刷新账单版本后重试")
	}
	b := &MonthlyBill{Month: month, Version: base + 1, UserID: userID, GeneratedAt: now, Start: start, End: end, Partial: partial, Timezone: "Asia/Shanghai", CNYPerCredit: "1", Policy: "balance-minus-transferred-rebate-v1", Models: []BillModel{}, SubscriptionModels: []BillModel{}, Days: []BillDay{}}
	var complete bool
	if err := tx.QueryRowContext(ctx, `SELECT u.email, GREATEST($2::timestamptz,u.created_at) >= c.complete_from AND NOT EXISTS (SELECT 1 FROM monthly_bill_missing_months m WHERE m.user_id=u.id AND m.month=$3) FROM users u CROSS JOIN monthly_bill_coverage c WHERE u.id=$1`, userID, start, month).Scan(&b.Email, &complete); err != nil {
		return nil, err
	}
	if !complete {
		return nil, errBillIncomplete
	}
	// Do not understate charges still waiting for durable delivery to usage_logs.
	var unsettled bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM usage_billing_events WHERE observed_at >= $2 AND observed_at < $3 AND state IN ('pending','applied','blocked') AND payload->'command'->>'UserID'=$1)`, fmt.Sprint(userID), start, end).Scan(&unsettled); err != nil {
		return nil, err
	}
	if unsettled {
		return nil, apperrors.Conflict("BILL_UNSETTLED", "该月仍有消费记录等待入账，请稍后生成")
	}
	if err := readBillUsage(ctx, tx, b); err != nil {
		return nil, err
	}
	if err := readBillTransfers(ctx, tx, b); err != nil {
		return nil, err
	}
	if err := reconcileBill(b); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_monthly_bills(user_id,month,version,generated_at,snapshot) VALUES($1,$2,$3,$4,$5)`, userID, month, b.Version, now, string(raw)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return b, nil
}

// Token totals follow UsageLog.TotalTokens: image detail counters may overlap
// with input/output totals and must not be added a second time.
func readBillUsage(ctx context.Context, tx *sql.Tx, b *MonthlyBill) error {
	for day := b.Start; day.Before(b.End); day = day.AddDate(0, 0, 1) {
		b.Days = append(b.Days, BillDay{Date: day.Format("2006-01-02"), Consumption: "0", Rebate: "0", Net: "0", Models: []BillModel{}, Transfers: []BillTransfer{}})
	}
	rows, err := tx.QueryContext(ctx, `SELECT to_char(created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD'), model, billing_type,
        COUNT(*)::text,
        SUM(input_tokens::bigint+output_tokens+cache_creation_tokens+cache_read_tokens)::text,
        SUM(image_count)::text, SUM(COALESCE(video_duration_seconds,0)::bigint * video_count)::text, SUM(video_count)::text,
        SUM(actual_cost)::text
        FROM usage_logs WHERE user_id=$1 AND created_at >= $2 AND created_at < $3
        GROUP BY 1,2,3 ORDER BY 1,2,3`, b.UserID, b.Start, b.End)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		var kind int
		var m BillModel
		if err := rows.Scan(&day, &m.Model, &kind, &m.Requests, &m.Tokens, &m.Images, &m.VideoSeconds, &m.Videos, &m.Cost); err != nil {
			return err
		}
		if kind == int(BillingTypeSubscription) {
			b.SubscriptionModels = append(b.SubscriptionModels, m)
			continue
		}
		if kind != int(BillingTypeBalance) {
			return fmt.Errorf("unsupported billing type %d", kind)
		}
		b.Models = append(b.Models, m)
		for i := range b.Days {
			if b.Days[i].Date == day {
				b.Days[i].Models = append(b.Days[i].Models, m)
				break
			}
		}
	}
	return rows.Err()
}

func readBillTransfers(ctx context.Context, tx *sql.Tx, b *MonthlyBill) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,created_at,amount::text FROM user_affiliate_ledger WHERE user_id=$1 AND action='transfer' AND amount>0 AND created_at >= $2 AND created_at < $3 ORDER BY created_at,id`, b.UserID, b.Start, b.End)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r BillTransfer
		if err := rows.Scan(&r.ID, &r.At, &r.Amount); err != nil {
			return err
		}
		date := r.At.In(billZone).Format("2006-01-02")
		for i := range b.Days {
			if b.Days[i].Date == date {
				b.Days[i].Transfers = append(b.Days[i].Transfers, r)
				break
			}
		}
	}
	return rows.Err()
}

func sumBillValues(values ...string) (string, error) {
	sum := decimal.Zero
	for _, value := range values {
		n, err := decimal.NewFromString(value)
		if err != nil {
			return "", err
		}
		sum = sum.Add(n)
	}
	return sum.String(), nil
}
func mergeBillModels(items []BillModel) ([]BillModel, string, error) {
	models := map[string]BillModel{}
	total := decimal.Zero
	for _, m := range items {
		n, err := decimal.NewFromString(m.Cost)
		if err != nil {
			return nil, "", err
		}
		total = total.Add(n)
		old, exists := models[m.Model]
		if exists {
			for _, pair := range [][2]*string{{&m.Requests, &old.Requests}, {&m.Tokens, &old.Tokens}, {&m.Images, &old.Images}, {&m.VideoSeconds, &old.VideoSeconds}, {&m.Videos, &old.Videos}, {&m.Cost, &old.Cost}} {
				v, err := sumBillValues(*pair[0], *pair[1])
				if err != nil {
					return nil, "", err
				}
				*pair[0] = v
			}
		}
		models[m.Model] = m
	}
	result := make([]BillModel, 0, len(models))
	for _, m := range models {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Model < result[j].Model })
	return result, total.String(), nil
}
func reconcileBill(b *MonthlyBill) error {
	var err error
	b.Models, b.Consumption, err = mergeBillModels(b.Models)
	if err != nil {
		return err
	}
	b.SubscriptionModels, b.Subscription, err = mergeBillModels(b.SubscriptionModels)
	if err != nil {
		return err
	}
	rebates, dailyCosts, dailyNet := decimal.Zero, decimal.Zero, decimal.Zero
	for i := range b.Days {
		d := &b.Days[i]
		d.Models, d.Consumption, err = mergeBillModels(d.Models)
		if err != nil {
			return err
		}
		rebate := decimal.Zero
		for _, r := range d.Transfers {
			n, e := decimal.NewFromString(r.Amount)
			if e != nil {
				return e
			}
			rebate = rebate.Add(n)
		}
		cost, e := decimal.NewFromString(d.Consumption)
		if e != nil {
			return e
		}
		d.Rebate = rebate.String()
		d.Net = cost.Sub(rebate).String()
		rebates = rebates.Add(rebate)
		dailyCosts = dailyCosts.Add(cost)
		dailyNet = dailyNet.Add(cost.Sub(rebate))
	}
	cost, err := decimal.NewFromString(b.Consumption)
	if err != nil {
		return err
	}
	if !cost.Equal(dailyCosts) || !dailyNet.Equal(cost.Sub(rebates)) {
		return errors.New("bill reconciliation failed")
	}
	b.Rebate = rebates.String()
	b.Net = cost.Sub(rebates).String()
	b.CNY = b.Net
	return nil
}
