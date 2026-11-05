package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	paymentprovider "github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Cancel & Expire ---

// Cancel rate limit configuration constants.
const (
	rateLimitUnitDay           = "day"
	rateLimitUnitMinute        = "minute"
	rateLimitUnitHour          = "hour"
	rateLimitModeFixed         = "fixed"
	checkPaidResultAlreadyPaid = "already_paid"
	checkPaidResultCancelled   = "cancelled"

	pendingPaymentReconcileLimit  = 20
	pendingCloudcatReconcileLimit = 20
	paymentVerificationCooldown   = 15 * time.Second
	cloudcatRecoveryWindow        = 24 * time.Hour
)

type checkPaidOptions struct {
	cancelIfUnpaid bool
}

func (s *PaymentService) checkCancelRateLimit(ctx context.Context, userID int64, cfg *PaymentConfig) error {
	if !cfg.CancelRateLimitEnabled || cfg.CancelRateLimitMax <= 0 {
		return nil
	}
	windowStart := cancelRateLimitWindowStart(cfg)
	operator := fmt.Sprintf("user:%d", userID)
	count, err := s.entClient.PaymentAuditLog.Query().
		Where(
			paymentauditlog.ActionEQ("ORDER_CANCELLED"),
			paymentauditlog.OperatorEQ(operator),
			paymentauditlog.CreatedAtGTE(windowStart),
		).Count(ctx)
	if err != nil {
		slog.Error("check cancel rate limit failed", "userID", userID, "error", err)
		return nil // fail open
	}
	if count >= cfg.CancelRateLimitMax {
		return infraerrors.TooManyRequests("CANCEL_RATE_LIMITED", "cancel rate limited").
			WithMetadata(map[string]string{
				"max":    strconv.Itoa(cfg.CancelRateLimitMax),
				"window": strconv.Itoa(cfg.CancelRateLimitWindow),
				"unit":   cfg.CancelRateLimitUnit,
			})
	}
	return nil
}

func cancelRateLimitWindowStart(cfg *PaymentConfig) time.Time {
	now := time.Now()
	w := cfg.CancelRateLimitWindow
	if w <= 0 {
		w = 1
	}
	unit := cfg.CancelRateLimitUnit
	if unit == "" {
		unit = rateLimitUnitDay
	}
	if cfg.CancelRateLimitMode == rateLimitModeFixed {
		switch unit {
		case rateLimitUnitMinute:
			t := now.Truncate(time.Minute)
			return t.Add(-time.Duration(w-1) * time.Minute)
		case rateLimitUnitDay:
			y, m, d := now.Date()
			t := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			return t.AddDate(0, 0, -(w - 1))
		default: // hour
			t := now.Truncate(time.Hour)
			return t.Add(-time.Duration(w-1) * time.Hour)
		}
	}
	// rolling window
	switch unit {
	case rateLimitUnitMinute:
		return now.Add(-time.Duration(w) * time.Minute)
	case rateLimitUnitDay:
		return now.AddDate(0, 0, -w)
	default: // hour
		return now.Add(-time.Duration(w) * time.Hour)
	}
}

