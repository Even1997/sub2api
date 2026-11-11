package service

import (
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	oauthImageToolModes = []string{"inherit", "enabled", "disabled", "block"}
	oauthPrismModels    = []string{"gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-luna"}
)

// OAuthCompactModelMapping is one compact-only model rewrite applied to a new OAuth account.
type OAuthCompactModelMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// OAuthInitialAccountSettings holds optional account configuration applied only
// to new OAuth accounts. A nil field leaves the imported or default value alone.
type OAuthInitialAccountSettings struct {
	RateMultiplier             *float64                   `json:"rate_multiplier,omitempty"`
	CostMultiplier             *float64                   `json:"cost_multiplier,omitempty"`
	ProxyID                    *int64                     `json:"proxy_id,omitempty"`
	AutoPauseOnExpired         *bool                      `json:"auto_pause_on_expired,omitempty"`
	OpenAIPassthrough          *bool                      `json:"openai_passthrough,omitempty"`
	OpenAIFlattenNamespaces    *bool                      `json:"openai_flatten_namespaces,omitempty"`
	OpenAILongContextBilling   *bool                      `json:"openai_long_context_billing,omitempty"`
	CodexCLIOnly               *bool                      `json:"codex_cli_only,omitempty"`
	CodexCLIOnlyAppServer      *bool                      `json:"codex_cli_only_app_server,omitempty"`
	CodexFingerprintMode       *string                    `json:"codex_fingerprint_mode,omitempty"`
	OpenAICompactMode          *string                    `json:"openai_compact_mode,omitempty"`
	OpenAIRequestTimezone      *string                    `json:"openai_request_timezone,omitempty"`
	SlotReservationEnabled     *bool                      `json:"slot_reservation_enabled,omitempty"`
	SlotReservationSeconds     *int                       `json:"slot_reservation_seconds,omitempty"`
	OpenAIWSMode               *string                    `json:"openai_ws_mode,omitempty"`
	OpenAIWSSSEAcceleration    *bool                      `json:"openai_ws_sse_acceleration,omitempty"`
	CodexImageToolMode         *string                    `json:"codex_image_tool_mode,omitempty"`
	PlanType                   *string                    `json:"plan_type,omitempty"`
	OpenAIModelAliases         *bool                      `json:"openai_model_aliases,omitempty"`
	CompactModelMappings       []OAuthCompactModelMapping `json:"openai_compact_model_mapping,omitempty"`
	AutoPause5hDisabled        *bool                      `json:"auto_pause_5h_disabled,omitempty"`
	AutoPause7dDisabled        *bool                      `json:"auto_pause_7d_disabled,omitempty"`
	AutoPause5hThreshold       *float64                   `json:"auto_pause_5h_threshold,omitempty"`
	AutoPause7dThreshold       *float64                   `json:"auto_pause_7d_threshold,omitempty"`
	AutoResetCreditEnabled     *bool                      `json:"auto_reset_credit_enabled,omitempty"`
	AutoResetCredit5hThreshold *float64                   `json:"auto_reset_credit_5h_threshold,omitempty"`
	AutoResetCredit7dThreshold *float64                   `json:"auto_reset_credit_7d_threshold,omitempty"`
	PrismBrowser               *bool                      `json:"openai_prism_browser,omitempty"`
	PrismBrowserModels         []string                   `json:"openai_prism_browser_models,omitempty"`
	InterceptWarmup            *bool                      `json:"intercept_warmup,omitempty"`
}

