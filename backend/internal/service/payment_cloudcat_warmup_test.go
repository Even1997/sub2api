package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentCloudcatWarmupFollowsEnabledConfiguration(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer proxy.Close()
	t.Setenv("CLOUDCAT_PROXY_URL", proxy.URL)
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	client := newPaymentConfigServiceTestClient(t)
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	paymentSvc := &PaymentService{entClient: client, configService: &PaymentConfigService{settingRepo: repo}}
	s := NewPaymentOrderExpiryService(paymentSvc, time.Minute)
	var stop func()
	defer func() {
		s.Stop()
		if stop != nil {
			stop()
		}
	}()
	s.syncCloudcatWarmup(&stop)
	require.Nil(t, stop, "no enabled cloudcat instance must mean no background probes")
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeCloudcat).
		SetName("warmup-test").SetConfig("{}").SetSupportedTypes(payment.TypeAlipay).SetEnabled(false).Save(context.Background())
	require.NoError(t, err)
	s.syncCloudcatWarmup(&stop)
	require.Nil(t, stop, "disabled cloudcat instances must not start warmup")
	_, err = inst.Update().SetEnabled(true).Save(context.Background())
	require.NoError(t, err)
	s.syncCloudcatWarmup(&stop)
	require.NotNil(t, stop, "enabled cloudcat instance must start warmup before a payment arrives")
	s.syncCloudcatWarmup(&stop)
	require.NotNil(t, stop, "configuration refresh must retain the running pool")
	_, err = inst.Update().SetEnabled(false).Save(context.Background())
	require.NoError(t, err)
	s.syncCloudcatWarmup(&stop)
	require.Nil(t, stop, "disabling the last cloudcat instance must stop its background probes")
	_, err = inst.Update().SetEnabled(true).Save(context.Background())
	require.NoError(t, err)
	s.syncCloudcatWarmup(&stop)
	require.NotNil(t, stop)
	repo.values[SettingPaymentEnabled] = "false"
	s.syncCloudcatWarmup(&stop)
	require.Nil(t, stop, "disabling payments must stop the pool")
}