func (s *PaymentService) CancelOrder(ctx context.Context, orderID, userID int64) (string, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return "", infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != userID {
		return "", infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	if o.Status != OrderStatusPending {
		return "", infraerrors.BadRequest("INVALID_STATUS", "order cannot be cancelled in current status")
	}
	return s.cancelCore(ctx, o, OrderStatusCancelled, fmt.Sprintf("user:%d", userID), "user closed order locally")
}

func (s *PaymentService) AdminCancelOrder(ctx context.Context, orderID int64) (string, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return "", infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusPending {
		return "", infraerrors.BadRequest("INVALID_STATUS", "order cannot be cancelled in current status")
	}
	return s.cancelCore(ctx, o, OrderStatusCancelled, "admin", "admin closed order locally")
}

// ResumeCloudcatOrder returns a fresh QR payload when available and keeps the
// original hosted checkout URL as a fallback.
func (s *PaymentService) ResumeCloudcatOrder(ctx context.Context, orderID, userID int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != userID {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	if !isCloudcatOrder(o) {
		return nil, infraerrors.BadRequest("RESUME_UNSUPPORTED", "this payment order cannot be reopened")
	}
	if o.Status != OrderStatusPending {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only pending payment orders can be reopened")
	}
	if !o.ExpiresAt.After(time.Now()) {
		return nil, infraerrors.BadRequest("ORDER_EXPIRED", "payment order has expired")
	}
	if o.PayURL == nil || strings.TrimSpace(*o.PayURL) == "" {
		return nil, infraerrors.Conflict("PAYMENT_URL_UNAVAILABLE", "payment URL is unavailable")
	}
	if err := paymentprovider.ValidateCloudcatPaymentURL(*o.PayURL); err != nil {
		return nil, infraerrors.Conflict("PAYMENT_URL_INVALID", "payment URL is invalid")
	}
	o.QrCode = nil
	prov, err := s.getOrderProvider(ctx, o)
	if err != nil {
		return o, nil
	}
	cloudcat, ok := prov.(interface {
		ResolvePaymentQRCode(context.Context, string) (string, error)
	})
	if !ok {
		return o, nil
	}
	qrCode, err := cloudcat.ResolvePaymentQRCode(ctx, *o.PayURL)
	if err != nil {
		slog.Warn("refresh cloudcat QR code failed", "orderID", o.ID, "error", err)
		return o, nil
	}
	o.QrCode = &qrCode
	return o, nil
}

func (s *PaymentService) cancelCore(ctx context.Context, o *dbent.PaymentOrder, fs, op, ad string) (string, error) {
	if o.PaymentTradeNo != "" || o.PaymentType != "" {
		embedded := psStringValue(o.ProviderKey) == payment.TypeBEpusdt &&
			psSnapshotStringValue(o.ProviderSnapshot["collection_mode"]) == "embedded"
		if embedded {
			if s.reconcilePaid(ctx, o) == checkPaidResultAlreadyPaid {
				return checkPaidResultAlreadyPaid, nil
			}
			// Expiry must retain the engine's recovery window for transfers
			// sent before the deadline but discovered after an RPC outage.
			if fs == OrderStatusCancelled {
				prov, err := s.getOrderProvider(ctx, o)
				if err != nil {
					return "", err
				}
				cp, ok := prov.(payment.CancelableProvider)
				if !ok {
					return "", fmt.Errorf("built-in payment cannot be cancelled")
				}
				// The engine atomically rejects cancellation after detection.
				// Do not mark the merchant order cancelled if that check fails.
				if err := cp.CancelPayment(ctx, paymentOrderQueryReference(o, prov)); err != nil {
					return "", infraerrors.BadRequest("PAYMENT_NOT_CANCELLABLE", "payment is no longer waiting; refresh its status")
				}
			}
		} else {
			// Cloudcat has no cancellation API. Reconcile it before closing so a
			// payment that already settled upstream is still fulfilled, while never
			// sending an upstream cancellation request for an unpaid checkout.
			checkPaidOpts := checkPaidOptions{cancelIfUnpaid: true}
			if isCloudcatOrder(o) {
				checkPaidOpts.cancelIfUnpaid = false
			}
			if s.checkPaidWithOptions(ctx, o, checkPaidOpts) == checkPaidResultAlreadyPaid {
				return checkPaidResultAlreadyPaid, nil
			}
		}
	}
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(OrderStatusPending)).SetStatus(fs).Save(ctx)
	if err != nil {
		return "", fmt.Errorf("update order status: %w", err)
	}
	if c == 0 {
		current, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if err != nil {
			return "", fmt.Errorf("reload order status: %w", err)
		}
		if current.Status == OrderStatusPaid || current.Status == OrderStatusRecharging || current.Status == OrderStatusCompleted || psIsRefundStatus(current.Status) {
			return checkPaidResultAlreadyPaid, nil
		}
		return checkPaidResultCancelled, nil
	}
	if c > 0 {
		auditAction := "ORDER_CANCELLED"
		if fs == OrderStatusExpired {
			auditAction = "ORDER_EXPIRED"
		}
		detail := map[string]any{"detail": ad}
		if isCloudcatOrder(o) && fs == OrderStatusCancelled {
			detail["scope"] = "local_only"
			detail["upstream_cancelled"] = false
		}
		s.writeAuditLog(ctx, o.ID, auditAction, op, detail)
	}
	return checkPaidResultCancelled, nil
}

func (s *PaymentService) reconcilePaid(ctx context.Context, o *dbent.PaymentOrder) string {
	return s.checkPaidWithOptions(ctx, o, checkPaidOptions{})
}

type paymentVerificationResult struct {
	order   *dbent.PaymentOrder
	outcome string
}

// reconcilePaidWithCooldown merges same-order checks in this process and uses
// Redis to suppress duplicate checks across instances. Cancellation and expiry
// paths deliberately call reconcilePaid directly because they need a final
// payment check before changing the local order state.
func (s *PaymentService) reconcilePaidWithCooldown(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentOrder, string, error) {
	if o == nil {
		return nil, "", nil
	}

	key := strconv.FormatInt(o.ID, 10)
	value, err, _ := s.verificationFlight.Do(key, func() (any, error) {
		if s.verificationCooldown != nil {
			acquired, gateErr := s.verificationCooldown.TryAcquire(ctx, o.ID, paymentVerificationCooldown)
			if gateErr != nil {
				slog.Warn("payment verification cooldown unavailable; continuing", "orderID", o.ID, "error", gateErr)
			} else if !acquired {
				current, reloadErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
				if reloadErr == nil {
					return &paymentVerificationResult{order: current}, nil
				}
				return &paymentVerificationResult{order: o}, nil
			}
		}

		outcome := s.reconcilePaid(ctx, o)
		if outcome != checkPaidResultAlreadyPaid {
			return &paymentVerificationResult{order: o, outcome: outcome}, nil
		}
		current, reloadErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if reloadErr != nil {
			return nil, fmt.Errorf("reload order after verification: %w", reloadErr)
		}
		return &paymentVerificationResult{order: current, outcome: outcome}, nil
	})
	if err != nil {
		return nil, "", err
	}
	result, ok := value.(*paymentVerificationResult)
	if !ok || result == nil || result.order == nil {
		return nil, "", fmt.Errorf("invalid payment verification result")
	}
	return result.order, result.outcome, nil
}

func (s *PaymentService) checkPaidWithOptions(ctx context.Context, o *dbent.PaymentOrder, opts checkPaidOptions) string {
	if isCloudcatOrder(o) && o.Status == OrderStatusRecharging {
		if err := s.ExecuteCloudcatBalanceFulfillment(ctx, o.ID); err != nil {
			slog.Warn("recover cloudcat fulfillment failed", "orderID", o.ID, "error", err)
			return ""
		}
		return checkPaidResultAlreadyPaid
	}
	prov, err := s.getOrderProvider(ctx, o)
	if err != nil {
		return ""
	}
	queryRef := paymentOrderQueryReference(o, prov)
	if queryRef == "" {
		return ""
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.QueryOrder(ctx, queryRef)
	finishProviderCall()
	if err != nil {
		slog.Warn("query upstream failed", "orderID", o.ID, "error", err)
		return ""
	}
	if resp.Status == payment.ProviderStatusPaid {
		if !isValidProviderAmount(resp.Amount) {
			s.writeAuditLog(ctx, o.ID, "PAYMENT_INVALID_AMOUNT", prov.ProviderKey(), map[string]any{
				"expected": o.PayAmount,
				"paid":     resp.Amount,
				"tradeNo":  resp.TradeNo,
				"queryRef": queryRef,
			})
			slog.Warn("query upstream returned invalid paid amount", "orderID", o.ID, "queryRef", queryRef, "paid", resp.Amount)
			retriedResp, retryOK := requeryPaidOrderOnce(ctx, prov, queryRef)
			if !retryOK {
				return ""
			}
			resp = retriedResp
		}
		notificationTradeNo := o.PaymentTradeNo
		if upstreamTradeNo := strings.TrimSpace(resp.TradeNo); paymentOrderShouldPersistUpstreamTradeNo(queryRef, upstreamTradeNo, notificationTradeNo) {
			if _, updateErr := s.entClient.PaymentOrder.Update().
				Where(paymentorder.IDEQ(o.ID)).
				SetPaymentTradeNo(upstreamTradeNo).
				Save(ctx); updateErr != nil {
				slog.Error("persist upstream trade no during checkPaid failed", "orderID", o.ID, "tradeNo", upstreamTradeNo, "error", updateErr)
			} else {
				o.PaymentTradeNo = upstreamTradeNo
			}
			notificationTradeNo = upstreamTradeNo
		}
		if strings.EqualFold(strings.TrimSpace(prov.ProviderKey()), payment.TypeCloudcat) {
			if err := s.handleCloudcatPaidQuery(ctx, o, resp); err != nil {
				slog.Error("cloudcat fulfillment failed during reconciliation", "orderID", o.ID, "error", err)
			}
			return checkPaidResultAlreadyPaid
		}
		if err := s.HandlePaymentNotification(ctx, &payment.PaymentNotification{TradeNo: notificationTradeNo, OrderID: o.OutTradeNo, Amount: resp.Amount, Status: payment.ProviderStatusSuccess, Metadata: resp.Metadata}, prov.ProviderKey()); err != nil {
			slog.Error("fulfillment failed during checkPaid", "orderID", o.ID, "error", err)
			// Still return already_paid — order was paid, fulfillment can be retried
		}
		return checkPaidResultAlreadyPaid
	}
	if resp.Status == payment.ProviderStatusFailed && strings.EqualFold(strings.TrimSpace(prov.ProviderKey()), payment.TypeCloudcat) {
		s.markCloudcatPaymentFailed(ctx, o)
		return ""
	}
	if !opts.cancelIfUnpaid {
		return ""
	}
	if cp, ok := prov.(payment.CancelableProvider); ok {
		finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
		_ = cp.CancelPayment(ctx, queryRef)
		finishProviderCall()
	}
	return ""
}

func requeryPaidOrderOnce(ctx context.Context, prov payment.Provider, queryRef string) (*payment.QueryOrderResponse, bool) {
	if prov == nil || strings.TrimSpace(queryRef) == "" {
		return nil, false
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.QueryOrder(ctx, queryRef)
	finishProviderCall()
	if err != nil {
		slog.Warn("query upstream retry failed", "queryRef", queryRef, "error", err)
		return nil, false
	}
	if resp == nil || resp.Status != payment.ProviderStatusPaid || !isValidProviderAmount(resp.Amount) {
		return nil, false
	}
	return resp, true
}

func paymentOrderQueryReference(order *dbent.PaymentOrder, prov payment.Provider) string {
	if order == nil {
		return ""
	}

	providerKey := ""
	if prov != nil {
		providerKey = strings.TrimSpace(prov.ProviderKey())
	}
	if providerKey == "" {
		if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
			providerKey = strings.TrimSpace(snapshot.ProviderKey)
		}
	}
	if providerKey == "" {
		providerKey = strings.TrimSpace(psStringValue(order.ProviderKey))
	}
	if providerKey == "" {
		providerKey = strings.TrimSpace(order.PaymentType)
	}

	switch payment.GetBasePaymentType(providerKey) {
	case payment.TypeAlipay, payment.TypeEasyPay, payment.TypeWxpay:
		return strings.TrimSpace(order.OutTradeNo)
	default:
		if tradeNo := strings.TrimSpace(order.PaymentTradeNo); tradeNo != "" {
			return tradeNo
		}
		return strings.TrimSpace(order.OutTradeNo)
	}
}

func paymentOrderShouldPersistUpstreamTradeNo(queryRef, upstreamTradeNo, currentTradeNo string) bool {
	upstreamTradeNo = strings.TrimSpace(upstreamTradeNo)
	if upstreamTradeNo == "" {
		return false
	}
	if strings.EqualFold(upstreamTradeNo, strings.TrimSpace(currentTradeNo)) {
		return false
	}
	if strings.EqualFold(upstreamTradeNo, strings.TrimSpace(queryRef)) {
		return false
	}
	return true
}

// VerifyOrderByOutTradeNo actively queries the upstream provider to check
// if a payment was made, and processes it if so. This handles the case where
// the provider's notify callback was missed (e.g. EasyPay popup mode).
func (s *PaymentService) VerifyOrderByOutTradeNo(ctx context.Context, outTradeNo string, userID int64) (*dbent.PaymentOrder, error) {
	outTradeNo, err := normalizeOrderLookupOutTradeNo(outTradeNo)
	if err != nil {
		return nil, err
	}
	o, err := s.entClient.PaymentOrder.Query().
		Where(paymentorder.OutTradeNo(outTradeNo)).
		Only(ctx)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != userID {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	// A paid upstream order may still be in fulfillment, so authenticated polling
	// keeps reconciling it while the verification gate prevents duplicate checks.
	if o.Status == OrderStatusPending || o.Status == OrderStatusExpired || (isCloudcatOrder(o) && (o.Status == OrderStatusPaid || o.Status == OrderStatusRecharging || o.Status == OrderStatusCancelled)) {
		o, _, err = s.reconcilePaidWithCooldown(ctx, o)
		if err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (s *PaymentService) handleCloudcatPaidQuery(ctx context.Context, o *dbent.PaymentOrder, resp *payment.QueryOrderResponse) error {
	if o == nil || resp == nil {
		return fmt.Errorf("cloudcat paid query is missing order data")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return fmt.Errorf("cloudcat order %d is not a balance recharge", o.ID)
	}
	if !isValidProviderAmount(resp.Amount) || math.Abs(resp.Amount-o.PayAmount) > paymentAmountToleranceForCurrency(PaymentOrderCurrency(o)) {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_AMOUNT_MISMATCH", payment.TypeCloudcat, map[string]any{
			"expected": o.PayAmount, "paid": resp.Amount, "tradeNo": resp.TradeNo,
		})
		return fmt.Errorf("cloudcat paid amount does not match order %d", o.ID)
	}
	if err := validateProviderSnapshotMetadata(o, payment.TypeCloudcat, resp.Metadata); err != nil {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", payment.TypeCloudcat, map[string]any{
			"detail": err.Error(), "tradeNo": resp.TradeNo,
		})
		return err
	}

	now := time.Now()
	grace := now.Add(-cloudcatRecoveryWindow)
	tradeNo := firstNonEmpty(strings.TrimSpace(resp.TradeNo), strings.TrimSpace(o.PaymentTradeNo))
	updated, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(o.ID),
		paymentorder.Or(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.And(paymentorder.StatusIn(OrderStatusExpired, OrderStatusCancelled), paymentorder.ExpiresAtGTE(grace)),
		),
	).SetStatus(OrderStatusPaid).
		SetPayAmount(resp.Amount).
		SetPaymentTradeNo(tradeNo).
		SetPaidAt(now).
		ClearFailedAt().
		ClearFailedReason().
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark cloudcat order paid: %w", err)
	}
	if updated > 0 {
		if o.Status == OrderStatusCancelled {
			s.writeAuditLog(ctx, o.ID, "CLOUDCAT_PAYMENT_AFTER_LOCAL_CLOSE", payment.TypeCloudcat, map[string]any{"tradeNo": tradeNo})
		}
		s.writeAuditLog(ctx, o.ID, "ORDER_PAID", payment.TypeCloudcat, map[string]any{
			"tradeNo": tradeNo, "paidAmount": resp.Amount,
		})
	}

	current, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	if err != nil {
		return fmt.Errorf("reload cloudcat paid order: %w", err)
	}
	code := strings.TrimSpace(resp.Metadata[payment.MetadataCloudcatRechargeCode])
	if code == "" {
		if !s.hasAuditLog(ctx, o.ID, "CLOUDCAT_PAID_AWAITING_CDK") {
			s.writeAuditLog(ctx, o.ID, "CLOUDCAT_PAID_AWAITING_CDK", payment.TypeCloudcat, map[string]any{
				"tradeNo": tradeNo,
			})
		}
		return nil
	}
	if len(code) > 64 {
		return fmt.Errorf("cloudcat delivered redeem code exceeds 64 characters")
	}
	duplicate, err := s.entClient.PaymentOrder.Query().Where(
		paymentorder.RechargeCodeEQ(code),
		paymentorder.IDNEQ(o.ID),
	).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check cloudcat redeem code ownership: %w", err)
	}
	if duplicate {
		return fmt.Errorf("cloudcat delivered redeem code is already attached to another payment order")
	}
	if current.Status == OrderStatusCompleted {
		if current.RechargeCode == code {
			return nil
		}
		return fmt.Errorf("completed cloudcat order has a different redeem code")
	}
	if current.Status != OrderStatusPaid {
		return fmt.Errorf("cloudcat order cannot attach a redeem code in status %s", current.Status)
	}
	if _, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(OrderStatusPaid),
	).SetRechargeCode(code).Save(ctx); err != nil {
		return fmt.Errorf("attach cloudcat redeem code: %w", err)
	}
	return s.ExecuteCloudcatBalanceFulfillment(ctx, o.ID)
}

