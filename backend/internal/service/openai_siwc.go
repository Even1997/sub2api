package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

const OpenAISIWCEnabledKey = "openai_siwc"

type OpenAISIWCOAuthRepository interface {
	SIWCRegistration(context.Context, int64, string) (string, string, error)
	GetSIWCCredentials(context.Context, int64) (string, error)
	CreateSIWCTask(context.Context, int64, string) (*OpenAIOAuthReauthTaskRecord, error)
	SaveSIWCCredentials(context.Context, int64, map[string]any, string, string, bool, *OpenAIOAuthReauthTaskRecord) (bool, error)
}
type openAISIWCTokenClient interface {
	SIWCToken(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error)
}

type siwcGrant struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ClientID     string `json:"client_id"`
	HostID       string `json:"host_id"`
	Subject      string `json:"subject"`
	Email        string `json:"email"`
	Workspace    string `json:"workspace"`
	Scope        string `json:"scope"`
	ExpiresAt    int64  `json:"expires_at"`
}

type OpenAISIWCStatus struct {
	Bound     bool                   `json:"bound"`
	Enabled   bool                   `json:"enabled"`
	Task      *OpenAIOAuthReauthTask `json:"task,omitempty"`
	ExpiresAt int64                  `json:"expires_at,omitempty"`
}

func (a *Account) IsSIWCEnabled() bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth && !a.IsShadow() && !a.IsOpenAIPersonalAccessToken() && !a.IsOpenAIAgentIdentity() && a.Extra[OpenAISIWCEnabledKey] == true
}

func (s *OpenAIOAuthReauthService) siwcRepository() (OpenAISIWCOAuthRepository, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := s.ensureDurableEncryption(); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(OpenAISIWCOAuthRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_UNAVAILABLE", "SIWC credential storage is unavailable")
	}
	return repo, nil
}
func (s *OpenAIOAuthReauthService) readSIWCGrant(ctx context.Context, id int64) (string, *siwcGrant, error) {
	repo, err := s.siwcRepository()
	if err != nil {
		return "", nil, err
	}
	ciphertext, err := repo.GetSIWCCredentials(ctx, id)
	if err != nil {
		return "", nil, errors.New("failed to read SIWC credentials")
	}
	if ciphertext == "" {
		return "", nil, nil
	}
	plain, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", nil, errors.New("failed to decrypt SIWC credentials")
	}
	var grant siwcGrant
	if json.Unmarshal([]byte(plain), &grant) != nil || grant.ClientID == "" || grant.Subject == "" {
		return "", nil, errors.New("invalid saved SIWC credentials")
	}
	return ciphertext, &grant, nil
}
func (s *OpenAIOAuthReauthService) SIWCStatus(ctx context.Context, id int64) (*OpenAISIWCStatus, error) {
	account, err := s.accountFor(ctx, id)
	if err != nil {
		return nil, err
	}
	_, grant, err := s.readSIWCGrant(ctx, id)
	if err != nil {
		return nil, err
	}
	status := &OpenAISIWCStatus{Enabled: account.IsSIWCEnabled()}
	if grant != nil {
		status.Bound = grant.AccessToken != "" && grant.RefreshToken != "" && openai.HasSIWCPermission(grant.Scope)
		status.ExpiresAt = grant.ExpiresAt
	}
	record, err := s.latestTaskForProfile(ctx, id, "siwc")
	if err != nil {
		return nil, err
	}
	if record != nil && record.OAuthProfile == "siwc" {
		status.Task = safeReauthTask(record)
	}
	return status, nil
}

// SIWCAccountSnapshot is the non-secret list row. Bound means a grant ciphertext
// exists. The list does not decrypt it, so access-token expiry is not available.
type SIWCAccountSnapshot struct {
	AccountID int64
	Bound     bool
	Enabled   bool
	Task      *OpenAIOAuthReauthTaskRecord
}

type siwcStatusLister interface {
	ListSIWCStatuses(context.Context, []int64) ([]SIWCAccountSnapshot, error)
}

type accountExtraWriter interface {
	UpdateAccountExtra(context.Context, int64, map[string]any) error
}

