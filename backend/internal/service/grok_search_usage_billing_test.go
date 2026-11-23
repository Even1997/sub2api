//go:build unit

package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokSearchReceiptAcrossEndpoints(t *testing.T) {
	const item = `{"type":"web_search_call","id":"ws1","status":"completed"}`
	for _, tc := range []struct {
		name, details string
		want          int
	}{
		{"missing", "", 1}, {"explicit_zero", `{"x_search_calls":0}`, 0},
		{"successful_searches_only", `{"x_search_calls":2,"web_search_calls":1,"document_search_calls":3,"code_interpreter_calls":8,"x_posts_fetched":15}`, 6},
		{"only_other_tools", `{"code_interpreter_calls":2}`, 0},
		{"invalid_receipt", `{"x_search_calls":-2}`, 1},
	} {
		for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s%s/stream=%t", tc.name, path, stream), func(t *testing.T) {
					usage := `"input_tokens":2,"output_tokens":1`
					if tc.details != "" {
						usage += `,"server_side_tool_usage_details":` + tc.details
					}
					terminal := fmt.Sprintf(`{"id":"resp_test","object":"response","model":"grok-4.7","status":"completed","output":[%s],"usage":{%s}}`, item, usage)
					done := "data: " + `{"type":"response.output_item.done","item":` + item + "}\n\n"
					body := done + done + "data: " + `{"type":"response.completed","response":` + terminal + "}\n\n"
					ct := "text/event-stream"
					if path == "/v1/responses" && !stream {
						body = terminal
						ct = "application/json"
					}
					c, _ := grokAuditContext(path)
					acct := healthyGrokOAuthGatewayTestAccount(9904, "test-token")
					upstream := &httpUpstreamRecorder{resp: grokAuditHTTPResponse(ct, body)}
					svc := grokAuditOAuthService(upstream, acct)
					req := []byte(fmt.Sprintf(`{"model":"grok-4.7","messages":[{"role":"user","content":"search"}],"max_tokens":256,"stream":%t,"prompt_cache_key":"test-session"}`, stream))
					var res *OpenAIForwardResult
					var err error
					switch path {
					case "/v1/responses":
						req = []byte(fmt.Sprintf(`{"model":"grok-4.7","input":"search","stream":%t}`, stream))
						res, err = svc.forwardGrokResponses(context.Background(), c, acct, req, "grok-4.7", stream, time.Now())
					case "/v1/messages":
						res, err = svc.ForwardAsAnthropic(context.Background(), c, acct, req, "", "")
					default:
						res, err = svc.ForwardAsChatCompletions(context.Background(), c, acct, req, "", "")
					}
					require.NoError(t, err)
					require.NotNil(t, res)
					require.Equal(t, tc.want, res.SearchCount)
					require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/responses"))
				})
			}
		}
	}
}

func TestGrokRawChatSearchReceipt(t *testing.T) {
	for _, stream := range []bool{false, true} {
		c, _ := grokAuditContext("/v1/chat/completions")
		acct := healthyGrokOAuthGatewayTestAccount(9905, "test-token")
		ct, body := "application/json", grokAuditChatJSON("grok-4.7")
		if stream {
			ct, body = "text/event-stream", grokAuditChatSSE("grok-4.7")
		}
		body = strings.ReplaceAll(body, `"total_tokens":3`, `"total_tokens":3,"server_side_tool_usage_details":{"x_search_calls":2}`)
		upstream := &httpUpstreamRecorder{resp: grokAuditHTTPResponse(ct, body)}
		req := []byte(fmt.Sprintf(`{"model":"grok-4.7","messages":[{"role":"user","content":"hi"}],"n":1,"stream":%t}`, stream))
		res, err := grokAuditOAuthService(upstream, acct).ForwardAsChatCompletions(context.Background(), c, acct, req, "", "")
		require.NoError(t, err)
		require.Equal(t, grokChatRawEndpoint, res.UpstreamEndpoint)
		require.Equal(t, 2, res.SearchCount)
	}
}