func isCloudcatOrder(o *dbent.PaymentOrder) bool {
	return o != nil && o.ProviderKey != nil && strings.EqualFold(strings.TrimSpace(*o.ProviderKey), payment.TypeCloudcat)
}

func (s *PaymentService) markCloudcatPaymentFailed(ctx context.Context, o *dbent.PaymentOrder) {
	if o == nil || o.Status != OrderStatusPending {
		return
	}
	now := time.Now()
	updated, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(OrderStatusPending),
	).SetStatus(OrderStatusFailed).
		SetFailedAt(now).
		SetFailedReason("upstream payment failed").
		Save(ctx)
	if err != nil {
		slog.Warn("mark cloudcat payment failed", "orderID", o.ID, "error", err)
		return
	}
	if updated > 0 {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_FAILED", payment.TypeCloudcat, map[string]any{"reason": "upstream payment failed"})
	}
}

// ReconcilePendingPaymentOrders actively checks recent pending Alipay and WeChat
// orders so missed provider notifications do not wait until order expiry to fulfill.
func (s *PaymentService) ReconcilePendingPaymentOrders(ctx context.Context) (int, error) {
	now := time.Now()
	orders, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.ExpiresAtGT(now),
			paymentorder.Or(
				paymentorder.PaymentTypeEQ(payment.TypeWxpay),
				paymentorder.PaymentTypeHasPrefix(payment.TypeWxpay+"_"),
				paymentorder.ProviderKeyEQ(payment.TypeWxpay),
				paymentorder.ProviderKeyHasPrefix(payment.TypeWxpay+"_"),
				paymentorder.PaymentTypeEQ(payment.TypeAlipay),
				paymentorder.PaymentTypeHasPrefix(payment.TypeAlipay+"_"),
				paymentorder.ProviderKeyEQ(payment.TypeAlipay),
				paymentorder.ProviderKeyHasPrefix(payment.TypeAlipay+"_"),
			),
		).
		Order(dbent.Asc(paymentorder.FieldCreatedAt)).
		Limit(pendingPaymentReconcileLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query pending payment orders: %w", err)
	}

	recovered := 0
	for _, order := range orders {
		_, result, err := s.reconcilePaidWithCooldown(ctx, order)
		if err != nil {
			return recovered, err
		}
		if result == checkPaidResultAlreadyPaid {
			recovered++
		}
	}
	return recovered, nil
}

