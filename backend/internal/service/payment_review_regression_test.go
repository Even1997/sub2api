package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type reviewRefundWallet struct {
	UserRepository
	balance                     float64
	adjustErr                   error
	adjustCalls, availableCalls int
}

func (w *reviewRefundWallet) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Balance: w.balance}, nil
}

func (w *reviewRefundWallet) AdjustBalance(_ context.Context, _ int64, delta float64) (BalanceChange, error) {
	w.adjustCalls++
	if w.adjustErr != nil {
		return BalanceChange{}, w.adjustErr
	}
	if w.balance+delta < 0 {
		return BalanceChange{}, ErrBalanceNegative
	}
	change := BalanceChange{Old: w.balance, New: w.balance + delta}
	w.balance = change.New
	return change, nil
}

func (w *reviewRefundWallet) DeductAvailableBalance(_ context.Context, _ int64, amount float64) (float64, error) {
	w.availableCalls++
	deducted := math.Min(amount, math.Max(0, w.balance))
	w.balance -= deducted
	return deducted, nil
}

func reviewPaymentOrder(t *testing.T, client *dbent.Client, name, status string) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	u, err := client.User.Create().SetEmail(name + "@example.com").SetPasswordHash("hash").SetUsername(name).Save(ctx)
	require.NoError(t, err)
	o, err := client.PaymentOrder.Create().SetUserID(u.ID).SetUserEmail(u.Email).SetUserName(u.Username).
		SetAmount(100).SetPayAmount(100).SetFeeRate(0).SetRechargeCode("review-" + name).SetOutTradeNo("review_" + name).
		SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").SetOrderType(payment.OrderTypeBalance).SetStatus(status).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.com").Save(ctx)
	require.NoError(t, err)
	return o
}

func TestReviewRefundReservesFullBalanceAtExecution(t *testing.T) {
	for _, tc := range []struct {
		name               string
		balance            float64
		force, wantSuccess bool
		err                error
	}{
		{name: "spent_after_preparation", balance: 25},
		{name: "full_balance", balance: 100, wantSuccess: true},
		{name: "forced_partial", balance: 25, force: true, wantSuccess: true},
		{name: "storage_error", balance: 100, err: errors.New("storage unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			o := reviewPaymentOrder(t, client, tc.name, OrderStatusCompleted)
			wallet := &reviewRefundWallet{balance: 100}
			svc := &PaymentService{entClient: client, userRepo: wallet}
			plan := &RefundPlan{OrderID: o.ID, Order: o, RefundAmount: 100, GatewayAmount: 100, Force: tc.force, DeductBalance: true}
			require.Nil(t, svc.prepDeduct(ctx, o, plan, tc.force))
			wallet.balance, wallet.adjustErr = tc.balance, tc.err
			result, err := svc.ExecuteRefund(ctx, plan)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantSuccess, result.Success)
				require.Equal(t, !tc.wantSuccess, result.RequireForce)
			}
			current, err := client.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			audit, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ("REFUND_NO_TRADE_NO")).Count(ctx)
			require.NoError(t, err)
			if tc.wantSuccess {
				require.Equal(t, OrderStatusRefunded, current.Status)
				require.Zero(t, wallet.balance)
				require.Equal(t, tc.balance, result.BalanceDeducted)
				require.Equal(t, 1, audit)
			} else {
				require.Equal(t, OrderStatusCompleted, current.Status)
				require.Equal(t, tc.balance, wallet.balance)
				require.Zero(t, audit, "failed reservation must never enter gateway refund")
			}
			require.Equal(t, tc.force, wallet.availableCalls == 1)
			require.Equal(t, !tc.force, wallet.adjustCalls == 1)
		})
	}
}

type reviewPendingProvider struct {
	payment.Provider
	queries []string
	fail    bool
}

func (*reviewPendingProvider) ProviderKey() string { return payment.TypeAlipay }
func (*reviewPendingProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay}
}
func (p *reviewPendingProvider) QueryOrder(_ context.Context, ref string) (*payment.QueryOrderResponse, error) {
	p.queries = append(p.queries, ref)
	if p.fail {
		return nil, errors.New("upstream unavailable")
	}
	return &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}, nil
}

func TestReviewPendingReconciliationRotatesAcrossBatches(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("upstream_failure_", fail), func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			prov := &reviewPendingProvider{fail: fail}
			registry := payment.NewRegistry()
			registry.Register(prov)
			svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}
			for i := 0; i < pendingPaymentReconcileLimit+1; i++ {
				o := reviewPaymentOrder(t, client, fmt.Sprint(i), OrderStatusPending)
				_, err := client.PaymentOrder.UpdateOneID(o.ID).SetUpdatedAt(time.Now().Add(-time.Hour)).Save(ctx)
				require.NoError(t, err)
			}
			for i := 0; i < 2; i++ {
				n, err := svc.ReconcilePendingPaymentOrders(ctx)
				require.NoError(t, err)
				require.Zero(t, n)
			}
			require.Len(t, prov.queries, 2*pendingPaymentReconcileLimit)
			seen := map[string]bool{}
			for _, ref := range prov.queries {
				seen[ref] = true
			}
			require.Len(t, seen, pendingPaymentReconcileLimit+1)
			require.Equal(t, "review_20", prov.queries[pendingPaymentReconcileLimit])
		})
	}
}
