package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *PaymentService) UpdateOrderRemark(ctx context.Context, orderID, userID int64, remark string) error {
	if !utf8.ValidString(remark) || utf8.RuneCountInString(remark) > 500 || strings.ContainsRune(remark, 0) {
		return infraerrors.BadRequest("INVALID_ORDER_REMARK", "Order remark must contain at most 500 characters and no null bytes")
	}
	// A personal remark must not reset updated_at, which also tracks payment
	// fulfillment leases and reconciliation attempts.
	result, err := s.entClient.ExecContext(ctx,
		"UPDATE payment_orders SET remark = $1 WHERE id = $2 AND user_id = $3", remark, orderID, userID)
	if err != nil {
		return fmt.Errorf("update order remark: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated order remarks: %w", err)
	}
	if updated == 0 {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	return nil
}

// ExportUserOrders uses an exclusive end instant so whole local days also work
// across time zones and daylight saving changes. Never silently truncate exports.
func (s *PaymentService) ExportUserOrders(ctx context.Context, userID int64, start, end time.Time, status string) ([]*dbent.PaymentOrder, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, infraerrors.BadRequest("INVALID_ORDER_EXPORT_RANGE", "Invalid export time range")
	}
	q := s.entClient.PaymentOrder.Query().Where(
		paymentorder.UserIDEQ(userID), paymentorder.CreatedAtGTE(start.UTC()), paymentorder.CreatedAtLT(end.UTC()),
	)
	if status != "" {
		q.Where(paymentorder.StatusEQ(status))
	}
	orders, err := q.Order(dbent.Desc(paymentorder.FieldCreatedAt), dbent.Desc(paymentorder.FieldID)).Limit(10001).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("export user orders: %w", err)
	}
	if len(orders) > 10000 {
		return nil, infraerrors.BadRequest("ORDER_EXPORT_TOO_LARGE", "More than 10,000 orders; select a shorter time range")
	}
	return orders, nil
}
