//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSRecoveryFinalResponseScope(t *testing.T) {
	for name, send := range map[string]func(*gin.Context, *service.UpstreamFailoverError, bool){
		"openai":    (&OpenAIGatewayHandler{}).handleFailoverExhausted,
		"messages":  (&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted,
		"responses": (&GatewayHandler{}).handleResponsesFailoverExhausted,
		"chat":      (&GatewayHandler{}).handleCCFailoverExhausted,
	} {
		for _, status := range []int{403, 429, 500} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/enabled=%t", name, status, enabled), func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					failure := &service.UpstreamFailoverError{StatusCode: status, ResponseBody: []byte(`{"error":{"message":"upstream rejected"}}`), ClientMessage: "PRIVATE"}
					if enabled {
						failure.Reason = service.ExcelBPSRecoveryFailureReason
					}
					send(c, failure, false)
					if enabled {
						want := http.StatusServiceUnavailable
						if status == 429 {
							want = 429
						}
						require.Equal(t, want, recorder.Code)
						require.Contains(t, recorder.Body.String(), service.ExcelBPSRecoveryClientMessage)
					} else {
						require.NotContains(t, recorder.Body.String(), service.ExcelBPSRecoveryClientMessage)
						if status == 403 {
							want := http.StatusForbidden
							if name == "openai" || name == "messages" {
								want = http.StatusBadGateway
							}
							require.Equal(t, want, recorder.Code, "preserve each adapter's existing native mapping")
						}
					}
					require.NotContains(t, recorder.Body.String(), "PRIVATE")
				})
			}
		}
	}
}

func TestExcelBPSRecoveryHeartbeatGetsOneTerminal(t *testing.T) {
	for name, send := range map[string]func(*gin.Context, *service.UpstreamFailoverError, bool){
		"responses": (&GatewayHandler{}).handleResponsesFailoverExhausted,
		"chat":      (&GatewayHandler{}).handleCCFailoverExhausted,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Header("Content-Type", "text/event-stream")
			n, _ := c.Writer.WriteString(": keepalive\n\n")
			recordGatewayStreamHeartbeat(c, n)
			c.Writer.Flush()
			f := &service.UpstreamFailoverError{StatusCode: 403, Reason: service.ExcelBPSRecoveryFailureReason}
			send(c, f, true)
			require.Equal(t, 200, recorder.Code)
			require.Contains(t, recorder.Body.String(), service.ExcelBPSRecoveryClientMessage)
			before := recorder.Body.String()
			send(c, f, true)
			require.Equal(t, before, recorder.Body.String(), "do not append another terminal after semantic output")
		})
	}
}

func TestExcelBPSFileTimeoutOneReplacement(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, allFailed := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%t/all_failed=%t", enabled, allFailed), func(t *testing.T) {
				u := &excelBPSRecoveryUpstream{status: 400, wire: `{"error":{"code":"invalid_value","param":"url","message":"Unable to download content from the provided URL before the timeout. Check the URL."}}`, allFailed: allFailed}
				h := newOpenAIResponsesFailoverTestHandlerWithSettings(t, u, 4, newExcelBPSRecoverySettings(enabled), enableExcelBPSFailoverAccounts)
				c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
				c.Request = httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"hello"}`))
				h.Responses(c)
				if enabled {
					require.Equal(t, []int64{1, 2}, u.ids)
				} else {
					require.Equal(t, []int64{1}, u.ids)
				}
				if enabled && !allFailed {
					require.Contains(t, rec.Body.String(), "RECOVERED")
				} else {
					require.Equal(t, 400, rec.Code)
				}
			})
		}
	}
}

func TestExcelBPSKnownTransientTypeRecovery(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			u := &excelBPSRecoveryUpstream{status: 200, wire: "data: " + `{"type":"response.failed","response":{"status":"failed","error":{"type":"api_error","code":"UNKNOWN_PRIVATE","message":"PRIVATE"}}}` + "\n\n"}
			h := newOpenAIResponsesFailoverTestHandlerWithSettings(t, u, 2, newExcelBPSRecoverySettings(enabled), enableExcelBPSFailoverAccounts)
			c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
			c.Request = httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"hello"}`))
			h.Responses(c)
			if enabled {
				require.Equal(t, []int64{1, 1, 2}, u.ids, "retain the existing single same-account retry before failover")
				require.Contains(t, rec.Body.String(), "RECOVERED")
			} else {
				require.Equal(t, []int64{1}, u.ids)
			}
			require.NotContains(t, rec.Body.String(), "PRIVATE")
		})
	}
}
