package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	openAICodexTicketExtraKeyPrefix  = "codex_turn_ticket:"
	OpenAICodexSkipHarvestExtraKey   = "codex_skip_harvest"
	openAICodexAstraMinVersion       = "0.153.4"
	openAICodexTicketStatePrefix     = "gAAAAA"
	openAICodexTicketDefaultModel    = "gpt-6-astra"
	openAICodexTicketDefaultSolModel = "gpt-5.6-sol"
	openAICodexTicketPersonalBlocks  = 10
	openAICodexTicketTeamBlocks      = 12
	openAICodexCredentialTTL         = 240 * time.Second
)

// ErrOpenAICodexTicketUnavailable 表示该号该模型没有可用的 Codex 门票，
// 且 fail_closed 禁止裸打业务请求。
var ErrOpenAICodexTicketUnavailable = errors.New("codex turn-state ticket unavailable")

type openAICodexTicket struct {
	Acquisition         string             `json:"acquisition,omitempty"`
	SourceAccountID     int64              `json:"source_account_id,omitempty"`
	ProbeVerified       bool               `json:"probe_verified,omitempty"`
	AccountID           int64              `json:"account_id"`
	Model               string             `json:"model"`
	State               string             `json:"state"`
	Length              int                `json:"length"`
	CapturedAt          time.Time          `json:"captured_at"`
	ExpiresAt           time.Time          `json:"expires_at"`
	Attempts            int                `json:"attempts"`
	Blocks              int                `json:"blocks,omitempty"`
	IssuedAt            time.Time          `json:"issued_at,omitempty"`
	Identity            string             `json:"identity,omitempty"`
	HarvestProxyURL     string             `json:"harvest_proxy_url,omitempty"`
	HarvestNodeID       string             `json:"harvest_node_id,omitempty"`
	HarvestNodeName     string             `json:"harvest_node_name,omitempty"`
	HarvestNodeProvider string             `json:"harvest_node_provider,omitempty"`
	HarvestSessionID    string             `json:"harvest_session_id,omitempty"`
	EdgeIP              string             `json:"edge_ip,omitempty"`
	Gateway             string             `json:"gateway,omitempty"`
	Transport           string             `json:"transport,omitempty"`
	HarvestLite         bool               `json:"harvest_lite,omitempty"`
	HarvestCookies      []string           `json:"harvest_cookies,omitempty"`
	HarvestCookiesAt    time.Time          `json:"harvest_cookies_at,omitempty"`
	Standby             *openAICodexTicket `json:"standby,omitempty"`
	Revoked             bool               `json:"revoked,omitempty"`
}

func codexTicketCookiesFresh(ticket *openAICodexTicket, now time.Time) bool {
	if ticket == nil || len(ticket.HarvestCookies) == 0 {
		return false
	}
	if ticket.Length == 780 {
		_, exp, err := codex780Route(ticket.HarvestCookies, ticket.Gateway, now)
		return err == nil && now.Before(exp)
	}
	captured := ticket.HarvestCookiesAt
	if captured.IsZero() {
		captured = ticket.CapturedAt
	}
	return !captured.IsZero() && now.Before(captured.Add(openAICodexCredentialTTL))
}

func codexTicketCookiesExpiry(ticket *openAICodexTicket) time.Time {
	if ticket == nil || len(ticket.HarvestCookies) == 0 {
		return time.Time{}
	}
	if ticket.Length == 780 {
		_, exp, _ := codex780Route(ticket.HarvestCookies, ticket.Gateway, time.Unix(0, 0))
		return exp
	}
	captured := ticket.HarvestCookiesAt
	if captured.IsZero() {
		captured = ticket.CapturedAt
	}
	if captured.IsZero() {
		return time.Time{}
	}
	return captured.Add(openAICodexCredentialTTL)
}

func extraTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true
		}
	case float64:
		return v != 0
	}
	return false
}

func openAICodexSkipHarvest(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	return extraTruthy(account.Extra[OpenAICodexSkipHarvestExtraKey])
}

func openAICodexTicketRemainingUntil(t *openAICodexTicket) time.Time {
	if t == nil {
		return time.Time{}
	}
	exp := t.ExpiresAt
	if !t.IssuedAt.IsZero() {
		issuedExpiry := t.IssuedAt.Add(codexTicketLifetime(t.Length))
		if exp.IsZero() || issuedExpiry.Before(exp) {
			exp = issuedExpiry
		}
	}
	if t.Length == 780 {
		cookieExpiry := codexTicketCookiesExpiry(t)
		if !cookieExpiry.IsZero() && (exp.IsZero() || cookieExpiry.Before(exp)) {
			exp = cookieExpiry
		}
	}
	return exp
}

func (s *OpenAIGatewayService) holdCodexTicketChat(account *Account) func() {
	if s == nil || account == nil || account.ID <= 0 {
		return func() {}
	}
	raw, _ := s.openaiCodexTicketChatHold.LoadOrStore(account.ID, new(int64))
	ctr, _ := raw.(*int64)
	if ctr == nil {
		return func() {}
	}
	atomic.AddInt64(ctr, 1)
	var once sync.Once
	return func() {
		once.Do(func() {
			atomic.AddInt64(ctr, -1)
		})
	}
}

func (s *OpenAIGatewayService) codexTicketChatHeld(accountID int64) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	raw, ok := s.openaiCodexTicketChatHold.Load(accountID)
	if !ok {
		return false
	}
	ctr, _ := raw.(*int64)
	return ctr != nil && atomic.LoadInt64(ctr) > 0
}

type codexTicketHarvestContextKey struct{}

func isCodexTicketHarvestContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(codexTicketHarvestContextKey{}).(bool)
	return value
}

type openAICodexTicketShape struct {
	Blocks   int
	IssuedAt time.Time
}

// parseOpenAICodexTicketShape validates the opaque envelope instead of trusting
// only its prefix and length. Valid current envelopes are 292 bytes for
// personal plans and 332 bytes for Team/Business/Enterprise plans.
func parseOpenAICodexTicketShape(value string) (openAICodexTicketShape, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\r\n\t ") {
		return openAICodexTicketShape{}, errors.New("invalid state encoding")
	}
	core := strings.TrimRight(value, "=")
	if len(value)-len(core) > 2 {
		return openAICodexTicketShape{}, errors.New("invalid state padding")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(core)
	if err != nil || len(raw) < 73 || raw[0] != 0x80 || (len(raw)-57)%16 != 0 {
		return openAICodexTicketShape{}, errors.New("unrecognized state envelope")
	}
	issuedUnix := binary.BigEndian.Uint64(raw[1:9])
	if issuedUnix < 1577836800 || issuedUnix >= 4102444800 {
		return openAICodexTicketShape{}, errors.New("state timestamp out of range")
	}
	return openAICodexTicketShape{
		Blocks:   (len(raw) - 57) / 16,
		IssuedAt: time.Unix(int64(issuedUnix), 0),
	}, nil
}

