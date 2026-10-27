package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func credentialRecoveryTestService(t *testing.T) (*OpenAIGatewayService, *Account, *modelRecoveryTestRepo, *excelBPSCredentialReadTestRepo) {
	t.Helper()
	s, a, accounts := modelRecoveryTestService()
	auth, reader, grants, _, _ := newExcelReauthTestService(t)
	a.Credentials = reader.account.Credentials
	reader.account = a
	counted := &excelBPSCredentialReadTestRepo{excelReauthTestRepo: grants}
	auth.repo = counted
	s.excelOAuthReauth = auth
	storeExcelTestCredentials(t, auth, grants, excelTestCredentials("ready", time.Now().Add(time.Hour)))
	s.handleExcelBPSCredentialReadError(t.Context(), a, errors.New("temporary credential read failure"))
	return s, a, accounts, counted
}

func dueCredentialRecovery(t *testing.T, s *OpenAIGatewayService, a *Account) *excelBPSCredentialWait {
	t.Helper()
	cache := &s.excelBPSCredentialCooldowns
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[a.ID]
	require.NotNil(t, entry)
	entry.nextCheckAt = time.Now().Add(-time.Second)
	return entry
}

func TestExcelBPSCredentialRecoveryWaitsForAuthorizationAndReadableGrant(t *testing.T) {
	for _, status := range []string{"queued", "running", "callback_processing"} {
		t.Run(status, func(t *testing.T) {
			s, a, accounts, grants := credentialRecoveryTestService(t)
			upstream := &httpUpstreamRecorder{}
			s.httpUpstream = upstream
			grants.task = &OpenAIOAuthReauthTaskRecord{Status: status, OAuthProfile: "excel"}
			s.syncExcelBPSCredentialRecovery(t.Context())
			require.Zero(t, grants.reads, "no check before the scheduled time")
			entry := dueCredentialRecovery(t, s, a)
			require.True(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"), "time alone cannot restore routing")
			s.syncExcelBPSCredentialRecovery(t.Context())
			require.Zero(t, grants.reads, "an older readable grant cannot bypass a running authorization")
			require.False(t, entry.checking)
			require.WithinDuration(t, time.Now().Add(30*time.Second), entry.nextCheckAt, time.Second)
			grants.task.Status = OpenAIOAuthReauthStatusSucceeded
			grants.err = errors.New("temporary storage failure")
			dueCredentialRecovery(t, s, a)
			s.syncExcelBPSCredentialRecovery(t.Context())
			require.True(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"), "completed authorization is insufficient if credentials cannot be read")
			require.Equal(t, 1, grants.reads)
			grants.err = nil
			dueCredentialRecovery(t, s, a)
			s.syncExcelBPSCredentialRecovery(t.Context())
			require.False(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"))
			require.Empty(t, s.excelBPSCredentialCooldowns.entries)
			require.Equal(t, 2, grants.reads)
			require.Zero(t, accounts.pauses)
			require.Empty(t, upstream.requests, "checks never send a business request")
			s.syncExcelBPSCredentialRecovery(t.Context())
			require.Equal(t, 2, grants.reads, "restored accounts no longer need background checks")
		})
	}
}

func TestExcelBPSCredentialRecoveryInvalidGrantStillPausesWholeAccount(t *testing.T) {
	s, a, accounts, grants := credentialRecoveryTestService(t)
	var err error
	grants.ciphertext, err = s.excelOAuthReauth.encryptor.Encrypt("invalid saved grant")
	require.NoError(t, err)
	dueCredentialRecovery(t, s, a)
	s.syncExcelBPSCredentialRecovery(t.Context())
	require.Equal(t, 1, accounts.pauses)
	require.True(t, s.isOpenAIAccountRuntimeBlocked(a))
	require.True(t, s.isExcelBPSCredentialCoolingDown(t.Context(), a, "gpt-6-astra"))
	a.Status, a.Schedulable = StatusError, false
	storeExcelTestCredentials(t, s.excelOAuthReauth, grants.excelReauthTestRepo, excelTestCredentials("repaired", time.Now().Add(time.Hour)))
	dueCredentialRecovery(t, s, a)
	s.syncExcelBPSCredentialRecovery(t.Context())
	require.Len(t, s.excelBPSCredentialCooldowns.entries, 1, "credential repair alone must not resume a persistently paused account")
	require.Equal(t, StatusError, a.Status)
}

func TestExcelBPSCredentialRecoveryScopeAndDeletedAccounts(t *testing.T) {
	for _, mode := range []string{"switch off", "protocol off", "native account", "paused account", "deleted", "account read failed", "pending route flag"} {
		t.Run(mode, func(t *testing.T) {
			s, a, accounts, grants := credentialRecoveryTestService(t)
			dueCredentialRecovery(t, s, a)
			settings := s.settingService.settingRepo.(*excelBPSImageSettingsRepo)
			switch mode {
			case "switch off":
				settings.values[SettingKeyExcelBPSRecoveryEnabled] = "false"
			case "protocol off":
				settings.values[SettingKeyExcelBPSEnabled] = "false"
			case "native account":
				a.Extra["openai_excel_bps"] = false
			case "paused account":
				a.Schedulable = false
			case "deleted":
				accounts.err = ErrAccountNotFound
			case "account read failed":
				accounts.err = errors.New("temporary storage error")
			case "pending route flag":
				a.Extra[ExcelBPSAuthorizationPendingKey] = true
			}
			s.syncExcelBPSCredentialRecovery(t.Context())
			if mode == "deleted" {
				require.Empty(t, s.excelBPSCredentialCooldowns.entries)
			} else {
				require.Len(t, s.excelBPSCredentialCooldowns.entries, 1)
			}
			if mode != "pending route flag" {
				require.Zero(t, grants.reads)
			} else {
				require.Equal(t, 1, grants.reads)
				require.Equal(t, true, a.Extra[ExcelBPSAuthorizationPendingKey], "the check cannot toggle routing behind the authorization worker")
			}
			require.Zero(t, accounts.pauses)
		})
	}
}

func TestExcelBPSCredentialRecoveryIgnoresStaleAndInterruptedResults(t *testing.T) {
	for _, changed := range []string{"new failure", "switch off", "cancel", "panic"} {
		t.Run(changed, func(t *testing.T) {
			s, a, _, grants := credentialRecoveryTestService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dueCredentialRecovery(t, s, a)
			grants.beforeRead = func() {
				switch changed {
				case "new failure":
					s.handleExcelBPSCredentialReadError(t.Context(), a, errors.New("new read failure"))
				case "switch off":
					s.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSRecoveryEnabled] = "false"
				case "cancel":
					cancel()
				case "panic":
					panic("private failure")
				}
			}
			s.syncExcelBPSCredentialRecovery(ctx)
			require.Len(t, s.excelBPSCredentialCooldowns.entries, 1)
			require.False(t, s.excelBPSCredentialCooldowns.entries[a.ID].checking)
		})
	}
}

func TestExcelBPSCredentialRecoverySingleFlightAndLifecycle(t *testing.T) {
	s, a, accounts, _ := credentialRecoveryTestService(t)
	s.httpUpstream = &httpUpstreamRecorder{}
	dueCredentialRecovery(t, s, a)
	entered := make(chan struct{})
	var calls atomic.Int32
	accounts.fetch = func(ctx context.Context) (*Account, error) {
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
		t.Fatal("credential worker did not start")
	}
	s.syncExcelBPSCredentialRecovery(t.Context())
	require.Equal(t, int32(1), calls.Load(), "only one scan may check an account at a time")
	done := make(chan struct{})
	go func() { s.StopBPSModelRecovery(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("credential check was not canceled and joined")
	}
	require.Len(t, s.excelBPSCredentialCooldowns.entries, 1)
	require.False(t, s.excelBPSCredentialCooldowns.entries[a.ID].checking)
	s.StartBPSModelRecovery()
	s.StopBPSModelRecovery()
	require.Equal(t, int32(1), calls.Load())
}
