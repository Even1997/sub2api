package openaidecisions

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecisionsURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com/", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com/v1/decisions", "https://api.openai.com/v1/decisions"},
		{"https://api.openai.com/v1/decisions/", "https://api.openai.com/v1/decisions"},
		{"https://relay.example/proxy", "https://relay.example/proxy/v1/decisions"},
		{"https://relay.example/proxy/v1", "https://relay.example/proxy/v1/decisions"},
		{"https://relay.example/proxy/v1/decisions", "https://relay.example/proxy/v1/decisions"},
		{"https://relay.example/V1/", "https://relay.example/v1/decisions"},
		{"https://relay.example/apiv1", "https://relay.example/apiv1/v1/decisions"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := DecisionsURL(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, raw := range []string{
		"https://user:pass@example.com",
		"https://example.com/?q=1",
		"https://example.com/#frag",
		"ftp://example.com",
		"https://example.com:99999",
		"not a url",
	} {
		t.Run("reject "+raw, func(t *testing.T) {
			_, err := DecisionsURL(raw)
			require.EqualError(t, err, "decisions invalid endpoint")
		})
	}
}

func TestEvaluateMapsPredicateProbabilitiesAndDiscardsErrorBody(t *testing.T) {
	secret := "do-not-echo-this-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/decisions", r.URL.Path)
		require.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(raw), `"type":"predicate"`)
		require.Contains(t, string(raw), `"name":"illicit"`)
		require.NotContains(t, string(raw), `"noul"`)
		_, _ = w.Write([]byte(`{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"illicit","probability":1},{"type":"predicate","name":"hate","probability":0.2}],"usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer server.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"` + secret + `"}`))
	}))
	defer denied.Close()
	result, status, err := Evaluate(context.Background(), server.Client(), server.URL+"/v1", "fixture-key", Request{
		Model: DefaultModel, Input: "fixture",
		Questions: []Question{{Type: "predicate", Name: "illicit", Instructions: "rule"}, {Type: "predicate", Name: "hate", Instructions: "rule"}},
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, "gpt-6-luna", result.Model)
	require.Equal(t, 1.0, result.Scores["illicit"])
	require.Equal(t, 0.2, result.Scores["hate"])
	require.Equal(t, 10, result.Usage.InputTokens)

	_, status, err = Evaluate(context.Background(), denied.Client(), denied.URL, "fixture-key", Request{
		Model: DefaultModel, Input: "fixture", Questions: []Question{{Type: "predicate", Name: "illicit", Instructions: "rule"}},
	})
	require.Equal(t, 401, status)
	require.EqualError(t, err, "decisions API status 401")
	require.NotContains(t, err.Error(), secret)
}

func TestEvaluateRejectsIncompleteAnswers(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"hate","probability":1.2}]}`,
		`{"model":"gpt-6-luna","answers":[{"type":"noul","name":"hate","probability":0.2}]}`,
		`{"model":"gpt-6-luna","answers":[]}`,
		`{"model":"","answers":[{"type":"predicate","name":"hate","probability":0.2}]}`,
		`{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"hate","probability":0.2},{"type":"predicate","name":"hate","probability":0.3}]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, _, err := Evaluate(context.Background(), server.Client(), server.URL, "k", Request{
			Model: DefaultModel, Input: "fixture", Questions: []Question{{Type: "predicate", Name: "hate", Instructions: "rule"}},
		})
		server.Close()
		require.Error(t, err)
		require.NotContains(t, err.Error(), body)
	}
}

func TestEvaluateDoesNotDisableTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default client must not accept the test certificate")
	}))
	defer server.Close()
	_, _, err := Evaluate(context.Background(), http.DefaultClient, server.URL, "k", Request{
		Model: DefaultModel, Input: "fixture", Questions: []Question{{Type: "predicate", Name: "hate", Instructions: "rule"}},
	})
	require.EqualError(t, err, "decisions transport unavailable")
}
