package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestOpenAIWSSSENamespaceSearchRouting(t *testing.T) {
	for _, tc := range []struct {
		name, path, value, mapping string
		nativeWS, wantBypass       bool
	}{
		{name: "sol", wantBypass: true},
		{name: "escaped_namespace", path: "tools.0.type", value: `"name\u0073pace"`, wantBypass: true},
		{name: "escaped_search", path: "input.0.type", value: `"web_search_\u0063all"`, wantBypass: true},
		{name: "astra", path: "model", value: `"gpt-6-astra"`, wantBypass: true},
		{name: "mapped_in", path: "model", value: `"client-alias"`, mapping: "gpt-6.1-sol", wantBypass: true},
		{name: "mapped_other_model", mapping: "gpt-5.1", wantBypass: true},
		{name: "new_model", path: "model", value: `"gpt-6.2-astra"`, wantBypass: true},
		{name: "native_ws", nativeWS: true},
		{name: "already_has_search", path: "tools.-1", value: `{"type":"web_search"}`},
		{name: "unfinished", path: "input.0.status", value: `"in_progress"`},
		{name: "compact_trigger", path: "input.-1", value: `{"type":"compaction_trigger"}`},
		{name: "no_namespace", path: "tools", value: `[{"type":"function","name":"record"}]`},
		{name: "explicit_none", path: "tool_choice", value: `"none"`},
		{name: "required", path: "tool_choice", value: `"required"`, wantBypass: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := sjson.Set(namespaceSearchHistory, "stream", true)
			require.NoError(t, err)
			if tc.path != "" {
				body, err = sjson.SetRaw(body, tc.path, tc.value)
				require.NoError(t, err)
			}
			a := wsSSETestAccount()
			if tc.mapping != "" {
				a.Credentials["model_mapping"] = map[string]any{gjson.Get(body, "model").String(): tc.mapping}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			if tc.nativeWS {
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
			}
			s := &OpenAIGatewayService{cfg: newOpenAIWSV2TestConfig()}
			d := s.resolveOpenAIHTTPWSSSEDecision(c, a, []byte(body), NewOpenAIWSProtocolResolver(s.cfg).Resolve(a))
			require.Equal(t, tc.wantBypass, d.Reason == openAINamespaceSearchHTTPReason)
			require.Equal(t, !tc.wantBypass, d.Transport == OpenAIUpstreamTransportResponsesWebsocketV2)
		})
	}
}

func TestOpenAIWSSSENamespaceSearchBypassesDialAndContinues(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6.2-astra", "future-model", "client-alias"} {
		for _, lite := range []bool{false, true} {
			for _, choice := range []string{"auto", "required"} {
				t.Run(fmt.Sprintf("%s/lite=%v/%s", model, lite, choice), func(t *testing.T) {
					body, err := sjson.Set(namespaceSearchHistory, "model", model)
					require.NoError(t, err)
					body, err = sjson.Set(body, "stream", true)
					require.NoError(t, err)
					body, err = sjson.Set(body, "tool_choice", choice)
					require.NoError(t, err)
					a := wsSSETestAccount()
					if model == "client-alias" {
						a.Credentials["model_mapping"] = map[string]any{model: "gpt-6.2-astra"}
					}
					d := &wsSSETestDialer{} // Any dial is a test failure; no WS connection is available.
					h := &httpUpstreamRecorder{}
					const call = `{"type":"function_call","id":"fc_record","call_id":"fc_record","namespace":"diagnostic","name":"record","arguments":"{\"value\":9007199254740993}"}`
					for _, output := range []string{"[" + call + "]", `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"receipt-ok"}]}]`} {
						h.responses = append(h.responses, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"id":"resp_test","output":` + output + `,"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"))})
					}
					s := wsSSETestService(t, d, h)
					for turn := 0; turn < 2; turn++ {
						if turn == 1 {
							for _, item := range []string{call, `{"type":"function_call_output","call_id":"fc_record","output":"receipt-ok"}`, `{"role":"user","content":"Continue with the recorded result"}`} {
								body, err = sjson.SetRaw(body, "input.-1", item)
								require.NoError(t, err)
							}
						}
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
						SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
						if lite {
							c.Request.Header.Set(responsesLiteHeader, "true")
						}
						result, err := s.Forward(context.Background(), c, a, []byte(body))
						require.NoError(t, err)
						require.NotNil(t, result)
						require.False(t, result.OpenAIWSMode)
						require.Zero(t, d.calls.Load())
						require.Len(t, h.requests, turn+1)
						require.Equal(t, openAINamespaceSearchHTTPReason, c.GetString("openai_ws_transport_reason"))
						require.Equal(t, choice, gjson.GetBytes(h.lastBody, "tool_choice").String())
						require.NotContains(t, string(h.lastBody), `"type":"web_search"`)
						require.JSONEq(t, gjson.Get(body, "input.0").Raw, gjson.GetBytes(h.lastBody, `input.#(type==web_search_call)`).Raw)
						if turn == 0 {
							require.Contains(t, rec.Body.String(), `"namespace":"diagnostic"`)
						} else {
							require.Contains(t, rec.Body.String(), "receipt-ok")
							require.Equal(t, "receipt-ok", gjson.GetBytes(h.lastBody, `input.#(type==function_call_output).output`).String())
							require.Equal(t, "diagnostic", gjson.GetBytes(h.lastBody, `input.#(type==function_call).namespace`).String())
						}
					}
				})
			}
		}
	}
}
