package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func expireBPSRateLimitForProbe(t *testing.T, s *OpenAIGatewayService, a *Account, model string) {
	t.Helper()
	state := bpsRateLimitState(t, s, a, model)
	state.until = time.Now().Add(-time.Second)
	s.excelBPSModelRateLimits.Store(excelBPSRateLimitKey{a.ID, model}, state)
	s.excelBPSModelCooldowns.entries[excelBPSModelKey{a.ID, model}].nextProbeAt = state.until
}

func TestBPSRateLimitRecoveryRequiresSuccessfulProbe(t *testing.T) {
	s, a, _ := modelRecoveryTestService()
	model := "gpt-6-astra"
	a.Credentials["model_mapping"] = map[string]any{"alias": model}
	u := &bpsProbeUpstream{mode: "failed_terminal"}
	s.httpUpstream = u
	s.coolDownExcelBPSAttempt(t.Context(), a, model, nil, nil, &basispoints.UpstreamFailure{Status: 429, Code: "rate_limit_exceeded"}, 200)
	s.syncExcelBPSModelRecovery(t.Context())
	require.Empty(t, u.bodies)
	expireBPSRateLimitForProbe(t, s, a, model)
	require.True(t, s.isExcelBPSCoolingDown(t.Context(), a, "alias"))
	s.syncExcelBPSModelRecovery(t.Context())
	require.Len(t, u.bodies, 1)
	require.True(t, s.isExcelBPSCoolingDown(t.Context(), a, "alias"))
	s.syncExcelBPSModelRecovery(t.Context())
	require.Len(t, u.bodies, 1, "failed checks wait for the configured interval")
	u = &bpsProbeUpstream{}
	s.httpUpstream = u
	expireBPSRateLimitForProbe(t, s, a, model)
	s.syncExcelBPSModelRecovery(t.Context())
	require.Len(t, u.bodies, 1)
	require.Equal(t, model, gjson.GetBytes(u.bodies[0], "model").String())
	require.False(t, s.isExcelBPSCoolingDown(t.Context(), a, "alias"))
}

func TestBPSRateLimitRecoveryHonorsSwitchAndManualPause(t *testing.T) {
	for _, mode := range []string{"switch_off", "paused", "native_route"} {
		t.Run(mode, func(t *testing.T) {
			s, a, _ := modelRecoveryTestService()
			u := &bpsProbeUpstream{}
			s.httpUpstream = u
			s.coolDownExcelBPSAttempt(t.Context(), a, "gpt-6-astra", nil, nil, nil, 429)
			expireBPSRateLimitForProbe(t, s, a, "gpt-6-astra")
			switch mode {
			case "switch_off":
				s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSRecoveryEnabled] = "false"
				require.False(t, s.isExcelBPSCoolingDown(t.Context(), a, "gpt-6-astra"))
			case "paused":
				a.Schedulable = false
			case "native_route":
				ctx := context.WithValue(t.Context(), excelBPSSchedulingKey{}, false)
				require.False(t, s.isExcelBPSCoolingDown(ctx, a, "gpt-6-astra"))
				a.Extra["openai_excel_bps"] = false
			}
			s.syncExcelBPSModelRecovery(t.Context())
			require.Empty(t, u.bodies)
			require.Len(t, s.excelBPSModelCooldowns.entries, 1, "never auto-resume a manually paused account")
		})
	}
}

func TestBPSRecoveryTagDoesNotAffectNativeOrDisabledRequests(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelAccount()
	for _, enabled := range []bool{false, true} {
		for _, bps := range []bool{false, true} {
			a.Extra["openai_excel_bps"] = bps
			ctx := context.WithValue(t.Context(), excelBPSRecoveryKey{}, enabled)
			f := &UpstreamFailoverError{StatusCode: 403}
			s.limitExcelBPS429Failover(ctx, nil, a, "gpt-6-astra", f)
			_, _, ok := f.ExcelBPSRecoveryClientResponse()
			require.Equal(t, enabled && bps, ok)
		}
	}
}

func TestBPSStreamDiagnosticsPreserveRecoveryBoundary(t *testing.T) {
	for _, visible := range []bool{false, true} {
		wire := bpsFailureWire(t, "response.failed", "UNRECOGNIZED_PRIVATE", "api_error", 0)
		if visible {
			wire = "data: " + `{"type":"response.output_text.delta","delta":"VISIBLE"}` + "\n\n" + wire
		}
		s := openAIExcelBPSRecoveryTestService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, wire)})
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := s.Forward(t.Context(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`))
		require.Error(t, err)
		value, exists := c.Get(OpsUpstreamErrorsKey)
		require.True(t, exists)
		events := value.([]*OpsUpstreamErrorEvent)
		found := false
		for _, event := range events {
			if event.Kind != "recovery_decision" {
				continue
			}
			found = true
			require.NotContains(t, event.Detail, "PRIVATE")
			require.Equal(t, visible, gjson.Get(event.Detail, "client_output_started").Bool())
			require.Equal(t, "api_error", gjson.Get(event.Detail, "source_type").String())
			require.Equal(t, "unrecognized", gjson.Get(event.Detail, "source_code").String())
			if visible {
				require.Equal(t, "output_or_request_committed", event.Reason)
			} else {
				require.Equal(t, "retry_before_output", event.Reason)
			}
		}
		require.True(t, found)
	}
}

func TestBPSRecoveryCancellationDoesNotPenalizeAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	u := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		require.NoError(t, req.Body.Close())
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &bpsCancelBody{cancel: cancel}}, nil
	}}
	s := openAIExcelBPSRecoveryTestService(nil)
	s.httpUpstream = u
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	result, err := s.Forward(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`))
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	_, exists := c.Get(OpsUpstreamErrorsKey)
	require.False(t, exists, "diagnostics must not turn client cancellation into an account failure")
}
