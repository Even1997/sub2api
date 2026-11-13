//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

const pelicanClaudeTestHTML = `<!DOCTYPE html><html><body><svg viewBox="0 0 400 300"><circle cx="200" cy="150" r="40"/></svg></body></html>`

// claudeThinkingUpstream streams like Claude Opus 5.5: thinking is always on,
// counts toward max_tokens and comes back as a thinking block with no text under
// the default display. The model thinks for thinkingTokens, then writes the answer.
type claudeThinkingUpstream struct {
	thinkingTokens int64
	answerTokens   int64
	refusal        string // stop_details.category of a refusal after thinking
	bodies         [][]byte
	urls           []string
}

func (u *claudeThinkingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.bodies = append(u.bodies, body)
	u.urls = append(u.urls, req.URL.String())
	maxTokens := gjson.GetBytes(body, "max_tokens").Int()

	var sse strings.Builder
	event := func(data any) {
		raw, _ := json.Marshal(data)
		sse.WriteString("data: " + string(raw) + "\n\n")
	}
	event(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "content": []any{}, "usage": map[string]any{"input_tokens": 40, "output_tokens": 1}}})
	event(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
	event(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": ""}})
	event(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "fixture-signature"}})
	event(map[string]any{"type": "content_block_stop", "index": 0})

	delta := map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}
	used := u.thinkingTokens + u.answerTokens
	switch {
	case u.refusal != "":
		delta["stop_reason"] = "refusal"
		delta["stop_details"] = map[string]any{"type": "refusal", "category": u.refusal}
		used = u.thinkingTokens
	case maxTokens <= u.thinkingTokens:
		delta["stop_reason"] = "max_tokens"
		used = maxTokens
	default:
		text := pelicanClaudeTestHTML
		if maxTokens < used {
			// The answer stops where the shared budget runs out.
			text = text[:int64(len(text))*(maxTokens-u.thinkingTokens)/u.answerTokens]
			delta["stop_reason"] = "max_tokens"
			used = maxTokens
		}
		event(map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}})
		event(map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": text}})
		event(map[string]any{"type": "content_block_stop", "index": 1})
	}
	event(map[string]any{"type": "message_delta", "delta": delta, "usage": map[string]any{"output_tokens": used}})
	event(map[string]any{"type": "message_stop"})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse.String())),
	}, nil
}

func (u *claudeThinkingUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func pelicanClaudeTestService(account *Account, upstream HTTPUpstream) *AccountTestService {
	return &AccountTestService{
		accountRepo:  &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
}

func pelicanClaudeOAuthAccount() *Account {
	return &Account{ID: 511, Name: "claude-pelican", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "fixture-oauth-token"}}
}
