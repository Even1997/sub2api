package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var excelBPSReplay basispoints.ReplayCache
var excelBPSCatalog basispoints.CatalogCache

type excelBPSSchedulingKey struct{}

const excelBPSUsagePolicyMessage = "this request was blocked by our usage policy"

func excelBPSErrorTypeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		if status >= 500 {
			return "server_error"
		}
		return "invalid_request_error"
	}
}

func isExcelBPSUsagePolicyRejection(status int, raw []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(raw, "error.code").String())) {
	case "content_policy_violation", "cyber_policy":
		return true
	}
	message := strings.TrimSpace(extractUpstreamErrorMessage(raw))
	if message == "" && !json.Valid(raw) {
		message = strings.TrimSpace(string(raw))
	}
	message = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(message), "403:"))
	message = strings.TrimSuffix(message, ".")
	return message == excelBPSUsagePolicyMessage
}

// WithExcelBPSScheduling keeps BPS cooldowns out of native-only routes and
// requests whose capabilities require the native Codex fallback.
func WithExcelBPSScheduling(ctx context.Context, body []byte) context.Context {
	if ctx.Value(excelBPSFailoverRouteKey{}) == nil {
		ctx = context.WithValue(ctx, excelBPSFailoverRouteKey{}, &excelBPSFailoverRoute{})
	}
	return context.WithValue(ctx, excelBPSSchedulingKey{}, basispoints.NativeFallbackReason(body) == "")
}

func (s *OpenAIGatewayService) coolDownExcelBPS(ctx context.Context, account *Account, headers http.Header, responseBody []byte) {
	disposition, _ := classifyOpenAIOAuth429(headers, responseBody)
	if disposition != openAIOAuth429Transient && s.rateLimitService != nil {
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		s.rateLimitService.handle429Cooldown(stateCtx, account, headers, responseBody)
		s.openaiOAuth429RetryStartedAt.Delete(account.ID)
		return
	}

	cooldown := time.Duration(defaultRateLimit429CooldownSeconds) * time.Second
	if s.rateLimitService != nil {
		if configured, enabled := s.rateLimitService.get429FallbackCooldown(ctx, account); enabled {
			cooldown = configured
		}
	}
	now := time.Now()
	if reset := parseRetryAfterResetTime(headers, now); reset != nil && reset.After(now) {
		cooldown = min(reset.Sub(now), time.Duration(maxRateLimit429CooldownSeconds)*time.Second)
	}
	until := now.Add(cooldown)
	for {
		old, loaded := s.excelBPSRateLimitedUntil.LoadOrStore(account.ID, until)
		if !loaded || !until.After(old.(time.Time)) {
			return
		}
		if s.excelBPSRateLimitedUntil.CompareAndSwap(account.ID, old, until) {
			return
		}
	}
}

func (s *OpenAIGatewayService) isExcelBPSCoolingDown(ctx context.Context, account *Account, requestedModel string) bool {
	if s == nil || account == nil {
		return false
	}
	if s.isExcelBPSRateLimited(account, requestedModel) {
		return true
	}
	if !account.IsExcelBPSEnabledForModel(requestedModel) || !s.excelBPSRecoveryEnabled(ctx) {
		return false
	}
	if bps, known := ctx.Value(excelBPSSchedulingKey{}).(bool); known && !bps {
		return false
	}
	key := excelBPSRateLimitKey{account.ID, account.GetMappedModel(requestedModel)}
	if value, ok := s.excelBPSModelRateLimits.Load(key); ok {
		state := value.(excelBPSRateLimitState)
		if time.Now().Before(state.until) {
			return true
		}
		if !time.Now().Before(state.until.Add(excelBPSRateLimitStreakTTL)) {
			s.excelBPSModelRateLimits.CompareAndDelete(key, value)
		}
	}
	return false
}

func (s *OpenAIGatewayService) isExcelBPSRateLimited(account *Account, model string) bool {
	if !account.IsExcelBPSEnabledForModel(model) {
		return false
	}
	value, ok := s.excelBPSRateLimitedUntil.Load(account.ID)
	if !ok {
		return false
	}
	if time.Now().Before(value.(time.Time)) {
		return true
	}
	s.excelBPSRateLimitedUntil.CompareAndDelete(account.ID, value)
	return false
}

// A failed BPS credential pauses the whole account until repaired and resumed.
// Keep invalidation scoped to the credential actually rejected by the upstream.
func (s *OpenAIGatewayService) handleExcelBPSUnauthorized(ctx context.Context, account *Account, status int, headers http.Header, raw []byte, tokens ...string) {
	if status != http.StatusUnauthorized || isQualityObservation(ctx) {
		return
	}
	recoveryEnabled := s.excelBPSRecoveryEnabled(ctx)
	if recoveryEnabled {
		defer s.pauseExcelBPSCredentials(ctx, account)
	}
	if s.excelOAuthReauth != nil && account.GetCredential("client_id") != openai.ExcelClientID {
		if len(tokens) > 0 {
			stateCtx, cancel := openAIAccountStateContext(ctx)
			defer cancel()
			var cache OpenAITokenCache
			if s.openAITokenProvider != nil {
				cache = s.openAITokenProvider.tokenCache
			}
			if err := s.excelOAuthReauth.invalidateExcelAccessToken(stateCtx, account.ID, tokens[0], cache); err != nil {
				logger.LegacyPrintf("service.openai_excel_bps", "Excel grant invalidation failed: account_id=%d", account.ID)
			}
		}
		return
	}
	if recoveryEnabled {
		if s.rateLimitService != nil && s.rateLimitService.tokenCacheInvalidator != nil && account.IsOAuth() {
			stateCtx, cancel := openAIAccountStateContext(ctx)
			defer cancel()
			_ = s.rateLimitService.tokenCacheInvalidator.InvalidateToken(stateCtx, account)
		}
		return
	}
	// With recovery disabled retain the existing single-grant 401 policy.
	if s.rateLimitService == nil {
		return
	}
	fields := map[string]string{"message": "Excel BPS authentication failed"}
	code := extractUpstreamErrorCode(raw)
	if code == "token_invalidated" || code == "token_revoked" {
		fields["code"] = code
	}
	authError := map[string]any{"error": fields}
	if gjson.GetBytes(raw, "detail").String() == "Unauthorized" {
		authError["detail"] = "Unauthorized"
	}
	body, _ := json.Marshal(authError)
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	s.rateLimitService.HandleUpstreamError(stateCtx, account, status, headers, body)
}