func validateInitialAccountSettings(c OAuthAutoConfig) error {
	bad := func(s string) error { return infraerrors.BadRequest("AUTO_CONFIG_INVALID", s) }
	s := c.AccountSettings
	if s.RateMultiplier != nil && (*s.RateMultiplier < 0 || math.IsNaN(*s.RateMultiplier) || math.IsInf(*s.RateMultiplier, 0)) {
		return bad("billing rate multiplier must be a finite number >= 0")
	}
	if s.CostMultiplier != nil {
		if err := ValidateAccountCostMultiplierExtra(map[string]any{AccountCostMultiplierExtraKey: *s.CostMultiplier}); err != nil {
			return bad("cost multiplier must be a finite number between 0 and 1000000")
		}
	}
	if s.ProxyID != nil && *s.ProxyID < 0 {
		return bad("proxy id must be >= 0")
	}
	if s.CodexCLIOnlyAppServer != nil && (s.CodexCLIOnly == nil || !*s.CodexCLIOnly) {
		return bad("app-server access requires Codex CLI only")
	}
	if s.CodexFingerprintMode != nil && !slices.Contains([]string{"off", "device", "session", "full"}, *s.CodexFingerprintMode) {
		return bad("invalid Codex fingerprint mode")
	}
	if s.OpenAICompactMode != nil && !slices.Contains([]string{OpenAICompactModeAuto, OpenAICompactModeForceOn, OpenAICompactModeForceOff}, *s.OpenAICompactMode) {
		return bad("invalid compact mode")
	}
	if s.OpenAIRequestTimezone != nil {
		if _, err := time.LoadLocation(*s.OpenAIRequestTimezone); err != nil {
			return bad("invalid request timezone")
		}
	}
	if s.SlotReservationEnabled != nil && *s.SlotReservationEnabled {
		if s.SlotReservationSeconds == nil || *s.SlotReservationSeconds < 1 || *s.SlotReservationSeconds > 3600 {
			return bad("slot reservation seconds must be 1–3600")
		}
	} else if s.SlotReservationSeconds != nil {
		return bad("slot reservation seconds require the feature to be enabled")
	}
	if s.OpenAIWSMode != nil && normalizeOpenAIWSIngressMode(*s.OpenAIWSMode) == "" {
		return bad("invalid WebSocket mode")
	}
	if s.CodexImageToolMode != nil && !slices.Contains(oauthImageToolModes, *s.CodexImageToolMode) {
		return bad("invalid Codex image tool mode")
	}
	if s.PlanType != nil {
		plan := strings.TrimSpace(*s.PlanType)
		if plan == "" || len(plan) > 64 || strings.ContainsAny(plan, " \t\r\n") {
			return bad("invalid plan type")
		}
	}
	if err := validateCompactMappings(s.CompactModelMappings); err != nil {
		return err
	}
	if !validFraction(s.AutoPause5hThreshold, false) || !validFraction(s.AutoPause7dThreshold, false) {
		return bad("auto-pause threshold must be between 0 and 1, exclusive of 0")
	}
	if !validFraction(s.AutoResetCredit5hThreshold, true) || !validFraction(s.AutoResetCredit7dThreshold, true) {
		return bad("auto-reset threshold must be 0 or between 0.001 and 1")
	}
	if s.PrismBrowser != nil && *s.PrismBrowser {
		if len(s.PrismBrowserModels) == 0 {
			return bad("Prism browser requires at least one model")
		}
	} else if len(s.PrismBrowserModels) > 0 {
		return bad("Prism models require Prism browser to be enabled")
	}
	seenPrism := map[string]struct{}{}
	for _, model := range s.PrismBrowserModels {
		if !slices.Contains(oauthPrismModels, model) {
			return bad("invalid Prism model")
		}
		if _, ok := seenPrism[model]; ok {
			return bad("Prism models must be unique")
		}
		seenPrism[model] = struct{}{}
	}
	return nil
}

