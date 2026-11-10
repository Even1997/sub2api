package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type selectionBalanceCache struct {
	service.BillingCache
	checks int
}

func (s *selectionBalanceCache) GetUserBalance(context.Context, int64) (float64, error) {
	s.checks++
	return -0.02, nil
}

// Exercise real mixed routing: the initial billing check is deferred, and
// selection must return the billing rejection without diagnosing model capacity.
func TestOpenAISelectionInsufficientBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages", "/ws"} {
		for _, streaming := range []string{"false", "true"} {
			t.Run(path+"/stream="+streaming, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Gateway.OpenAIWS.Enabled = true
				cfg.Gateway.OpenAIWS.APIKeyEnabled = true
				cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
				cache := &selectionBalanceCache{}
				billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				keys := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 99, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true,
					SubscriptionType: "standard", RateMultiplier: 1, AllowMessagesDispatch: true}
				key, state, err := keys.PrepareAPIKeyRouting(context.Background(), &service.APIKey{
					ID: 100, UserID: 101, User: &service.User{ID: 101, Status: service.StatusActive},
					MixedMode: true, MixedGroupIDs: []int64{99}, MixedGroups: []*service.Group{group},
				})
				require.NoError(t, err)
				require.NotNil(t, state)
				repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{{
					ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.test"},
					Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true,
						"openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModePassthrough},
				}}}
				// A nil upstream deliberately fails if a rejected request is forwarded.
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
					service.NewBillingService(cfg, nil), nil, billing, nil, &service.DeferredService{},
					nil, nil, nil, nil, nil, nil, nil)
				h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, keys,
					nil, nil, nil, nil, cfg)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID})
					c.Request = c.Request.WithContext(service.WithAPIKeyRoutingState(c.Request.Context(), state))
				})
				router.POST("/v1/responses", h.Responses)
				router.POST("/v1/chat/completions", h.ChatCompletions)
				router.POST("/v1/messages", h.Messages)
				router.GET("/v1/responses", h.ResponsesWebSocket)
				if path == "/ws" {
					server := httptest.NewServer(router)
					defer server.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
					require.NoError(t, err)
					defer conn.CloseNow()
					require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hello","stream":`+streaming+`}`)))
					_, _, err = conn.Read(ctx)
					require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
					require.ErrorContains(t, err, service.InsufficientUserBalanceMessage)
					return
				}
				body := `{"model":"gpt-6.1-sol","input":"hello","max_tokens":10,"messages":[{"role":"user","content":"hello"}],"stream":` + streaming + `}`
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), `"type":"billing_error"`)
				require.Contains(t, rec.Body.String(), service.InsufficientUserBalanceMessage)
				require.Equal(t, 1, cache.checks, "balance rejection must not retry")
				require.Empty(t, rec.Header().Get("Retry-After"))
			})
		}
	}
}
