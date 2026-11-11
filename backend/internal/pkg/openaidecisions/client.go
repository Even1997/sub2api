package openaidecisions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

type Question struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

type Request struct {
	Model     string     `json:"model"`
	Input     string     `json:"input"`
	Questions []Question `json:"questions"`
}

type Result struct {
	Model  string
	Scores map[string]float64
	Usage  Usage
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

const (
	DefaultBaseURL = "https://api.openai.com"
	DecisionsPath  = "/v1/decisions"
	DefaultModel   = "gpt-6-luna"
)

// DecisionsURL accepts a server root, API base or full Decisions endpoint.
// Host allowlists, private-network access and HTTP policy remain the caller's
// responsibility; this only validates URL syntax and normalizes the API path.
func DecisionsURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	normalized, err := urlvalidator.ValidateURLFormat(baseURL, true)
	if err != nil {
		return "", errors.New("decisions invalid endpoint")
	}
	u, err := url.Parse(normalized)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("decisions invalid endpoint")
	}
	base := strings.TrimRight(u.String(), "/")
	path := strings.ToLower(strings.TrimRight(u.EscapedPath(), "/"))
	switch {
	case strings.HasSuffix(path, DecisionsPath):
		base = base[:len(base)-len(DecisionsPath)]
	case strings.HasSuffix(path, "/v1"):
		base = base[:len(base)-len("/v1")]
	}
	endpoint, err := url.JoinPath(base, DecisionsPath)
	if err != nil {
		return "", errors.New("decisions invalid endpoint")
	}
	return endpoint, nil
}

// Evaluate performs one attempt. The caller owns timeouts, retries and key rotation.
// Non-2xx bodies are discarded and never included in the error.
func Evaluate(ctx context.Context, client *http.Client, baseURL, key string, input Request) (*Result, int, error) {
	endpoint, err := DecisionsURL(baseURL)
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, 0, errors.New("decisions invalid request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("decisions invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, errors.New("decisions transport unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, resp.StatusCode, fmt.Errorf("decisions API status %d", resp.StatusCode)
	}
	var out struct {
		Model   string `json:"model"`
		Usage   Usage  `json:"usage"`
		Answers []struct {
			Type        string   `json:"type"`
			Name        string   `json:"name"`
			Probability *float64 `json:"probability"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || strings.TrimSpace(out.Model) == "" {
		return nil, resp.StatusCode, errors.New("decisions invalid response")
	}
	result := &Result{Model: out.Model, Usage: out.Usage, Scores: make(map[string]float64, len(input.Questions))}
	seen := map[string]struct{}{}
	for _, answer := range out.Answers {
		name := strings.TrimSpace(answer.Name)
		if _, dup := seen[name]; dup {
			return nil, resp.StatusCode, fmt.Errorf("decisions invalid answer for %s", name)
		}
		seen[name] = struct{}{}
		if answer.Type != "predicate" || answer.Probability == nil || math.IsNaN(*answer.Probability) || math.IsInf(*answer.Probability, 0) || *answer.Probability < 0 || *answer.Probability > 1 {
			return nil, resp.StatusCode, fmt.Errorf("decisions invalid answer for %s", name)
		}
		result.Scores[name] = *answer.Probability
	}
	for _, question := range input.Questions {
		if _, ok := result.Scores[question.Name]; !ok {
			return nil, resp.StatusCode, fmt.Errorf("decisions invalid answer for %s", question.Name)
		}
	}
	return result, resp.StatusCode, nil
}