func validFraction(value *float64, allowZero bool) bool {
	if value == nil {
		return true
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1 {
		return false
	}
	if *value == 0 {
		return allowZero
	}
	return !allowZero || *value >= 0.001
}

func validateCompactMappings(rules []OAuthCompactModelMapping) error {
	if len(rules) > 100 {
		return infraerrors.BadRequest("AUTO_CONFIG_INVALID", "at most 100 compact model mappings allowed")
	}
	seen := map[string]struct{}{}
	for _, rule := range rules {
		from, to := strings.TrimSpace(rule.From), strings.TrimSpace(rule.To)
		if from == "" || to == "" || len(from) > 256 || len(to) > 256 || strings.ContainsAny(from, " \t\r\n") || strings.ContainsAny(to, " \t\r\n") {
			return infraerrors.BadRequest("AUTO_CONFIG_INVALID", "compact model mappings need a source and target")
		}
		if _, ok := seen[from]; ok {
			return infraerrors.BadRequest("AUTO_CONFIG_INVALID", "compact model mapping sources must be unique")
		}
		seen[from] = struct{}{}
	}
	return nil
}

func applyInitialAccountSettings(input *CreateAccountInput, c OAuthAutoConfig) {
	s := c.AccountSettings
	if s.RateMultiplier != nil {
		v := *s.RateMultiplier
		input.RateMultiplier = &v
	}
	if s.ProxyID != nil {
		// 0 selects "no proxy" and overrides a proxy chosen on the create form.
		// Create treats that sentinel as unset instead of inserting a foreign key.
		v := *s.ProxyID
		input.ProxyID = &v
	}
	if s.AutoPauseOnExpired != nil {
		v := *s.AutoPauseOnExpired
		input.AutoPauseOnExpired = &v
	}
	extra := map[string]any{}
	if s.CostMultiplier != nil {
		extra[AccountCostMultiplierExtraKey] = *s.CostMultiplier
	}
	if c.Platform == PlatformOpenAI {
		putBool(extra, "openai_passthrough", s.OpenAIPassthrough)
		if s.OpenAIPassthrough != nil && !*s.OpenAIPassthrough {
			extra["openai_oauth_passthrough"] = false
		}
		putBool(extra, "openai_responses_flatten_namespaces", s.OpenAIFlattenNamespaces)
		putBool(extra, openAILongContextBillingEnabledKey, s.OpenAILongContextBilling)
		putBool(extra, "codex_cli_only", s.CodexCLIOnly)
		putBool(extra, "codex_cli_only_allow_app_server", s.CodexCLIOnlyAppServer)
		putString(extra, codexFingerprintModeExtraKey, s.CodexFingerprintMode)
		putString(extra, "openai_compact_mode", s.OpenAICompactMode)
		putString(extra, "openai_request_timezone", s.OpenAIRequestTimezone)
		putBool(extra, SlotReservationEnabled, s.SlotReservationEnabled)
		if s.SlotReservationSeconds != nil {
			extra[SlotReservationSeconds] = *s.SlotReservationSeconds
		}
		if s.OpenAIWSMode != nil {
			mode := normalizeOpenAIWSIngressMode(*s.OpenAIWSMode)
			extra["openai_oauth_responses_websockets_v2_mode"] = mode
			extra["openai_oauth_responses_websockets_v2_enabled"] = mode != OpenAIWSIngressModeOff
		}
		putBool(extra, "openai_oauth_ws_sse_acceleration", s.OpenAIWSSSEAcceleration)
		applyImageToolMode(extra, s.CodexImageToolMode)
		putBool(extra, "auto_pause_5h_disabled", s.AutoPause5hDisabled)
		putBool(extra, "auto_pause_7d_disabled", s.AutoPause7dDisabled)
		putFloat(extra, "auto_pause_5h_threshold", s.AutoPause5hThreshold)
		putFloat(extra, "auto_pause_7d_threshold", s.AutoPause7dThreshold)
		putBool(extra, "auto_reset_credit_enabled", s.AutoResetCreditEnabled)
		putFloat(extra, "auto_reset_credit_5h_threshold", s.AutoResetCredit5hThreshold)
		putFloat(extra, "auto_reset_credit_7d_threshold", s.AutoResetCredit7dThreshold)
		putBool(extra, "openai_prism_browser", s.PrismBrowser)
		if s.PrismBrowser != nil && *s.PrismBrowser {
			extra["openai_prism_browser_models"] = append([]string(nil), s.PrismBrowserModels...)
		}
	}
	if len(extra) > 0 {
		if input.Extra == nil {
			input.Extra = map[string]any{}
		}
		for key, value := range extra {
			input.Extra[key] = value
		}
	}
	applyInitialCredentials(input, c)
}

func applyImageToolMode(extra map[string]any, mode *string) {
	if mode == nil {
		return
	}
	switch *mode {
	case "enabled":
		extra["codex_image_generation_bridge"] = true
	case "disabled":
		extra["codex_image_generation_bridge"] = false
	case "block":
		extra["codex_image_generation_explicit_tool_policy"] = "strip"
	}
}

func applyInitialCredentials(input *CreateAccountInput, c OAuthAutoConfig) {
	s := c.AccountSettings
	writes := map[string]any{}
	if c.Platform == PlatformOpenAI {
		if s.PlanType != nil {
			writes["plan_type"] = strings.TrimSpace(*s.PlanType)
		}
		if s.OpenAIModelAliases != nil {
			if *s.OpenAIModelAliases {
				writes["model_mapping_mode"] = "aliases"
			} else {
				writes["model_mapping_mode"] = "whitelist"
			}
		}
		if len(s.CompactModelMappings) > 0 {
			mapping := map[string]any{}
			for _, rule := range s.CompactModelMappings {
				mapping[strings.TrimSpace(rule.From)] = strings.TrimSpace(rule.To)
			}
			writes["compact_model_mapping"] = mapping
		}
	}
	if s.InterceptWarmup != nil && (c.Platform == PlatformAnthropic || c.Platform == PlatformAntigravity) {
		writes["intercept_warmup_requests"] = *s.InterceptWarmup
	}
	if len(writes) == 0 {
		return
	}
	input.Credentials = maps.Clone(input.Credentials)
	if input.Credentials == nil {
		input.Credentials = map[string]any{}
	}
	for key, value := range writes {
		input.Credentials[key] = value
	}
}

func putBool(extra map[string]any, key string, value *bool) {
	if value != nil {
		extra[key] = *value
	}
}

func putString(extra map[string]any, key string, value *string) {
	if value != nil {
		extra[key] = *value
	}
}

func putFloat(extra map[string]any, key string, value *float64) {
	if value != nil {
		extra[key] = *value
	}
}
