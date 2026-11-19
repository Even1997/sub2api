package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const namespaceSearchHistory = `{"model":"gpt-6.1-sol","instructions":"Keep the original task","input":[{"type":"web_search_call","id":"ws_record","status":"completed","action":{"type":"search","query":"recorded query"}},{"role":"user","content":"Continue with the original tools"}],"tools":[{"type":"namespace","name":"diagnostic","description":"Test tools","tools":[{"type":"function","name":"record","parameters":{"type":"object","properties":{"value":{"type":"integer","enum":[9007199254740993]}}}}]}],"tool_choice":"auto"}`

func TestOAuthNamespaceSearchHistoryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, path, value string
		want              bool
	}{
		{"sol", "model", `"gpt-6.1-sol"`, true},
		{"astra", "model", `"gpt-6-astra"`, true},
		{"escaped_namespace", "tools.0.type", `"name\u0073pace"`, true},
		{"escaped_search", "input.0.type", `"web_search_\u0063all"`, true},
		{"other_model", "model", `"gpt-5.5"`, true},
		{"model_suffix", "model", `"gpt-6.1-sol-other"`, true},
		{"new_model", "model", `"gpt-6.2-astra"`, true},
		{"required", "tool_choice", `"required"`, true},
		{"none", "tool_choice", `"none"`, false},
		{"explicit", "tool_choice", `{"type":"function","name":"record"}`, false},
		{"restricted", "tool_choice", `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"record"}]}`, false},
		{"null_choice", "tool_choice", `null`, true},
		{"incomplete", "input.0.status", `"in_progress"`, false},
		{"missing_status", "input.0.status", `null`, false},
		{"mixed_history", "input.-1", `{"type":"web_search_call","status":"in_progress"}`, false},
		{"compaction", "input.-1", `{"type":"compaction_trigger"}`, false},
		{"caller_search", "tools.-1", `{"type":"web_search"}`, false},
		{"lite_caller_search", "input.-1", `{"type":"additional_tools","tools":[{"type":"web_search"}]}`, false},
		{"no_namespace", "tools", `[{"type":"function","name":"record"}]`, false},
		{"no_history", "input", `[{"role":"user","content":"hello"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := sjson.SetRaw(namespaceSearchHistory, tc.path, tc.value)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, decodeOpenAIJSONUseNumber([]byte(body), &decoded))
			require.Equal(t, tc.want, preserveOpenAIHTTPNamespaceSearchHistory(decoded))
			require.Equal(t, tc.want, preserveOpenAIHTTPNamespaceSearchHistoryBody([]byte(body)))
		})
	}
}

func TestOAuthNamespaceSearchHistoryEscapedHTTP(t *testing.T) {
	for name, replacer := range map[string]*strings.Replacer{
		"namespace":       strings.NewReplacer(`"namespace"`, `"name\u0073pace"`),
		"search":          strings.NewReplacer(`"web_search_call"`, `"web_search_\u0063all"`),
		"keys_and_values": strings.NewReplacer(`"type"`, `"\u0074ype"`, `"input"`, `"\u0069nput"`, `"tools"`, `"\u0074ools"`, `"namespace"`, `"name\u0073pace"`, `"web_search_call"`, `"web_search_\u0063all"`),
	} {
		for _, lite := range []bool{false, true} {
			caseName := name
			if lite {
				caseName += "/lite"
			}
			t.Run(caseName, func(t *testing.T) {
				body := namespaceSearchHistory
				if lite {
					tools := gjson.Get(body, "tools").Raw
					var err error
					body, err = sjson.SetRaw(body, "input.-1", `{"type":"additional_tools","role":"developer","tools":`+tools+`}`)
					require.NoError(t, err)
					body, err = sjson.SetRaw(body, "tools", `[]`)
					require.NoError(t, err)
				}
				escaped := replacer.Replace(body)
				require.JSONEq(t, body, escaped)
				require.True(t, preserveOpenAIHTTPNamespaceSearchHistoryBody([]byte(escaped)))
				opts := openAIResponsesCompatibilityOptions{ResponsesLite: lite, PreserveHTTPNamespaceSearchHistory: true}
				account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				want, _, err := normalizeOpenAIResponsesCompatibilityBodyWithOptions([]byte(body), account, opts)
				require.NoError(t, err)
				got, _, err := normalizeOpenAIResponsesCompatibilityBodyWithOptions([]byte(escaped), account, opts)
				require.NoError(t, err)
				require.JSONEq(t, string(want), string(got))
				require.Equal(t, "auto", gjson.GetBytes(got, "tool_choice").String())
				require.Contains(t, string(got), "9007199254740993")
			})
		}
	}
}

func TestOAuthNamespaceSearchHistoryHTTPAndWS(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6.2-astra", "future-model"} {
		for _, lite := range []bool{false, true} {
			for _, http := range []bool{false, true} {
				for _, raw := range []bool{false, true} {
					name := model
					if lite {
						name += "/lite"
					}
					if http {
						name += "/http"
					} else {
						name += "/ws-policy"
					}
					if raw {
						name += "/raw"
					} else {
						name += "/map"
					}
					t.Run(name, func(t *testing.T) {
						body, err := sjson.Set(namespaceSearchHistory, "model", model)
						require.NoError(t, err)
						if lite {
							tools := gjson.Get(body, "tools").Raw
							body, err = sjson.SetRaw(body, "input.-1", `{"type":"additional_tools","role":"developer","tools":`+tools+`}`)
							require.NoError(t, err)
							body, err = sjson.SetRaw(body, "tools", `[]`)
							require.NoError(t, err)
						}
						var wire []byte
						if raw {
							wire, _, err = normalizeOpenAIResponsesCompatibilityBodyWithOptions([]byte(body), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, openAIResponsesCompatibilityOptions{ResponsesLite: lite, PreserveHTTPNamespaceSearchHistory: http})
						} else {
							var decoded map[string]any
							require.NoError(t, decodeOpenAIJSONUseNumber([]byte(body), &decoded))
							result := applyCodexOAuthTransformWithOptions(decoded, codexOAuthTransformOptions{ResponsesLite: lite, PreserveHTTPNamespaceSearchHistory: http})
							require.NoError(t, result.Error)
							wire, err = json.Marshal(decoded)
						}
						require.NoError(t, err)
						require.JSONEq(t, gjson.Get(body, "input.0").Raw, gjson.GetBytes(wire, `input.#(type==web_search_call)`).Raw)
						if http {
							require.Equal(t, "auto", gjson.GetBytes(wire, "tool_choice").String())
							require.NotContains(t, string(wire), `"type":"web_search"`)
							require.Contains(t, string(wire), "9007199254740993")
							// A second compatibility pass must not start injecting tools.
							if raw {
								again, _, err := normalizeOpenAIResponsesCompatibilityBodyWithOptions(wire, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, openAIResponsesCompatibilityOptions{ResponsesLite: lite, PreserveHTTPNamespaceSearchHistory: true})
								require.NoError(t, err)
								require.JSONEq(t, string(wire), string(again))
							}
						} else {
							require.Equal(t, "allowed_tools", gjson.GetBytes(wire, "tool_choice.type").String())
							require.Contains(t, string(wire), `"type":"web_search"`)
						}
					})
				}
			}
		}
	}
}
