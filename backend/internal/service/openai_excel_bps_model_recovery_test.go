package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type modelRecoveryTestRepo struct {
	AccountRepository
	account *Account
	err     error
	fetch   func(context.Context) (*Account, error)
	pauses  int
}

func (r *modelRecoveryTestRepo) GetByID(ctx context.Context, _ int64) (*Account, error) {
	if r.fetch != nil {
		return r.fetch(ctx)
	}
	return r.account, r.err
}
func (r *modelRecoveryTestRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.pauses++
	return nil
}

func modelRecoveryTestService() (*OpenAIGatewayService, *Account, *modelRecoveryTestRepo) {
	s := openAIExcelBPSRecoveryTestService(nil)
	a := excelAccount()
	repo := &modelRecoveryTestRepo{account: a}
	s.accountRepo = repo
	return s, a, repo
}

func dueModelRecovery(t *testing.T, s *OpenAIGatewayService, a *Account, model string) *excelBPSModelCooldown {
	t.Helper()
	s.coolDownExcelBPSModel(t.Context(), a, model)
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[excelBPSModelKey{a.ID, model}]
	require.NotNil(t, entry)
	entry.nextProbeAt = time.Now().Add(-time.Second)
	return entry
}

func TestExcelBPSModelRecoveryUsesExactModelAndWaitsForProbe(t *testing.T) {
	s, a, _ := modelRecoveryTestService()
	// The upstream ID also appears as a public alias: probing must not map twice.
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "gpt-6-astra": "gpt-5.6-sol"}
	upstream := &bpsProbeUpstream{}
	s.httpUpstream = upstream
	s.coolDownExcelBPSModel(t.Context(), a, "gpt-6-astra")
	s.syncExcelBPSModelRecovery(t.Context())
	require.Empty(t, upstream.bodies, "do not probe before the deadline")
	require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "alias"))
	require.False(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-5.6-sol"))
	other := *a
	other.ID++
	require.False(t, s.isExcelBPSModelCoolingDown(t.Context(), &other, "alias"))
	dueModelRecovery(t, s, a, "gpt-6-astra")
	require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "alias"), "expiry alone cannot release isolation")
	s.syncExcelBPSModelRecovery(t.Context())
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.False(t, gjson.GetBytes(upstream.bodies[0], "store").Bool())
	require.Contains(t, string(upstream.bodies[0]), "Reply with exactly this nonce")
	require.False(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "alias"))
	s.syncExcelBPSModelRecovery(t.Context())
	require.Len(t, upstream.bodies, 1, "successful recovery removes future probe work")
}

func TestExcelBPSModelRecoveryFailureKeepsIsolationAndReschedules(t *testing.T) {
	for _, mode := range []string{"wrong_basic", "failed_terminal"} {
		t.Run(mode, func(t *testing.T) {
			s, a, _ := modelRecoveryTestService()
			upstream := &bpsProbeUpstream{mode: mode}
			s.httpUpstream = upstream
			entry := dueModelRecovery(t, s, a, "gpt-6-astra")
			s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSModelRateLimitCooldownMinutes] = "7"
			before := time.Now()
			s.syncExcelBPSModelRecovery(t.Context())
			require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
			require.False(t, entry.probing)
			require.WithinDuration(t, before.Add(7*time.Minute), entry.nextProbeAt, time.Second)
			s.syncExcelBPSModelRecovery(t.Context())
			require.Len(t, upstream.bodies, 1, "failed probes must not spin")
			upstream = &bpsProbeUpstream{}
			s.httpUpstream = upstream
			entry.nextProbeAt = time.Now().Add(-time.Second)
			s.syncExcelBPSModelRecovery(t.Context())
			require.Len(t, upstream.bodies, 1)
			require.False(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
		})
	}
}