func openAICodexTicketIsTeamPlan(account *Account) bool {
	if account == nil {
		return false
	}
	plan := strings.ToLower(strings.TrimSpace(account.GetCredential("plan_type")))
	return strings.Contains(plan, "team") || strings.Contains(plan, "business") || strings.Contains(plan, "enterprise")
}

func openAICodexTicketExpectedBlocks(account *Account) int {
	if openAICodexTicketIsTeamPlan(account) {
		return openAICodexTicketTeamBlocks
	}
	return openAICodexTicketPersonalBlocks
}

func openAICodexTicketExpectedLength(account *Account) int {
	return base64.URLEncoding.EncodedLen(57 + 16*openAICodexTicketExpectedBlocks(account))
}

func openAICodexTicketTargetLength(account *Account, cfg config.OpenAICodexTicketConfig) int {
	// 292 is the historical default. Derive the Team length from the account
	// plan unless an explicit non-default target is configured.
	if cfg.TargetLength > 0 && cfg.TargetLength != 292 {
		return cfg.TargetLength
	}
	return openAICodexTicketExpectedLength(account)
}

func ticketIdentity(account *Account) string {
	if account == nil {
		return ""
	}
	identity := account.GetCredential("chatgpt_account_id") + "\x00" + account.GetCredential("email")
	if account.AuthConnectionID != "" {
		identity += fmt.Sprintf("\x00connection:%s/client:%s/revision:%d", account.AuthConnectionID, account.GetCredential("client_id"), account.AuthConnectionRevision)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}
func ticketIdentityMatches(account *Account, ticket *openAICodexTicket) bool {
	if account != nil && account.AuthConnectionID != "" {
		return ticket != nil && ticket.Identity != "" && ticket.Identity == ticketIdentity(account)
	}
	return ticket != nil && (ticket.Identity == "" || ticket.Identity == ticketIdentity(account))
}

func ticketIdentityTreeMatches(account *Account, ticket *openAICodexTicket) bool {
	if !ticketIdentityMatches(account, ticket) {
		return false
	}
	return ticket.Standby == nil || ticketIdentityTreeMatches(account, ticket.Standby)
}

func openAICodexTicketKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(model))
}

func openAICodexTicketExtraKey(model string) string {
	return openAICodexTicketExtraKeyPrefix + strings.TrimSpace(model)
}

func normalizeOpenAICodexTicketModel(model string) string {
	return strings.TrimSpace(model)
}

// NormalizeOpenAICodexTicketModels removes empty and duplicate model names
// while preserving the configured order. An empty result disables ticket
// gating for every model while keeping the global feature enabled.
func NormalizeOpenAICodexTicketModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func extractOpenAICodexTicketModel(body []byte) string {
	return normalizeOpenAICodexTicketModel(gjson.GetBytes(body, "model").String())
}

func (s *OpenAIGatewayService) openAICodexTicketConfig() config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{}
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.OpenAICodexTicket
	}
	if cfg.TargetLength <= 0 {
		cfg.TargetLength = 292
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 3600
	}
	if cfg.RefreshBeforeSeconds <= 0 {
		cfg.RefreshBeforeSeconds = 600
	}
	if cfg.HarvestProbeIntervalSeconds <= 0 {
		cfg.HarvestProbeIntervalSeconds = 6
	}
	if cfg.HarvestCooldownSeconds <= 0 {
		cfg.HarvestCooldownSeconds = 180
	}
	if cfg.MaxProbesPerRound <= 0 {
		cfg.MaxProbesPerRound = 6
	}
	if cfg.HarvestAttemptTimeoutSeconds <= 0 {
		cfg.HarvestAttemptTimeoutSeconds = 25
	}
	if cfg.Models == nil {
		cfg.Models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	if s != nil && s.settingService != nil {
		cfg.Models = s.settingService.GetOpenAICodexTicketModels(context.Background(), cfg.Models)
		cfg.FailClosed = s.settingService.GetOpenAICodexTicketFailClosed(context.Background(), cfg.FailClosed)
	}
	return cfg
}

func (s *OpenAIGatewayService) openAICodexTicketGatedModel(model string) bool {
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" {
		return false
	}
	for _, item := range s.openAICodexTicketConfig().Models {
		if normalizeOpenAICodexTicketModel(item) == model {
			return true
		}
	}
	return false
}

// OpenAICodexTicketStatus 是给管理端看的门票摘要，不含 state blob。
type OpenAICodexTicketStatus struct {
	Model            string             `json:"model"`
	Length           int                `json:"length,omitempty"`
	Ready            bool               `json:"ready"`
	RemainingSeconds int64              `json:"remaining_seconds"`
	Blocked          bool               `json:"blocked"`
	ExpiresAt        *time.Time         `json:"expires_at,omitempty"`
	Standby          bool               `json:"standby,omitempty"`
	StandbyExpiresAt *time.Time         `json:"standby_expires_at,omitempty"`
	IssuedAt         *time.Time         `json:"issued_at,omitempty"`
	Transport        string             `json:"transport,omitempty"`
	Gateway          string             `json:"gateway,omitempty"`
	EdgeIP           string             `json:"edge_ip,omitempty"`
	CookieCount      int                `json:"cookie_count,omitempty"`
	CookieExpiresAt  *time.Time         `json:"cookie_expires_at,omitempty"`
	Probe            *CodexProbeSummary `json:"probe,omitempty"`
}

