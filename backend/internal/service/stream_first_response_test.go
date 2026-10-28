package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setStreamFirstResponseForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := gatewayForwardingCache.Load()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{streamFirstResponseEnabled: enabled, openAITTFTMode: OpenAITTFTModeVisible, expiresAt: time.Now().Add(time.Minute).UnixNano()})
	t.Cleanup(func() {
		if previous != nil {
			gatewayForwardingCache.Store(previous)
		} else {
			gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
		}
	})
}

func TestStreamFirstResponseClassification(t *testing.T) {
	for _, data := range []string{"", ": ping", "[DONE]", "null", "[]", "broken", `{"type":"ping"}`, `{"type":"keepalive"}`, `{"type":"heartbeat"}`} {
		require.False(t, isStreamFirstResponse(data, ""), data)
	}
	require.False(t, isStreamFirstResponse("{}", "ping"))
	for _, data := range []string{`{"type":"response.created"}`, `{"type":"message_start"}`, `{"choices":[{"delta":{"role":"assistant"}}]}`, `{"candidates":[]}`} {
		require.True(t, isStreamFirstResponse(data, ""), data)
	}
}

type firstResponseFlushWriter struct {
	*httptest.ResponseRecorder
	once    sync.Once
	flushed chan int
	start   time.Time
	match   string
}

func (w *firstResponseFlushWriter) Flush() {
	w.ResponseRecorder.Flush()
	match := w.match
	if match == "" {
		match = "response.created"
	}
	if strings.Contains(w.Body.String(), match) {
		w.once.Do(func() { w.flushed <- int(time.Since(w.start).Milliseconds()) })
	}
}

func firstResponseHTTPStream(ctx context.Context, c *gin.Context, body io.ReadCloser, start time.Time, passthrough bool) (*openaiStreamingResult, error) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	account := &Account{ID: 1, Platform: PlatformOpenAI}
	if passthrough {
		r, err := svc.handleStreamingResponsePassthrough(ctx, resp, c, account, start, "test", "test")
		if r == nil {
			return nil, err
		}
		return &openaiStreamingResult{usage: r.usage, firstTokenMs: r.firstTokenMs}, err
	}
	return svc.handleStreamingResponse(ctx, resp, c, account, start, "test", "test")
}

func TestStreamFirstResponseHTTPFlushAndTiming(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, passthrough := range []bool{false, true} {
			name := "native"
			if passthrough {
				name = "passthrough"
			}
			if enabled {
				name += "/on"
			} else {
				name += "/off"
			}
			t.Run(name, func(t *testing.T) {
				setStreamFirstResponseForTest(t, enabled)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				start := time.Now().Add(-time.Second)
				w := &firstResponseFlushWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 1), start: start}
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				reader, writer := io.Pipe()
				defer reader.Close()
				release := make(chan struct{})
				var once sync.Once
				releaseContent := func() { once.Do(func() { close(release) }) }
				defer releaseContent()
				metadataSent := make(chan struct{})
				go func() {
					defer writer.Close()
					_, _ = io.WriteString(writer, "data: "+`{"type":"response.created","response":{"id":"resp_first"}}`+"\n\n")
					close(metadataSent)
					<-release
					_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"hello"}`+"\n\n")
					_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"resp_first","usage":{"input_tokens":7,"output_tokens":2}}}`+"\n\n")
				}()
				var result *openaiStreamingResult
				var streamErr error
				done := make(chan struct{})
				go func() {
					defer close(done)
					result, streamErr = firstResponseHTTPStream(ctx, c, reader, start, passthrough)
				}()
				select {
				case <-metadataSent:
				case <-ctx.Done():
					t.Fatal("metadata was not read")
				}
				firstFlushMs := 0
				if enabled {
					select {
					case firstFlushMs = <-w.flushed:
					case <-ctx.Done():
						t.Fatal("metadata was buffered until content")
					}
				} else {
					select {
					case <-w.flushed:
						t.Fatal("disabled mode flushed metadata")
					case <-time.After(30 * time.Millisecond):
					}
				}
				contentReleasedMs := int(time.Since(start).Milliseconds())
				releaseContent()
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("stream did not finish")
				}
				require.NoError(t, streamErr)
				require.NotNil(t, result)
				require.NotNil(t, result.firstTokenMs)
				if enabled {
					require.LessOrEqual(t, *result.firstTokenMs, firstFlushMs)
				} else {
					require.GreaterOrEqual(t, *result.firstTokenMs, contentReleasedMs)
				}
				require.Equal(t, 7, result.usage.InputTokens)
				require.Equal(t, 2, result.usage.OutputTokens)
				require.Contains(t, w.Body.String(), "response.completed")
			})
		}
	}
}