func TestExcelBPSModelRecoveryRejectsHTTPAndMalformedStreams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		wire   string
	}{
		{"401", 401, "revoked"}, {"403", 403, "denied"}, {"404", 404, "missing"}, {"429", 429, "limited"}, {"500", 500, "error"},
		{"empty", 200, ""}, {"html", 200, "<html>ok</html>"}, {"partial", 200, "data: {"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, a, repo := modelRecoveryTestService()
			calls := 0
			s.httpUpstream = &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				calls++
				require.Equal(t, "bps.openai.com", req.URL.Host)
				require.Equal(t, HTTPUpstreamProfileExcelBPS, HTTPUpstreamProfileFromContext(req.Context()))
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.wire))}, nil
			}}
			dueModelRecovery(t, s, a, "gpt-6-astra")
			s.syncExcelBPSModelRecovery(t.Context())
			require.Equal(t, 1, calls)
			require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
			if tc.status == 401 {
				require.Equal(t, 1, repo.pauses)
			} else {
				require.Zero(t, repo.pauses)
			}
		})
	}
}

func TestExcelBPSModelRecoverySwitchAndScope(t *testing.T) {
	for _, name := range []string{"switch off", "protocol off", "settings unavailable", "BPS off", "native account", "paused", "expired", "removed model", "account cooldown", "model rate limit", "deleted", "database failure"} {
		t.Run(name, func(t *testing.T) {
			s, a, repo := modelRecoveryTestService()
			upstream := &bpsProbeUpstream{}
			s.httpUpstream = upstream
			dueModelRecovery(t, s, a, "gpt-6-astra")
			settings := s.settingService.settingRepo.(*excelBPSImageSettingsRepo)
			until := time.Now().Add(time.Hour)
			switch name {
			case "switch off":
				settings.values[SettingKeyExcelBPSRecoveryEnabled] = "false"
			case "protocol off":
				settings.values[SettingKeyExcelBPSEnabled] = "false"
			case "settings unavailable":
				settings.err = errors.New("unavailable")
			case "BPS off":
				a.Extra["openai_excel_bps"] = false
			case "native account":
				a.Platform = PlatformAnthropic
			case "paused":
				a.Schedulable = false
			case "expired":
				past := time.Now().Add(-time.Minute)
				a.ExpiresAt = &past
				a.AutoPauseOnExpired = true
			case "removed model":
				a.Extra["openai_excel_bps_models"] = []string{"gpt-5.6-sol"}
			case "account cooldown":
				s.excelBPSRateLimitedUntil.Store(a.ID, until)
			case "model rate limit":
				s.excelBPSModelRateLimits.Store(excelBPSRateLimitKey{a.ID, "gpt-6-astra"}, excelBPSRateLimitState{until: until})
			case "deleted":
				repo.err = ErrAccountNotFound
				repo.account = nil
			case "database failure":
				repo.err = errors.New("database unavailable")
			}
			s.syncExcelBPSModelRecovery(t.Context())
			require.Empty(t, upstream.bodies)
			if name == "deleted" {
				require.Empty(t, s.excelBPSModelCooldowns.entries)
			} else {
				require.Len(t, s.excelBPSModelCooldowns.entries, 1)
			}
			require.Zero(t, repo.pauses)
			if name == "switch off" {
				require.False(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
				s.coolDownExcelBPSModel(t.Context(), a, "gpt-5.6-sol")
				require.Len(t, s.excelBPSModelCooldowns.entries, 1)
				settings.values[SettingKeyExcelBPSRecoveryEnabled] = "true"
				require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
				s.syncExcelBPSModelRecovery(t.Context())
				require.Len(t, upstream.bodies, 1)
			}
		})
	}
}

func TestExcelBPSModelRecoveryStaleSuccessCannotClearNewFailure(t *testing.T) {
	for _, changed := range []string{"new failure", "switch off", "cancel"} {
		t.Run(changed, func(t *testing.T) {
			s, a, _ := modelRecoveryTestService()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dueModelRecovery(t, s, a, "gpt-6-astra")
			s.httpUpstream = &bpsTestUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
				switch changed {
				case "new failure":
					s.coolDownExcelBPSModel(t.Context(), a, "gpt-6-astra")
				case "switch off":
					s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSRecoveryEnabled] = "false"
				case "cancel":
					cancel()
				}
				return (&bpsProbeUpstream{}).Do(req, proxy, a.ID, a.Concurrency)
			}}
			s.syncExcelBPSModelRecovery(ctx)
			require.Len(t, s.excelBPSModelCooldowns.entries, 1)
		})
	}
}

