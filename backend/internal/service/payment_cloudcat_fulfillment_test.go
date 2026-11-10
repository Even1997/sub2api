//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type cloudcatFulfillmentFixture struct {
	client       *dbent.Client
	order        *dbent.PaymentOrder
	redeemRepo   *paymentOrderLifecycleRedeemRepo
	userRepo     *mockUserRepo
	balanceCalls int
	service      *PaymentService
}

func newCloudcatFulfillmentFixture(t *testing.T, code *RedeemCode) *cloudcatFulfillmentFixture {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	user, err := client.User.Create().
		SetEmail("cloudcat-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com").
		SetPasswordHash("hash").
		SetUsername("cloudcat-user").
		Save(ctx)
	require.NoError(t, err)

	cardCode := "CC-" + strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 10)
	providerKey := payment.TypeCloudcat
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(5).
		SetPayAmount(5).
		SetFeeRate(0).
		SetRechargeCode(cardCode).
		SetOutTradeNo("cloudcat_" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("CAT-TEST").
		SetOrderType(payment.OrderTypeBalance).
		SetProviderKey(providerKey).
		SetStatus(OrderStatusPaid).
		SetPaidAt(time.Now()).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("localhost").
		Save(ctx)
	require.NoError(t, err)

	redeemRepo := &paymentOrderLifecycleRedeemRepo{codesByCode: map[string]*RedeemCode{}}
	if code != nil {
		cloned := *code
		cloned.Code = cardCode
		if cloned.ID == 0 {
			cloned.ID = 1
		}
		redeemRepo.codesByCode[cardCode] = &cloned
	}
	userRepo := &mockUserRepo{getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username}}
	fixture := &cloudcatFulfillmentFixture{client: client, order: order, redeemRepo: redeemRepo, userRepo: userRepo}
	userRepo.updateBalanceFn = func(_ context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		require.Equal(t, order.Amount, amount)
		fixture.balanceCalls++
		return nil
	}
	redeemService := &RedeemService{redeemRepo: redeemRepo, userRepo: userRepo, entClient: client}
	fixture.service = &PaymentService{entClient: client, redeemService: redeemService}
	return fixture
}

func TestCloudcatBalanceFulfillmentRedeemsExistingCardWithoutLeakingItToAudit(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused})
	ctx := context.Background()

	require.NoError(t, fixture.service.ExecuteBalanceFulfillment(ctx, fixture.order.ID))
	require.Equal(t, 1, fixture.balanceCalls)
	require.Len(t, fixture.redeemRepo.useCalls, 1)

	reloaded, err := fixture.client.PaymentOrder.Get(ctx, fixture.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)

	logs, err := fixture.client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(fixture.order.ID, 10))).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	for _, entry := range logs {
		require.NotContains(t, entry.Detail, fixture.order.RechargeCode)
	}
	claim, err := fixture.client.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(fixture.order.ID, 10)),
		paymentauditlog.ActionEQ(cloudcatCDKClaimedAuditAction),
	).Only(ctx)
	require.NoError(t, err)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(claim.Detail), &detail))
	require.Equal(t, cloudcatCodeFingerprint(fixture.order.RechargeCode), detail["codeFingerprint"])
}

func TestCloudcatBalanceFulfillmentRejectsInvalidInventoryCards(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	tests := []struct {
		name string
		code *RedeemCode
	}{
		{name: "missing local card"},
		{name: "wrong type", code: &RedeemCode{Type: RedeemTypeSubscription, Value: 5, Status: StatusUnused}},
		{name: "wrong value", code: &RedeemCode{Type: RedeemTypeBalance, Value: 6, Status: StatusUnused}},
		{name: "expired", code: &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused, ExpiresAt: &expired}},
		{name: "disabled", code: &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusDisabled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newCloudcatFulfillmentFixture(t, tt.code)
			err := fixture.service.ExecuteBalanceFulfillment(context.Background(), fixture.order.ID)
			require.Error(t, err)
			require.Zero(t, fixture.balanceCalls)
			require.Empty(t, fixture.redeemRepo.useCalls)
			reloaded, getErr := fixture.client.PaymentOrder.Get(context.Background(), fixture.order.ID)
			require.NoError(t, getErr)
			require.Equal(t, OrderStatusPaid, reloaded.Status)
		})
	}
}