func OpenAICodexTicketStatuses(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []OpenAICodexTicketStatus {
	if !isOpenAICodexTicketAccount(account) || !cfg.Enabled {
		return nil
	}
	models := cfg.Models
	if models == nil {
		models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	out := make([]OpenAICodexTicketStatus, 0, len(models))
	for _, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" || !isOpenAICodexTicketAccount(account, model) {
			continue
		}
		status := OpenAICodexTicketStatus{Model: model}
		ticket := parseOpenAICodexTicketFromAny(0, model, nil)
		if account != nil && account.Extra != nil {
			ticket = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		}
		if ticket != nil && !ticketIdentityTreeMatches(account, ticket) {
			ticket = nil
		}
		usingStandby := false
		if ticket != nil && ticket.Standby != nil && ticket.Standby.validForAccount(account, now, cfg) {
			standbyExp := ticket.Standby.ExpiresAt
			status.StandbyExpiresAt = &standbyExp
			if !ticket.validForAccount(account, now, cfg) {
				ticket = ticket.Standby
				status.StandbyExpiresAt = nil
				usingStandby = true
			}
		}
		if ticket.validForAccount(account, now, cfg) {
			status.Ready = true
			status.Standby = usingStandby
			status.Length = ticket.Length
			remaining := int64(ticket.ExpiresAt.Sub(now) / time.Second)
			if remaining < 0 {
				remaining = 0
			}
			status.RemainingSeconds = remaining
			exp := ticket.ExpiresAt
			status.ExpiresAt = &exp
			if !ticket.IssuedAt.IsZero() {
				issued := ticket.IssuedAt
				status.IssuedAt = &issued
			}
			status.Transport = ticket.Transport
			status.Gateway = ticket.Gateway
			status.EdgeIP = ticket.EdgeIP
			if len(ticket.HarvestCookies) > 0 {
				status.CookieCount = len(ticket.HarvestCookies)
				if !ticket.HarvestCookiesAt.IsZero() {
					exp := ticket.HarvestCookiesAt.Add(openAICodexCredentialTTL)
					status.CookieExpiresAt = &exp
				}
			}
		}
		status.Blocked = cfg.FailClosed && !status.Ready && !openAICodexSkipHarvest(account)
		out = append(out, status)
	}
	return out
}

func (s *OpenAIGatewayService) manualHarvestLiveModels(account *Account, models []string) map[string]struct{} {
	got := map[string]struct{}{}
	if s == nil || account == nil {
		return got
	}
	cfg := s.openAICodexTicketConfig()
	now := time.Now()
	target := openAICodexTicketTargetLength(account, cfg)
	for _, model := range models {
		ticket := s.lookupOpenAICodexTicket(account, model)
		if ticket != nil && ticket.valid(now, target) {
			got[model] = struct{}{}
		}
	}
	return got
}

func (s *OpenAIGatewayService) markManualHarvestLiveModels(account *Account, models []string, got map[string]struct{}) {
	if got == nil {
		return
	}
	for model := range s.manualHarvestLiveModels(account, models) {
		got[model] = struct{}{}
	}
}

func (s *OpenAIGatewayService) accountHasLiveCodexTicket(account *Account) bool {
	if s == nil || account == nil {
		return false
	}
	return len(s.manualHarvestLiveModels(account, s.openAICodexTicketConfig().Models)) > 0
}

