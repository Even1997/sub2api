package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type autoConfigGroups struct {
	GroupRepository
	group *Group
	err   error
}

func (s autoConfigGroups) GetByID(context.Context, int64) (*Group, error) { return s.group, s.err }
func TestOAuthAutoConfigInitialFieldsAndScope(t *testing.T) {
	cfg := DefaultOAuthAutoConfig()
	cfg.Enabled = true
	cfg.GroupIDs = []int64{2}
	cfg.Priority = 0
	cfg.LoadFactor = 90
	cfg.Concurrency = 7
	cfg.Revision = "r1"
	raw, _ := json.Marshal(cfg)
	settings := NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil)
	svc := &adminServiceImpl{settingService: settings, groupRepo: autoConfigGroups{group: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive}}}
	for _, tt := range []struct {
		kind, platform string
		apply          bool
	}{{AccountTypeOAuth, PlatformOpenAI, true}, {AccountTypeAPIKey, PlatformOpenAI, false}, {AccountTypeOAuth, PlatformAnthropic, false}} {
		in := &CreateAccountInput{Platform: tt.platform, Type: tt.kind, Priority: 50, Concurrency: 3, GroupIDs: []int64{1}, Extra: map[string]any{"keep": "unchanged"}}
		require.NoError(t, svc.ApplyOAuthAutoConfig(context.Background(), in))
		if tt.apply {
			require.Equal(t, 0, in.Priority)
			require.Equal(t, 7, in.Concurrency)
			require.Equal(t, 90, *in.LoadFactor)
			require.Equal(t, []int64{2}, in.GroupIDs)
		} else {
			require.Equal(t, 50, in.Priority)
			require.Equal(t, []int64{1}, in.GroupIDs)
		}
		require.Equal(t, "unchanged", in.Extra["keep"])
	}
	svc.groupRepo = autoConfigGroups{err: errors.New("deleted")}
	require.Error(t, svc.ApplyOAuthAutoConfig(context.Background(), &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.ErrorIs(t, svc.ApplyOAuthAutoConfig(context.Background(), nil), ErrAccountNilInput)
}
func TestAutoConfigIndependentSwitchesAndValidation(t *testing.T) {
	c := DefaultOAuthAutoConfig()
	c.UpgradeEnabled = true
	c.UpgradeGroupIDs = []int64{3}
	require.NoError(t, ValidateOAuthAutoConfig(c))
	c.Enabled = true
	require.NoError(t, ValidateOAuthAutoConfig(c))
	c.GroupIDs = []int64{2}
	require.NoError(t, ValidateOAuthAutoConfig(c))
	c.GroupIDs = []int64{2, 2}
	require.Error(t, ValidateOAuthAutoConfig(c))
	c.GroupIDs = []int64{-1}
	require.Error(t, ValidateOAuthAutoConfig(c))
}
func TestAutoConfigAdvanceFailureCooldownCapAndManualChange(t *testing.T) {
	c := DefaultOAuthAutoConfig()
	c.Revision = "r1"
	c.SuccessesPerStep = 2
	c.UpgradeStep = 2
	c.MaxConcurrency = 4
	now := time.Now()
	good := AccountConcurrencyResult{StartedAt: now, Success: true}
	bad := good
	bad.Success = false
	state, n := AdvanceConcurrency(AutoConfigConcurrencyState{}, 3, c, good, now)
	require.Equal(t, 3, n)
	require.Equal(t, 1, state.Successes)
	state, n = AdvanceConcurrency(state, n, c, bad, now)
	require.Zero(t, state.Successes)
	state, n = AdvanceConcurrency(state, n, c, good, now)
	require.Zero(t, state.Successes)
	now = now.Add(61 * time.Second)
	good.StartedAt = now
	state, n = AdvanceConcurrency(state, n, c, good, now)
	state, n = AdvanceConcurrency(state, n, c, good, now)
	require.Equal(t, 4, n)
	require.Zero(t, state.Successes)
	require.NotNil(t, state.LastUpgradeAt)
	state, n = AdvanceConcurrency(state, n, c, good, now)
	require.Equal(t, 4, n)
	state, n = AdvanceConcurrency(state, 9, c, good, now)
	require.Equal(t, 9, n, "never decrease a manually raised concurrency")
	c.Revision = "r2"
	state, _ = AdvanceConcurrency(state, 3, c, good, now)
	require.Equal(t, 1, state.Successes)
	require.Equal(t, "r2", state.Revision)
}

func TestAutoConfigRecoveryRampStopsAtCapturedTarget(t *testing.T) {
	c := DefaultOAuthAutoConfig()
	c.Revision = "r1"
	c.SuccessesPerStep = 1
	c.UpgradeStep = 2
	c.MaxConcurrency = 100
	now := time.Now()
	state := AutoConfigConcurrencyState{Revision: "r1", Concurrency: 5, RecoveryTarget: 9}
	good := AccountConcurrencyResult{StartedAt: now, Success: true}
	state, n := AdvanceConcurrency(state, 5, c, good, now)
	require.Equal(t, 7, n)
	state.PausedUntil = now.Add(-time.Second)
	state, n = AdvanceConcurrency(state, 7, c, good, now)
	require.Equal(t, 9, n)
	require.Equal(t, 9, state.RecoveryTarget)
}

func TestAutoConfigPendingQualityFenceSurvivesCooldown(t *testing.T) {
	c := DefaultOAuthAutoConfig()
	c.Revision, c.SuccessesPerStep, c.MaxConcurrency = "r1", 1, 100
	now := time.Now()
	state := AutoConfigConcurrencyState{Revision: "r1", Concurrency: 5, RecoveryTarget: 5, PausedUntil: now.Add(-time.Hour)}
	state, n := AdvanceConcurrency(state, 5, c, AccountConcurrencyResult{StartedAt: now, Success: true}, now)
	require.Equal(t, 5, n)
	require.Equal(t, 5, state.RecoveryTarget)
}
func TestAutoConfigQueueOverflowFailsClosed(t *testing.T) {
	s := NewAccountOpsService(nil, nil, nil)
	c := DefaultOAuthAutoConfig()
	c.UpgradeEnabled = true
	s.autoConfig.Store(c)
	for i := 0; i < cap(s.autoResults)+1; i++ {
		s.ObserveConcurrencyResult(AccountConcurrencyResult{AccountID: 1})
	}
	require.True(t, s.AutoConfigBlocked())
	require.Equal(t, cap(s.autoResults), len(s.autoResults))
}
func TestAutoConfigSaveRejectsCrossPlatformGroups(t *testing.T) {
	s := NewAccountOpsService(&accountOpsSettingsStub{}, nil, nil)
	s.autoGroups = autoConfigGroups{group: &Group{Platform: PlatformAnthropic, Status: StatusActive}}
	c := DefaultOAuthAutoConfig()
	c.Enabled = true
	c.GroupIDs = []int64{2}
	_, err := s.SaveOAuthAutoConfig(context.Background(), c)
	require.Error(t, err)
}

type autoConfigAccountRepo struct {
	AccountRepository
	created *Account
	groups  []int64
}

func (r *autoConfigAccountRepo) Create(_ context.Context, a *Account) error {
	a.ID = 8
	r.created = a
	return nil
}
func (r *autoConfigAccountRepo) BindGroups(_ context.Context, _ int64, ids []int64) error {
	r.groups = ids
	return nil
}
func TestAutoConfigCRSNewAccountsOnly(t *testing.T) {
	cfg := DefaultOAuthAutoConfig()
	cfg.Enabled = true
	cfg.QualityRule = initialQualityRuleFixture()
	cfg.Priority = 7
	cfg.Concurrency = 9
	cfg.GroupIDs = []int64{5}
	raw, _ := json.Marshal(cfg)
	settings := NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil)
	policy := &adminServiceImpl{settingService: settings, groupRepo: autoConfigGroups{group: &Group{ID: 5, Platform: PlatformOpenAI, Status: StatusActive}}}
	repo := &autoConfigAccountRepo{}
	syncer := &CRSSyncService{accountRepo: repo, autoConfigure: policy.ApplyOAuthAutoConfig}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Priority: 50, Concurrency: 3, Status: StatusError, Schedulable: false}
	require.NoError(t, syncer.createSyncedAccount(context.Background(), a))
	require.NotNil(t, repo.created.InitialQualityPlan)
	require.Equal(t, "gpt-5.4", repo.created.InitialQualityPlan.ModelID)
	require.Equal(t, 9, repo.created.Concurrency)
	require.Equal(t, 7, repo.created.Priority)
	require.Equal(t, []int64{5}, repo.groups)
	require.Equal(t, StatusError, repo.created.Status)
	require.False(t, repo.created.Schedulable)
}