func TestCloudcatBalanceFulfillmentRejectsCardBoundToAnotherOrder(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused})
	ctx := context.Background()
	providerKey := payment.TypeCloudcat
	_, err := fixture.client.PaymentOrder.Create().
		SetUserID(fixture.order.UserID).
		SetUserEmail(fixture.order.UserEmail).
		SetUserName(fixture.order.UserName).
		SetAmount(5).
		SetPayAmount(5).
		SetFeeRate(0).
		SetRechargeCode(fixture.order.RechargeCode).
		SetOutTradeNo("cloudcat_duplicate_" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("CAT-DUPLICATE").
		SetOrderType(payment.OrderTypeBalance).
		SetProviderKey(providerKey).
		SetStatus(OrderStatusPaid).
		SetPaidAt(time.Now()).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("localhost").
		Save(ctx)
	require.NoError(t, err)

	require.Error(t, fixture.service.ExecuteCloudcatBalanceFulfillment(ctx, fixture.order.ID))
	require.Zero(t, fixture.balanceCalls)
	reloaded, err := fixture.client.PaymentOrder.Get(ctx, fixture.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
}

func TestCloudcatUsedCardRequiresMatchingOrderAuditEvidence(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUsed})
	usedBy := fixture.order.UserID
	fixture.redeemRepo.codesByCode[fixture.order.RechargeCode].UsedBy = &usedBy

	err := fixture.service.ExecuteCloudcatBalanceFulfillment(context.Background(), fixture.order.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit evidence")
	require.Zero(t, fixture.balanceCalls)
	reloaded, getErr := fixture.client.PaymentOrder.Get(context.Background(), fixture.order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
}

func TestCloudcatUsedCardWithMatchingAuditCompletesWithoutCreditingAgain(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUsed})
	usedBy := fixture.order.UserID
	fixture.redeemRepo.codesByCode[fixture.order.RechargeCode].UsedBy = &usedBy
	ctx := context.Background()
	require.NoError(t, fixture.service.claimCloudcatCDK(ctx, fixture.order, cloudcatCodeFingerprint(fixture.order.RechargeCode)))

	require.NoError(t, fixture.service.ExecuteCloudcatBalanceFulfillment(ctx, fixture.order.ID))
	require.Zero(t, fixture.balanceCalls)
	require.Empty(t, fixture.redeemRepo.useCalls)
	reloaded, err := fixture.client.PaymentOrder.Get(ctx, fixture.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
}

func TestCloudcatOrderCreationLeavesRechargeCodeEmptyUntilCardDelivery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	userEntity, err := client.User.Create().
		SetEmail("cloudcat-create@example.com").
		SetPasswordHash("hash").
		SetUsername("cloudcat-create-user").
		Save(ctx)
	require.NoError(t, err)
	service := &PaymentService{entClient: client}
	selection := &payment.InstanceSelection{ProviderKey: payment.TypeCloudcat, InstanceID: "cloudcat-main"}
	order, err := service.createOrderInTx(
		ctx,
		CreateOrderRequest{UserID: userEntity.ID, Amount: 5, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeBalance},
		&User{ID: userEntity.ID, Email: userEntity.Email, Username: userEntity.Username},
		nil,
		&PaymentConfig{OrderTimeoutMin: 15},
		5,
		5,
		0,
		5,
		0,
		selection,
	)
	require.NoError(t, err)
	require.Empty(t, order.RechargeCode)
	require.NotNil(t, order.ProviderKey)
	require.True(t, strings.EqualFold(payment.TypeCloudcat, *order.ProviderKey))
}

func TestCloudcatSelectedAmountMustMatchConfiguredProduct(t *testing.T) {
	selection := &payment.InstanceSelection{
		ProviderKey: payment.TypeCloudcat,
		Config: map[string]string{
			"products": `[{"amount":"1.00","itemUrl":"https://catfk.com/item/one"}]`,
		},
	}

	require.NoError(t, validateSelectedCreateOrderAmountCurrency("1.00", selection))
	err := validateSelectedCreateOrderAmountCurrency("2.00", selection)
	require.Error(t, err)
	require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
}

