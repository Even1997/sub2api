package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const contextErrorInspectionLimit = 64 << 10

// A classifier may read ahead, but the normal error handler must still see
// every byte and any transport error that followed the prefix.
type inspectedUpstreamBody struct {
	io.ReadCloser
	prefix  *bytes.Reader
	readErr error
}

func (b *inspectedUpstreamBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	if b.readErr != nil {
		err := b.readErr
		b.readErr = nil
		return 0, err
	}
	return b.ReadCloser.Read(p)
}

func inspectUpstreamErrorBody(resp *http.Response, limit int64) []byte {
	if resp.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	resp.Body = &inspectedUpstreamBody{ReadCloser: resp.Body, prefix: bytes.NewReader(body), readErr: err}
	if err != nil || int64(len(body)) > limit {
		return nil
	}
	return body
}

// Inspect deterministic context rejections before retry/cooldown policy. Keep
// the original response available when it is not a recognized request error.
func (s *GatewayService) interceptAnthropicContextRejection(c *gin.Context, account *Account, resp *http.Response) bool {
	if account == nil || account.Platform != PlatformAnthropic || resp == nil || resp.Body == nil ||
		(resp.StatusCode != 400 && resp.StatusCode != 422 && resp.StatusCode != 502) {
		return false
	}
	if !IsUpstreamContextRejection(resp.StatusCode, inspectUpstreamErrorBody(resp, contextErrorInspectionLimit)) {
		return false
	}
	_ = resp.Body.Close()
	resp.Body = http.NoBody
	writeGatewayContextRejection(c, resp.StatusCode)
	return true
}

func writeGatewayContextRejection(c *gin.Context, upstreamStatus int) {
	const code = "context_length_exceeded"
	const message = "Input exceeds the model context window. Shorten or compact the conversation and try again."
	setOpsUpstreamError(c, upstreamStatus, code+": "+message, "")
	MarkOpsRequestScopedError(c, "invalid_request_error")
	MarkResponseCommitted(c)
	payload := gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "code": code, "message": message}}
	if c.Writer.Written() {
		if !strings.HasPrefix(strings.ToLower(c.Writer.Header().Get("Content-Type")), "text/event-stream") {
			return
		}
		MarkOpsStreamErrorValue(c, OpsStreamError{ErrType: "invalid_request_error", Code: code,
			Message: message, IntendedStatus: http.StatusBadRequest, RequestScoped: true})
		c.SSEvent("error", payload)
		c.Writer.Flush()
	} else {
		c.Writer.Header().Del("Content-Type") // The stream may have set its headers before its first write.
		c.JSON(http.StatusBadRequest, payload)
	}
}

var errGatewayContextRejection = errors.New("request context limit exceeded")