// ReconcileCloudcatOrders checks both unpaid orders and paid orders waiting for
// card delivery. Active checkout pages also trigger this through VerifyOrder.
func (s *PaymentService) ReconcileCloudcatOrders(ctx context.Context) (int, error) {
	orders, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.ProviderKeyEQ(payment.TypeCloudcat),
			paymentorder.PaymentTradeNoNEQ(""),
			paymentorder.Or(
				paymentorder.StatusIn(OrderStatusPending, OrderStatusPaid),
				paymentorder.And(paymentorder.StatusEQ(OrderStatusRecharging), paymentorder.UpdatedAtLTE(time.Now().Add(-paymentFulfillmentLeaseDuration))),
				paymentorder.And(
					paymentorder.StatusIn(OrderStatusExpired, OrderStatusCancelled),
					paymentorder.ExpiresAtGTE(time.Now().Add(-cloudcatRecoveryWindow)),
				),
			),
		).
		Order(dbent.Asc(paymentorder.FieldUpdatedAt), dbent.Asc(paymentorder.FieldID)).
		Limit(pendingCloudcatReconcileLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query cloudcat orders for reconciliation: %w", err)
	}

	reconciled := 0
	for _, order := range orders {
		if err := ctx.Err(); err != nil {
			return reconciled, err
		}
		// Rotate attempted orders so a slow or undelivered order cannot starve
		// the rest of the queue across the bounded reconciliation cycles.
		if order.Status != OrderStatusRecharging {
			if _, err := s.entClient.PaymentOrder.Update().Where(
				paymentorder.IDEQ(order.ID), paymentorder.StatusEQ(order.Status),
			).SetUpdatedAt(time.Now()).Save(ctx); err != nil {
				return reconciled, err
			}
		}
		_, result, err := s.reconcilePaidWithCooldown(ctx, order)
		if err != nil {
			return reconciled, err
		}
		if result == checkPaidResultAlreadyPaid {
			reconciled++
		}
	}
	return reconciled, nil
}