func TestStreamFirstResponseHTTPFailureDoesNotFailover(t *testing.T) {
	setStreamFirstResponseForTest(t, true)
	for _, passthrough := range []bool{false, true} {
		for _, metadata := range []bool{false, true} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			prefix := ""
			if metadata {
				prefix = "data: " + `{"type":"response.created","response":{"id":"resp_first"}}` + "\n\n"
			}
			body := io.NopCloser(strings.NewReader(prefix + "data: " + `{"type":"response.failed","response":{"id":"resp_first","error":{"type":"server_error","code":"server_is_overloaded","message":"busy"}}}` + "\n\n"))
			_, err := firstResponseHTTPStream(context.Background(), c, body, time.Now(), passthrough)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			if metadata {
				require.False(t, errors.As(err, &failover))
				require.Contains(t, w.Body.String(), "response.created")
				require.Contains(t, w.Body.String(), "response.failed")
				continue
			}
			require.ErrorAs(t, err, &failover)
			require.True(t, failover.RetryableOnSameAccount)
			require.True(t, failover.IsOpenAICapacityShed())
			require.NotContains(t, w.Body.String(), "response.failed")
		}
	}
}

func TestStreamFirstResponseHTTPEmptyCompletion(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, passthrough := range []bool{false, true} {
			setStreamFirstResponseForTest(t, enabled)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := io.NopCloser(strings.NewReader("event: response.created\ndata: " + `{"type":"response.created","response":{"id":"resp_first"}}` + "\n\nevent: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"resp_first","output":[]}}` + "\n\n"))
			_, err := firstResponseHTTPStream(context.Background(), c, body, time.Now(), passthrough)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.Equal(t, !enabled, errors.As(err, &failover))
			if enabled {
				require.Contains(t, w.Body.String(), `"type":"response.failed"`)
				require.NotContains(t, w.Body.String(), `"type":"response.completed"`)
			} else {
				require.Empty(t, w.Body.String())
			}
		}
	}
}

func TestStreamFirstResponseRawChatUsageEvent(t *testing.T) {
	for _, usageEvent := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			setStreamFirstResponseForTest(t, enabled)
			start := time.Now().Add(-time.Second)
			w := &firstResponseFlushWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 1), start: start, match: "chat_first"}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			reader, writer := io.Pipe()
			defer reader.Close()
			release := make(chan struct{})
			var once sync.Once
			releaseContent := func() { once.Do(func() { close(release) }) }
			defer releaseContent()
			go func() {
				defer writer.Close()
				metadata := `{"id":"chat_first","choices":[{"index":0,"delta":{"role":"assistant"}}]}`
				if usageEvent {
					metadata = `{"id":"chat_first","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":0}}`
				}
				_, _ = io.WriteString(writer, "data: "+metadata+"\n\n")
				<-release
				_, _ = io.WriteString(writer, "data: "+`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`+"\n\ndata: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`+"\n\ndata: [DONE]\n\n")
			}()
			var result *OpenAIForwardResult
			var streamErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				svc := &OpenAIGatewayService{cfg: &config.Config{}}
				result, streamErr = svc.streamRawChatCompletions(c, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}, &Account{ID: 1, Platform: PlatformOpenAI}, "test", "test", "test", nil, nil, start, openAISilentRefusalMinRequestBodyBytes)
			}()
			firstFlushMs := 0
			if enabled {
				select {
				case firstFlushMs = <-w.flushed:
				case <-time.After(3 * time.Second):
					t.Fatal("usage-only first response was buffered")
				}
			} else if usageEvent {
				select {
				case <-w.flushed:
				case <-time.After(3 * time.Second):
					t.Fatal("legacy raw chat must still forward usage events")
				}
			} else {
				select {
				case <-w.flushed:
					t.Fatal("legacy raw chat must buffer role metadata while checking silent refusal")
				case <-time.After(30 * time.Millisecond):
				}
			}
			releasedMs := int(time.Since(start).Milliseconds())
			releaseContent()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("raw chat stream did not finish")
			}
			require.NoError(t, streamErr)
			require.NotNil(t, result.FirstTokenMs)
			if enabled {
				require.LessOrEqual(t, *result.FirstTokenMs, firstFlushMs)
			} else if usageEvent {
				require.GreaterOrEqual(t, *result.FirstTokenMs, releasedMs)
			} else {
				require.LessOrEqual(t, *result.FirstTokenMs, releasedMs)
			}
			require.Equal(t, 7, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
		}
	}
}