func (s *OpenAIGatewayService) moveExcelBPSOn403(ctx context.Context, account *Account) bool {
	target, enabled := account.ExcelBPS403GroupTarget()
	if !enabled || isQualityObservation(ctx) {
		return false
	}
	repo, ok := s.accountRepo.(AccountExcelBPSGroupRepository)
	if !ok {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	changed, err := repo.MoveExcelBPSOn403(stateCtx, account)
	if err != nil {
		logger.LegacyPrintf("service.openai_excel_bps", "automatic group action failed: account_id=%d error_type=%T", account.ID, err)
		return false
	}
	if changed {
		logger.LegacyPrintf("service.openai_excel_bps", "automatically updated groups after upstream HTTP 403: account_id=%d target_group_id=%d", account.ID, target)
	}
	return changed
}

func (s *OpenAIGatewayService) disableExcelBPSOn403(ctx context.Context, account *Account) bool {
	if !account.IsExcelBPSAutoDisableOn403Enabled() || isQualityObservation(ctx) {
		return false
	}
	repo, ok := s.accountRepo.(AccountExcelBPSRepository)
	if !ok {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	changed, err := repo.DisableExcelBPSOn403(stateCtx, account)
	if err != nil {
		// Do not log upstream bodies, credentials or database query arguments.
		logger.LegacyPrintf("service.openai_excel_bps", "auto-disable failed: account_id=%d error_type=%T", account.ID, err)
		return false
	}
	if changed {
		logger.LegacyPrintf("service.openai_excel_bps", "automatically disabled Excel BPS after upstream HTTP 403: account_id=%d", account.ID)
	}
	return changed
}

func (s *OpenAIGatewayService) excelBPSImageRelay(ctx context.Context) (*basispoints.ImageRelay, error) {
	settings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.excelBPSImageRelayForSettings(settings)
}

func (s *OpenAIGatewayService) excelBPSImageRelayForSettings(settings ExcelBPSImageRelaySettings) (*basispoints.ImageRelay, error) {
	if !settings.Enabled || settings.Mode == ExcelBPSImageModeNative {
		if s != nil {
			s.excelBPSImagesMu.Lock()
			if s.excelBPSImages != nil {
				_ = s.excelBPSImages.Close()
				s.excelBPSImages = nil
			}
			s.excelBPSImagesMu.Unlock()
		}
		return nil, nil
	}
	origin := settings.BaseURL
	if s.cfg != nil && s.cfg.Runtime.ImageRelayOwnerOrigin != "" {
		if err := config.ValidateImageRelayOwnerOrigin(s.cfg.Runtime.ImageRelayOwnerOrigin); err != nil {
			return nil, err
		}
		origin = strings.TrimRight(s.cfg.Runtime.ImageRelayOwnerOrigin, "/")
	}
	var err error
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	if s.excelBPSImages == nil {
		dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
		if dataDir == "" {
			dataDir = "./data"
		}
		s.excelBPSImages, err = basispoints.NewImageRelay(origin, filepath.Join(dataDir, "bps-images"))
	}
	if err == nil {
		err = s.excelBPSImages.Configure(origin, settings.Limits)
	}
	return s.excelBPSImages, err
}

func (s *OpenAIGatewayService) CloseExcelBPSImages() error {
	if s == nil {
		return nil
	}
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	return s.excelBPSImages.Close()
}

// ServeExcelBPSImage allows the upstream to retrieve an unguessable temporary URL.
func (s *OpenAIGatewayService) ServeExcelBPSImage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	relay, err := s.excelBPSImageRelay(c.Request.Context())
	if err != nil || relay == nil {
		http.NotFound(c.Writer, c.Request)
		return
	}
	relay.ServeHTTP(c.Writer, c.Request)
}

func excelBPSAccountID(account *Account, accessToken string) string {
	if accountID := strings.TrimSpace(account.GetChatGPTAccountID()); accountID != "" {
		return accountID
	}
	claims, err := openai.DecodeIDToken(accessToken)
	if err != nil || claims.OpenAIAuth == nil {
		return ""
	}
	return strings.TrimSpace(claims.OpenAIAuth.ChatGPTAccountID)
}

func newExcelBPSRequest(ctx context.Context, body []byte, token, accountID string) (*http.Request, error) {
	return newExcelBPSRequestTo(ctx, basispoints.ResponsesURL, "text/event-stream", body, token, accountID)
}

func newExcelBPSRequestTo(ctx context.Context, targetURL, accept string, body []byte, token, accountID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + token}, "Chatgpt-Account-Id": {accountID}, "X-Openai-Account-Id": {accountID},
		"X-Basispoints-Auth-Mode": {"chatgpt"}, "Content-Type": {"application/json"}, "Accept": {accept},
		"Origin": {"https://bps.openai.com"}, "User-Agent": {"Mozilla/5.0"},
		"X-Openai-Internal-Basispoints-Client-Product":       {"basispoints-excel-plugin"},
		"X-Openai-Internal-Basispoints-Client-Agent-Profile": {"excel"},
	}
	return req, nil
}