// VerifyOrderPublic returns the currently persisted public order state without
// triggering any upstream reconciliation. Signed resume-token recovery is the
// only public recovery path allowed to query upstream state.
func (s *PaymentService) VerifyOrderPublic(ctx context.Context, outTradeNo string) (*dbent.PaymentOrder, error) {
	outTradeNo, err := normalizeOrderLookupOutTradeNo(outTradeNo)
	if err != nil {
		return nil, err
	}
	o, err := s.entClient.PaymentOrder.Query().
		Where(paymentorder.OutTradeNo(outTradeNo)).
		Only(ctx)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	return o, nil
}

func normalizeOrderLookupOutTradeNo(raw string) (string, error) {
	outTradeNo := strings.TrimSpace(raw)
	if outTradeNo == "" {
		return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is required")
	}
	if len(outTradeNo) > 64 {
		return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is invalid")
	}
	for _, ch := range outTradeNo {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '_' || ch == '-':
		default:
			return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is invalid")
		}
	}
	return outTradeNo, nil
}

func (s *PaymentService) ExpireTimedOutOrders(ctx context.Context) (int, error) {
	now := time.Now()
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.StatusEQ(OrderStatusPending), paymentorder.ExpiresAtLTE(now)).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query expired: %w", err)
	}
	n := 0
	for _, o := range orders {
		// Check upstream payment status before expiring — the user may have
		// paid just before timeout and the webhook hasn't arrived yet.
		outcome, _ := s.cancelCore(ctx, o, OrderStatusExpired, "system", "order expired")
		if outcome == checkPaidResultAlreadyPaid {
			slog.Info("order was paid during expiry", "orderID", o.ID)
			continue
		}
		if outcome != "" {
			n++
		}
	}
	return n, nil
}

