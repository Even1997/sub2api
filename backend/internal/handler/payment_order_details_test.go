//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestOrderRemarksAndExport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:order_details?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	owner, err := client.User.Create().SetEmail("order-owner@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	other, err := client.User.Create().SetEmail("order-other@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("local", 8*3600))
	end := start.AddDate(0, 0, 1)
	create := func(userID int64, created time.Time, status string) *dbent.PaymentOrder {
		o, err := client.PaymentOrder.Create().SetUserID(userID).SetUserEmail("private@example.com").SetUserName("owner").
			SetUserNotes("private admin note").SetAmount(10).SetPayAmount(10).SetRechargeCode("private-code").
			SetOutTradeNo(fmt.Sprintf("order-%d-%d", userID, created.UnixNano())).SetPaymentType(payment.TypeAlipay).
			SetPaymentTradeNo("").SetExpiresAt(end.UTC()).SetCreatedAt(created.UTC()).SetStatus(status).SetClientIP("").SetSrcHost("").Save(ctx)
		require.NoError(t, err)
		return o
	}
	order := create(owner.ID, start, service.OrderStatusCompleted)
	for i := 1; i < 25; i++ {
		create(owner.ID, start.Add(time.Duration(i)*time.Minute), service.OrderStatusCompleted)
	}
	create(owner.ID, start.Add(-time.Second), service.OrderStatusCompleted)
	create(owner.ID, end.Add(-time.Nanosecond), service.OrderStatusCompleted)
	create(owner.ID, end, service.OrderStatusCompleted)
	create(owner.ID, start.Add(time.Hour), service.OrderStatusPending)
	create(other.ID, start, service.OrderStatusCompleted)
	h := NewPaymentHandler(service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, nil, nil, nil, nil), nil)
	request := func(userID int64, method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(order.ID)}}
		if userID > 0 {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
		}
		handler(c)
		return recorder
	}

	for _, tc := range []struct {
		name, remark string
		userID       int64
		want         int
	}{
		{"anonymous", "secret", 0, 401}, {"other owner", "secret", other.ID, 404},
		{"save chinese", strings.Repeat("备", 500), owner.ID, 200},
		{"too long", strings.Repeat("备", 501), owner.ID, 400},
		{"null byte", "bad\x00note", owner.ID, 400}, {"clear", "", owner.ID, 200},
		{"save multiline", "财务\n\"十月\", =SUM(A1)", owner.ID, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := client.PaymentOrder.GetX(ctx, order.ID).Remark
			body, err := json.Marshal(map[string]string{"remark": tc.remark})
			require.NoError(t, err)
			r := request(tc.userID, "PATCH", "/remark", string(body), h.UpdateOrderRemark)
			require.Equal(t, tc.want, r.Code, r.Body.String())
			stored := client.PaymentOrder.GetX(ctx, order.ID)
			if tc.want == 200 {
				require.Equal(t, tc.remark, stored.Remark)
			} else {
				require.Equal(t, before, stored.Remark)
			}
			require.Equal(t, service.OrderStatusCompleted, stored.Status)
			require.True(t, stored.UpdatedAt.Equal(order.UpdatedAt), "remarks must not reset the payment lease")
			require.Equal(t, "private admin note", *stored.UserNotes)
		})
	}
	for _, body := range []string{"{}", `{"remark":null}`, `{"remark":123}`} {
		require.Equal(t, 400, request(owner.ID, "PATCH", "/remark", body, h.UpdateOrderRemark).Code)
	}
	query := url.Values{"start_time": {start.Format(time.RFC3339Nano)}, "end_time": {end.Format(time.RFC3339Nano)}, "status": {service.OrderStatusCompleted}}
	r := request(owner.ID, "GET", "/export?"+query.Encode(), "", h.ExportMyOrders)
	require.Equal(t, 200, r.Code, r.Body.String())
	var result struct {
		Data []PaymentOrderResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
	require.Len(t, result.Data, 26)
	for _, item := range result.Data {
		require.Equal(t, owner.ID, item.UserID)
		require.False(t, item.CreatedAt.Before(start))
		require.True(t, item.CreatedAt.Before(end))
	}
	require.Equal(t, "财务\n\"十月\", =SUM(A1)", result.Data[len(result.Data)-1].Remark)
	require.NotContains(t, r.Body.String(), "private")
	require.Equal(t, 401, request(0, "GET", "/export?"+query.Encode(), "", h.ExportMyOrders).Code)
	query.Set("end_time", start.Format(time.RFC3339))
	require.Equal(t, 400, request(owner.ID, "GET", "/export?"+query.Encode(), "", h.ExportMyOrders).Code)
	query.Set("start_time", "invalid")
	require.Equal(t, 400, request(owner.ID, "GET", "/export?"+query.Encode(), "", h.ExportMyOrders).Code)
	_, err = db.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<10000)
		INSERT INTO payment_orders (user_id, user_email, user_name, amount, pay_amount, recharge_code, out_trade_no,
		payment_type, payment_trade_no, status, expires_at, client_ip, src_host, created_at, updated_at)
		SELECT user_id, user_email, user_name, amount, pay_amount, recharge_code, 'bulk-' || n,
		payment_type, payment_trade_no, status, expires_at, client_ip, src_host, created_at, updated_at
		FROM payment_orders, seq WHERE id = ?`, order.ID)
	require.NoError(t, err)
	query.Set("start_time", start.Format(time.RFC3339))
	query.Set("end_time", end.Format(time.RFC3339))
	r = request(owner.ID, "GET", "/export?"+query.Encode(), "", h.ExportMyOrders)
	require.Equal(t, 400, r.Code)
	require.Contains(t, r.Body.String(), "ORDER_EXPORT_TOO_LARGE")
}
