package service

import (
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

// A request-local description of the recovery boundary, with no client content,
// upstream messages, URLs, tokens or arbitrary identifiers.
type excelBPSStreamDiagnostics struct {
	started, lastEvent time.Time
	events             int
}

func (d *excelBPSStreamDiagnostics) record(c *gin.Context, account *Account, status int, failure *basispoints.UpstreamFailure, outputStarted, canRecover bool, reason string) {
	if c == nil || account == nil {
		return
	}
	now := time.Now()
	fields := map[string]any{
		"elapsed_ms": now.Sub(d.started).Milliseconds(), "event_count": d.events,
		"client_output_started": outputStarted, "safe_to_failover": canRecover,
		"recovery_decision": reason,
	}
	if !d.lastEvent.IsZero() {
		fields["last_event_age_ms"] = now.Sub(d.lastEvent).Milliseconds()
	}
	if failure != nil {
		fields["error_code"], fields["error_type"] = failure.Code, failure.Type
		fields["source_code"], fields["source_type"] = failure.SourceCode, failure.SourceType
	}
	detail, _ := json.Marshal(fields)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID,
		UpstreamStatusCode: status, Kind: "recovery_decision", Stage: "stream",
		Reason: reason, Message: "Excel BPS stream recovery decision: " + reason, Detail: string(detail),
	})
}