func TestStreamFirstResponseRawChatEmptyDoesNotSucceed(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		setStreamFirstResponseForTest(t, enabled)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		body := io.NopCloser(strings.NewReader("data: " + `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\ndata: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
		svc := &OpenAIGatewayService{cfg: &config.Config{}}
		_, err := svc.streamRawChatCompletions(c, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, &Account{ID: 1, Platform: PlatformOpenAI}, "test", "test", "test", nil, nil, time.Now(), openAISilentRefusalMinRequestBodyBytes)
		require.Error(t, err)
		var failover *UpstreamFailoverError
		require.Equal(t, !enabled, errors.As(err, &failover))
	}
}

func TestStreamFirstResponseProtocolConversionsFlushMetadata(t *testing.T) {
	setStreamFirstResponseForTest(t, true)
	for _, protocol := range []string{"openai-chat", "openai-messages", "gemini-messages", "gemini-chat", "antigravity-chat", "antigravity-responses"} {
		t.Run(protocol, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			start := time.Now().Add(-time.Second)
			match := "chat.completion.chunk"
			if strings.HasSuffix(protocol, "messages") {
				match = "message_start"
			}
			if strings.HasSuffix(protocol, "responses") {
				match = "response.created"
			}
			w := &firstResponseFlushWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 1), start: start, match: match}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			reader, writer := io.Pipe()
			defer reader.Close()
			release := make(chan struct{})
			var once sync.Once
			releaseContent := func() { once.Do(func() { close(release) }) }
			defer releaseContent()
			go func() {
				defer writer.Close()
				if strings.HasPrefix(protocol, "openai") {
					_, _ = io.WriteString(writer, "data: "+`{"type":"response.created","response":{"id":"resp_first","model":"test"}}`+"\n\n")
					<-release
					_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"hello"}`+"\n\n"+"data: "+`{"type":"response.completed","response":{"id":"resp_first","usage":{"input_tokens":7,"output_tokens":2}}}`+"\n\n")
				} else {
					metadata := `{"candidates":[]}`
					content := `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2}}`
					if strings.HasPrefix(protocol, "antigravity") {
						metadata = `{"response":` + metadata + "}"
						content = `{"response":` + content + "}"
					}
					_, _ = io.WriteString(writer, "data: "+metadata+"\n\n")
					<-release
					_, _ = io.WriteString(writer, "data: "+content+"\n\n")
				}
			}()
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
			var firstTokenMs *int
			var streamErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				openai := &OpenAIGatewayService{cfg: &config.Config{}}
				gemini := &GeminiMessagesCompatService{}
				antigravity := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
				switch protocol {
				case "openai-chat":
					r, err := openai.handleChatStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "test", "test", "test", start, 10)
					streamErr = err
					if r != nil {
						firstTokenMs = r.FirstTokenMs
					}
				case "openai-messages":
					r, err := openai.handleAnthropicStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "test", "test", "test", start)
					streamErr = err
					if r != nil {
						firstTokenMs = r.FirstTokenMs
					}
				case "gemini-messages":
					r, err := gemini.handleStreamingResponse(c, resp, start, "test")
					streamErr = err
					if r != nil {
						firstTokenMs = r.firstTokenMs
					}
				case "gemini-chat":
					r, err := gemini.handleChatCompletionsStreamingResponseFromGemini(c, resp, start, "test", false, true)
					streamErr = err
					if r != nil {
						firstTokenMs = r.firstTokenMs
					}
				case "antigravity-chat":
					r, err := antigravity.handleChatCompletionsStreamingFromAntigravity(c, resp, start, "test", true)
					streamErr = err
					if r != nil {
						firstTokenMs = r.firstTokenMs
					}
				case "antigravity-responses":
					r, err := antigravity.handleResponsesStreamingFromAntigravity(c, resp, start, "test")
					streamErr = err
					if r != nil {
						firstTokenMs = r.firstTokenMs
					}
				}
			}()
			var firstFlushMs int
			select {
			case firstFlushMs = <-w.flushed:
			case <-ctx.Done():
				t.Fatal("converted metadata was not flushed")
			}
			releaseContent()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("conversion did not finish")
			}
			require.NoError(t, streamErr)
			require.NotNil(t, firstTokenMs)
			require.LessOrEqual(t, *firstTokenMs, firstFlushMs)
			require.Contains(t, w.Body.String(), "hello")
		})
	}
}

type firstResponseSettingsRepo struct {
	SettingRepository
	values map[string]string
}

func (r *firstResponseSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return r.values, nil
}
func (r *firstResponseSettingsRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *firstResponseSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.values = values
	return nil
}

func TestStreamFirstResponseSettingsRoundTrip(t *testing.T) {
	setStreamFirstResponseForTest(t, false)
	repo := &firstResponseSettingsRepo{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.False(t, svc.parseSettings(repo.values).StreamFirstResponseEnabled)
	for _, enabled := range []bool{true, false} {
		require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{StreamFirstResponseEnabled: enabled}))
		require.Equal(t, enabled, svc.parseSettings(repo.values).StreamFirstResponseEnabled)
		require.Equal(t, enabled, svc.IsStreamFirstResponseEnabled(context.Background()))
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
		require.Equal(t, enabled, svc.IsStreamFirstResponseEnabled(context.Background()), "reload after cache expiry")
	}
}
