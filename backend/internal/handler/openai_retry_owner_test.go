//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type retryOwnerBindings struct {
	service.GatewayCache
	owners map[string]int64
}

func (b *retryOwnerBindings) GetSessionAccountID(_ context.Context, _ int64, key string) (int64, error) {
	if owner := b.owners[key]; owner > 0 {
		return owner, nil
	}
	return 0, service.ErrStickySessionNotFound
}

func (b *retryOwnerBindings) SetSessionAccountID(_ context.Context, _ int64, key string, owner int64, _ time.Duration) error {
	b.owners[key] = owner
	return nil
}

func (b *retryOwnerBindings) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (b *retryOwnerBindings) DeleteSessionAccountID(_ context.Context, _ int64, key string) error {
	delete(b.owners, key)
	return nil
}

// The first attempt releases its real ConcurrencyService slot before another
// request occupies A. A barrier exposes the busy state to retry selection.
type retryOwnerSlots struct {
	concurrencyCacheMock
	mu       sync.Mutex
	released []int64
	acquired []int64
	busy     bool
	busySeen chan struct{}
	busyOnce sync.Once
}

func (s *retryOwnerSlots) AcquireAccountSlot(_ context.Context, id int64, _ int, _ string) (bool, error) {
	s.mu.Lock()
	busy := id == 1 && s.busy
	if !busy {
		s.acquired = append(s.acquired, id)
	}
	s.mu.Unlock()
	if busy {
		s.busyOnce.Do(func() { close(s.busySeen) })
	}
	return !busy, nil
}

func (s *retryOwnerSlots) GetAccountsLoadBatch(_ context.Context, accounts []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]*service.AccountLoadInfo, len(accounts))
	for _, a := range accounts {
		load := &service.AccountLoadInfo{AccountID: a.ID}
		if a.ID == 1 && s.busy {
			load.CurrentConcurrency = 1
			load.LoadRate = 100
		}
		out[a.ID] = load
	}
	return out, nil
}

func (s *retryOwnerSlots) ReleaseAccountSlot(_ context.Context, id int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, id)
	if len(s.released) == 1 {
		s.busy = true
	}
	return nil
}

type retryOwnerUpstream struct {
	service.HTTPUpstream
	mu  sync.Mutex
	ids []int64
}

func (u *retryOwnerUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.ids = append(u.ids, id)
	fail := len(u.ids) == 1
	u.mu.Unlock()
	if fail {
		return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error","message":"temporarily unavailable"}}`))}, nil
	}
	body, _ := io.ReadAll(req.Body)
	response := `{"id":"resp_retry","object":"response","status":"completed","model":"gpt-5.1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`
	contentType := "application/json"
	if gjson.GetBytes(body, "stream").Bool() {
		contentType = "text/event-stream"
		response = "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":" + response + "}\n\ndata: [DONE]\n\n"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}, nil
}

func TestOAuthRetryOwnerBusyAfterSlotRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := service.NewSettingService(&firstConcurrencySettingsRepo{values: map[string]string{}}, &config.Config{})
	require.NoError(t, settings.UpdateSettings(context.Background(), &service.SystemSettings{OpenAIAdvancedSchedulerEnabled: true}))
	t.Cleanup(func() { require.NoError(t, settings.UpdateSettings(context.Background(), &service.SystemSettings{})) })
	for _, scenario := range []string{"busy_timeout", "recovered", "canceled"} {
		for _, maxSwitches := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/switches=%d", scenario, maxSwitches), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				slots := &retryOwnerSlots{busySeen: make(chan struct{})}
				bindings := &retryOwnerBindings{owners: map[string]int64{}}
				upstream := &retryOwnerUpstream{}
				accounts := issue715Accounts()
				for i := range accounts {
					accounts[i].Concurrency = 1
					accounts[i].Priority = 1
				}
				h := newOpenAIResponsesFailoverTestHandlerWithConcurrency(t, upstream, bindings, settings, nil, service.NewConcurrencyService(slots), accounts...)
				h.maxAccountSwitches = maxSwitches
				h.cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				h.cfg.Gateway.Scheduling.StickySessionWaitTimeout = time.Second
				h.cfg.Gateway.Scheduling.FallbackWaitTimeout = time.Second
				h.cfg.Gateway.Scheduling.StickySessionMaxWaiting = 4
				h.cfg.Gateway.OpenAIWS.LBTopK = 2
				h.cfg.Gateway.Scheduling.LoadBatchEnabled = true
				workerDone := make(chan struct{})
				if scenario != "busy_timeout" {
					go func() {
						defer close(workerDone)
						select {
						case <-ctx.Done():
							return
						case <-slots.busySeen:
						}
						if scenario == "canceled" {
							cancel()
						} else {
							slots.mu.Lock()
							slots.busy = false
							slots.mu.Unlock()
						}
					}()
				} else {
					close(workerDone)
				}
				c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)
				body := `{"model":"gpt-5.1","input":"hello","stream":false,"prompt_cache_key":"retry-owner"}`
				c.Request.Body = io.NopCloser(strings.NewReader(body))
				c.Request.ContentLength = int64(len(body))
				session := h.gatewayService.GenerateSessionHash(c, []byte(body))
				require.NotEmpty(t, session)
				bindings.owners["openai:"+session] = 1
				h.Responses(c)
				select {
				case <-slots.busySeen:
				default:
					t.Fatal("retry never observed the released owner's occupied slot")
				}
				<-workerDone
				want := []int64{1}
				if scenario == "recovered" {
					want = []int64{1, 1}
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				}
				if scenario == "busy_timeout" {
					require.NotEqual(t, http.StatusOK, rec.Code, rec.Body.String())
				}
				require.Equal(t, want, upstream.ids, "B must receive zero sends, including with no switch budget")
				slots.mu.Lock()
				acquired := append([]int64(nil), slots.acquired...)
				released := append([]int64(nil), slots.released...)
				slots.mu.Unlock()
				require.Equal(t, acquired, released, "every actual attempt releases its slot")
				require.Equal(t, int64(1), bindings.owners["openai:"+session], "the session binding must still belong to A")
			})
		}
	}
}
