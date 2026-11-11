package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openaidecisions"
	"github.com/stretchr/testify/require"
)

func TestContentModerationDecisionsProfileDoesNotClobberOtherEngines(t *testing.T) {
	repo := &contentModerationTestSettingRepo{values: map[string]string{}}
	s := &ContentModerationService{settingRepo: repo}
	openai, typesafeEngine, decisions := "openai", "typesafe", "decisions"
	openaiBase, typesafeBase, decisionsBase := "https://openai.example", "https://typesafe.example", "https://decisions.example/relay"
	openaiModel, typesafeModel, decisionsModel := "omni-moderation-latest", "jev-latest", "gpt-6-luna"
	openaiKeys, typesafeKeys, decisionsKeys := []string{"openai-key"}, []string{"typesafe-key"}, []string{"decisions-key"}

	_, err := s.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Engine: &openai, BaseURL: &openaiBase, Model: &openaiModel, APIKeys: &openaiKeys,
	})
	require.NoError(t, err)
	_, err = s.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Engine: &typesafeEngine, EngineConfigs: map[string]UpdateContentModerationEngineInput{
			"typesafe": {BaseURL: &typesafeBase, Model: &typesafeModel, APIKeys: &typesafeKeys},
		},
	})
	require.NoError(t, err)
	view, err := s.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Engine: &decisions, EngineConfigs: map[string]UpdateContentModerationEngineInput{
			"decisions": {BaseURL: &decisionsBase, Model: &decisionsModel, APIKeys: &decisionsKeys},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "decisions", view.Engine)
	require.Equal(t, "https://decisions.example/relay", view.BaseURL)
	require.Equal(t, "gpt-6-luna", view.Model)
	require.Len(t, view.Thresholds, 15)
	require.False(t, view.CategorySettings["jailbreak"].Enabled)
	require.Equal(t, "https://openai.example", view.EngineConfigs["openai"].BaseURL)
	require.Equal(t, "omni-moderation-latest", view.EngineConfigs["openai"].Model)
	require.Equal(t, 1, view.EngineConfigs["openai"].APIKeyCount)
	require.Len(t, view.EngineConfigs["openai"].Thresholds, 13)
	require.Equal(t, "https://typesafe.example", view.EngineConfigs["typesafe"].BaseURL)
	require.Equal(t, "jev-latest", view.EngineConfigs["typesafe"].Model)
	require.Equal(t, 1, view.EngineConfigs["typesafe"].APIKeyCount)

	reloaded, err := (&ContentModerationService{settingRepo: repo}).GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, view.EngineConfigs["openai"].BaseURL, reloaded.EngineConfigs["openai"].BaseURL)
	require.Equal(t, view.EngineConfigs["typesafe"].Model, reloaded.EngineConfigs["typesafe"].Model)
	require.Equal(t, "https://decisions.example/relay", reloaded.EngineConfigs["decisions"].BaseURL)

	bad := "https://user:pass@decisions.example"
	_, err = s.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{EngineConfigs: map[string]UpdateContentModerationEngineInput{"decisions": {BaseURL: &bad}}})
	require.Error(t, err)
	reloaded, err = (&ContentModerationService{settingRepo: repo}).GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://decisions.example/relay", reloaded.EngineConfigs["decisions"].BaseURL)

	updates := []ContentModerationKeyURLUpdate{{KeyHash: "fixture", BaseURL: "https://example.com"}}
	_, err = s.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{EngineConfigs: map[string]UpdateContentModerationEngineInput{"decisions": {KeyURLUpdates: updates}}})
	require.Error(t, err)
}

func TestContentModerationDecisionsCallMapsScoresAndBlocksImageOnly(t *testing.T) {
	var calls atomic.Int32
	secret := "upstream-secret-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/relay/v1/decisions", r.URL.Path)
		require.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
		var request openaidecisions.Request
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "gpt-6-luna", request.Model)
		require.Equal(t, "需要审核的文字", request.Input)
		require.Len(t, request.Questions, 15)
		require.Equal(t, "predicate", request.Questions[0].Type)
		require.Contains(t, request.Questions[0].Instructions, "仅判断待审文本")
		answers := make([]map[string]any, 0, len(request.Questions))
		for _, question := range request.Questions {
			answers = append(answers, map[string]any{"type": "predicate", "name": question.Name, "probability": 0.1})
		}
		answers[0] = map[string]any{"type": "predicate", "name": request.Questions[0].Name, "probability": 0.1}
		for i, question := range request.Questions {
			if question.Name == "sexual" {
				answers[i]["probability"] = 0.9
			}
			if question.Name == "jailbreak" {
				answers[i]["probability"] = 1
			}
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"model": "gpt-6-luna", "answers": answers, "usage": map[string]int{"input_tokens": 11, "output_tokens": 3}}))
	}))
	defer server.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"`+secret+`"}`)
	}))
	defer denied.Close()

	cfg := defaultContentModerationConfig()
	cfg.Engine = ContentModerationEngineDecisions
	cfg.BaseURL = server.URL + "/relay"
	cfg.Model = openaidecisions.DefaultModel
	cfg.APIKeys = []string{"fixture-key"}
	s := &ContentModerationService{httpClient: server.Client()}
	result, err := s.callModeration(context.Background(), cfg, ContentModerationInput{Text: "需要审核的文字"}.ModerationInput())
	require.NoError(t, err)
	require.Equal(t, 0.9, result.CategoryScores["sexual"])
	require.Equal(t, 1.0, result.CategoryScores["jailbreak"])
	require.Equal(t, DecisionsModerationRulesVersion, result.EngineMeta.RulesVersion)
	blocked, _, _, matched := evaluateModerationScores(result.CategoryScores, cfg)
	require.True(t, blocked)
	require.Contains(t, matched, "sexual")
	require.NotContains(t, matched, "jailbreak")

	cfg.RetryCount = 0
	cfg.BaseURL = denied.URL
	_, err = s.callModeration(context.Background(), cfg, ContentModerationInput{Text: "需要审核的文字"}.ModerationInput())
	require.EqualError(t, err, "decisions API status 401")
	require.NotContains(t, err.Error(), secret)

	cfg.BaseURL = server.URL + "/relay"
	s = &ContentModerationService{httpClient: server.Client()}
	_, err = s.callModeration(context.Background(), cfg, ContentModerationInput{Images: []string{"https://private.invalid/image.png"}}.ModerationInput())
	require.ErrorIs(t, err, errDecisionsImageOnlyUnaudited)
	callsBeforeBlock := calls.Load()
	cfg.Mode = ContentModerationModePreBlock
	repo := &contentModerationTestRepo{}
	s.repo = repo
	decision := s.checkSync(context.Background(), ContentModerationCheckInput{UserID: 9, Endpoint: "/v1/images/generations"}, cfg, ContentModerationInput{Images: []string{"https://private.invalid/image.png"}}, "image-hash", nil, true)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.Equal(t, callsBeforeBlock, calls.Load())
	require.Equal(t, "block", repo.logs[0].Action)
}