func TestAutoConfigCanDisableAfterGroupDeleted(t *testing.T) {
	s := NewAccountOpsService(&accountOpsSettingsStub{}, nil, nil)
	s.autoGroups = autoConfigGroups{err: ErrGroupNotFound}
	cfg := DefaultOAuthAutoConfig()
	cfg.GroupIDs = []int64{5}
	cfg.UpgradeGroupIDs = []int64{6}
	_, err := s.SaveOAuthAutoConfig(context.Background(), cfg)
	require.NoError(t, err, "disabling must remain possible after a target group is deleted")
}

func TestAutoConfigImportedMarkerCannotCreateHistory(t *testing.T) {
	original := map[string]any{"auto_config_initial_revision": "imported", "keep": true}
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: original}
	svc := &adminServiceImpl{}
	require.NoError(t, svc.ApplyOAuthAutoConfig(context.Background(), input))
	require.NotContains(t, input.Extra, "auto_config_initial_revision")
	require.Equal(t, true, input.Extra["keep"])
	require.Equal(t, "imported", original["auto_config_initial_revision"], "do not mutate the caller's map")
}

type autoConfigHistoryAccountRepo struct {
	*autoConfigAccountRepo
	recorded int
}

func (r *autoConfigHistoryAccountRepo) RecordAutoConfigInitial(_ context.Context, _ *Account, _ []int64) error {
	r.recorded++
	return nil
}
func TestAutoConfigInitialHistoryOnlyAfterNewAccount(t *testing.T) {
	repo := &autoConfigHistoryAccountRepo{autoConfigAccountRepo: &autoConfigAccountRepo{}}
	a := &Account{ID: 7, Extra: map[string]any{"keep": true}}
	recordAutoConfigInitial(context.Background(), repo, a, []int64{3})
	require.Zero(t, repo.recorded)
	a.Extra["auto_config_initial_revision"] = "r1"
	recordAutoConfigInitial(context.Background(), repo, a, []int64{3})
	require.Equal(t, 1, repo.recorded)
}