func (s *OpenAIGatewayService) openAICodexTicketEnabled() bool {
	return s.openAICodexTicketEnabledContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketEnabledContext(ctx context.Context) bool {
	if s == nil {
		return false
	}
	fallback := s.cfg != nil && s.cfg.Gateway.OpenAICodexTicket.Enabled
	if s.settingService != nil {
		return s.settingService.GetOpenAICodexTicketEnabled(ctx, fallback)
	}
	return fallback
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURL() string {
	return s.openAICodexTicketHarvestProxyURLContext(context.Background())
}

// openAICodexTicketHarvestProxySetting returns the configured harvest exit
// verbatim, including the IP-pool sentinel.
func (s *OpenAIGatewayService) openAICodexTicketHarvestProxySetting(ctx context.Context) string {
	if s.settingService != nil {
		if proxy := s.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(s.openAICodexTicketConfig().HarvestProxyURL)
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestIPPoolEnabled(ctx context.Context) bool {
	return s.openAICodexTicketHarvestProxySetting(ctx) == OpenAICodexTicketHarvestIPPoolURL
}

// A pool-harvested ticket records its concrete exit, so request pinning never
// needs the pool itself. Without a recorded exit it must fall back to the
// account's own proxy rather than an arbitrary pool member.
func (s *OpenAIGatewayService) openAICodexTicketHarvestFixedProxyURL(ctx context.Context) string {
	proxy := s.openAICodexTicketHarvestProxySetting(ctx)
	if proxy == OpenAICodexTicketHarvestIPPoolURL {
		return ""
	}
	return proxy
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURLContext(ctx context.Context) string {
	proxy := s.openAICodexTicketHarvestProxySetting(ctx)
	if proxy == OpenAICodexTicketHarvestIPPoolURL {
		exit, _ := s.pickHarvestIPPoolExit(ctx, nil)
		return exit.url
	}
	return proxy
}

type harvestIPPoolExit struct {
	url  string
	name string
}

// Membership is cached with a short TTL and rotated round-robin, so consecutive
// harvest attempts spread across the whole active pool without a per-attempt DB
// read. An empty pool yields no exit, which callers treat as "skip this round"
// rather than harvesting on a direct connection.
const harvestIPPoolRefreshTTL = 15 * time.Second

func (s *OpenAIGatewayService) pickHarvestIPPoolExit(ctx context.Context, tried map[string]bool) (harvestIPPoolExit, bool) {
	s.refreshHarvestIPPool(ctx)
	s.harvestIPPoolMu.Lock()
	exits := s.harvestIPPoolExits
	s.harvestIPPoolMu.Unlock()
	if len(exits) == 0 {
		return harvestIPPoolExit{}, false
	}
	start := s.harvestIPPoolCursor.Add(1) - 1
	for i := range exits {
		exit := exits[(start+uint64(i))%uint64(len(exits))]
		if !tried[exit.url] {
			return exit, true
		}
	}
	return harvestIPPoolExit{}, false
}

func (s *OpenAIGatewayService) harvestIPPoolMember(ctx context.Context, proxyURL string) (harvestIPPoolExit, bool) {
	s.refreshHarvestIPPool(ctx)
	s.harvestIPPoolMu.Lock()
	defer s.harvestIPPoolMu.Unlock()
	for _, exit := range s.harvestIPPoolExits {
		if exit.url == proxyURL {
			return exit, true
		}
	}
	return harvestIPPoolExit{}, false
}

// pickHarvestIPPoolExitFor keeps a refresh on the exit its ticket was
// harvested from while that proxy is still an active pool member, then rotates.
func (s *OpenAIGatewayService) pickHarvestIPPoolExitFor(ctx context.Context, account *Account, model string, tried map[string]bool) (harvestIPPoolExit, bool) {
	if pinned := s.lookupOpenAICodexTicket(account, model); pinned != nil {
		if url := strings.TrimSpace(pinned.HarvestProxyURL); url != "" && !tried[url] {
			if exit, ok := s.harvestIPPoolMember(ctx, url); ok {
				return exit, true
			}
		}
	}
	return s.pickHarvestIPPoolExit(ctx, tried)
}

func (s *OpenAIGatewayService) refreshHarvestIPPool(ctx context.Context) {
	if s.proxyRepo == nil {
		return
	}
	now := time.Now()
	s.harvestIPPoolMu.Lock()
	fresh := now.Before(s.harvestIPPoolSyncedAt.Add(harvestIPPoolRefreshTTL))
	if !fresh {
		s.harvestIPPoolSyncedAt = now
	}
	s.harvestIPPoolMu.Unlock()
	if fresh {
		return
	}
	proxies, err := s.proxyRepo.ListActive(ctx)
	if err != nil {
		// Keep the last membership on a transient listing error rather than
		// collapsing the pool to empty and skipping every harvest round.
		logger.FromContext(ctx).Warn("openai_codex_ticket.ip_pool_refresh_failed", zap.Error(err))
		return
	}
	exits := make([]harvestIPPoolExit, 0, len(proxies))
	for i := range proxies {
		if proxies[i].IsExpired(now) {
			continue
		}
		exits = append(exits, harvestIPPoolExit{url: proxies[i].URL(), name: strings.TrimSpace(proxies[i].Name)})
	}
	s.harvestIPPoolMu.Lock()
	s.harvestIPPoolExits = exits
	s.harvestIPPoolMu.Unlock()
}

func (t *openAICodexTicket) valid(now time.Time, targetLen int) bool {
	if t == nil {
		return false
	}
	if t.Revoked {
		return false
	}
	if t.Length == 780 {
		if !t.ProbeVerified {
			return false
		}
		shape, err := parseOpenAICodexTicketShape(t.State)
		if err != nil || shape.IssuedAt.After(now.Add(30*time.Second)) || !now.Before(shape.IssuedAt.Add(openAICodexCredentialTTL)) || (t.Transport != "sse" && t.Transport != "websocket") || !codexTicketCookiesFresh(t, now) {
			return false
		}
		if _, _, err := codex780Route(t.HarvestCookies, t.Gateway, now); err != nil {
			return false
		}
		if targetLen != 780 {
			return false
		}
		state := strings.TrimSpace(t.State)
		if len(state) != 780 || t.Length != 780 {
			return false
		}
		if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
			return false
		}
		if !t.IssuedAt.IsZero() && (t.IssuedAt.After(now.Add(30*time.Second)) || !now.Before(t.IssuedAt.Add(codexTicketLifetime(t.Length)))) {
			return false
		}
		return true
	}
	state := strings.TrimSpace(t.State)
	if len(state) != targetLen || t.Length != targetLen {
		return false
	}
	if _, err := parseOpenAICodexTicketShape(state); err != nil && !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
		return false
	}
	shape, err := parseOpenAICodexTicketShape(state)
	if err != nil {
		return false
	}
	if t.Blocks != 0 && t.Blocks != shape.Blocks {
		return false
	}
	if !t.IssuedAt.IsZero() && !t.IssuedAt.Equal(shape.IssuedAt) {
		return false
	}
	if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
		return false
	}
	if !t.IssuedAt.IsZero() && (t.IssuedAt.After(now.Add(30*time.Second)) || !now.Before(t.IssuedAt.Add(codexTicketLifetime(t.Length)))) {
		return false
	}
	return true
}

func (t *openAICodexTicket) validForAccount(account *Account, now time.Time, cfg config.OpenAICodexTicketConfig) bool {
	if t == nil || !t.valid(now, openAICodexTicketTargetLength(account, cfg)) {
		return false
	}
	if t.Length == 780 {
		return true
	}
	shape, err := parseOpenAICodexTicketShape(t.State)
	return err == nil && shape.Blocks == openAICodexTicketExpectedBlocks(account)
}

func (t *openAICodexTicket) needsRefresh(now time.Time, refreshBefore time.Duration) bool {
	if t == nil || t.ExpiresAt.IsZero() {
		return true
	}
	if t.Length == 780 && refreshBefore > 60*time.Second {
		refreshBefore = 60 * time.Second
	}
	return !openAICodexTicketRemainingUntil(t).After(now.Add(refreshBefore))
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicket(account *Account, model string) *openAICodexTicket {
	if s == nil {
		return nil
	}
	s.openaiCodexTicketStateMu.Lock()
	defer s.openaiCodexTicketStateMu.Unlock()
	return s.lookupOpenAICodexTicketLocked(account, model)
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicketLocked(account *Account, model string) *openAICodexTicket {
	if account != nil && (account.AuthConnectionID != "" || account.HasManagedOpenAIConnections()) {
		return nil
	}
	if s == nil || account == nil || account.ID <= 0 {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" {
		return nil
	}
	key := openAICodexTicketKey(account.ID, model)
	cfg := s.openAICodexTicketConfig()
	now := time.Now()
	targetLen := openAICodexTicketTargetLength(account, cfg)
	var mem *openAICodexTicket
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		mem, _ = raw.(*openAICodexTicket)
	}
	var extra *openAICodexTicket
	if account.Extra != nil {
		extra = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	}
	if mem != nil && !ticketIdentityTreeMatches(account, mem) {
		mem = nil
	}
	if extra != nil && !ticketIdentityTreeMatches(account, extra) {
		extra = nil
	}
	if targetLen == 780 {
		controls, _ := s.harvestControls(context.Background())
		protocol := controls.Transport
		if protocol == "" {
			protocol = "sse"
		}
		if mem != nil && mem.Transport != protocol {
			mem = nil
		}
		if extra != nil && extra.Transport != protocol {
			extra = nil
		}
	}
	if mem != nil && mem.Revoked {
		return mem
	}
	if mem != nil && !mem.validForAccount(account, now, cfg) && mem.Standby != nil && mem.Standby.validForAccount(account, now, cfg) {
		promoted := *mem.Standby
		promoted.Standby = nil
		s.openaiCodexTickets.Store(key, &promoted)
		return &promoted
	}
	if extra != nil && !extra.validForAccount(account, now, cfg) && extra.Standby != nil && extra.Standby.validForAccount(account, now, cfg) {
		promoted := *extra.Standby
		promoted.Standby = nil
		extra = &promoted
	}
	if extra.validForAccount(account, now, cfg) && (mem == nil || extra.CapturedAt.After(mem.CapturedAt)) {
		s.openaiCodexTickets.Store(key, extra)
		return extra
	}
	if mem.validForAccount(account, now, cfg) {
		return mem
	}
	if extra != nil {
		s.openaiCodexTickets.Store(key, extra)
		return extra
	}
	if mem != nil {
		s.openaiCodexTickets.Delete(key)
	}
	return nil
}

func parseOpenAICodexTicketFromAny(accountID int64, model string, raw any) *openAICodexTicket {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ticket openAICodexTicket
	if err := json.Unmarshal(b, &ticket); err != nil {
		return nil
	}
	ticket.AccountID = accountID
	if strings.TrimSpace(model) != "" {
		ticket.Model = model
	}
	ticket.State = strings.TrimSpace(ticket.State)
	if ticket.Length == 0 {
		ticket.Length = len(ticket.State)
	}
	if ticket.State == "" {
		return nil
	}
	if !normalizeOpenAICodexTicketShape(&ticket) {
		return nil
	}
	return &ticket
}

func normalizeOpenAICodexTicketShape(ticket *openAICodexTicket) bool {
	if ticket == nil || strings.TrimSpace(ticket.State) == "" {
		return false
	}
	shape, err := parseOpenAICodexTicketShape(ticket.State)
	if err != nil {
		return false
	}
	if ticket.Length == 0 {
		ticket.Length = len(ticket.State)
	}
	if ticket.Length != len(ticket.State) || (ticket.Blocks != 0 && ticket.Blocks != shape.Blocks) {
		return false
	}
	ticket.Blocks = shape.Blocks
	ticket.IssuedAt = shape.IssuedAt
	if ticket.Standby != nil && !normalizeOpenAICodexTicketShape(ticket.Standby) {
		ticket.Standby = nil
	}
	return true
}

func (s *OpenAIGatewayService) storeOpenAICodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) error {
	if account != nil && (account.AuthConnectionID != "" || account.HasManagedOpenAIConnections()) {
		return errors.New("managed authorization cannot inherit account ticket or harvest state")
	}
	if s == nil || account == nil || ticket == nil || account.ID <= 0 {
		return errors.New("invalid ticket store input")
	}
	if ticket.Length == 780 && !ticket.valid(time.Now(), 780) {
		return errors.New("ticket probe validation required before storage")
	}
	incoming := *ticket
	expectedBlocks := openAICodexTicketExpectedBlocks(account)
	if incoming.Length == 780 {
		expectedBlocks = incoming.Blocks
	}
	if !normalizeOpenAICodexTicketShape(&incoming) || (expectedBlocks != 0 && incoming.Blocks != expectedBlocks) {
		return errors.New("invalid ticket shape")
	}
	incoming.Identity = ticketIdentity(account)
	incoming.Model = normalizeOpenAICodexTicketModel(incoming.Model)
	incoming.AccountID = account.ID
	if incoming.Model == "" {
		return errors.New("empty ticket model")
	}
	s.openaiCodexTicketStateMu.Lock()
	defer s.openaiCodexTicketStateMu.Unlock()
	key := openAICodexTicketKey(account.ID, incoming.Model)
	cfg := s.openAICodexTicketConfig()
	current := s.lookupOpenAICodexTicketLocked(account, incoming.Model)
	now := time.Now()
	stored := &incoming
	if current != nil && current.validForAccount(account, now, cfg) && current.State != incoming.State {
		primary := *current
		primary.Standby = &incoming
		stored = &primary
	} else if current != nil && current.validForAccount(account, now, cfg) && current.State == incoming.State {
		// A repeated probe must not discard a still-useful standby ticket.
		stored = current
	}
	s.openaiCodexTickets.Store(key, stored)
	if s.accountRepo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		openAICodexTicketExtraKey(incoming.Model): stored,
	}); err != nil {
		logger.L().Warn("openai_codex_ticket persist failed",
			zap.Int64("account_id", account.ID),
			zap.String("model", incoming.Model),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// applyOpenAICodexTicket 在出站请求上覆盖 x-codex-turn-state。
// 请求路径只注入已捕获的有效门票，不现场打票；无票则返回
// ErrOpenAICodexTicketUnavailable。打票由后台 harvester 完成。
func (s *OpenAIGatewayService) applyOpenAICodexTicket(ctx context.Context, account *Account, model string, h http.Header, transport ...string) error {
	if s == nil || h == nil || !isOpenAICodexTicketAccount(account, model) {
		return nil
	}
	if s.codexTurnStateOwner(ctx, account) != codexTurnStateOwnerLegacyTicket {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketGatedModel(model) {
		return nil
	}
	cfg := s.openAICodexTicketConfig()
	if !openAICodexSkipHarvest(account) && cfg.FailClosed && s.openAICodexTicketHarvestExcluded(account) {
		return denyOpenAITicket()
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	if ticket.validForAccount(account, time.Now(), cfg) {
		protocol := "sse"
		if (len(transport) > 0 && transport[0] == "websocket") || h.Get("OpenAI-Beta") == openAIWSBetaV1Value || h.Get("OpenAI-Beta") == openAIWSBetaV2Value || strings.EqualFold(h.Get("Upgrade"), "websocket") {
			protocol = "websocket"
		}
		if ticket.Transport != "" && ticket.Transport != protocol {
			if h.Get(openAICodexTurnStateHeader) == ticket.State {
				h.Del(openAICodexTurnStateHeader)
			}
			if cfg.FailClosed {
				return denyOpenAITicket()
			}
			return nil
		}
		h.Set(openAICodexTurnStateHeader, ticket.State)
		return nil
	}
	if openAICodexSkipHarvest(account) {
		return nil
	}
	if !cfg.FailClosed {
		return nil
	}
	return denyOpenAITicket()
}

func harvestTicketSessionID(ticket *openAICodexTicket) string {
	if ticket == nil || ticket.Length == 780 {
		return ""
	}
	return strings.TrimSpace(ticket.HarvestSessionID)
}

func (s *OpenAIGatewayService) harvestPinnedSessionForModel(ctx context.Context, account *Account, model string) string {
	if s == nil || account == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.openAICodexTicketEnabledContext(ctx) {
		return ""
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketGatedModel(model) {
		return ""
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	if !ticket.validForAccount(account, time.Now(), s.openAICodexTicketConfig()) {
		return ""
	}
	return harvestTicketSessionID(ticket)
}

func (s *OpenAIGatewayService) harvestPinsCodexIdentity(ctx context.Context, account *Account, model string) bool {
	return s.harvestPinnedSessionForModel(ctx, account, model) != ""
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestExcluded(account *Account) bool {
	if s == nil || s.settingService == nil || account == nil {
		return false
	}
	scope, err := s.settingService.GetCodexTicketHarvestScope(context.Background())
	if err != nil {
		return true
	}
	return !scope.includes(account)
}

func (s *OpenAIGatewayService) openAICodexTicketReadyForRequest(account *Account, requestedModel string, requireCompact bool) bool {
	if s == nil || account == nil || !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabled() {
		return false
	}
	outbound := s.openAICodexTicketOutboundModel(account, requestedModel, requireCompact)
	if !isOpenAICodexTicketAccount(account, outbound) {
		return false
	}
	model := normalizeOpenAICodexTicketModel(outbound)
	if model == "" || !s.openAICodexTicketGatedModel(model) {
		return false
	}
	if account.AuthConnectionID != "" {
		h.Del("Cookie")
		h.Del(openAICodexTurnStateHeader)
		return ErrOpenAICodexTicketUnavailable
	}
	cfg := s.openAICodexTicketConfig()
	if cfg.FailClosed && !openAICodexSkipHarvest(account) && s.openAICodexTicketHarvestExcluded(account) {
		return false
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	return ticket.valid(time.Now(), openAICodexTicketTargetLength(account, cfg))
}

const openAICodexTicketWakeMinInterval = time.Second

// openAICodexTicketOutboundModel 预测本请求真正出站的模型名，也就是
// applyOpenAICodexTicket 注入时读到的 body.model。
//
// 调度门控与注入必须按同一个模型名判定门票。普通请求下二者同源：Forward 的
// upstreamModel 与本函数都走 resolveOpenAIAccountUpstreamModelForRequest，且
// Forward 会把 body.model 改写成该值后才注入。但 /responses/compact 例外——
// Forward 会把出站模型进一步改写为 compact 映射或 gateway.openai_compact_model
// （默认非空），此时若门控仍按客户端原始模型判定，就会把「实际出站是非门控
// 模型、根本不需要票」的 compact 请求整片误拦成不可调度。
func (s *OpenAIGatewayService) openAICodexTicketOutboundModel(account *Account, requestedModel string, requireCompact bool) string {
	if account.IsExcelBPSEnabledForModel(requestedModel) {
		return account.GetMappedModel(requestedModel)
	}
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if !account.IsOpenAI() {
		return canonicalOpenAIAccountSchedulingModel(account, model)
	}
	_, upstreamModel := resolveOpenAIForwardMappedModels(account, model, requireCompact)
	if requireCompact {
		// 与 Forward 同序：compact 兜底模型优先于普通/compact 映射结果。
		if compactModel := strings.TrimSpace(s.resolveOpenAICompactFallbackModel(account, model)); compactModel != "" {
			upstreamModel = compactModel
		}
	}
	if upstreamModel = strings.TrimSpace(upstreamModel); upstreamModel != "" {
		return upstreamModel
	}
	return model
}

// outboundModel 必须是真正会发给上游的模型名（openAICodexTicketOutboundModel），
// 不是客户端原始模型：注入侧读的是出站 body.model，两侧口径必须一致。
func (s *OpenAIGatewayService) openAICodexTicketBlocksAccount(account *Account, outboundModel string) bool {
	if account.isPrismBrowserUpstreamModelEnabled(outboundModel) {
		return false
	}
	if s == nil || !isOpenAICodexTicketAccount(account, outboundModel) || !s.openAICodexTicketEnabled() {
		return false
	}
	if s.codexTurnStateOwner(context.Background(), account) != codexTurnStateOwnerLegacyTicket {
		return false
	}
	cfg := s.openAICodexTicketConfig()
	if !cfg.FailClosed {
		return false
	}
	model := normalizeOpenAICodexTicketModel(outboundModel)
	if !s.openAICodexTicketGatedModel(model) {
		return false
	}
	if openAICodexSkipHarvest(account) {
		return false
	}
	if s.openAICodexTicketHarvestExcluded(account) {
		return true
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	return !ticket.validForAccount(account, time.Now(), cfg)
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbe(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration) (state string, status int, err error) {
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	body := []byte(`{"model":` + jsonString(model) + `,"store":false,"stream":true,"instructions":"Reply with exactly: pong","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAIHarvest))
	req.Close = true
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", uuid.NewString())
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(attemptCtx, s.accountRepo, req.Header, account); err != nil {
		return "", 0, err
	}
	applyOpenAICodexTicketHarvestIdentity(req.Header, model)

	// Synthetic probes must use the dedicated no-reuse transport even when the
	// production account is bound to a plugin. This also avoids reading pluginManager
	// while handlers are still wiring it during gateway construction.
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", 0, err
	}
	if resp == nil {
		return "", 0, errors.New("nil upstream response")
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.Body == nil {
		return "", resp.StatusCode, errors.New("probe response body is empty")
	}
	const maxProbeBody = 2 << 20
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProbeBody+1))
	if readErr != nil {
		return "", resp.StatusCode, readErr
	}
	if len(bodyBytes) > maxProbeBody {
		return "", resp.StatusCode, errors.New("probe response body too large")
	}
	if resp.StatusCode == http.StatusOK {
		if validateErr := validateCodexProbeResponse(bodyBytes, model); validateErr != nil {
			return "", resp.StatusCode, validateErr
		}
	}
	return extractOpenAICodexTurnState(resp.Header), resp.StatusCode, nil
}

func (s *OpenAIGatewayService) ticketProbeCoolingDown(accountID int64, model string, now time.Time) bool {
	if s == nil {
		return false
	}
	key := openAICodexTicketKey(accountID, model)
	value, _ := s.openaiCodexTicketProbeCooldown.Load(key)
	until, ok := value.(time.Time)
	if !ok || !until.After(now) {
		s.openaiCodexTicketProbeCooldown.Delete(key)
		return false
	}
	return true
}

func (s *OpenAIGatewayService) cooldownTicketProbe(accountID int64, model string, cfg config.OpenAICodexTicketConfig) {
	if s == nil {
		return
	}
	s.openaiCodexTicketProbeCooldown.Store(openAICodexTicketKey(accountID, model), time.Now().Add(time.Duration(cfg.HarvestCooldownSeconds)*time.Second))
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

func applyOpenAICodexTicketHarvestIdentity(h http.Header, model string) {
	ensureCodexIdentityHeaders(h)
	enforceCodexIdentityHeaders(h)
	version := strings.TrimSpace(h.Get("version"))
	if needsOpenAICodexAstraVersion(model) && (version == "" || CompareVersions(version, openAICodexAstraMinVersion) < 0) {
		h.Set("version", openAICodexAstraMinVersion)
		h.Set("user-agent", buildCodexCLIUserAgent(openAICodexAstraMinVersion))
		h.Set("originator", openai.CodexDefaultOriginator)
	}
}

func needsOpenAICodexAstraVersion(model string) bool {
	m := strings.ToLower(normalizeOpenAICodexTicketModel(model))
	return strings.Contains(m, "gpt-6") || strings.Contains(m, "astra")
}

func (s *OpenAIGatewayService) StartOpenAICodexTicketHarvester() {
	if s == nil || !s.cfg.RunsBackgroundJobs() {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	if s.openaiCodexTicketStopped || s.openaiCodexTicketDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.openaiCodexTicketCancel = cancel
	s.openaiCodexTicketDone = done
	go func() {
		defer close(done)
		coverageDone := make(chan struct{})
		go func() { defer close(coverageDone); s.harvestCoverageLoop(ctx) }()
		defer func() { cancel(); <-coverageDone }()
		s.openAICodexTicketHarvestLoop(ctx)
	}()
	logger.L().Info("openai_codex_ticket harvester started",
		zap.Int("ttl_seconds", s.openAICodexTicketConfig().TTLSeconds),
		zap.Strings("models", s.openAICodexTicketConfig().Models),
	)
}

func (s *OpenAIGatewayService) StopOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	s.openaiCodexTicketStopped = true
	cancel, done := s.openaiCodexTicketCancel, s.openaiCodexTicketDone
	s.openaiCodexTicketLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	var controlWake <-chan struct{}
	if s.codexHarvest != nil {
		controlWake = s.codexHarvest.wake
	}
	settingsWake := s.settingService.codexHarvestWakeups()
	var lastRound time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-controlWake:
			s.resetHarvestWakeTimer(timer, lastRound)
		case <-settingsWake:
			s.resetHarvestWakeTimer(timer, lastRound)
		case <-timer.C:
			s.refreshOpenAICodexTickets(ctx)
			lastRound = time.Now()
			s.armHarvestIntervalTimer(ctx, timer, lastRound)
		}
	}
}

func (s *OpenAIGatewayService) resetHarvestWakeTimer(timer *time.Timer, lastRound time.Time) {
	delay := time.Until(lastRound.Add(openAICodexTicketWakeMinInterval))
	if delay < 0 {
		delay = 0
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
	next := time.Now().Add(delay)
	if s.codexHarvest != nil {
		s.codexHarvest.setRuntime(func(r *CodexHarvestRuntime) { r.NextRoundAt = &next })
	}
}

func (s *OpenAIGatewayService) armHarvestIntervalTimer(ctx context.Context, timer *time.Timer, lastRound time.Time) {
	controls, _ := s.harvestControls(ctx)
	next := lastRound.Add(time.Duration(controls.Speed.RoundIntervalSeconds) * time.Second)
	if s.codexHarvest != nil {
		s.codexHarvest.setRuntime(func(r *CodexHarvestRuntime) { r.NextRoundAt = &next })
	}
	delay := time.Until(next)
	if delay < 0 {
		delay = 0
	}
	timer.Reset(delay)
}

// refreshOpenAICodexTickets probes each account/model with a missing or soon-to-expire
// ticket once. The loop waits for all probes, then waits the configured interval
// before starting the next cycle.
func (s *OpenAIGatewayService) refreshOpenAICodexTickets(ctx context.Context) {
	if s == nil || s.accountRepo == nil || ctx.Err() != nil {
		return
	}
	// Prime the runtime setting while preserving explicit per-account legacy mode.
	_ = s.openAICodexTicketEnabledContext(ctx)
	if !s.codexHarvestRoundActive.CompareAndSwap(false, true) {
		return
	}
	defer s.codexHarvestRoundActive.Store(false)
	controls, configured := s.harvestControls(ctx)
	legacyParallel := !configured && !controls.NodeMemoryEnabled
	round := &codexHarvestRound{limit: controls.Speed.MaxRequestsPerRound}
	if round.limit <= 0 {
		round.limit = 6
	}
	ctx = context.WithValue(ctx, codexHarvestRoundKey{}, round)
	if s.codexHarvest != nil {
		s.codexHarvest.setRuntime(func(r *CodexHarvestRuntime) {
			r.Running = true
			r.RequestsUsed = 0
			r.RequestBudget = round.limit
			r.NextRoundAt = nil
		})
		defer s.codexHarvest.setRuntime(func(r *CodexHarvestRuntime) { r.Running = false; r.CurrentNode = "" })
	}
	observeCodexHarvestProxy(ctx, s.openAICodexTicketHarvestProxySetting(ctx))
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		logger.L().Warn("openai_codex_ticket list accounts failed", zap.Error(err))
		return
	}
	cfg := s.harvestTicketConfig(ctx)
	now := time.Now()
	refreshBefore := time.Duration(cfg.RefreshBeforeSeconds) * time.Second
	scope := CodexTicketHarvestScope{Mode: "all"}
	if s.settingService != nil {
		scope, err = s.settingService.GetCodexTicketHarvestScope(ctx)
		if err != nil {
			logger.L().Warn("openai_codex_ticket harvest scope unavailable; skipping round", zap.Error(err))
			return
		}
	}

	// Build independent priority queues. A duplicated account (for example from
	// a group join) must only consume one account/model slot.
	tiers := make(map[codexHarvestTier][]Account)
	seen := make(map[int64]bool, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if account.HasManagedOpenAIConnections() || seen[account.ID] || openAICodexSkipHarvest(&account) || !scope.includes(&account) || !scope.allowsAccount(&account) || !isOpenAICodexTicketAccount(&account) || s.codexTurnStateOwner(ctx, &account) != codexTurnStateOwnerLegacyTicket {
			continue
		}
		seen[account.ID] = true
		tier := scope.tier(&account)
		tiers[tier] = append(tiers[tier], account)
	}

	keys := make([]codexHarvestTier, 0, len(tiers))
	for tier := range tiers {
		keys = append(keys, tier)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Schedulable != keys[j].Schedulable {
			return keys[i].Schedulable
		}
		if keys[i].Priority != keys[j].Priority {
			return keys[i].Priority < keys[j].Priority
		}
		return keys[i].AccountPriority < keys[j].AccountPriority
	})
	// Drop cursors for tiers no longer present after a group or priority change.
	s.openaiCodexTicketCursors.Range(func(key, _ any) bool {
		tier, ok := key.(codexHarvestTier)
		if !ok {
			return true
		}
		if _, exists := tiers[tier]; !exists {
			s.openaiCodexTicketCursors.Delete(key)
		}
		return true
	})

	counts := [2]int{}
	probeCtx := context.WithValue(ctx, codexTicketHarvestContextKey{}, true)
	for _, tier := range keys {
		pool := tiers[tier]
		sort.Slice(pool, func(i, j int) bool { return pool[i].ID < pool[j].ID })
		if ctx.Err() != nil || round.Used() >= round.limit {
			break
		}
		total := len(pool) * len(cfg.Models)
		if total == 0 {
			continue
		}
		stored, _ := s.openaiCodexTicketCursors.LoadOrStore(tier, &atomic.Uint64{})
		cursor := stored.(*atomic.Uint64)
		start := int(cursor.Load() % uint64(total))
		before, scheduled := round.Used(), 0
		var wg sync.WaitGroup
		for offset := 0; offset < total && round.Used() < round.limit && ctx.Err() == nil; offset++ {
			if legacyParallel && scheduled >= round.limit-before {
				break
			}
			index := (start + offset) % total
			cursor.Store(uint64((index + 1) % total))
			account := pool[index/len(cfg.Models)]
			model := normalizeOpenAICodexTicketModel(cfg.Models[index%len(cfg.Models)])
			if model == "" || s.ticketProbeCoolingDown(account.ID, model, now) {
				continue
			}
			ticket := s.lookupOpenAICodexTicket(&account, model)
			if ticket.validForAccount(&account, now, cfg) && !ticket.needsRefresh(now, refreshBefore) {
				continue
			}
			if ticket != nil && ticket.Standby != nil && ticket.Standby.validForAccount(&account, now, cfg) && !ticket.Standby.needsRefresh(now, refreshBefore) {
				continue
			}
			acc := account
			acc.Extra = maps.Clone(account.Extra)
			acc.Credentials = maps.Clone(account.Credentials)
			if legacyParallel {
				scheduled++
				wg.Add(1)
				go func(acc Account, model string) {
					defer wg.Done()
					s.probeOnceOpenAICodexTicket(probeCtx, &acc, model)
				}(acc, model)
			} else {
				s.probeOnceOpenAICodexTicket(probeCtx, &acc, model)
			}
		}
		wg.Wait()
		if tier.Schedulable {
			counts[0] += round.Used() - before
		} else {
			counts[1] += round.Used() - before
		}
	}
	if round.Used() > 0 {
		logger.L().Info("openai_codex_ticket probe cycle", zap.Int("probed", round.Used()),
			zap.Int("schedulable_probed", counts[0]), zap.Int("deferred_probed", counts[1]),
			zap.String("scope", scope.Mode), zap.Int("selected_groups", len(scope.GroupIDs)))
	}
}

// IsOpenAICodexTicketExtraKey identifies server-managed ticket material.
func IsOpenAICodexTicketExtraKey(key string) bool {
	return strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix)
}

// MergeOpenAICodexTicketExtra preserves only persisted tickets, never summaries or
// blobs supplied by an account edit. The repository repeats this under the row
// lock so a concurrent harvest cannot be overwritten by a stale admin snapshot.
func MergeOpenAICodexTicketExtra(extra, current map[string]any) map[string]any {
	result := maps.Clone(extra)
	for key := range result {
		if IsOpenAICodexTicketExtraKey(key) || key == ExcelBPSCredentialStateExtraKey {
			delete(result, key)
		}
	}
	for key, value := range current {
		if IsOpenAICodexTicketExtraKey(key) || key == ExcelBPSCredentialStateExtraKey {
			if result == nil {
				result = make(map[string]any)
			}
			result[key] = value
		}
	}
	return result
}

// ValidateOpenAICodexTicketHarvestProxyURL validates only syntax, without making
// a network request or including credentials in validation errors.
func ValidateOpenAICodexTicketHarvestProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// The IP 管理 pool sentinel is not a dialable exit; it is resolved per attempt.
	if raw == OpenAICodexTicketHarvestIPPoolURL {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("harvest proxy must be an HTTP(S) or SOCKS5(h) URL with a host and no path, query or fragment")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("harvest proxy scheme must be http, https, socks5 or socks5h")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("harvest proxy port must be between 1 and 65535")
		}
	}
	return nil
}

// MaskProxyURL never returns a stored proxy password, even for invalid legacy data.
func MaskProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || ValidateOpenAICodexTicketHarvestProxyURL(raw) != nil {
		return ""
	}
	// The pool sentinel carries no credentials; surface it verbatim so the panel
	// can restore the "IP 管理 pool" selection after save.
	if raw == OpenAICodexTicketHarvestIPPoolURL {
		return raw
	}
	parsed, _ := url.Parse(raw)
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

// IsMaskedProxyURL recognizes the exact password placeholder emitted by the API.
func IsMaskedProxyURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return false
	}
	password, ok := parsed.User.Password()
	return ok && password == "***"
}

// Credential shadows do not own tickets. Keep their existing forwarding policy
// instead of imposing a gate for a key the harvester never populates.
func isOpenAICodexTicketAccount(account *Account, upstreamModels ...string) bool {
	if account.IsSIWCEnabled() {
		return false
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() || account.isExcelBPSAllModelsEnabled() {
		return false
	}
	return len(upstreamModels) == 0 || !account.isExcelBPSUpstreamModelEnabled(upstreamModels[0])
}

// IsOpenAICodexTurnStatePrivateExtraKey also protects retired State Pool data
// that may still exist in older account records.
func IsOpenAICodexTurnStatePrivateExtraKey(key string) bool {
	return IsOpenAICodexTicketExtraKey(key) || key == "codex_harvest_proxy_url" || strings.HasPrefix(key, "codex_turn_state_")
}

// StripRetiredCodexTurnStateExtra prevents imports and account updates from
// persisting configuration or credentials for the removed State Pool engine.
func StripRetiredCodexTurnStateExtra(extra map[string]any) {
	for key := range extra {
		if strings.HasPrefix(key, "codex_turn_state_") {
			delete(extra, key)
		}
	}
}

// RedactOpenAICodexTurnStateExtra strips ticket and retired State Pool data from exports
// without changing the source account or unrelated backup fields.
func RedactOpenAICodexTurnStateExtra(extra map[string]any) map[string]any {
	redacted := maps.Clone(extra)
	for key := range redacted {
		if IsOpenAICodexTurnStatePrivateExtraKey(key) {
			delete(redacted, key)
		}
	}
	return redacted
}
