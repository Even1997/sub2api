//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestIssue715HTTPExplicitRejectionCanMoveIntactToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("stream=%v/status=%d", stream, status), func(t *testing.T) {
				accounts := issue715Accounts()
				upstream := &issue715Upstream{portable: true}
				handler := newOpenAIResponsesFailoverTestHandlerWithCache(t, upstream, nil, accounts...)
				first := issue758Request(t, handler, issue715Body(false, stream))
				require.Equal(t, http.StatusOK, first.Code, first.Body.String())
				initial := issue758CompletedResponse(t, first.Body.String(), stream)
				require.Equal(t, "fc_715_once", initial.Get("output.0.call_id").String())
				// The client executes this one tool and submits only its saved result.
				balance, toolExecutions := 11, 0
				for _, item := range initial.Get("output").Array() {
					if item.Get("type").String() == "function_call" {
						balance += int(gjson.Get(item.Get("arguments").String(), "add").Int())
						toolExecutions++
					}
				}
				upstream.rejectStatus = status
				next := issue758Request(t, handler, issue715Body(true, stream))
				require.Equal(t, http.StatusOK, next.Code, next.Body.String())
				response := issue758CompletedResponse(t, next.Body.String(), stream)
				require.Empty(t, response.Get("output.#(type==function_call)").Raw)
				require.Contains(t, next.Body.String(), "Same task: ledger balance 18")
				require.Equal(t, []int64{1, 1, 2}, upstream.ids)
				for _, body := range upstream.bodies[1:] {
					require.JSONEq(t, issue715Agent, gjson.GetBytes(body, "input.#(type==agent_message)").Raw)
					require.Equal(t, "ledger balance 18; applied once", gjson.GetBytes(body, "input.#(type==function_call_output).output").String())
				}
				require.JSONEq(t, gjson.GetBytes(upstream.bodies[1], "input").Raw, gjson.GetBytes(upstream.bodies[2], "input").Raw)
				require.Equal(t, 18, balance)
				require.Equal(t, 1, toolExecutions)
			})
		}
	}
}