func TestExcelBPSModelRecoverySingleFlightAndLifecycle(t *testing.T) {
	s, a, repo := modelRecoveryTestService()
	s.httpUpstream = &bpsProbeUpstream{}
	dueModelRecovery(t, s, a, "gpt-6-astra")
	entered := make(chan struct{})
	var calls atomic.Int32
	repo.fetch = func(ctx context.Context) (*Account, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.StartBPSModelRecovery()
	s.StartBPSModelRecovery()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	s.syncExcelBPSModelRecovery(t.Context())
	require.Equal(t, int32(1), calls.Load(), "one claim across concurrent scans")
	stopped := make(chan struct{})
	go func() { s.StopBPSModelRecovery(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not cancel and join")
	}
	require.Len(t, s.excelBPSModelCooldowns.entries, 1)
	require.False(t, s.excelBPSModelCooldowns.entries[excelBPSModelKey{a.ID, "gpt-6-astra"}].probing)
	s.StartBPSModelRecovery()
	s.StopBPSModelRecovery()
	require.Equal(t, int32(1), calls.Load())
}

func TestExcelBPSModelRecoveryProbeKeepsRateLimitPolicy(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, a, _ := modelRecoveryTestService()
			wire := "limited"
			if status == 200 {
				wire = bpsFailureWire(t, "response.failed", "rate_limit_exceeded", "", 0)
			}
			s.httpUpstream = &httpUpstreamRecorder{resp: bpsCompletionResponse(status, wire)}
			dueModelRecovery(t, s, a, "gpt-6-astra")
			s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSModelRateLimitCooldownMinutes] = "90"
			before := time.Now()
			s.syncExcelBPSModelRecovery(t.Context())
			require.WithinDuration(t, before.Add(90*time.Minute), bpsRateLimitState(t, s, a, "gpt-6-astra").until, time.Second)
			require.True(t, s.isExcelBPSCoolingDown(t.Context(), a, "gpt-6-astra"))
			s.excelBPSModelCooldowns.entries[excelBPSModelKey{a.ID, "gpt-6-astra"}].nextProbeAt = time.Now().Add(-time.Second)
			upstream := &bpsProbeUpstream{}
			s.httpUpstream = upstream
			s.syncExcelBPSModelRecovery(t.Context())
			require.Empty(t, upstream.bodies, "unavailable-model probes must respect the longer model rate-limit interval")
		})
	}
}

func TestExcelBPSModelRecoveryStreamCredentialsPauseWholeAccount(t *testing.T) {
	s, a, repo := modelRecoveryTestService()
	s.httpUpstream = &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsFailureWire(t, "response.failed", "token_revoked", "", 401))}
	dueModelRecovery(t, s, a, "gpt-6-astra")
	s.syncExcelBPSModelRecovery(t.Context())
	require.Equal(t, 1, repo.pauses)
	require.True(t, s.isOpenAIAccountRuntimeBlocked(a))
	require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
}

func TestExcelBPSModelRecoveryCancellationClosesProbeBody(t *testing.T) {
	s, a, _ := modelRecoveryTestService()
	reader, writer := io.Pipe()
	defer writer.Close()
	entered := make(chan struct{})
	s.httpUpstream = &bpsTestUpstream{send: func(_ *http.Request, _ string) (*http.Response, error) {
		close(entered)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
	}}
	dueModelRecovery(t, s, a, "gpt-6-astra")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { s.syncExcelBPSModelRecovery(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close the response body")
	}
	require.True(t, s.isExcelBPSModelCoolingDown(t.Context(), a, "gpt-6-astra"))
}