func TestOAuthAutoConfigOptionalGroupsAndAccountSettings(t *testing.T) {
	cfg := DefaultOAuthAutoConfig()
	cfg.Enabled = true
	cfg.Revision = "r2"
	rate := 1.5
	cost := 0.25
	proxyID := int64(9)
	pause := true
	passthrough := true
	seconds := 30
	slot := true
	mode := "session"
	ws := OpenAIWSIngressModePassthrough
	image := "block"
	plan := "plus"
	pause5h := 0.8
	reset := true
	reset5h := 1.0
	prism := true
	aliases := true
	sse := true
	cfg.AccountSettings = OAuthInitialAccountSettings{
		RateMultiplier: ratePtr(rate), CostMultiplier: &cost, ProxyID: &proxyID, AutoPauseOnExpired: &pause,
		OpenAIPassthrough: &passthrough, CodexFingerprintMode: &mode, SlotReservationEnabled: &slot, SlotReservationSeconds: &seconds, OpenAIWSMode: &ws,
		OpenAIWSSSEAcceleration: &sse, CodexImageToolMode: &image, PlanType: &plan, OpenAIModelAliases: &aliases,
		CompactModelMappings:       []OAuthCompactModelMapping{{From: "gpt-5.4", To: "gpt-5.4-mini"}},
		AutoPause5hThreshold:       &pause5h,
		AutoResetCreditEnabled:     &reset,
		AutoResetCredit5hThreshold: &reset5h,
		PrismBrowser:               &prism,
		PrismBrowserModels:         []string{"gpt-6-luna"},
	}
	raw, _ := json.Marshal(cfg)
	settings := NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil)
	svc := &adminServiceImpl{settingService: settings}
	in := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Priority: 50, Concurrency: 3, GroupIDs: []int64{8}, Extra: map[string]any{"keep": "yes"}, Credentials: map[string]any{"access_token": "secret"}}
	require.NoError(t, svc.ApplyOAuthAutoConfig(context.Background(), in))
	require.Equal(t, []int64{8}, in.GroupIDs)
	require.Equal(t, 1.5, *in.RateMultiplier)
	require.Equal(t, int64(9), *in.ProxyID)
	require.True(t, *in.AutoPauseOnExpired)
	require.Equal(t, "yes", in.Extra["keep"])
	require.Equal(t, 0.25, in.Extra[AccountCostMultiplierExtraKey])
	require.Equal(t, true, in.Extra["openai_passthrough"])
	require.Equal(t, "session", in.Extra[codexFingerprintModeExtraKey])
	require.Equal(t, OpenAIWSIngressModePassthrough, in.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, true, in.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, "secret", in.Credentials["access_token"])
	require.Equal(t, "strip", in.Extra["codex_image_generation_explicit_tool_policy"])
	require.Equal(t, true, in.Extra["openai_oauth_ws_sse_acceleration"])
	require.Equal(t, 0.8, in.Extra["auto_pause_5h_threshold"])
	require.Equal(t, true, in.Extra["auto_reset_credit_enabled"])
	require.Equal(t, 1.0, in.Extra["auto_reset_credit_5h_threshold"])
	require.Equal(t, true, in.Extra["openai_prism_browser"])
	require.Equal(t, []string{"gpt-6-luna"}, in.Extra["openai_prism_browser_models"])
	require.Equal(t, "plus", in.Credentials["plan_type"])
	require.Equal(t, "aliases", in.Credentials["model_mapping_mode"])
	require.Equal(t, map[string]any{"gpt-5.4": "gpt-5.4-mini"}, in.Credentials["compact_model_mapping"])
	badMode := "sideways"
	invalid := cfg
	invalid.AccountSettings.CodexImageToolMode = &badMode
	require.Error(t, ValidateOAuthAutoConfig(invalid))
	cfg.AccountSettings = OAuthInitialAccountSettings{}
	cfg.Platform = PlatformAnthropic
	warmup := true
	cfg.AccountSettings.InterceptWarmup = &warmup
	raw, _ = json.Marshal(cfg)
	settings = NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil)
	svc.settingService = settings
	original := map[string]any{"access_token": "secret"}
	in = &CreateAccountInput{Platform: PlatformAnthropic, Type: AccountTypeOAuth, GroupIDs: []int64{4}, Credentials: original}
	require.NoError(t, svc.ApplyOAuthAutoConfig(context.Background(), in))
	require.Equal(t, []int64{4}, in.GroupIDs)
	require.Equal(t, true, in.Credentials["intercept_warmup_requests"])
	require.NotContains(t, original, "intercept_warmup_requests")
	require.NoError(t, ValidateOAuthAutoConfig(cfg))
	bad := cfg
	bad.AccountSettings.CodexCLIOnlyAppServer = &warmup
	require.Error(t, ValidateOAuthAutoConfig(bad))
}