// BPS deliberately bypasses Codex ticket/cookie injection and OAuth plugins:
// only the selected account's bearer and ChatGPT account ID belong on this host.
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time) (forwardResult *OpenAIForwardResult, forwardErr error) {
	ctx = s.withExcelBPSRecovery(ctx)
	recoveryEnabled := s.excelBPSRecoveryEnabled(ctx)
	// Compaction and tool repairs belong to the same selected-account attempt.
	var policyRejectionOnce sync.Once
	recordPolicyRejection := func() {
		policyRejectionOnce.Do(func() { s.recordExcelBPSPolicyRejection(ctx, account) })
	}
	var compactUsage OpenAIUsage
	var compactID string
	originalImagePolicyModel := gjson.GetBytes(body, "model").String()
	var compactEffort string
	defer func() {
		if compactID == "" || !openAIUsageHasTokens(&compactUsage) {
			return
		}
		if forwardResult == nil {
			forwardResult = &OpenAIForwardResult{Model: originalImagePolicyModel, ReasoningEffort: &compactEffort, UpstreamModel: gjson.GetBytes(body, "model").String(), UpstreamEndpoint: "/basispoints/api/responses", RequestID: compactID, Stream: gjson.GetBytes(body, "stream").Bool(), Duration: time.Since(start)}
		}
		addImagePolicyUsage(&forwardResult.Usage, compactUsage)
	}()
	fail := func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
		// A compact keepalive may already have committed SSE headers. Otherwise
		// finish a single JSON response so the handler cannot append another error.
		committed := StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		if committed {
			failureParam := ""
			if len(param) > 0 {
				failureParam = param[0]
			}
			writeOpenAICompactSSEFailureMessageParam(c, status, code, message, failureParam)
		} else {
			errorType := excelBPSErrorTypeForStatus(status)
			errorBody := gin.H{"type": errorType, "code": code, "message": message}
			if len(param) > 0 && param[0] != "" {
				errorBody["param"] = param[0]
			}
			c.JSON(status, gin.H{"error": errorBody})
		}
		return nil, &excelBPSForwardError{code: code}
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	var rateLimitOnce sync.Once
	recordRateLimit := func(headers http.Header, raw []byte, failure *basispoints.UpstreamFailure, httpStatus int) {
		rateLimitOnce.Do(func() { s.coolDownExcelBPSAttempt(ctx, account, model, headers, raw, failure, httpStatus) })
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	clientCanceled := func() (*OpenAIForwardResult, error) {
		StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		MarkOpsClientCancellation(c, stream)
		// No response or metered usage exists before headers; do not create a usage row.
		return nil, context.Canceled
	}
	failoverRateLimited := func(retryAfter string) (*OpenAIForwardResult, error) {
		lockExcelBPSFailoverRoute(ctx, c, account, originalModel)
		headers := make(http.Header)
		if retryAfter = strings.TrimSpace(retryAfter); retryAfter != "" {
			headers.Set("Retry-After", retryAfter)
		}
		if !isQualityObservation(ctx) {
			s.coolDownExcelBPS(ctx, account, headers, nil)
		}
		if isExcelBPSClientCancellation(c, ctx.Err()) {
			return clientCanceled()
		}
		failover := s.openAIUpstreamFailoverError(
			http.StatusTooManyRequests,
			headers,
			[]byte(`{"error":{"type":"rate_limit_error","code":"basispoints_rate_limited","message":"Excel BPS rate limit exceeded"}}`),
			"Excel BPS rate limit exceeded",
			false,
		)
		failover.SuppressScheduleFailure = true
		failover.SkipOAuth429AttemptLimit = true
		return nil, failover
	}
	// A queued/sticky selection may predate another request's credential
	// failure. Recheck before preparation, compaction or any upstream send.
	if isExcelBPSClientCancellation(c, ctx.Err()) {
		return clientCanceled()
	}
	if s.isExcelBPSCredentialCoolingDown(ctx, account, originalModel) {
		return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, http.StatusBadGateway, nil, "basispoints_auth_cooling_down", false)
	}
	imageSettings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
	}
	// Reserve before the body is rewritten or converted. A rejection has not
	// written anything yet, so the handler may switch to another account.
	releaseImageAdmission, err := s.admitExcelBPSImageRequest(c, imageSettings, len(body))
	if err != nil {
		return nil, err
	}
	defer releaseImageAdmission()
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid model request")
	}
	identity, transient := resolveExcelBPSIdentity(c, body, getAPIKeyIDFromContext(c), account.IsExcelBPSMihomoEnabled())
	if identity != "" {
		body, err = sjson.SetBytes(body, "prompt_cache_key", identity)
		if err != nil {
			return nil, err
		}
	}
	if isOpenAIResponsesCompactPath(c) {
		var request map[string]any
		if err = json.Unmarshal(body, &request); err != nil {
			return fail(400, "basispoints_request_invalid", "Invalid compact request")
		}
		var input []any
		switch v := request["input"].(type) {
		case []any:
			input = v
		case string:
			input = []any{map[string]any{"role": "user", "content": v}}
		default:
			return fail(400, "basispoints_request_invalid", "Compact requires input")
		}
		request["input"] = append(input, map[string]any{"type": "compaction_trigger"})
		request["tool_choice"] = "none"
		body, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	scope := fmt.Sprintf("account:%d/key:%d/thread:%s", account.ID, getAPIKeyIDFromContext(c), identity)
	if transient {
		scope = "transient:" + scope
	}
	imageSettings, err = s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
	}
	if !imageSettings.Enabled && account.IsExcelBPSIgnoreImagesEnabled() {
		body, err = basispoints.StripInputImages(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	if err = basispoints.ValidateNewAgentMessage(body); err != nil {
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if account.IsExcelBPSIgnoreEncryptedContentEnabled() {
		body, err = basispoints.StripEncryptedContent(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	imagePolicy, err := s.prepareExcelImagePolicy(ctx, c, body, imageSettings, scope+"/model:"+model, identity != "" && !transient)
	if err != nil {
		var policyErr *excelImagePolicyError
		if errors.As(err, &policyErr) {
			return fail(policyErr.Status, policyErr.Code, policyErr.Message)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if len(imagePolicy.reconciledBody) > 0 {
		maxImageMiB, maxTotalMiB := imageSettings.Limits.MaxImageMiB, imageSettings.Limits.MaxTotalMiB
		if imageSettings.Mode == ExcelBPSImageModeNative {
			maxImageMiB, maxTotalMiB = 20, 32
		}
		if err = basispoints.ValidateImageBudget(body, maxImageMiB, maxTotalMiB); err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
		body = imagePolicy.reconciledBody
	}
	var compactOutput []any
	var compactUsageWire map[string]any
	var images *basispoints.NativeImages
	if imageSettings.Enabled && imageSettings.Mode == ExcelBPSImageModeNative {
		images, err = basispoints.PrepareNativeImagesWithLimit(body, imagePolicy.maxImages)
		if err == nil {
			body, err = images.Body()
		}
	} else {
		var relay *basispoints.ImageRelay
		relay, err = s.excelBPSImageRelayForSettings(imageSettings)
		if err != nil {
			return fail(503, "basispoints_image_relay_unavailable", "Excel BPS image relay is unavailable")
		}
		body, err = relay.RewriteWithImageLimit(body, scope, imagePolicy.maxImages)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrImageRelayFull) {
			return fail(503, "basispoints_image_relay_full", err.Error())
		}
		if errors.Is(err, basispoints.ErrImageRelayStorage) {
			return fail(503, "basispoints_image_relay_unavailable", err.Error())
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	// Validate the complete request before uploading any attachments. The native
	// plan contains valid placeholder IDs until all local protocol checks pass.
	var replay *basispoints.ReplayCache
	var catalog *basispoints.CatalogCache
	if identity != "" {
		replay, catalog = &excelBPSReplay, &excelBPSCatalog
		if s.cfg != nil && s.cfg.Gateway.ExcelBPSStateRedisEnabled && !transient {
			store, ok := s.cache.(basispoints.StateStore)
			if !ok {
				return fail(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
			}
			replay, catalog = basispoints.NewPersistentCaches(ctx, store)
		}
	}
	var upstreamBody []byte
	var bridge *basispoints.Bridge
	if images != nil {
		upstreamBody, bridge, err = images.PrepareWithCatalog(scope, replay, catalog)
	} else {
		upstreamBody, bridge, err = basispoints.PrepareWithCatalog(body, scope, replay, catalog)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrStateUnavailable) {
			return fail(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
		}
		var schemaErr *basispoints.ToolSchemaValidationError
		if errors.As(err, &schemaErr) {
			return fail(400, "basispoints_request_invalid", schemaErr.Error(), schemaErr.Path)
		}
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			return fail(400, "basispoints_request_invalid", err.Error(), contentErr.Path)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		if !recoveryEnabled {
			if IsExcelAuthorizationError(err) {
				code := "basispoints_auth_" + strings.ToLower(strings.TrimPrefix(infraerrors.Reason(err), "OPENAI_EXCEL_AUTH_"))
				return fail(503, code, infraerrors.Message(err))
			}
			return fail(502, "basispoints_auth_unavailable", "Excel OAuth credential is unavailable; see Credential Operations")
		}
		s.handleExcelBPSCredentialReadError(ctx, account, err)
		return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, http.StatusBadGateway, nil, "basispoints_auth_unavailable", false)
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return fail(400, "basispoints_account_id_missing", "Excel BPS requires chatgpt_account_id")
	}
	requestAcquire := s.excelBPSAcquireFor(account)
	attachmentProxy := ""
	if account.Proxy != nil {
		attachmentProxy = account.Proxy.URL()
	}
	if images != nil && images.HasImages() && account.IsExcelBPSMihomoEnabled() {
		var lease excelBPSLease
		// Pin the attachment upload and the Responses request to the same exit,
		// whichever pool the account chose.
		attachmentProxy, lease, err = acquireExcelBPSAttachmentProxy(ctx, c, account, scope, requestAcquire)
		if err != nil {
			if isExcelBPSClientCancellation(c, err) {
				return clientCanceled()
			}
			return fail(503, "basispoints_proxy_unavailable", "No healthy BPS session proxy is available; retry later")
		}
		defer lease.Release()
		requestAcquire = pinnedExcelBPSAcquire(attachmentProxy, lease)
	}
	if images != nil && images.HasImages() {
		attachmentScope := ""
		if identity != "" {
			attachmentScope = scope + "\x00" + accountID + "\x00" + token
		}
		body, err = images.Upload(ctx, &s.excelBPSAttachments, attachmentScope, func(uploadCtx context.Context, img basispoints.InlineAttachment) (string, error) {
			SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
			return s.uploadExcelBPSAttachment(uploadCtx, account, token, accountID, attachmentProxy, img)
		})
		if err != nil {
			if isExcelBPSClientCancellation(c, err) {
				return clientCanceled()
			}
			status, code := http.StatusBadGateway, "basispoints_attachment_error"
			var uploadError *excelBPSAttachmentError
			if errors.As(err, &uploadError) {
				status = uploadError.status
			}
			if errors.Is(err, basispoints.ErrAttachmentBusy) {
				status = http.StatusServiceUnavailable
			}
			if status == http.StatusTooManyRequests && uploadError != nil {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, true)
				return failoverRateLimited(uploadError.retryAfter)
			}
			recordExcelBPSAttachmentFailure(ctx, c, account, err, false)
			if status == http.StatusUnauthorized {
				if !recoveryEnabled {
					return fail(status, code, "Excel BPS attachment authentication failed; request was not replayed")
				}
				return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, status, nil, "basispoints_auth_unavailable", false)
			}
			return fail(status, code, "Excel BPS attachment upload failed; request was not replayed and account scheduling was not changed")
		}
		upstreamBody, bridge, err = bridge.Reprepare(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))

	if imagePolicy.split > 0 {
		logger.LegacyPrintf("service.openai_excel_bps", "image policy auto compact: account_id=%d history_images=%d new_images=%d limit=%d", account.ID, basispoints.CountInlineImages(imagePolicy.history.Input[:imagePolicy.split]), basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]), imageSettings.Limits.MaxImages)
		uploaded, inspectErr := basispoints.InspectImageHistory(body)
		if inspectErr != nil {
			return fail(400, "basispoints_request_invalid", inspectErr.Error())
		}
		compactBody, buildErr := uploaded.WithInput(uploaded.Input[:imagePolicy.split], true)
		if buildErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare image compaction")
		}
		compactWire, _, buildErr := bridge.Reprepare(compactBody)
		if buildErr != nil {
			return fail(400, "basispoints_request_invalid", buildErr.Error())
		}
		SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
		cr, cl, cp, compactErr := s.doExcelBPSRequest(requestCtx, c, account, scope, compactWire, token, accountID, requestAcquire)
		if cl != nil {
			defer cl.Release()
		}
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction connection failed; generation was not started")
		}
		compactID = cr.Header.Get("x-request-id")
		if compactID == "" {
			compactID = "image-compaction"
		}
		if cr.StatusCode < 200 || cr.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(cr.Body, 512<<10))
			_ = cr.Body.Close()
			s.handleExcelBPSUnauthorized(ctx, account, cr.StatusCode, cr.Header, raw, token)
			if recoveryEnabled && cr.StatusCode == http.StatusTooManyRequests {
				recordRateLimit(cr.Header, raw, nil, cr.StatusCode)
				return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, cr.StatusCode, cr.Header, "basispoints_rate_limited", false)
			}
			if isExcelBPSUsagePolicyRejection(cr.StatusCode, raw) {
				if recoveryEnabled {
					recordPolicyRejection()
					return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, cr.StatusCode, cr.Header, "content_policy_violation", false)
				}
				return fail(cr.StatusCode, "content_policy_violation", "Excel BPS blocked image history compaction under its usage policy; generation was not started")
			}
			if cr.StatusCode == 403 {
				s.moveExcelBPSOn403(ctx, account)
				s.disableExcelBPSOn403(ctx, account)
			}
			return fail(cr.StatusCode, "basispoints_image_compaction_failed", "Image history compaction was rejected; generation was not started")
		}
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, cr.Header)
		compactEffort = bridge.Effort
		compactResponse, compactErr := basispoints.ReadImageCompaction(cr.Body, func(payload []byte) { s.parseSSEUsageBytes(payload, &compactUsage) })
		_ = cr.Body.Close()
		if compactResponse != nil {
			encoded, _ := json.Marshal(map[string]any{"type": "response.completed", "response": compactResponse})
			s.parseSSEUsageBytes(encoded, &compactUsage)
			compactUsageWire, _ = compactResponse["usage"].(map[string]any)
		}
		if compactErr != nil {
			var limited *basispoints.UpstreamFailure
			if recoveryEnabled && errors.As(compactErr, &limited) && limited.Status == http.StatusTooManyRequests {
				recordRateLimit(nil, nil, limited, cr.StatusCode)
				return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, limited.Status, nil, limited.Code, false)
			}
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction did not complete; generation was not started")
		}
		window, _, compactErr := basispoints.CompactWindow(compactResponse)
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", compactErr.Error())
		}
		next := append(append([]any{}, window...), uploaded.Input[imagePolicy.split:]...)
		// Count retained inline tool images, not the uploaded attachment placeholders.
		if basispoints.CountInlineImages(window)+basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]) > imageSettings.Limits.MaxImages {
			return fail(400, "basispoints_image_compaction_insufficient", "Compacted history still exceeds the image limit; reduce new images or compact manually")
		}
		body, compactErr = uploaded.WithInput(next, false)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare compacted continuation")
		}
		upstreamBody, bridge, compactErr = bridge.Reprepare(body)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", compactErr.Error())
		}
		if compactErr = imagePolicy.checkpoint(ctx, window); compactErr != nil {
			return fail(503, "basispoints_image_checkpoint_unavailable", "Could not save compacted history safely; retry or run compact manually")
		}
		compactOutput = window
		if cl != nil {
			requestAcquire = pinnedExcelBPSAcquire(cp, cl)
		}
	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	resp, lease, proxyURL, err := s.doExcelBPSRequest(requestCtx, c, account, scope, upstreamBody, token, accountID, requestAcquire)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		if errors.Is(err, errExcelBPSProxyUnavailable) {
			return fail(503, "basispoints_proxy_unavailable", "No healthy BPS session proxy is available; retry later")
		}
		return fail(502, "basispoints_transport_error", "Excel BPS connection failed; request was not replayed after sending")
	}
	if lease != nil {
		defer lease.Release()
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		observeBPSTestResponse(ctx, resp, model, token, "")
	}
	if resp.StatusCode == http.StatusBadRequest {
		// Read and close before retrying: the HTTP body owns the account's
		// concurrency slot. Keep the proxy lease for the exact same exit.
		const maxRejectionBytes = 512 << 10
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRejectionBytes+1))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if retryBody, retry := prepareExcelBPSInvalidEncryptedRetry(upstreamBody, raw); !isControlledExperiment(ctx) && retry && readErr == nil && len(raw) <= maxRejectionBytes && ctx.Err() == nil {
			retryReq, retryErr := newExcelBPSRequest(requestCtx, retryBody, token, accountID)
			if retryErr != nil {
				return fail(502, "basispoints_transport_error", "Excel BPS recovery request could not be prepared")
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: basispoints.ResponsesURL, Kind: "invalid_encrypted_content_retry",
				Message: "Excel BPS rejected encrypted reasoning; retrying once without opaque reasoning on the same route",
			})
			logger.LegacyPrintf("service.openai_excel_bps", "retrying invalid encrypted reasoning once: account_id=%d", account.ID)
			c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
			// Do not re-enter proxy acquisition or transport retries after sending.
			resp, err = s.doPriorityObservedHTTP(retryReq, proxyURL, account)
			if err == nil {
				s.guardExcelBPSProgress(requestCtx, resp)
			}
			s.rateLimitService.observeQualityResponse(retryReq.Context(), account, resp, err)
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
			if err != nil {
				if isExcelBPSClientCancellation(c, err) {
					return clientCanceled()
				}
				if lease != nil && ctx.Err() == nil {
					lease.ReportFailure()
				}
				return fail(502, "basispoints_transport_error", "Excel BPS recovery connection failed; request was not replayed again")
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				observeBPSTestResponse(ctx, resp, model, token, "")
			}
			upstreamBody = retryBody
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if lease != nil && resp.StatusCode >= 500 && ctx.Err() == nil {
			lease.ReportUpstreamFailure()
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		if err := ctx.Err(); resp.StatusCode == http.StatusTooManyRequests && err != nil {
			return nil, err
		}
		observeBPSTestResponse(ctx, resp, model, token, gjson.GetBytes(raw, "error.code").String())
		// BPS throttles its own endpoint. A BPS 429 must not write Codex
		// quota/cooldown state; it cools only the BPS route and fails over.
		// Preserve the original rejection for Ops without exposing it to clients.
		// BPS errors can echo request fields, so redact before storing diagnostics.
		upstreamMessage := fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode)
		upstreamDetail := ""
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			safeBody := excelBPSSanitizeErrorBody(string(raw), token, account)
			maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
			if maxBytes <= 0 {
				maxBytes = 2048
			}
			upstreamDetail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
			if message := strings.TrimSpace(extractUpstreamErrorMessage([]byte(safeBody))); message != "" {
				upstreamMessage = truncateString(message, 2048)
			}
		}
		setOpsUpstreamError(c, resp.StatusCode, upstreamMessage, upstreamDetail)
		kind := "http_error"
		if resp.StatusCode == http.StatusTooManyRequests {
			kind = "failover"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			UpstreamURL: basispoints.ResponsesURL, Kind: kind,
			Message: upstreamMessage, Detail: upstreamDetail, UpstreamResponseBody: upstreamDetail,
		})
		if resp.StatusCode == http.StatusUnauthorized {
			s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw, token)
			if !recoveryEnabled {
				return fail(resp.StatusCode, "basispoints_upstream_error", "Excel BPS authentication failed; request was not replayed")
			}
			return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, resp.StatusCode, resp.Header, "basispoints_auth_unavailable", false)
		}
		if isExcelBPSUsagePolicyRejection(resp.StatusCode, raw) {
			if recoveryEnabled {
				// A single rejection only switches accounts. Repeated rejections
				// honor the account's opt-in BPS auto-disable threshold.
				recordPolicyRejection()
				return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, resp.StatusCode, resp.Header, "content_policy_violation", false)
			}
			return fail(resp.StatusCode, "content_policy_violation", "Excel BPS blocked this request under its usage policy; request was not replayed and account scheduling was not changed")
		}
		code := gjson.GetBytes(raw, "error.code").String()
		if isExcelBPSModelPermissionError(resp.StatusCode, code) && (recoveryEnabled || resp.StatusCode == http.StatusForbidden) {
			if !isQualityObservation(ctx) {
				s.coolDownExcelBPSModel(ctx, account, model)
			}
			if !recoveryEnabled {
				return fail(resp.StatusCode, code, "This model is not available on the selected Excel BPS account")
			}
			return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, resp.StatusCode, resp.Header, code, false)
		}
		message := "Excel BPS rejected this request"
		if recoveryEnabled && resp.StatusCode >= 500 && s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMessage, raw) {
			return nil, s.newExcelBPSFailover(ctx, c, account, originalModel, resp.StatusCode, resp.Header, "basispoints_server_error", true)
		}
		errorCode := "basispoints_upstream_error"
		if resp.StatusCode == http.StatusBadRequest && isExcelBPSInvalidEncryptedContent(raw) {
			errorCode = "invalid_encrypted_content"
			message = "Excel BPS could not verify encrypted conversation state; resend the original plaintext history or start a new conversation"
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lockExcelBPSFailoverRoute(ctx, c, account, originalModel)
			if !isQualityObservation(ctx) {
				recordRateLimit(resp.Header, raw, nil, resp.StatusCode)
			}
			// The request was rejected before any output. Leave the response open
			// so the handler can try another account within its existing budget.
			failover := s.openAIUpstreamFailoverError(
				http.StatusTooManyRequests,
				resp.Header,
				[]byte(`{"error":{"type":"rate_limit_error","code":"basispoints_rate_limited","message":"Excel BPS rate limit exceeded"}}`),
				"Excel BPS rate limit exceeded",
				false,
			)
			failover.SuppressScheduleFailure = true
			failover.SkipOAuth429AttemptLimit = true
			s.limitExcelBPS429Failover(ctx, c, account, originalModel, failover)
			return nil, failover
		}
		if resp.StatusCode == http.StatusForbidden {
			// Apply group routing before the independent protocol switch is disabled.
			moved := s.moveExcelBPSOn403(ctx, account)
			disabled := s.disableExcelBPSOn403(ctx, account)
			switch {
			case moved && disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled and account groups were updated; request was not replayed"
			case disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled for this account; request was not replayed"
			case moved:
				message = "Excel BPS rejected this request; account groups were automatically updated; request was not replayed"
			}
		}
		return fail(resp.StatusCode, errorCode, message)
	}
	// BPS and Codex share quota. Refresh at the HTTP boundary even if the client
	// disconnects or a later stream/protocol error prevents normal completion.
	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	var repairUsagePolicyRejected atomic.Bool
	var repairStarted atomic.Bool
	var observedFailure atomic.Pointer[basispoints.UpstreamFailure]
	requestCtx = basispoints.WithUpstreamFailureObserver(requestCtx, func(failure *basispoints.UpstreamFailure) { observedFailure.Store(failure) })
	converted := bridge.StreamWithRepairs(requestCtx, resp.Body, func(repairCtx context.Context, failed map[string]any, validation error) (map[string]any, error) {
		repairStarted.Store(true)
		if isControlledExperiment(ctx) {
			return nil, errors.New("controlled experiment disables BPS tool repair submissions")
		}
		markPriorityAttemptFailure(repairCtx)
		correctedBody, err := basispoints.BuildToolRepairRequest(upstreamBody, failed, validation)
		if err != nil {
			return nil, err
		}
		repairReq, err := newExcelBPSRequest(repairCtx, correctedBody, token, accountID)
		if err != nil {
			return nil, err
		}
		repairResp, err := s.doExcelBPSRepairRequest(repairReq, proxyURL, account)
		if err == nil {
			s.guardExcelBPSProgress(repairCtx, repairResp)
		}
		s.rateLimitService.observeQualityResponse(repairReq.Context(), account, repairResp, err)
		if err != nil {
			if repairCtx.Err() != nil {
				return nil, repairCtx.Err()
			}
			return nil, fmt.Errorf("excel BPS correction connection failed")
		}
		defer func() { _ = repairResp.Body.Close() }()
		stop := context.AfterFunc(repairCtx, func() { _ = repairResp.Body.Close() })
		defer stop()
		if repairResp.StatusCode >= 200 && repairResp.StatusCode < 300 {
			observeBPSTestResponse(repairCtx, repairResp, model, token, "")
		}
		if repairResp.StatusCode < 200 || repairResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repairResp.Body, 512<<10))
			observeBPSTestResponse(repairCtx, repairResp, model, token, gjson.GetBytes(raw, "error.code").String())
			if recoveryEnabled && isExcelBPSUsagePolicyRejection(repairResp.StatusCode, raw) {
				recordPolicyRejection()
				repairUsagePolicyRejected.Store(true)
				return nil, errExcelBPSUsagePolicyBlocked
			}
			if repairResp.StatusCode == http.StatusTooManyRequests {
				// Output was already accepted: cool only the BPS route.
				// Quota headers on this correction must not rewrite Codex windows.
				if recoveryEnabled {
					recordRateLimit(repairResp.Header, nil, &basispoints.UpstreamFailure{Status: http.StatusTooManyRequests, Code: "basispoints_rate_limited", RateLimit: basispoints.ParseRateLimitHint(raw)}, repairResp.StatusCode)
				} else {
					s.coolDownExcelBPS(repairCtx, account, nil, nil)
				}
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repairResp.StatusCode, repairResp.Header, raw, token)
			if isExcelBPSModelPermissionError(repairResp.StatusCode, gjson.GetBytes(raw, "error.code").String()) && (recoveryEnabled || repairResp.StatusCode == http.StatusForbidden) && !isQualityObservation(repairCtx) {
				s.coolDownExcelBPSModel(ctx, account, model)
			}
			if repairResp.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS correction returned HTTP %d", repairResp.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repairResp.Header)
		upstreamBody = correctedBody
		return basispoints.ReadToolRepairResponse(repairResp.Body)
	}, func(repairCtx context.Context) (io.ReadCloser, error) {
		repairStarted.Store(true)
		markPriorityAttemptFailure(repairCtx)
		repairBody, err := basispoints.RepairRequest(upstreamBody)
		if err != nil {
			return nil, err
		}
		retry, err := newExcelBPSRequest(repairCtx, repairBody, token, accountID)
		if err != nil {
			return nil, err
		}
		if isControlledExperiment(ctx) {
			return nil, errors.New("controlled experiment disables BPS correction submissions")
		}
		repaired, err := s.doExcelBPSRepairRequest(retry, proxyURL, account)
		if err == nil {
			s.guardExcelBPSProgress(repairCtx, repaired)
		}
		s.rateLimitService.observeQualityResponse(retry.Context(), account, repaired, err)
		if err != nil {
			return nil, fmt.Errorf("excel BPS tool correction transport failed")
		}
		if repaired.StatusCode >= 200 && repaired.StatusCode < 300 {
			observeBPSTestResponse(repairCtx, repaired, model, token, "")
		}
		if repaired.StatusCode < 200 || repaired.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repaired.Body, 512<<10))
			_ = repaired.Body.Close()
			observeBPSTestResponse(repairCtx, repaired, model, token, gjson.GetBytes(raw, "error.code").String())
			if recoveryEnabled && isExcelBPSUsagePolicyRejection(repaired.StatusCode, raw) {
				recordPolicyRejection()
				repairUsagePolicyRejected.Store(true)
				return nil, errExcelBPSUsagePolicyBlocked
			}
			if repaired.StatusCode == http.StatusTooManyRequests {
				if recoveryEnabled {
					recordRateLimit(repaired.Header, nil, &basispoints.UpstreamFailure{Status: http.StatusTooManyRequests, Code: "basispoints_rate_limited", RateLimit: basispoints.ParseRateLimitHint(raw)}, repaired.StatusCode)
				} else {
					s.coolDownExcelBPS(repairCtx, account, nil, nil)
				}
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repaired.StatusCode, repaired.Header, raw, token)
			if isExcelBPSModelPermissionError(repaired.StatusCode, gjson.GetBytes(raw, "error.code").String()) && (recoveryEnabled || repaired.StatusCode == http.StatusForbidden) && !isQualityObservation(repairCtx) {
				s.coolDownExcelBPSModel(ctx, account, model)
			}
			if repaired.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS tool correction returned HTTP %d", repaired.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repaired.Header)
		return repaired.Body, nil
	})
	if len(compactOutput) > 0 {
		converted = basispoints.WithCompactedWindow(requestCtx, converted, compactOutput, compactUsageWire)
	}
	defer func() { _ = converted.Close() }()
	// The bridge sees the body after group policy mapping. Keep the original
	// client effort for usage display, and the BPS-normalized effort for billing.
	requestedEffort := coalesceRequestedReasoningEffort(RequestedReasoningEffortFromContext(ctx), &bridge.RequestedEffort)
	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: requestedEffort, RequestID: resp.Header.Get("x-request-id")}
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
	}
	scanner := newOpenAISSEReadPump(converted, 16<<20)
	defer scanner.Close()
	stage := newDefaultOpenAIFirstOutputStage()
	defer func() { _ = stage.Close() }()
	outputStarted, commitEvent := !recoveryEnabled, false
	writerSize := OpenAICompactKeepaliveAdjustedWrittenSize(c)
	heartbeatBytes := 0
	canRecover := func() bool {
		return recoveryEnabled && !outputStarted && !IsResponseCommitted(c) && !isQualityObservation(ctx) &&
			!isControlledExperiment(ctx) && !isExcelBPSClientCancellation(c, ctx.Err())
	}
	recoverAttempt := func(status int, code string, retrySame bool) (*OpenAIForwardResult, error) {
		failure := s.newExcelBPSFailover(ctx, c, account, originalModel, status, resp.Header, code, retrySame)
		failure.SafeToFailoverAfterWrite = c.Writer.Written() && OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSize+heartbeatBytes
		if failure.SafeToFailoverAfterWrite {
			// The handler permits only one replacement after heartbeat writes.
			// Spend that budget on another account rather than a same-account retry.
			failure.RetryableOnSameAccount = false
		}
		result.Duration = time.Since(start)
		return result, failure
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	keepalive := func() {
		if stream && ctx.Err() == nil {
			n, _ := c.Writer.WriteString(": keepalive\n\n")
			heartbeatBytes += n
			c.Writer.Flush()
		}
	}
	var completed []byte
	var upstreamFailure *basispoints.UpstreamFailure
	var bridgeFailure *basispoints.BridgeFailure
	terminal := ""
	terminalSuccessful := false
	cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled()
	firstResponseEnabled := stream && s.settingService.IsStreamFirstResponseEnabled(ctx)
	for scanner.Next(ctx, 0, heartbeat.C, keepalive) {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := []byte(strings.TrimPrefix(line, "data: "))
			kind := gjson.GetBytes(payload, "type").String()
			observeStreamFirstResponse(&result.FirstTokenMs, firstResponseEnabled, start, string(payload), kind)
			s.parseSSEUsageBytes(payload, &result.Usage)
			if len(compactOutput) > 0 && (kind == "response.completed" || kind == "response.failed" || kind == "response.cancelled" || kind == "response.incomplete") {
				subtractImagePolicyUsage(&result.Usage, compactUsage)
			}
			if cacheCreationAsInput {
				payload, err = excelBPSDownstreamUsage(payload)
				if err != nil {
					return fail(http.StatusBadGateway, "basispoints_usage_invalid", "Excel BPS usage could not be normalized")
				}
				line = "data: " + string(payload)
			}
			if result.FirstTokenMs == nil && (kind == "response.output_text.delta" || kind == "response.output_item.added") {
				ms := int(time.Since(start).Milliseconds())
				result.FirstTokenMs = &ms
			}
			if stream && openAIStreamDataStartsClientOutput(string(payload), kind) {
				commitEvent = true
			}
			switch kind {
			case "response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":
				bridgeFailure = basispoints.ParseBridgeFailure(payload)
				if bridgeFailure != nil && repairUsagePolicyRejected.Load() && canRecover() {
					return recoverAttempt(http.StatusForbidden, "content_policy_violation", false)
				}
				if kind != "response.completed" && bridgeFailure == nil {
					s.rateLimitService.observeQualityStatus(ctx, account, openAIStreamFailureStatus(payload, extractOpenAISSEErrorMessage(payload)))
				}
				if kind == "response.completed" && lease != nil {
					lease.ReportSuccess()
				}
				terminal = kind
				result.UpstreamTerminalEvent = kind
				terminalSuccessful = IsSuccessfulStreamTerminal(payload)
				completed = []byte(gjson.GetBytes(payload, "response").Raw)
				result.ResponseID = gjson.GetBytes(payload, "response.id").String()
				result.UpstreamResponseModel = gjson.GetBytes(payload, "response.model").String()
				upstreamFailure = basispoints.ParseUpstreamFailure(payload)
				if recoveryEnabled && upstreamFailure != nil {
					switch upstreamFailure.Status {
					case http.StatusUnauthorized:
						s.handleExcelBPSUnauthorized(ctx, account, upstreamFailure.Status, resp.Header, payload, token)
					case http.StatusTooManyRequests:
						// The HTTP 200 quota headers do not describe this in-band 429.
						if !isQualityObservation(ctx) {
							if observed := observedFailure.Load(); observed != nil && observed.Status == upstreamFailure.Status {
								upstreamFailure.RateLimit = observed.RateLimit
							}
							recordRateLimit(nil, nil, upstreamFailure, resp.StatusCode)
						}
					case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
						if isExcelBPSModelPermissionError(upstreamFailure.Status, upstreamFailure.Code) && !isQualityObservation(ctx) {
							s.coolDownExcelBPSModel(ctx, account, model)
						}
					}
					if canRecover() && excelBPSFailureCanRecover(upstreamFailure) && (upstreamFailure.Status != http.StatusTooManyRequests || !repairStarted.Load()) {
						return recoverAttempt(upstreamFailure.Status, upstreamFailure.Code, upstreamFailure.Status >= 500)
					}
				}
				commitEvent = true
				if bridgeFailure != nil {
					markPriorityAttemptFailure(ctx)
					setOpsUpstreamError(c, resp.StatusCode, "", "")
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
						ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
						UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
						UpstreamURL: basispoints.ResponsesURL, Kind: "bridge_error", Stage: bridgeFailure.FailureStage,
						Reason: bridgeFailure.RepairSkippedReason, Message: bridgeFailure.Message,
					})
					MarkOpsStreamErrorValue(c, OpsStreamError{
						ErrType: "server_error", Code: "basispoints_protocol_error", Message: bridgeFailure.Message,
						IntendedStatus: http.StatusBadGateway, CountTowardsSLA: true, NonStream: !stream,
						BridgeFailure: bridgeFailure,
					})
				}
				if upstreamFailure != nil {
					// Preserve the actual HTTP status separately from the event's
					// semantic status. Already delivered output is never replayed.
					setOpsUpstreamError(c, resp.StatusCode, upstreamFailure.Message, "")
					MarkOpsStreamErrorValue(c, OpsStreamError{
						ErrType: upstreamFailure.Type, Code: upstreamFailure.Code, Message: upstreamFailure.Message,
						IntendedStatus: upstreamFailure.Status, CountTowardsSLA: true, NonStream: !stream,
					})
				}
			}
		}
		if stream {
			if outputStarted {
				_, err = c.Writer.WriteString(line + "\n")
			} else {
				_, err = stage.WriteString(line + "\n")
				if err != nil && canRecover() {
					return recoverAttempt(http.StatusBadGateway, "basispoints_preoutput_buffer_failed", false)
				}
			}
			if err != nil {
				result.ClientDisconnect = true
				if isExcelBPSClientCancellation(c, c.Request.Context().Err()) {
					MarkOpsClientCancellation(c, stream)
				}
				result.Duration = time.Since(start)
				return result, err
			}
			if line == "" && (outputStarted || commitEvent) {
				if !outputStarted {
					outputStarted = true
					if err = stage.CommitTo(c.Writer); err != nil {
						result.ClientDisconnect = true
						return result, err
					}
				}
				c.Writer.Flush()
			}
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err = scanner.Err(); err != nil || terminal == "" {
		if ctx.Err() != nil {
			if isExcelBPSClientCancellation(c, ctx.Err()) {
				MarkOpsClientCancellation(c, stream)
			}
			result.ClientDisconnect = true
			return result, ctx.Err()
		}
		// An interrupted attempt can be replaced only before semantic output.
		// Client cancellation never starts another attempt or penalizes the node.
		if lease != nil {
			lease.ReportStreamFailure()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, proxyURL, err, "stream", c.GetInt("excel_bps_upstream_attempt"), false)
		if canRecover() {
			return recoverAttempt(http.StatusBadGateway, "basispoints_stream_incomplete", false)
		}
		MarkOpsStreamError(c, "basispoints_stream_incomplete", "Excel BPS stream ended before completion", http.StatusBadGateway)
		s.rateLimitService.observeQualityStatus(ctx, account, http.StatusBadGateway)
		MarkResponseCommitted(c)
		if stream {
			_, _ = c.Writer.WriteString("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"type\":\"server_error\",\"code\":\"basispoints_stream_incomplete\",\"message\":\"Upstream stream ended before completion\"}}}\n\n")
			c.Writer.Flush()
		} else {
			c.JSON(502, gin.H{"error": gin.H{"type": "server_error", "code": "basispoints_stream_incomplete", "message": "Excel BPS stream ended before completion"}})
		}
		return result, fmt.Errorf("excel BPS stream incomplete")
	}
	if terminal != "response.completed" {
		MarkResponseCommitted(c)
	}
	if !stream {
		if upstreamFailure != nil {
			if StopOpenAICompactSSEKeepaliveCommitted(c) {
				writeOpenAICompactSSEFailureMessage(c, upstreamFailure.Status, upstreamFailure.Code, upstreamFailure.Message)
			} else {
				c.JSON(upstreamFailure.Status, gin.H{"error": upstreamFailure.Details()})
			}
		} else if terminal != "response.completed" {
			message := "Excel BPS did not complete the response"
			detail := map[string]any{"code": "basispoints_protocol_error", "message": message}
			if bridgeFailure != nil {
				detail = bridgeFailure.Details()
				message = bridgeFailure.Message
			}
			if StopOpenAICompactSSEKeepaliveCommitted(c) {
				writeOpenAICompactSSEFailureMessage(c, http.StatusBadGateway, "basispoints_protocol_error", message)
			} else {
				c.JSON(http.StatusBadGateway, gin.H{"error": detail})
			}
		} else {
			c.Data(200, "application/json", completed)
		}
	}
	if terminal != "response.completed" {
		return result, fmt.Errorf("excel BPS terminal: %s", terminal)
	}
	if terminalSuccessful && !result.ClientDisconnect && !repairUsagePolicyRejected.Load() {
		s.resetExcelBPSPolicyRejections(ctx, account)
	}
	imagePolicy.finish(ctx, imagePolicy.compact || len(compactOutput) > 0 || (imagePolicy.history != nil && imagePolicy.history.Count < imageSettings.Limits.MaxImages-imageSettings.WarningRemaining))
	if recoveryEnabled && terminalSuccessful && !result.ClientDisconnect && !isQualityObservation(ctx) {
		s.resetExcelBPSModelRateLimit(account, model, start)
	}
	s.bindHTTPResponseAccount(ctx, c, account, result.ResponseID)
	if stream && terminalSuccessful && !result.ClientDisconnect {
		MarkOpsStreamCompleted(c, account.ID)
	}
	return result, nil
}

var excelBPSBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[^\s"',;<>]+`)
var excelBPSURLCredentialsPattern = regexp.MustCompile(`(https?://)[^/\s@]+@`)
var excelBPSAttachmentIDPattern = regexp.MustCompile(`\bfile-[A-Za-z0-9_-]+`)
var excelBPSImageCapabilityPattern = regexp.MustCompile(`/api/bps-images/[A-Za-z0-9_-]+`)

func excelBPSSanitizeErrorBody(raw, token string, account *Account) string {
	if !json.Valid([]byte(raw)) {
		return ""
	}
	secrets := append([]string{token}, excelBPSAccountSecrets(account)...)
	fields := make(map[string]string)
	for _, key := range []string{"message", "code", "type", "param"} {
		value := gjson.Get(raw, "error."+key)
		if value.Type != gjson.String {
			continue
		}
		clean := value.String()
		for _, secret := range secrets {
			if secret != "" {
				clean = strings.ReplaceAll(clean, secret, "[redacted]")
			}
		}
		clean = excelBPSBearerPattern.ReplaceAllString(clean, "Bearer [redacted]")
		clean = excelBPSURLCredentialsPattern.ReplaceAllString(clean, "${1}[redacted]@")
		clean = excelBPSImageCapabilityPattern.ReplaceAllString(clean, "/api/bps-images/[redacted]")
		clean = excelBPSAttachmentIDPattern.ReplaceAllString(clean, "file-[redacted]")
		clean = sanitizeUpstreamErrorMessage(clean)
		fields[key] = truncateString(logredact.RedactText(clean, "authorization", "api_key", "apikey", "token", "secret", "key", "cookie", "ticket", "recovery_ticket"), 2048)
	}
	encoded, _ := json.Marshal(map[string]any{"error": fields})
	return string(encoded)
}

func excelBPSAccountSecrets(account *Account) []string {
	var secrets []string
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key", "session_key", "cookie"} {
		if value := account.GetCredential(key); value != "" {
			secrets = append(secrets, value)
		}
	}
	if account.Proxy != nil && account.Proxy.Password != "" {
		secrets = append(secrets, account.Proxy.Password)
	}
	return secrets
}

// Explicit conversation identity remains sticky. Anonymous requests get a
// request-local identity reused across internal retries, never across callers.
func resolveExcelBPSIdentity(c *gin.Context, body []byte, apiKeyID int64, managed bool) (string, bool) {
	if identity, _ := resolveOpenAIWSExecutionScope(c, body, apiKeyID); identity != "" {
		return identity, false
	}
	if !managed {
		return "", false
	}
	const key = "excel_bps_transient_identity"
	if identity := c.GetString(key); identity != "" {
		return identity, true
	}
	identity := uuid.NewString()
	c.Set(key, identity)
	return identity, true
}
