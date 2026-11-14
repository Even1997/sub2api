package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// 分组开启缓存读取降低比例后，客户端看到的 usage 必须与本地计费口径一致：
// 一部分 cache_read 被挪回普通 input，而本地记账仍用上游原始 usage。
func TestAnthropicPassthroughNonStreamingAppliesGroupCacheReadReduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("api_key", &APIKey{Group: &Group{CacheReadReductionPercent: 50}})

	body := `{"id":"msg_1","type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":90}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	svc := &GatewayService{cfg: &config.Config{}}
	usage, err := svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1})
	require.NoError(t, err)

	require.Equal(t, 10, usage.InputTokens, "本地计费必须保留上游原始 input_tokens")
	require.Equal(t, 90, usage.CacheReadInputTokens, "本地计费必须保留上游原始 cache_read")

	written := rec.Body.Bytes()
	require.Equal(t, 55, int(gjson.GetBytes(written, "usage.input_tokens").Int()))
	require.Equal(t, 45, int(gjson.GetBytes(written, "usage.cache_read_input_tokens").Int()))
	require.Equal(t, 3, int(gjson.GetBytes(written, "usage.output_tokens").Int()))
	require.Equal(t, "ok", gjson.GetBytes(written, "content.0.text").String(), "除 usage 外的字段必须原样保留")
}

func TestCacheReadReductionDynamicAnthropicPartialStream(t *testing.T) {
	for _, name := range []string{"standard", "passthrough", "native"} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			ctx := context.WithValue(context.Background(), ctxkey.Model, "requested")
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			policy := CacheReadReductionRule{Model: "requested", Percent: 5, StartHitRatePercent: float64Ptr(80)}
			c.Set("api_key", &APIKey{Group: &Group{CacheReadReductionRules: []CacheReadReductionRule{policy}}})
			body := "data: {\"type\":\"message_start\",\"message\":{\"model\":\"mapped\",\"usage\":{\"input_tokens\":40,\"cache_read_input_tokens\":960,\"output_tokens\":0}}}\n\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":80,\"output_tokens\":10}}\n\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":20}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}
			var result *streamingResult
			var err error
			switch name {
			case "passthrough":
				result, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(ctx, resp, c, &Account{ID: 1}, time.Now(), "requested")
			case "native":
				native := &OpenAIGatewayService{cfg: &config.Config{}}
				forward, nativeErr := native.handleNativeAnthropicStreamingResponse(ctx, resp, c, &Account{ID: 1}, "requested", "mapped", "mapped", nil, time.Now())
				require.NoError(t, nativeErr)
				result = &streamingResult{usage: &ClaudeUsage{InputTokens: forward.Usage.InputTokens - forward.Usage.CacheReadInputTokens, CacheReadInputTokens: forward.Usage.CacheReadInputTokens, OutputTokens: forward.Usage.OutputTokens}}
			default:
				result, err = svc.handleStreamingResponse(ctx, resp, c, &Account{ID: 1}, time.Now(), "requested", "mapped", false)
			}
			require.NoError(t, err)
			require.Equal(t, 80, result.usage.InputTokens, "billing retains original upstream buckets")
			require.Equal(t, 960, result.usage.CacheReadInputTokens)
			var client ClaudeUsage
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				if data, ok := extractAnthropicSSEDataLine(line); ok {
					parseSSEUsagePassthrough(data, &client)
				}
			}
			// 960 / 1040 hit rate => 3.076923% reduction => 30 cached tokens moved.
			require.Equal(t, 110, client.InputTokens)
			require.Equal(t, 930, client.CacheReadInputTokens)
			require.Equal(t, 20, client.OutputTokens)
			billed := *result.usage
			applyClaudeCacheReadReduction(&billed, cacheReadReductionPercent(policy, 1040, 960))
			require.Equal(t, billed, client)
		})
	}
}

func TestCacheReadReductionDynamicAntigravityClaudeStream(t *testing.T) {
	svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(context.Background(), ctxkey.Model, "requested"))
	c.Set("api_key", &APIKey{Group: &Group{CacheReadReductionRules: []CacheReadReductionRule{{Model: "requested", Percent: 5, StartHitRatePercent: float64Ptr(80)}}}})
	body := `data: {"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":960,"candidatesTokenCount":20}}}` + "\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	result, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "requested")
	require.NoError(t, err)
	require.Equal(t, 960, result.usage.CacheReadInputTokens)
	var client ClaudeUsage
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if data, ok := extractAnthropicSSEDataLine(line); ok {
			parseSSEUsagePassthrough(data, &client)
		}
	}
	require.Equal(t, 78, client.InputTokens)
	require.Equal(t, 922, client.CacheReadInputTokens)
	require.Equal(t, 20, client.OutputTokens)
}

func TestAnthropicPassthroughNonStreamingKeepsUsageWhenGroupPercentZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("api_key", &APIKey{Group: &Group{}})

	body := `{"id":"msg_1","type":"message","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":90}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	svc := &GatewayService{cfg: &config.Config{}}
	_, err := svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1})
	require.NoError(t, err)

	require.JSONEq(t, body, rec.Body.String(), "未配置比例时响应必须逐字节等价于上游")
}

func TestReduceClientVisibleSSEFramePreservesEventAndSeparator(t *testing.T) {
	frame := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":90,\"output_tokens\":1}}}\n\n"

	got := reduceClientVisibleSSEBlock(frame, 50)

	require.Contains(t, got, "event: message_start\n")
	require.True(t, strings.HasSuffix(got, "\n\n"))
	dataLine := strings.SplitN(got, "\n", 3)[1]
	require.Equal(t, 55, int(gjson.Get(dataLine, "message.usage.input_tokens").Int()))
	require.Equal(t, 45, int(gjson.Get(dataLine, "message.usage.cache_read_input_tokens").Int()))
}

func TestReduceClientVisibleSSEFrameKeepsNonUsageEventsByteIdentical(t *testing.T) {
	frame := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"

	require.Equal(t, frame, reduceClientVisibleSSEBlock(frame, 50))
}

func TestDownstreamCacheReadReductionPercentReadsGroupSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.Zero(t, downstreamCacheReadReductionPercent(c), "未鉴权请求按 0 处理")

	c.Set("api_key", &APIKey{Group: &Group{CacheReadReductionPercent: 37.5}})
	require.InDelta(t, 37.5, downstreamCacheReadReductionPercent(c), 1e-9)
}
