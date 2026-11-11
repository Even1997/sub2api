package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Migrate legacy account-level fields into the single template setting.
func normalizeOAuthInitialWS(c OAuthAutoConfig) OAuthAutoConfig {
	if c.AccountSettings.OpenAIWSMode != nil {
		mode := normalizeOpenAIWSIngressMode(*c.AccountSettings.OpenAIWSMode)
		c.OpenAIWSMode = &mode
		c.AccountSettings.OpenAIWSMode = nil
	}
	if c.AccountSettings.OpenAIWSSSEAcceleration != nil {
		c.OpenAIWSSSEAcceleration = c.AccountSettings.OpenAIWSSSEAcceleration
		c.AccountSettings.OpenAIWSSSEAcceleration = nil
	}
	return c
}

func validateOAuthInitialWSMode(mode *string) error {
	if mode == nil {
		return nil
	}
	switch *mode {
	case OpenAIWSIngressModeOff, OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
		return nil
	default:
		return infraerrors.BadRequest("AUTO_CONFIG_INVALID", "OpenAI WS mode must be off, ctx_pool, passthrough, http_bridge, shared or dedicated")
	}
}

// Missing fields belong to legacy templates and do not take ownership of the
// imported transport settings. Explicit false/off values do override imports.
func applyOAuthInitialWS(extra map[string]any, c OAuthAutoConfig) {
	if c.OpenAIWSMode != nil {
		extra["openai_oauth_responses_websockets_v2_mode"] = *c.OpenAIWSMode
		extra["openai_oauth_responses_websockets_v2_enabled"] = *c.OpenAIWSMode != OpenAIWSIngressModeOff
	}
	if c.OpenAIWSSSEAcceleration != nil {
		extra["openai_oauth_ws_sse_acceleration"] = *c.OpenAIWSSSEAcceleration
	}
}