func TestCloudcatPaidQueryRejectsAmountAndChannelMismatchBeforeCDKFulfillment(t *testing.T) {
	tests := []struct {
		name        string
		amount      float64
		metadata    map[string]string
		auditAction string
	}{
		{
			name:        "amount mismatch",
			amount:      6,
			metadata:    map[string]string{"channel_id": "1", payment.MetadataCloudcatRechargeCode: "UNTRUSTED-CDK"},
			auditAction: "PAYMENT_AMOUNT_MISMATCH",
		},
		{
			name:        "channel mismatch",
			amount:      5,
			metadata:    map[string]string{"channel_id": "2", payment.MetadataCloudcatRechargeCode: "UNTRUSTED-CDK"},
			auditAction: "PAYMENT_PROVIDER_METADATA_MISMATCH",
		},
		{
			name:        "channel missing",
			amount:      5,
			metadata:    map[string]string{},
			auditAction: "PAYMENT_PROVIDER_METADATA_MISMATCH",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newCloudcatFulfillmentFixture(t, nil)
			ctx := context.Background()
			order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
				SetStatus(OrderStatusPending).
				SetRechargeCode("").
				SetProviderSnapshot(map[string]any{
					"schema_version": 1,
					"provider_key":   payment.TypeCloudcat,
					"merchant_id":    "1",
				}).
				Save(ctx)
			require.NoError(t, err)

			err = fixture.service.handleCloudcatPaidQuery(ctx, order, &payment.QueryOrderResponse{
				TradeNo:  "CAT-MISMATCH",
				Status:   payment.ProviderStatusPaid,
				Amount:   tt.amount,
				Metadata: tt.metadata,
			})
			require.Error(t, err)
			require.Zero(t, fixture.balanceCalls)
			require.Empty(t, fixture.redeemRepo.useCalls)

			reloaded, getErr := fixture.client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, getErr)
			require.Equal(t, OrderStatusPending, reloaded.Status)
			require.Empty(t, reloaded.RechargeCode)

			exists, auditErr := fixture.client.PaymentAuditLog.Query().Where(
				paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
				paymentauditlog.ActionEQ(tt.auditAction),
			).Exist(ctx)
			require.NoError(t, auditErr)
			require.True(t, exists)
		})
	}
}

func TestCloudcatPendingOrderCanResumeOnlyForOwnerBeforeExpiry(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	payURL := "https://catfk.com/pay/CAT-TEST"
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPending).
		SetPayURL(payURL).
		Save(ctx)
	require.NoError(t, err)

	resumed, err := fixture.service.ResumeCloudcatOrder(ctx, order.ID, order.UserID)
	require.NoError(t, err)
	require.NotNil(t, resumed.PayURL)
	require.Equal(t, payURL, *resumed.PayURL)
	require.Equal(t, order.ID, resumed.ID)

	_, err = fixture.service.ResumeCloudcatOrder(ctx, order.ID, order.UserID+1)
	require.Error(t, err)
}

func TestCloudcatResumeReturnsFreshQRCode(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	inst, err := fixture.client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeCloudcat).
		SetName("cloudcat-resume").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeAlipay).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	payURL := "https://catfk.com/pay/CAT-TEST"
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPending).
		SetPayURL(payURL).
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)

	provider := &cloudcatResumeQRProvider{
		paymentOrderLifecycleQueryProvider: &paymentOrderLifecycleQueryProvider{key: payment.TypeCloudcat},
		qrCode:                             "https://qr.alipay.com/cloudcat-resume",
	}
	fixture.service.loadBalancer = &captureLoadBalancer{}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))

	resumed, err := fixture.service.ResumeCloudcatOrder(ctx, order.ID, order.UserID)
	require.NoError(t, err)
	require.NotNil(t, resumed.QrCode)
	require.Equal(t, provider.qrCode, *resumed.QrCode)
	require.Equal(t, payURL, provider.payURL)
}

func TestCloudcatWechatResumeKeepsHostedCheckoutWithoutResolvingQRCode(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	payURL := "https://catfk.com/pay/CAT-WX-TEST"
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPending).
		SetPaymentType(payment.TypeWxpay).
		SetPayURL(payURL).
		Save(ctx)
	require.NoError(t, err)

	provider := &cloudcatResumeQRProvider{
		paymentOrderLifecycleQueryProvider: &paymentOrderLifecycleQueryProvider{key: payment.TypeCloudcat},
		qrCode:                             "https://qr.alipay.com/should-not-be-used",
	}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))

	resumed, err := fixture.service.ResumeCloudcatOrder(ctx, order.ID, order.UserID)
	require.NoError(t, err)
	require.Nil(t, resumed.QrCode)
	require.NotNil(t, resumed.PayURL)
	require.Equal(t, payURL, *resumed.PayURL)
	require.Empty(t, provider.payURL)
}

type cloudcatResumeQRProvider struct {
	*paymentOrderLifecycleQueryProvider
	qrCode string
	payURL string
}

func (p *cloudcatResumeQRProvider) ResolvePaymentQRCode(_ context.Context, payURL string) (string, error) {
	p.payURL = payURL
	return p.qrCode, nil
}

