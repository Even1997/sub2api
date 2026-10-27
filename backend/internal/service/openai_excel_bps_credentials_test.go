package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestExcelBPSCredentialPendingAvoidsRepeatedSelection(t *testing.T) {
	s := openAIExcelBPSRecoveryTestService(nil)
	repo, _ := setupExcelBPSAuth(s)
	a := excelAccount()
	ctx := WithExcelBPSScheduling(context.Background(), nil)
	s.pauseExcelBPSCredentialError(ctx, a, infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_PENDING", "authorization pending"))
	require.Zero(t, repo.errorCalls, "pending authorization must not permanently pause the account")
	require.Zero(t, repo.tempCalls, "native routes must retain their scheduling state")
	require.True(t, s.isOpenAIAccountRequestRuntimeBlocked(ctx, a, "gpt-6-astra", false), "new BPS requests must temporarily skip the pending grant")
}

func TestExcelBPSCredentialCooldownScopeSwitchAndExpiry(t *testing.T) {
	s := openAIExcelBPSRecoveryTestService(nil)
	repo, _ := setupExcelBPSAuth(s)
	a := excelAccount()
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra", "gpt-6.1-sol"}
	ctx := WithExcelBPSScheduling(t.Context(), nil)
	s.handleExcelBPSCredentialReadError(ctx, a, errors.New("storage temporarily unavailable"))
	require.True(t, s.isExcelBPSCredentialCoolingDown(ctx, a, "gpt-6-astra"))
	require.True(t, s.isExcelBPSCredentialCoolingDown(ctx, a, "gpt-6.1-sol"), "the shared BPS grant affects every BPS model on this account")
	require.False(t, s.isExcelBPSCredentialCoolingDown(ctx, a, "gpt-5.1"), "native models remain eligible")
	native := context.WithValue(ctx, excelBPSSchedulingKey{}, false)
	require.False(t, s.isOpenAIAccountRequestRuntimeBlocked(native, a, "gpt-6-astra", false))
	other := *a
	other.ID++
	require.False(t, s.isExcelBPSCredentialCoolingDown(ctx, &other, "gpt-6-astra"))
	require.False(t, s.isOpenAIAccountRuntimeBlocked(a))
	require.Zero(t, repo.errorCalls+repo.tempCalls)
	require.True(t, a.IsSchedulable())

	saved := s.excelBPSCredentialCooldowns.entries[a.ID]
	require.WithinDuration(t, time.Now().Add(30*time.Second), saved, time.Second)
	settings := s.settingService.settingRepo.(*excelBPSImageSettingsRepo)
	settings.values[SettingKeyExcelBPSRecoveryEnabled] = "false"
	require.False(t, s.isExcelBPSCredentialCoolingDown(WithExcelBPSScheduling(t.Context(), nil), a, "gpt-6-astra"))
	s.handleExcelBPSCredentialReadError(t.Context(), a, errors.New("storage failure"))
	require.Equal(t, saved, s.excelBPSCredentialCooldowns.entries[a.ID], "disabled recovery neither uses nor extends the cooldown")
	settings.values[SettingKeyExcelBPSRecoveryEnabled] = "true"
	require.True(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"))
	s.excelBPSCredentialCooldowns.entries[a.ID] = time.Now().Add(-time.Second)
	require.False(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"))
	require.Empty(t, s.excelBPSCredentialCooldowns.entries)
}

func TestExcelBPSCredentialFailureClassificationAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		err                   error
		read, paused, cooling bool
	}{
		{"pending", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_PENDING", "private"), false, false, true},
		{"status unavailable", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_UNAVAILABLE", "private"), false, false, true},
		{"storage read", errors.New("private storage failure"), true, false, true},
		{"refresh transient", errors.New("excel OAuth refresh failed; credentials were retained, retry later"), true, false, true},
		{"unknown probe", errors.New("model response was incomplete"), false, false, false},
		{"missing auth", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REQUIRED", "private"), true, true, false},
		{"failed auth", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_FAILED", "private"), true, true, false},
		{"verification", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_VERIFICATION_REQUIRED", "private"), true, true, false},
		{"config", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_CONFIG_REQUIRED", "private"), true, true, false},
		{"revoked", errors.New("token_revoked"), true, true, false},
		{"invalid grant", errors.New("invalid_grant"), true, true, false},
		{"missing token", errors.New("access_token not found in credentials"), true, true, false},
		{"invalid saved token", errors.New("invalid saved Excel access token"), true, true, false},
		{"cancel", context.Canceled, true, false, false},
		{"deadline", context.DeadlineExceeded, true, false, false},
		{"nil", nil, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openAIExcelBPSRecoveryTestService(nil)
			repo, _ := setupExcelBPSAuth(s)
			a := excelAccount()
			if tc.read {
				s.handleExcelBPSCredentialReadError(t.Context(), a, tc.err)
			} else {
				s.pauseExcelBPSCredentialError(t.Context(), a, tc.err)
			}
			require.Equal(t, tc.paused, repo.errorCalls == 1)
			require.Equal(t, tc.paused, s.isOpenAIAccountRuntimeBlocked(a))
			require.Equal(t, tc.cooling, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"))
			require.Zero(t, repo.tempCalls)
		})
	}
}

func TestExcelBPSCredentialCooldownDoesNotPenalizeObservationOrCancellation(t *testing.T) {
	for _, mode := range []string{"observation", "experiment", "cancel", "native account", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s := openAIExcelBPSRecoveryTestService(nil)
			repo, _ := setupExcelBPSAuth(s)
			a := excelAccount()
			ctx := t.Context()
			switch mode {
			case "observation":
				ctx = withPelicanTestOptions(ctx, pelicanTestOptions{observeOnly: true})
			case "experiment":
				ctx = context.WithValue(ctx, controlledExperimentContextKey{}, &controlledExperimentMode{})
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "native account":
				a.Extra["openai_excel_bps"] = false
			case "disabled":
				s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSRecoveryEnabled] = "false"
			}
			s.handleExcelBPSCredentialReadError(ctx, a, errors.New("private storage error"))
			require.Empty(t, s.excelBPSCredentialCooldowns.entries)
			require.Zero(t, repo.errorCalls+repo.tempCalls)
		})
	}
}

func TestExcelBPSCredentialCooldownDiagnosticsAreSafe(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(t.Context(), zap.New(core).With(zap.String("request_id", "request_fixture")))
	s := openAIExcelBPSRecoveryTestService(nil)
	s.handleExcelBPSCredentialReadError(ctx, excelAccount(), errors.New("PRIVATE_TOKEN_DB_RESPONSE"))
	entries := logs.FilterMessage("openai.excel_bps_credential_failure").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "credential_read_failed", fields["reason"])
	require.Equal(t, "cooldown_bps", fields["action"])
	require.Equal(t, "bps_account", fields["scope"])
	require.Equal(t, int64(30), fields["cooldown_seconds"])
	require.Equal(t, "request_fixture", fields["request_id"])
	require.NotNil(t, fields["retry_at"])
	require.NotContains(t, fmt.Sprint(fields), "PRIVATE_TOKEN_DB_RESPONSE")
}

type excelBPSCredentialReadTestRepo struct {
	*excelReauthTestRepo
	reads int
	err   error
}

func (r *excelBPSCredentialReadTestRepo) GetExcelCredentials(ctx context.Context, id int64) (string, error) {
	r.reads++
	if r.err != nil {
		return "", r.err
	}
	return r.excelReauthTestRepo.GetExcelCredentials(ctx, id)
}

func TestExcelBPSCredentialReadFailureBacksOffSubsequentForwards(t *testing.T) {
	for _, storageFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(storageFailure), func(t *testing.T) {
			u := &httpUpstreamRecorder{}
			s := openAIExcelBPSRecoveryTestService(u)
			accountRepo, _ := setupExcelBPSAuth(s)
			auth, _, repo, _, _ := newExcelReauthTestService(t)
			repo.task = &OpenAIOAuthReauthTaskRecord{Status: "queued", OAuthProfile: "excel"}
			counted := &excelBPSCredentialReadTestRepo{excelReauthTestRepo: repo}
			if storageFailure {
				counted.err = errors.New("temporary database failure")
			}
			auth.repo = counted
			s.excelOAuthReauth = auth
			a := excelAccount()
			for attempt := range 2 {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, err := s.Forward(t.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"test","stream":true}`))
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.True(t, failover.ShouldRetryNextAccount())
				require.False(t, failover.RetryableOnSameAccount)
				require.False(t, IsResponseCommitted(c))
				require.Empty(t, rec.Body.String())
				require.Equal(t, 1, counted.reads, "attempt %d must not reread credentials while cooling down", attempt+1)
			}
			require.Empty(t, u.requests)
			require.Zero(t, accountRepo.errorCalls+accountRepo.tempCalls)
		})
	}
}

func TestExcelBPSCredentialCooldownRechecksStaleForwardSelection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			u := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[]}}\n\n")}
			s := openAIExcelBPSRecoveryTestService(u)
			a := excelAccount()
			s.handleExcelBPSCredentialReadError(t.Context(), a, errors.New("temporarily unavailable"))
			s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSRecoveryEnabled] = strconv.FormatBool(enabled)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := s.Forward(t.Context(), c, a, []byte("{\"model\":\"gpt-6-astra\",\"input\":\"test\",\"stream\":true}"))
			if enabled {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.True(t, failover.ShouldRetryNextAccount())
				require.False(t, failover.RetryableOnSameAccount)
				require.Empty(t, rec.Body.String())
				require.False(t, IsResponseCommitted(c))
				require.Empty(t, u.requests, "no compaction, attachment or main upstream request")
			} else {
				require.NoError(t, err)
				require.Len(t, u.requests, 1)
				require.Contains(t, rec.Body.String(), "response.completed")
			}
		})
	}
}

func TestExcelBPSCredentialCooldownSchedulers(t *testing.T) {
	for _, advanced := range []bool{true, false} {
		t.Run(strconv.FormatBool(advanced), func(t *testing.T) {
			ctx := WithExcelBPSScheduling(t.Context(), nil)
			groupID := int64(101301)
			a := excelAccount()
			a.ID, a.Concurrency = 38301, 1
			a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
			b := *a
			b.ID++
			accounts := []Account{*a, b}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			s := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, cfg: cfg,
				rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)), concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
			s.settingService = openAIExcelBPSRecoveryTestService(nil).settingService
			selectModel := func(model string) (*AccountSelectionResult, error) {
				selected, _, err := s.SelectAccountWithScheduler(ctx, &groupID, "", "", model, nil, OpenAIUpstreamTransportAny, false)
				if selected != nil && selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
				return selected, err
			}
			s.handleExcelBPSCredentialReadError(ctx, a, errors.New("temporary storage error"))
			for range 3 {
				selected, err := selectModel("gpt-6-astra")
				require.NoError(t, err)
				require.Equal(t, b.ID, selected.Account.ID)
			}
			s.handleExcelBPSCredentialReadError(ctx, &b, errors.New("temporary storage error"))
			_, err := selectModel("gpt-6-astra")
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Contains(t, err.Error(), excelBPSCredentialFilterReason+"=2")
			selected, err := selectModel("gpt-5.1")
			require.NoError(t, err)
			require.NotNil(t, selected.Account)
		})
	}
}

func TestExcelBPSCredentialCooldownConcurrentAndBounded(t *testing.T) {
	s := openAIExcelBPSRecoveryTestService(nil)
	a := excelAccount()
	ctx := context.WithValue(t.Context(), excelBPSRecoveryKey{}, true)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			s.handleExcelBPSCredentialReadError(ctx, a, errors.New("read failed"))
			s.isExcelBPSCredentialCoolingDown(ctx, a, "gpt-6-astra")
		})
	}
	wg.Wait()
	require.Len(t, s.excelBPSCredentialCooldowns.entries, 1)
	cache := &s.excelBPSCredentialCooldowns
	for id := range int64(excelBPSCredentialMaxEntries) {
		cache.entries[id+10000] = time.Now().Add(time.Minute)
	}
	delete(cache.entries, a.ID)
	s.handleExcelBPSCredentialReadError(ctx, a, errors.New("read failed"))
	require.Len(t, cache.entries, excelBPSCredentialMaxEntries)
	require.NotContains(t, cache.entries, a.ID, "active cooldowns are not evicted at capacity")
	cache.entries[10000] = time.Now().Add(-time.Second)
	s.handleExcelBPSCredentialReadError(ctx, a, errors.New("read failed"))
	require.Len(t, cache.entries, excelBPSCredentialMaxEntries)
	require.Contains(t, cache.entries, a.ID, "expired entries free capacity")
}
