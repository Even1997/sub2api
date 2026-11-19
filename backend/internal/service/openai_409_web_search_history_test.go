package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Exercise the rebuilt request through the ordinary OAuth transform and final
// send guard. Compatibility must not turn an intact replay into a local 409.
func TestOpenAI409OAuthWebSearchHistoryFinalSend(t *testing.T) {
	const function = `{"type":"function","name":"record","parameters":{"type":"object"}}`
	const search = `,{"type":"web_search_call","id":"ws_fixture","status":"completed","action":{"type":"search","query":"fixture"}}`
	for _, model := range []string{"gpt-6-astra", "gpt-6.2-astra", "future-model"} {
		for _, tc := range []struct {
			name, history, tools, choice, wantChoice string
			lite, compact, wantSearch                bool
		}{
			{name: "no_search_history", tools: `[` + function + `]`, choice: `"auto"`, wantChoice: `"auto"`},
			{name: "caller_function", history: search, tools: `[` + function + `]`, choice: `"auto"`, wantChoice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"record"}]}`, wantSearch: true},
			{name: "http_namespace", history: search, tools: `[{"type":"namespace","name":"diagnostic","tools":[` + function + `]}]`, choice: `"auto"`, wantChoice: `"auto"`},
			{name: "http_lite_namespace", history: `,{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"diagnostic","tools":[` + function + `]}]}` + search, tools: `[]`, choice: `"required"`, wantChoice: `"required"`, lite: true},
			{name: "no_tools", history: search, tools: `[]`, choice: `"auto"`, wantChoice: `"none"`, wantSearch: true},
			{name: "explicit_none", history: search, tools: `[]`, choice: `"none"`, wantChoice: `"none"`, wantSearch: true},
			{name: "explicit_function", history: search, tools: `[` + function + `]`, choice: `{"type":"function","name":"record"}`, wantChoice: `{"type":"function","name":"record"}`, wantSearch: true},
			{name: "search_declared", history: search, tools: `[{"type":"web_search"}]`, choice: `"auto"`, wantChoice: `"auto"`, wantSearch: true},
			{name: "lite_no_tools", history: search, tools: `[]`, choice: `"auto"`, wantChoice: `"none"`, lite: true, wantSearch: true},
			{name: "lite_caller_tools", history: `,{"type":"additional_tools","role":"developer","tools":[` + function + `]}` + search, tools: `[]`, choice: `"auto"`, wantChoice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"record"}]}`, lite: true, wantSearch: true},
			{name: "lite_search_declared", history: `,{"type":"additional_tools","role":"developer","tools":[{"type":"web_search"}]}` + search, tools: `[]`, choice: `"auto"`, wantChoice: `"auto"`, lite: true, wantSearch: true},
			{name: "compact", history: search, tools: `[]`, choice: `"auto"`, wantChoice: `"auto"`, compact: true},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAI409OmitEncryptedHistoryEnabled: "true"}}, nil)}
				opaqueBody, err := sjson.Set(issue715OpaqueBody, "model", model)
				require.NoError(t, err)
				issue715LearnRoute(t, svc, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, opaqueBody)
				body := strings.TrimSuffix(opaqueBody, "]}") + tc.history + `,{"type":"function_call","name":"record","call_id":"call_record","arguments":"{}"},{"type":"function_call_output","call_id":"call_record","output":"already completed once"},{"role":"user","content":"continue using the existing results"}],"tools":` + tc.tools + `,"tool_choice":` + tc.choice + `}`
				c, _ := newTurnStateTestContext(t, 715, "web-history-recovery")
				c.Set("api_key", &APIKey{ID: 715, GroupID: int64Ptr(715)})
				ctx := svc.WithOpenAIAgentMessageRecovery(context.Background(), c, []byte(body), "web-history-recovery", model)
				next, clean, changed, err := svc.TryRebuildOpenAI409History(ctx, c, []byte(body), "web-history-recovery", model, openAIAgentRecoveryError("owner_unavailable"))
				require.NoError(t, err)
				require.True(t, changed)
				account := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				next, clean, err = svc.prepareOpenAI409HTTPProjection(next, c, account, clean)
				require.NoError(t, err)
				r := openAIAgentRecoveryFromContext(next)
				originalInput, originalControls := r.inputDigest, r.replayControlsDigest
				var decoded map[string]any
				require.NoError(t, decodeOpenAIJSONUseNumber(clean, &decoded))
				opts := codexOAuthTransformOptions{PreserveReadableReplay: true, SkipDefaultInstructions: true, ResponsesLite: tc.lite, IsCompact: tc.compact, PreserveHTTPNamespaceSearchHistory: true}
				require.NoError(t, prepareOpenAI409OAuthReplay(next, c, decoded, opts))
				transformed := applyCodexOAuthTransformWithOptions(decoded, opts)
				require.NoError(t, transformed.Error)
				wire, err := json.Marshal(decoded)
				require.NoError(t, err)
				req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(wire))
				require.NoError(t, err)
				finish, err := svc.prepareOpenAIAgentRecoveryAttempt(next, account, req)
				finish(false)
				require.NoError(t, err, "history compatibility must be included in the frozen OAuth projection")
				require.Equal(t, originalInput, r.inputDigest, "retain the source snapshot")
				require.Equal(t, originalControls, r.replayControlsDigest)
				require.JSONEq(t, tc.wantChoice, gjson.GetBytes(wire, "tool_choice").Raw)
				searchCount, resultCount := 0, 0
				countTools := func(tools gjson.Result) {
					for _, tool := range tools.Array() {
						if tool.Get("type").String() == "web_search" {
							searchCount++
							require.False(t, tool.Get("external_web_access").Exists(), "recovery must preserve the native provider schema")
						}
					}
				}
				countTools(gjson.GetBytes(wire, "tools"))
				for _, item := range gjson.GetBytes(wire, "input").Array() {
					countTools(item.Get("tools"))
					if item.Get("type").String() == "function_call_output" {
						resultCount++
						require.Equal(t, "already completed once", item.Get("output").String())
					}
				}
				require.Equal(t, 1, resultCount, "preserve completed tool results exactly once")
				if tc.wantSearch {
					require.Equal(t, 1, searchCount)
				} else {
					require.Zero(t, searchCount)
				}
				if tc.lite {
					require.False(t, gjsonToolsContainWebSearch(gjson.GetBytes(wire, "tools")))
				}
				// Later transforms must be idempotent for the protected fields.
				require.NoError(t, applyCodexOAuthTransformWithOptions(decoded, opts).Error)
				again, err := json.Marshal(decoded)
				require.NoError(t, err)
				require.Equal(t, openAI409ControlsDigest(wire), openAI409ControlsDigest(again))
				require.Equal(t, openAIAgentRecoveryInputDigest(wire), openAIAgentRecoveryInputDigest(again))
				// The fix must not permit subsequent arbitrary tool/instruction edits.
				for _, field := range []string{"tools", "instructions"} {
					tampered, err := sjson.SetBytes(wire, field, "unexpected mutation")
					require.NoError(t, err)
					req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(tampered))
					require.NoError(t, err)
					finish, err := svc.prepareOpenAIAgentRecoveryAttempt(next, account, req)
					finish(false)
					var recovery *OpenAIAgentRecoveryError
					require.ErrorAs(t, err, &recovery)
					require.Equal(t, "controls_integrity", recovery.InternalSubreason())
				}
			})
		}
	}
}