func TestCloudcatResumeRejectsExpiredCompletedAndUntrustedOrders(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		expiresAt time.Time
		payURL    string
	}{
		{name: "expired", status: OrderStatusPending, expiresAt: time.Now().Add(-time.Minute), payURL: "https://catfk.com/pay/CAT-TEST"},
		{name: "completed", status: OrderStatusCompleted, expiresAt: time.Now().Add(time.Hour), payURL: "https://catfk.com/pay/CAT-TEST"},
		{name: "untrusted URL", status: OrderStatusPending, expiresAt: time.Now().Add(time.Hour), payURL: "https://evil.example/pay/CAT-TEST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newCloudcatFulfillmentFixture(t, nil)
			ctx := context.Background()
			order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
				SetStatus(tt.status).
				SetExpiresAt(tt.expiresAt).
				SetPayURL(tt.payURL).
				Save(ctx)
			require.NoError(t, err)

			_, err = fixture.service.ResumeCloudcatOrder(ctx, order.ID, order.UserID)
			require.Error(t, err)
		})
	}
}

func TestCloudcatPendingOrderCanBeClosedLocally(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPending).
		SetPayURL("https://catfk.com/pay/CAT-TEST").
		Save(ctx)
	require.NoError(t, err)

	outcome, err := fixture.service.CancelOrder(ctx, order.ID, order.UserID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	reloaded, getErr := fixture.client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)

	logEntry, logErr := fixture.client.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
		paymentauditlog.ActionEQ("ORDER_CANCELLED"),
	).Only(ctx)
	require.NoError(t, logErr)
	require.Contains(t, logEntry.Detail, `"scope":"local_only"`)
	require.Contains(t, logEntry.Detail, `"upstream_cancelled":false`)
}

func TestCloudcatCloseReportsPaymentThatWonTheStatusRace(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	stalePending, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPending).
		SetRechargeCode("").
		Save(ctx)
	require.NoError(t, err)
	_, err = fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusPaid).
		Save(ctx)
	require.NoError(t, err)

	outcome, err := fixture.service.cancelCore(ctx, stalePending, OrderStatusCancelled, "user:test", "user closed order locally")
	require.NoError(t, err)
	require.Equal(t, checkPaidResultAlreadyPaid, outcome)
}

func TestCloudcatClosedOrderRecoversLatePaidQueryExactlyOnce(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused})
	ctx := context.Background()
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusCancelled).
		SetRechargeCode("").
		Save(ctx)
	require.NoError(t, err)

	paid := &payment.QueryOrderResponse{
		TradeNo:  "CAT-LATE-PAID",
		Status:   payment.ProviderStatusPaid,
		Amount:   order.PayAmount,
		Metadata: map[string]string{payment.MetadataCloudcatRechargeCode: fixture.order.RechargeCode},
	}
	require.NoError(t, fixture.service.handleCloudcatPaidQuery(ctx, order, paid))
	require.NoError(t, fixture.service.handleCloudcatPaidQuery(ctx, order, paid))
	require.Equal(t, 1, fixture.balanceCalls)
	require.Len(t, fixture.redeemRepo.useCalls, 1)

	reloaded, getErr := fixture.client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
}

func TestCloudcatReconciliationRecoversClosedExpiredAndInterruptedOrders(t *testing.T) {
	for _, status := range []string{OrderStatusCancelled, OrderStatusExpired, OrderStatusPaid, OrderStatusRecharging} {
		t.Run(status, func(t *testing.T) {
			fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused})
			ctx := context.Background()
			inst, err := fixture.client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeCloudcat).SetName("reconcile").SetConfig("{}").SetSupportedTypes(payment.TypeAlipay).Save(ctx)
			require.NoError(t, err)
			_, err = fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
				SetStatus(status).SetExpiresAt(time.Now().Add(-time.Hour)).
				SetUpdatedAt(time.Now().Add(-time.Hour)).SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).Save(ctx)
			require.NoError(t, err)
			p := &paymentOrderLifecycleQueryProvider{key: payment.TypeCloudcat, resp: &payment.QueryOrderResponse{
				TradeNo: fixture.order.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 5,
				Metadata: map[string]string{payment.MetadataCloudcatRechargeCode: fixture.order.RechargeCode},
			}}
			fixture.service.loadBalancer = &captureLoadBalancer{}
			t.Cleanup(replacePaymentProviderFactoryForTest(t, p))
			count, err := fixture.service.ReconcileCloudcatOrders(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			reloaded, err := fixture.client.PaymentOrder.Get(ctx, fixture.order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCompleted, reloaded.Status)
			require.Equal(t, 1, fixture.balanceCalls)
			count, err = fixture.service.ReconcileCloudcatOrders(ctx)
			require.NoError(t, err)
			require.Zero(t, count)
			require.Equal(t, 1, fixture.balanceCalls)
		})
	}
}