func ratePtr(v float64) *float64 { return &v }

func TestAutoConfigZeroOrMissingProxyIsNotInserted(t *testing.T) {
	zero := int64(0)
	chosen := int64(9)
	cfg := DefaultOAuthAutoConfig()
	cfg.Enabled = true
	cfg.AccountSettings.ProxyID = &zero
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := &adminServiceImpl{settingService: NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil)}
	in := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &chosen}
	require.NoError(t, svc.ApplyOAuthAutoConfig(context.Background(), in))
	require.NotNil(t, in.ProxyID)
	require.Zero(t, *in.ProxyID)
	require.NoError(t, svc.resolveCreateProxyID(context.Background(), in))
	account, err := buildAccountForCreate(in, in.Extra)
	require.NoError(t, err)
	require.Nil(t, account.ProxyID)

	kept := int64(9)
	direct := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &kept}
	account, err = buildAccountForCreate(direct, nil)
	require.NoError(t, err)
	require.Equal(t, int64(9), *account.ProxyID)

	missing := int64(4)
	next, err := proxyIDAfterLookup(&missing, ErrProxyNotFound)
	require.NoError(t, err)
	require.Nil(t, next)
	_, err = proxyIDAfterLookup(&missing, errors.New("db down"))
	require.Error(t, err)
}
