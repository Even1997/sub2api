package service

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

type pelicanTestContextKey struct{}

type pelicanTestOptions struct {
	// Runtime-only evidence from the actual own-credential upstream response.
	directOAuth401        *bool
	directOAuthRevoked401 *bool
	nativeOAuthTransport  *bool
	qualityBatch          *QualityBatchEvidence
	prompt                string
	reasoningEffort       string
	testChannel           string
	observeOnly           bool
}

func withPelicanTestOptions(ctx context.Context, options pelicanTestOptions) context.Context {
	ctx = context.WithValue(ctx, qualityProbeContextKey{}, true)
	return context.WithValue(ctx, pelicanTestContextKey{}, options)
}

func pelicanTestOptionsFromContext(ctx context.Context) (pelicanTestOptions, bool) {
	options, ok := ctx.Value(pelicanTestContextKey{}).(pelicanTestOptions)
	return options, ok
}

// Record the chosen test path and mapped target, never credentials or URLs.
func recordQualityBatchRoute(c *gin.Context, account *Account, model, route string) {
	options, ok := pelicanTestOptionsFromContext(c.Request.Context())
	if ok && options.qualityBatch != nil {
		options.qualityBatch.Route = route
		options.qualityBatch.MappedModel = account.GetMappedModel(model)
	}
}

func isQualityObservation(ctx context.Context) bool {
	if isControlledExperiment(ctx) {
		return true
	}
	options, _ := pelicanTestOptionsFromContext(ctx)
	return options.observeOnly
}

// TestPelicanAccountConnection is the dedicated account test path for the
// Pelican UI. The existing /test endpoint deliberately keeps its historical
// probe payload; only this endpoint opts into the user prompt and reasoning.
func (s *AccountTestService) TestPelicanAccountConnection(c *gin.Context, accountID int64, modelID, prompt, reasoningEffort string) error {
	options, _ := pelicanTestOptionsFromContext(c.Request.Context())
	options.prompt = strings.TrimSpace(prompt)
	options.reasoningEffort = normalizePelicanReasoningEffort(reasoningEffort)
	ctx := withPelicanTestOptions(c.Request.Context(), options)
	c.Request = c.Request.WithContext(ctx)
	return s.TestAccountConnection(c, accountID, modelID, options.prompt, AccountTestModeDefault)
}

func normalizePelicanReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func createPelicanClaudePayload(modelID, prompt string) (map[string]any, error) {
	payload, err := createTestPayload(modelID)
	if err != nil {
		return nil, err
	}
	messages, ok := payload["messages"].([]map[string]any)
	if !ok || len(messages) == 0 {
		return payload, nil
	}
	content, ok := messages[0]["content"].([]map[string]any)
	if ok && len(content) > 0 {
		content[0]["text"] = promptOrDefault(prompt)
	}
	return payload, nil
}

func createPelicanOpenAIPayload(modelID string, isOAuth bool, prompt, reasoningEffort string) map[string]any {
	payload := createOpenAITestPayload(modelID, isOAuth)
	input, ok := payload["input"].([]map[string]any)
	if ok && len(input) > 0 {
		content, ok := input[0]["content"].([]map[string]any)
		if ok && len(content) > 0 {
			content[0]["text"] = promptOrDefault(prompt)
		}
	}
	if effort := normalizePelicanReasoningEffort(reasoningEffort); effort != "" {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
	return payload
}

func createClaudeTestPayloadForContext(ctx context.Context, modelID string) (map[string]any, error) {
	options, ok := pelicanTestOptionsFromContext(ctx)
	if !ok {
		return createTestPayload(modelID)
	}
	return createPelicanClaudePayload(modelID, options.prompt)
}

func createOpenAITestPayloadForContext(ctx context.Context, modelID string, isOAuth bool) map[string]any {
	options, ok := pelicanTestOptionsFromContext(ctx)
	if !ok {
		return createOpenAITestPayload(modelID, isOAuth)
	}
	return createPelicanOpenAIPayload(modelID, isOAuth, options.prompt, options.reasoningEffort)
}

func pelicanTestPromptForContext(ctx context.Context) string {
	options, ok := pelicanTestOptionsFromContext(ctx)
	if !ok {
		return "hi"
	}
	return promptOrDefault(options.prompt)
}

func promptOrDefault(prompt string) string {
	if value := strings.TrimSpace(prompt); value != "" {
		return value
	}
	return "hi"
}