func TestCloudcatClosedOrderRejectsLateWebhook(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	order, err := fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).
		SetStatus(OrderStatusCancelled).
		SetRechargeCode("").
		Save(ctx)
	require.NoError(t, err)

	err = fixture.service.toPaid(ctx, order, "CAT-LATE-WEBHOOK", order.PayAmount, payment.TypeCloudcat)
	require.NoError(t, err)
	require.Zero(t, fixture.balanceCalls)

	reloaded, getErr := fixture.client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}

func TestCloudcatReconciliationRotatesBeyondTheBatchLimit(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, nil)
	ctx := context.Background()
	old := time.Now().Add(-time.Hour)
	inst, err := fixture.client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeCloudcat).SetName("reconcile").SetConfig("{}").SetSupportedTypes(payment.TypeAlipay).Save(ctx)
	require.NoError(t, err)
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeCloudcat, resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}
	fixture.service.loadBalancer = &captureLoadBalancer{}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, p))
	var lastID int64
	for i := 0; i <= pendingCloudcatReconcileLimit; i++ {
		order, err := fixture.client.PaymentOrder.Create().SetUserID(fixture.order.UserID).
			SetUserEmail(fixture.order.UserEmail).SetUserName(fixture.order.UserName).SetClientIP("127.0.0.1").SetSrcHost("localhost").SetAmount(5).SetPayAmount(5).SetFeeRate(0).
			SetRechargeCode("").SetOutTradeNo("rotation-" + strconv.Itoa(i)).SetPaymentType(payment.TypeAlipay).
			SetPaymentTradeNo("CAT-" + strconv.Itoa(i)).SetOrderType(payment.OrderTypeBalance).
			SetProviderKey(payment.TypeCloudcat).SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
			SetStatus(OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).SetUpdatedAt(old).Save(ctx)
		require.NoError(t, err)
		lastID = order.ID
	}
	_, err = fixture.service.ReconcileCloudcatOrders(ctx)
	require.NoError(t, err)
	last, err := fixture.client.PaymentOrder.Get(ctx, lastID)
	require.NoError(t, err)
	require.WithinDuration(t, old, last.UpdatedAt, time.Millisecond)
	_, err = fixture.service.ReconcileCloudcatOrders(ctx)
	require.NoError(t, err)
	last, err = fixture.client.PaymentOrder.Get(ctx, lastID)
	require.NoError(t, err)
	require.True(t, last.UpdatedAt.After(old.Add(time.Minute)), "orders beyond the first batch must be attempted")
}

func TestCloudcatSignedResumeWaitsForCardAndCompletesExactlyOnce(t *testing.T) {
	fixture := newCloudcatFulfillmentFixture(t, &RedeemCode{Type: RedeemTypeBalance, Value: 5, Status: StatusUnused})
	ctx := context.Background()
	inst, err := fixture.client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeCloudcat).SetName("resume-delivery").SetConfig("{}").SetSupportedTypes(payment.TypeAlipay).Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(inst.ID, 10)
	_, err = fixture.client.PaymentOrder.UpdateOneID(fixture.order.ID).SetRechargeCode("").SetProviderInstanceID(instanceID).Save(ctx)
	require.NoError(t, err)
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeCloudcat, responses: []*payment.QueryOrderResponse{
		{TradeNo: fixture.order.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 5},
		{TradeNo: fixture.order.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 5, Metadata: map[string]string{payment.MetadataCloudcatRechargeCode: fixture.order.RechargeCode}},
	}}
	fixture.service.loadBalancer = &captureLoadBalancer{}
	fixture.service.resumeService = NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	t.Cleanup(replacePaymentProviderFactoryForTest(t, p))
	token, err := fixture.service.resumeService.CreateToken(ResumeTokenClaims{
		OrderID: fixture.order.ID, UserID: fixture.order.UserID, ProviderInstanceID: instanceID,
		ProviderKey: payment.TypeCloudcat, PaymentType: payment.TypeAlipay, CanonicalReturnURL: "https://app.example.com/payment/result",
	})
	require.NoError(t, err)
	order, err := fixture.service.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, order.Status)
	require.Zero(t, fixture.balanceCalls)
	order, err = fixture.service.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, order.Status)
	_, err = fixture.service.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, 2, p.queryCalls)
	require.Equal(t, 1, fixture.balanceCalls)
}