// getOrderProvider creates a provider using the order's original instance config.
// Falls back to registry lookup if instance ID is missing (legacy orders).
func (s *PaymentService) getOrderProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("load order provider instance: %w", err)
	}
	if inst != nil {
		return s.createProviderFromInstance(ctx, inst)
	}
	if !paymentOrderAllowsRegistryFallback(o) {
		return nil, fmt.Errorf("order %d provider instance is unresolved", o.ID)
	}
	providerKey := paymentOrderFallbackProviderKey(s.registry, o)
	if providerKey == "" {
		return nil, fmt.Errorf("order %d provider fallback key is missing", o.ID)
	}
	if !s.webhookRegistryFallbackAllowed(ctx, providerKey) {
		return nil, fmt.Errorf("order %d provider fallback is ambiguous for %s", o.ID, providerKey)
	}
	s.EnsureProviders(ctx)
	return s.registry.GetProvider(o.PaymentType)
}

func paymentOrderAllowsRegistryFallback(order *dbent.PaymentOrder) bool {
	if order == nil {
		return false
	}
	if psOrderProviderSnapshot(order) != nil {
		return false
	}
	if strings.TrimSpace(psStringValue(order.ProviderInstanceID)) != "" {
		return false
	}
	if strings.TrimSpace(psStringValue(order.ProviderKey)) != "" {
		return false
	}
	return true
}

func paymentOrderFallbackProviderKey(registry *payment.Registry, order *dbent.PaymentOrder) string {
	if order == nil {
		return ""
	}
	if registry != nil {
		if key := strings.TrimSpace(registry.GetProviderKey(payment.PaymentType(order.PaymentType))); key != "" {
			return key
		}
	}
	return strings.TrimSpace(payment.GetBasePaymentType(strings.TrimSpace(order.PaymentType)))
}

func (s *PaymentService) createProviderFromInstance(ctx context.Context, inst *dbent.PaymentProviderInstance) (payment.Provider, error) {
	if inst == nil {
		return nil, fmt.Errorf("payment provider instance is missing")
	}

	cfg, err := s.loadBalancer.GetInstanceConfig(ctx, int64(inst.ID))
	if err != nil {
		return nil, fmt.Errorf("load provider instance config: %w", err)
	}
	if inst.PaymentMode != "" {
		cfg["paymentMode"] = inst.PaymentMode
	}

	instID := strconv.FormatInt(int64(inst.ID), 10)
	prov, err := createPaymentProviderFromInstance(inst.ProviderKey, instID, cfg)
	if err != nil {
		return nil, fmt.Errorf("create provider from instance: %w", err)
	}
	return prov, nil
}

func psStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
