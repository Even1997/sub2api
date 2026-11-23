package handler

import (
	"bytes"
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const probeBillingSpoolLimit int64 = 64 << 20

// Opt-in settlement responses are spooled to a private temporary file. They
// cannot expose a successful usage receipt until the ledger commits.
type probeBillingSpool struct {
	gin.ResponseWriter
	file         *os.File
	header       http.Header
	status, size int
	stream       bool
	failure      error
}

func newProbeBillingSpool(writer gin.ResponseWriter, stream bool) (*probeBillingSpool, error) {
	file, err := os.CreateTemp("", "sub2api-probe-settlement-*")
	if err != nil {
		return nil, err
	}
	// Unlink on Unix while retaining the open descriptor: a process crash cannot
	// leave response data behind. Other platforms still remove it on close.
	_ = os.Remove(file.Name())
	return &probeBillingSpool{ResponseWriter: writer, file: file, header: writer.Header().Clone(), status: 200, size: -1, stream: stream}, nil
}
func (w *probeBillingSpool) Header() http.Header { return w.header }
func (w *probeBillingSpool) Status() int         { return w.status }
func (w *probeBillingSpool) Size() int           { return w.size }
func (w *probeBillingSpool) Written() bool       { return w.size >= 0 }
func (w *probeBillingSpool) WriteHeader(code int) {
	if !w.Written() {
		w.status = code
	}
}
func (w *probeBillingSpool) WriteHeaderNow() {
	if w.size < 0 {
		w.size = 0
	}
}
func (w *probeBillingSpool) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *probeBillingSpool) Write(b []byte) (int, error) {
	if w.stream && probeBillingComment(b) {
		// Queue/upstream keepalives can arrive before the actual protocol is
		// known, or between lines of a JSON fallback. They are not response data.
		return len(b), nil
	}
	w.WriteHeaderNow()
	if int64(w.size)+int64(len(b)) > probeBillingSpoolLimit {
		w.failure = errors.New("probe penalty response exceeds 64 MiB")
		return 0, w.failure
	}
	n, err := w.file.Write(b)
	w.size += n
	if err != nil {
		w.failure = err
	}
	return n, err
}

func probeBillingComment(body []byte) bool {
	comment := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if line[0] != ':' {
			return false
		}
		comment = true
	}
	return comment
}

func (w *probeBillingSpool) close() { _ = w.file.Close(); _ = os.Remove(w.file.Name()) }
func (w *probeBillingSpool) commitHeaders() {
	for k, v := range w.header {
		w.ResponseWriter.Header()[k] = v
	}
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.Header().Del("ETag")
	w.ResponseWriter.WriteHeader(w.status)
}