type OpenAISIWCListItem struct {
	AccountID   int64                  `json:"account_id"`
	Bound       bool                   `json:"bound"`
	Refreshable bool                   `json:"refreshable"`
	Enabled     bool                   `json:"enabled"`
	Task        *OpenAIOAuthReauthTask `json:"task,omitempty"`
}

func normalizeSIWCStatusIDs(ids []int64) ([]int64, error) {
	if len(ids) > 200 {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_STATUS_TOO_MANY", "at most 200 accounts can be queried")
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// ListSIWCStatuses reports binding and the latest SIWC task without decrypting grants.
// A stored ciphertext is treated as refreshable. Access-token expiry is not a failure.
func (s *OpenAIOAuthReauthService) ListSIWCStatuses(ctx context.Context, ids []int64) ([]OpenAISIWCListItem, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_UNAVAILABLE", "SIWC status listing is unavailable")
	}
	lister, ok := s.repo.(siwcStatusLister)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_UNAVAILABLE", "SIWC status listing is unavailable")
	}
	clean, err := normalizeSIWCStatusIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(clean) == 0 {
		return []OpenAISIWCListItem{}, nil
	}
	rows, err := lister.ListSIWCStatuses(ctx, clean)
	if err != nil {
		return nil, err
	}
	items := make([]OpenAISIWCListItem, 0, len(rows))
	for _, row := range rows {
		item := OpenAISIWCListItem{AccountID: row.AccountID, Bound: row.Bound, Refreshable: row.Bound, Enabled: row.Enabled}
		if row.Task != nil && row.Task.OAuthProfile == "siwc" {
			item.Task = safeReauthTask(row.Task)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *OpenAIOAuthReauthService) SetSIWCEnabled(ctx context.Context, id int64, enabled bool) error {
	account, err := s.accountFor(ctx, id)
	if err != nil {
		return err
	}
	writer, ok := s.accounts.(accountExtraWriter)
	if !ok {
		return infraerrors.ServiceUnavailable("OPENAI_SIWC_UNAVAILABLE", "SIWC protocol setting is unavailable")
	}
	return writer.UpdateAccountExtra(ctx, account.ID, map[string]any{OpenAISIWCEnabledKey: enabled})
}

// Each attempt binds the exact account snapshot, grant, nonce, PKCE and egress.
// Reuse the host and issued client ID without copying any Codex bearer token.
func (s *OpenAIOAuthReauthService) BeginSIWCAuthorization(ctx context.Context, id int64) (*OpenAIAuthURLResult, error) {
	account, err := s.accountFor(ctx, id)
	if err != nil {
		return nil, err
	}
	proxy, err := s.reauthProxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	return s.beginSIWCAuthorization(ctx, account, proxy)
}
func (s *OpenAIOAuthReauthService) beginSIWCAuthorization(ctx context.Context, account *Account, proxy string) (*OpenAIAuthURLResult, error) {
	repo, err := s.siwcRepository()
	if err != nil {
		return nil, err
	}
	host, previous, err := repo.SIWCRegistration(ctx, account.ID, "urn:uuid:"+uuid.NewString())
	if err != nil {
		return nil, errors.New("failed to prepare SIWC host registration")
	}
	clientID, hint := "", ""
	if previous != "" {
		plain, decryptErr := s.encryptor.Decrypt(previous)
		if decryptErr != nil {
			return nil, errors.New("failed to decrypt SIWC registration")
		}
		var grant siwcGrant
		if json.Unmarshal([]byte(plain), &grant) != nil {
			return nil, errors.New("invalid SIWC registration")
		}
		clientID, hint = grant.ClientID, grant.IDToken
	}
	state, err := openai.GenerateState()
	if err != nil {
		return nil, err
	}
	verifier, err := openai.GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	nonce, err := openai.GenerateState()
	if err != nil {
		return nil, err
	}
	sessionID, err := openai.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	err = s.setSIWCSession(ctx, sessionID, &openai.OAuthSession{State: state, CodeVerifier: verifier, ClientID: clientID, ProxyURL: proxy,
		RedirectURI: openai.SIWCRedirectURI, CreatedAt: time.Now(), SIWCAccountID: account.ID, SIWCNonce: nonce, SIWCHostID: host,
		SIWCPreviousGrant: previous, SIWCExpectedCredentials: account.Credentials})
	if err != nil {
		return nil, err
	}
	return &OpenAIAuthURLResult{SessionID: sessionID, AuthURL: openai.BuildSIWCAuthorizationURL(state, openai.GenerateCodeChallenge(verifier), nonce, host, clientID, hint, account.GetCredential("email"))}, nil
}

func (s *OpenAIOAuthReauthService) CompleteSIWCAuthorization(ctx context.Context, id int64, sessionID, callback string, task *OpenAIOAuthReauthTaskRecord) error {
	repo, err := s.siwcRepository()
	if err != nil {
		return err
	}
	session, ok := s.readSIWCSession(ctx, sessionID, false)
	if !ok || session.SIWCAccountID != id || session.ConnectionID != "" {
		return infraerrors.BadRequest("OPENAI_SIWC_SESSION_INVALID", "SIWC session expired or belongs to another account")
	}
	if len(callback) > maxOpenAIOAuthReauthURL {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	extracted, ok := openai.ExtractSIWCCallback(callback)
	if !ok {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	callback = extracted
	u, err := url.Parse(callback)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:1455" || u.Path != "/auth/callback" || u.RawPath != "" || u.User != nil || u.Fragment != "" {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	for _, key := range []string{"state", "code", "client_id", "error"} {
		if len(q[key]) > 1 {
			return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Duplicate OAuth callback parameter")
		}
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(session.State)) != 1 {
		return infraerrors.BadRequest("OPENAI_SIWC_STATE_INVALID", "Invalid SIWC state")
	}
	if q.Get("error") != "" {
		s.deleteSIWCSession(ctx, sessionID)
		return infraerrors.BadRequest("OPENAI_SIWC_CONSENT_REQUIRED", "SIWC authorization was declined or requires user interaction")
	}
	clientID := q.Get("client_id")
	if session.ClientID != "" {
		if clientID != "" && clientID != session.ClientID {
			return infraerrors.BadRequest("OPENAI_SIWC_CLIENT_MISMATCH", "SIWC client changed")
		}
		clientID = session.ClientID
	}
	if clientID == "" || clientID == openai.SIWCDynamicClientID || q.Get("code") == "" {
		return infraerrors.BadRequest("OPENAI_SIWC_REGISTRATION_INCOMPLETE", "SIWC callback requires code and issued client ID")
	}
	// Consume before exchanging; failed exchanges need a fresh authorization.
	consumed, valid := s.readSIWCSession(ctx, sessionID, true)
	if !valid || consumed.State != session.State || consumed.SIWCAccountID != id {
		return infraerrors.Conflict("OPENAI_SIWC_SESSION_CONSUMED", "SIWC session was already used")
	}
	client, ok := s.oauth.oauthClient.(openAISIWCTokenClient)
	if !ok {
		return errors.New("SIWC token client is unavailable")
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	token, identity, err := client.SIWCToken(exchangeCtx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {q.Get("code")}, "code_verifier": {session.CodeVerifier}, "redirect_uri": {session.RedirectURI}}, session.ProxyURL, session.SIWCNonce)
	if err != nil {
		return err
	}
	if identity == nil || token.RefreshToken == "" {
		return errors.New("SIWC credentials are incomplete")
	}
	account, err := s.accountFor(ctx, id)
	if err != nil {
		return err
	}
	info, err := siwcBoundIdentity(token, identity.Email, account)
	if err != nil {
		return err
	}
	if err = validateReauthToken(account, info); err != nil {
		return err
	}
	if session.SIWCPreviousGrant != "" {
		plain, _ := s.encryptor.Decrypt(session.SIWCPreviousGrant)
		var previous siwcGrant
		if json.Unmarshal([]byte(plain), &previous) != nil || previous.Subject != identity.Subject {
			return errors.New("SIWC subscriber changed")
		}
	}
	// The issued client pins the authorized workspace upstream. Workspace below
	// is a local account snapshot, not a claim decoded from SIWC auth metadata.
	grant := &siwcGrant{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, IDToken: token.IDToken, ClientID: clientID, HostID: session.SIWCHostID, Subject: identity.Subject, Email: identity.Email, Workspace: account.GetCredential("chatgpt_account_id"), Scope: token.Scope, ExpiresAt: time.Now().Unix() + token.ExpiresIn}
	raw, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	ciphertext, err := s.encryptor.Encrypt(string(raw))
	if err != nil {
		return errors.New("failed to encrypt SIWC credentials")
	}
	persistCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	changed, err := repo.SaveSIWCCredentials(persistCtx, id, session.SIWCExpectedCredentials, session.SIWCPreviousGrant, ciphertext, true, task)
	if err != nil {
		return err
	}
	if !changed {
		return infraerrors.Conflict("OPENAI_SIWC_ACCOUNT_CHANGED", "Account or SIWC registration changed during authorization; retry")
	}
	return nil
}

// A Codex-shaped access or ID token still supplies the workspace. SIWC resource
// tokens are often opaque and carry no chatgpt_account_id; the signed ID token
// has already proved the email, so the existing account workspace is used only
// when that email matches.
func siwcBoundIdentity(token *openai.TokenResponse, email string, account *Account) (*OpenAITokenInfo, error) {
	if token != nil {
		if info, ok := siwcJWTIdentity(token.AccessToken, email); ok {
			return info, nil
		}
		if info, ok := siwcJWTIdentity(token.IDToken, email); ok {
			return info, nil
		}
	}
	storedEmail, workspace, userID := "", "", ""
	if account != nil {
		storedEmail = strings.TrimSpace(account.GetCredential("email"))
		workspace = strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
		userID = strings.TrimSpace(account.GetCredential("chatgpt_user_id"))
	}
	if workspace != "" && storedEmail != "" && strings.EqualFold(storedEmail, strings.TrimSpace(email)) {
		return &OpenAITokenInfo{Email: email, ChatGPTAccountID: workspace, ChatGPTUserID: userID}, nil
	}
	return nil, errors.New("SIWC token identity or expiry is missing")
}

func siwcJWTIdentity(raw, email string) (*OpenAITokenInfo, bool) {
	claims, err := openai.DecodeIDToken(raw)
	if err != nil || claims.Exp <= time.Now().Unix() || claims.OpenAIAuth == nil || claims.OpenAIAuth.ChatGPTAccountID == "" {
		return nil, false
	}
	user := claims.GetUserInfo()
	return &OpenAITokenInfo{Email: email, ChatGPTAccountID: user.ChatGPTAccountID, ChatGPTUserID: user.ChatGPTUserID}, true
}

var siwcRefreshLocks sync.Map

func lockSIWCRefresh(ctx context.Context, id int64, cache OpenAITokenCache) (func(), error) {
	actual, _ := siwcRefreshLocks.LoadOrStore(id, newContextMutex())
	mu, ok := actual.(*contextMutex)
	if !ok {
		return nil, errors.New("SIWC refresh lock is unavailable")
	}
	if err := mu.Lock(ctx); err != nil {
		return nil, err
	}
	key := "openai:siwc:account:" + strconv.FormatInt(id, 10)
	if cache != nil {
		acquired, err := cache.AcquireRefreshLock(ctx, key, 60*time.Second)
		if err != nil || !acquired {
			mu.Unlock()
			return nil, errors.New("SIWC refresh is busy; retry later")
		}
		return func() {
			defer mu.Unlock()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = cache.ReleaseRefreshLock(cleanup, key)
		}, nil
	}
	return mu.Unlock, nil
}

func (s *OpenAIOAuthReauthService) SIWCAccessToken(ctx context.Context, account *Account, cache OpenAITokenCache) (string, error) {
	_, grant, err := s.readSIWCGrant(ctx, account.ID)
	if err != nil {
		return "", err
	}
	if grant == nil {
		return "", infraerrors.BadRequest("OPENAI_SIWC_AUTH_REQUIRED", "Authorize SIWC for this account first")
	}
	if err = validateSIWCGrant(account, grant); err != nil {
		return "", err
	}
	if grant.ExpiresAt > time.Now().Add(openAITokenRefreshSkew).Unix() {
		return grant.AccessToken, nil
	}
	unlock, err := lockSIWCRefresh(ctx, account.ID, cache)
	if err != nil {
		return "", err
	}
	defer unlock()
	refreshCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	previous, grant, err := s.readSIWCGrant(refreshCtx, account.ID)
	if err != nil || grant == nil {
		return "", errors.New("SIWC authorization is unavailable")
	}
	fresh, err := s.accountFor(refreshCtx, account.ID)
	if err != nil {
		return "", err
	}
	if err = validateSIWCGrant(fresh, grant); err != nil {
		return "", err
	}
	if grant.ExpiresAt > time.Now().Add(openAITokenRefreshSkew).Unix() {
		return grant.AccessToken, nil
	}
	client, ok := s.oauth.oauthClient.(openAISIWCTokenClient)
	if !ok {
		return "", errors.New("SIWC token client is unavailable")
	}
	proxy, err := s.reauthProxyURL(refreshCtx, fresh.ProxyID)
	if err != nil {
		return "", err
	}
	token, identity, err := client.SIWCToken(refreshCtx, url.Values{"grant_type": {"refresh_token"}, "client_id": {grant.ClientID}, "refresh_token": {grant.RefreshToken}}, proxy, "")
	if err != nil {
		return "", err
	}
	if identity != nil && identity.Subject != grant.Subject {
		return "", errors.New("SIWC refresh subscriber mismatch")
	}
	email := grant.Email
	if identity != nil && identity.Email != "" {
		email = identity.Email
	}
	info, err := siwcBoundIdentity(token, email, fresh)
	if err != nil {
		return "", err
	}
	if err = validateReauthToken(fresh, info); err != nil {
		return "", err
	}
	grant.AccessToken = token.AccessToken
	grant.ExpiresAt = time.Now().Unix() + token.ExpiresIn
	if token.RefreshToken != "" {
		grant.RefreshToken = token.RefreshToken
	}
	if token.IDToken != "" {
		grant.IDToken = token.IDToken
	}
	if token.Scope != "" {
		grant.Scope = token.Scope
	}
	raw, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	replacement, err := s.encryptor.Encrypt(string(raw))
	if err != nil {
		return "", errors.New("failed to encrypt refreshed SIWC credentials")
	}
	repo, err := s.siwcRepository()
	if err != nil {
		return "", err
	}
	persistCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	changed, err := repo.SaveSIWCCredentials(persistCtx, account.ID, fresh.Credentials, previous, replacement, false, nil)
	if err != nil || !changed {
		return "", errors.New("SIWC credentials changed or could not be saved; retry later")
	}
	return grant.AccessToken, nil
}
func validateSIWCGrant(account *Account, grant *siwcGrant) error {
	if !openai.HasSIWCPermission(grant.Scope) || grant.AccessToken == "" || grant.RefreshToken == "" || grant.ClientID == "" {
		return errors.New("SIWC plan usage authorization is missing")
	}
	// Expired access tokens are expected here; identity is the validated saved grant.
	if account.GetCredential("chatgpt_account_id") != grant.Workspace {
		return errors.New("SIWC workspace no longer matches the account")
	}
	if email := strings.TrimSpace(account.GetCredential("email")); email == "" || !strings.EqualFold(email, strings.TrimSpace(grant.Email)) {
		return errors.New("SIWC email no longer matches the account")
	}
	return nil
}

// A resource-server 401 invalidates only this access token. Keep the rotating
// refresh token and both other OAuth grants; refresh on the next request.
func (s *OpenAIOAuthReauthService) rejectSIWCAccessToken(ctx context.Context, account *Account, token string, cache OpenAITokenCache) {
	if account != nil && account.AuthConnectionID != "" {
		_ = s.FailAuthConnection(ctx, account, 401)
		return
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	unlock, err := lockSIWCRefresh(markCtx, account.ID, cache)
	if err != nil {
		return
	}
	defer unlock()
	previous, grant, err := s.readSIWCGrant(markCtx, account.ID)
	if err != nil || grant == nil || grant.AccessToken != token {
		return
	}
	fresh, err := s.accountFor(markCtx, account.ID)
	if err != nil {
		return
	}
	grant.ExpiresAt = 0
	raw, err := json.Marshal(grant)
	if err != nil {
		return
	}
	ciphertext, err := s.encryptor.Encrypt(string(raw))
	if err != nil {
		return
	}
	repo, err := s.siwcRepository()
	if err != nil {
		return
	}
	_, _ = repo.SaveSIWCCredentials(markCtx, account.ID, fresh.Credentials, previous, ciphertext, false, nil)
}
