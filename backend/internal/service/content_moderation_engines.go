package service

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openaidecisions"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
)

const (
	ContentModerationEngineOpenAI    = "openai"
	ContentModerationEngineTypeSafe  = "typesafe"
	ContentModerationEngineDecisions = "decisions"
)

func moderationEngineHasOptionalCategories(engine string) bool {
	switch engine {
	case ContentModerationEngineTypeSafe, ContentModerationEngineDecisions:
		return true
	default:
		return false
	}
}

// CategorySettings applies only to the two optional TypeSafe categories.
// The original thirteen categories retain their existing always-enabled behavior.
type ContentModerationCategorySetting struct {
	Enabled bool `json:"enabled"`
}

func mergeModerationCategorySettings(engine string, base, override map[string]ContentModerationCategorySetting) map[string]ContentModerationCategorySetting {
	out := map[string]ContentModerationCategorySetting{}
	if moderationEngineHasOptionalCategories(engine) {
		for _, category := range []string{"jailbreak", "control_bypass"} {
			out[category] = base[category]
			if setting, ok := override[category]; ok {
				out[category] = setting
			}
		}
	}
	return out
}

func moderationCategoryEnabled(engine string, settings map[string]ContentModerationCategorySetting, category string) bool {
	if category == "jailbreak" || category == "control_bypass" {
		return moderationEngineHasOptionalCategories(engine) && settings[category].Enabled
	}
	return true
}

func validateModerationThresholds(engine string, thresholds *map[string]float64) error {
	if thresholds != nil {
		for _, category := range ContentModerationCategories(engine) {
			if value, ok := (*thresholds)[category]; ok && (math.IsNaN(value) || math.IsInf(value, 0)) {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_THRESHOLD", "审计阈值必须为有限数值")
			}
		}
	}
	return nil
}

type ContentModerationEngineConfig struct {
	BaseURL          string                                      `json:"base_url"`
	BaseURLs         []string                                    `json:"base_urls,omitempty"`
	Model            string                                      `json:"model"`
	ProxyID          *int64                                      `json:"proxy_id,omitempty"`
	APIKeys          []string                                    `json:"api_keys,omitempty"`
	APIKeyBaseURLs   map[string]string                           `json:"api_key_base_urls,omitempty"`
	PausedKeyHashes  []string                                    `json:"paused_key_hashes,omitempty"`
	TimeoutMS        int                                         `json:"timeout_ms"`
	RetryCount       int                                         `json:"retry_count"`
	Thresholds       map[string]float64                          `json:"thresholds"`
	CategorySettings map[string]ContentModerationCategorySetting `json:"category_settings"`
}

type ContentModerationKeyURLUpdate struct {
	KeyHash         string  `json:"key_hash"`
	BaseURL         string  `json:"base_url"`
	ExpectedBaseURL *string `json:"expected_base_url"`
}

type UpdateContentModerationEngineInput struct {
	KeyURLUpdates      []ContentModerationKeyURLUpdate              `json:"key_url_updates"`
	BaseURL            *string                                      `json:"base_url"`
	Model              *string                                      `json:"model"`
	ProxyID            *int64                                       `json:"proxy_id"`
	APIKey             *string                                      `json:"api_key"`
	APIKeys            *[]string                                    `json:"api_keys"`
	APIKeySlots        *[]ContentModerationAPIKeySlot               `json:"api_key_slots"`
	APIKeysMode        string                                       `json:"api_keys_mode"`
	DeleteAPIKeyHashes *[]string                                    `json:"delete_api_key_hashes"`
	ClearAPIKey        bool                                         `json:"clear_api_key"`
	TimeoutMS          *int                                         `json:"timeout_ms"`
	RetryCount         *int                                         `json:"retry_count"`
	Thresholds         *map[string]float64                          `json:"thresholds"`
	CategorySettings   *map[string]ContentModerationCategorySetting `json:"category_settings"`
}

type ContentModerationAPIKeySlot struct {
	Key      string `json:"key"`
	KeepHash string `json:"keep_hash"`
	Paused   bool   `json:"paused"`
}

func applyModerationAPIKeySlots(existing []string, slots []ContentModerationAPIKeySlot, engine string) ([]string, error) {
	byHash := make(map[string]string, len(existing))
	for _, key := range existing {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		byHash[scopedModerationKeyHash(key, engine)] = key
	}
	filled := 0
	for _, slot := range slots {
		if strings.TrimSpace(slot.Key) != "" || strings.TrimSpace(slot.KeepHash) != "" {
			filled++
		}
	}
	if filled == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(slots))
	for i, slot := range slots {
		key := strings.TrimSpace(slot.Key)
		if key != "" {
			out = append(out, key)
			continue
		}
		hash := strings.TrimSpace(slot.KeepHash)
		if hash == "" {
			return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", fmt.Sprintf("第 %d 行缺少 API Key", i+1))
		}
		kept, ok := byHash[hash]
		if !ok {
			return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", fmt.Sprintf("第 %d 行保存的 API Key 已变化，请重新填写", i+1))
		}
		out = append(out, kept)
	}
	return trimModerationAPIKeys(out), nil
}

func pausedHashesForSlots(keys []string, slots []ContentModerationAPIKeySlot, engine string) []string {
	out := make([]string, 0)
	index := 0
	for _, slot := range slots {
		if strings.TrimSpace(slot.Key) == "" && strings.TrimSpace(slot.KeepHash) == "" {
			continue
		}
		if index >= len(keys) {
			break
		}
		if slot.Paused {
			out = append(out, scopedModerationKeyHash(keys[index], engine))
		}
		index++
	}
	return out
}

type ContentModerationEngineMeta struct {
	CategorySettings  map[string]ContentModerationCategorySetting `json:"category_settings,omitempty"`
	MatchedCategories []string                                    `json:"matched_categories,omitempty"`
	Engine            string                                      `json:"engine"`
	Model             string                                      `json:"model"`
	RulesVersion      string                                      `json:"rules_version"`
	SkippedImages     int                                         `json:"skipped_images"`
}

func moderationEngine(engine string) string {
	if engine == "" {
		return ContentModerationEngineOpenAI
	}
	return engine
}

func validModerationEngine(engine string) bool {
	return engine == ContentModerationEngineOpenAI || engine == ContentModerationEngineTypeSafe || engine == ContentModerationEngineDecisions
}

func moderationEngineDefaults(engine string) *ContentModerationEngineConfig {
	p := &ContentModerationEngineConfig{BaseURL: defaultContentModerationBaseURL, Model: defaultContentModerationModel, TimeoutMS: 3000, RetryCount: 2, Thresholds: ContentModerationDefaultThresholds(engine), CategorySettings: mergeModerationCategorySettings(engine, nil, nil)}
	switch engine {
	case ContentModerationEngineTypeSafe:
		p.BaseURL, p.Model = typesafe.DefaultBaseURL, typesafe.JevLatestModel
	case ContentModerationEngineDecisions:
		p.BaseURL, p.Model = openaidecisions.DefaultBaseURL, openaidecisions.DefaultModel
	}
	return p
}

func (cfg *ContentModerationConfig) nestedEngineConfig(engine string) (profile **ContentModerationEngineConfig, ok bool) {
	switch engine {
	case ContentModerationEngineTypeSafe:
		return &cfg.TypeSafe, true
	case ContentModerationEngineDecisions:
		return &cfg.Decisions, true
	default:
		return nil, false
	}
}

func (cfg *ContentModerationConfig) engineProfile(engine string) *ContentModerationEngineConfig {
	var p ContentModerationEngineConfig
	if slot, nested := cfg.nestedEngineConfig(engine); nested {
		if *slot == nil {
			return moderationEngineDefaults(engine)
		}
		p = **slot
		defaults := moderationEngineDefaults(engine)
		if strings.TrimSpace(p.BaseURL) == "" {
			p.BaseURL = defaults.BaseURL
		}
		if strings.TrimSpace(p.Model) == "" {
			p.Model = defaults.Model
		}
	} else {
		p = ContentModerationEngineConfig{BaseURL: cfg.BaseURL, BaseURLs: append([]string(nil), cfg.BaseURLs...), Model: cfg.Model, ProxyID: cfg.ProxyID, APIKeys: cfg.apiKeys(), PausedKeyHashes: append([]string(nil), cfg.PausedKeyHashes...), TimeoutMS: cfg.TimeoutMS, RetryCount: cfg.RetryCount, Thresholds: cfg.Thresholds, CategorySettings: cfg.CategorySettings}
	}
	p.APIKeyBaseURLs = cloneModerationKeyURLs(p.APIKeyBaseURLs)
	p.ProxyID = cloneInt64Ptr(p.ProxyID)
	p.APIKeys = append([]string(nil), p.APIKeys...)
	p.PausedKeyHashes = append([]string(nil), p.PausedKeyHashes...)
	p.BaseURLs = append([]string(nil), p.BaseURLs...)
	p.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(engine), p.Thresholds)
	p.CategorySettings = mergeModerationCategorySettings(engine, nil, p.CategorySettings)
	p.normalize(engine)
	return &p
}

func (p *ContentModerationEngineConfig) normalize(engine string) {
	if p == nil {
		return
	}
	defaults := moderationEngineDefaults(engine)
	p.BaseURLs = collectModerationBaseURLs(p.BaseURLs)
	if len(p.BaseURLs) == 0 {
		p.BaseURLs = collectModerationBaseURLs([]string{p.BaseURL})
	}
	if len(p.BaseURLs) == 0 {
		p.BaseURLs = []string{defaults.BaseURL}
	}
	p.BaseURL = p.BaseURLs[0]
	p.Model = strings.TrimSpace(p.Model)
	if p.Model == "" {
		p.Model = defaults.Model
	}
	if p.TimeoutMS <= 0 {
		p.TimeoutMS = defaults.TimeoutMS
	}
	if p.TimeoutMS > maxContentModerationTimeoutMS {
		p.TimeoutMS = maxContentModerationTimeoutMS
	}
	if p.RetryCount < 0 {
		p.RetryCount = defaults.RetryCount
	}
	if len(p.Thresholds) == 0 {
		p.Thresholds = cloneFloatMap(defaults.Thresholds)
	}
}

func (p *ContentModerationEngineConfig) baseURLs() []string {
	if p == nil {
		return nil
	}
	if len(p.BaseURLs) > 0 {
		return p.BaseURLs
	}
	if strings.TrimSpace(p.BaseURL) != "" {
		return []string{strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")}
	}
	return nil
}

func (cfg *ContentModerationConfig) applyEngineProfile(p *ContentModerationEngineConfig) {
	cfg.BaseURL, cfg.BaseURLs, cfg.Model, cfg.ProxyID = p.BaseURL, append([]string(nil), p.BaseURLs...), p.Model, cloneInt64Ptr(p.ProxyID)
	cfg.APIKey, cfg.APIKeys = "", append([]string(nil), p.APIKeys...)
	cfg.PausedKeyHashes = append([]string(nil), p.PausedKeyHashes...)
	cfg.APIKeyBaseURLs = cloneModerationKeyURLs(p.APIKeyBaseURLs)
	cfg.TimeoutMS, cfg.RetryCount = p.TimeoutMS, p.RetryCount
	cfg.Thresholds = cloneFloatMap(p.Thresholds)
	cfg.CategorySettings = maps.Clone(p.CategorySettings)
}

func (cfg *ContentModerationConfig) effectiveEngine(engine string) *ContentModerationConfig {
	out := cloneContentModerationConfig(cfg)
	out.Engine = moderationEngine(engine)
	out.applyEngineProfile(cfg.engineProfile(out.Engine))
	return out
}

func (s *ContentModerationService) updateEngineProfile(ctx context.Context, cfg *ContentModerationConfig, engine string, input UpdateContentModerationEngineInput) error {
	if !validModerationEngine(engine) {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_ENGINE", "内容审计引擎无效")
	}
	p := cfg.engineProfile(engine)
	defaults := moderationEngineDefaults(engine)
	if input.BaseURL != nil {
		urls := collectModerationBaseURLs([]string{*input.BaseURL})
		if len(urls) == 0 {
			urls = []string{defaults.BaseURL}
		}
		p.BaseURLs = urls
		p.BaseURL = urls[0]
	}
	if input.Model != nil {
		p.Model = strings.TrimSpace(*input.Model)
		if p.Model == "" {
			p.Model = defaults.Model
		}
	}
	if input.ProxyID != nil {
		p.ProxyID = cloneInt64Ptr(input.ProxyID)
		if *input.ProxyID <= 0 {
			p.ProxyID = nil
		}
	}
	if input.TimeoutMS != nil {
		p.TimeoutMS = *input.TimeoutMS
	}
	if input.RetryCount != nil {
		p.RetryCount = *input.RetryCount
	}
	if err := validateModerationThresholds(engine, input.Thresholds); err != nil {
		return err
	}
	if input.CategorySettings != nil {
		p.CategorySettings = mergeModerationCategorySettings(engine, p.CategorySettings, *input.CategorySettings)
	}
	if input.Thresholds != nil {
		p.Thresholds = mergeContentModerationThresholds(defaults.Thresholds, *input.Thresholds)
	}
	if input.ClearAPIKey {
		p.APIKeys = []string{}
		p.PausedKeyHashes = nil
	} else if input.APIKeySlots != nil {
		keys, err := applyModerationAPIKeySlots(p.APIKeys, *input.APIKeySlots, engine)
		if err != nil {
			return err
		}
		p.APIKeys = keys
		p.PausedKeyHashes = pausedHashesForSlots(keys, *input.APIKeySlots, engine)
	} else {
		mode := normalizeContentModerationAPIKeysMode(input.APIKeysMode)
		if input.DeleteAPIKeyHashes != nil && mode != contentModerationAPIKeysModeReplace {
			p.APIKeys = deleteModerationAPIKeysByHash(p.APIKeys, *input.DeleteAPIKeyHashes, engine)
		}
		if input.APIKeys != nil {
			if mode == contentModerationAPIKeysModeReplace {
				p.APIKeys = normalizeModerationAPIKeys(*input.APIKeys)
			} else {
				p.APIKeys = normalizeModerationAPIKeys(append(p.APIKeys, *input.APIKeys...))
			}
		}
		if input.APIKey != nil {
			p.APIKeys = normalizeModerationAPIKeys(append(p.APIKeys, *input.APIKey))
		}
	}
	if len(input.KeyURLUpdates) > 0 {
		if engine != ContentModerationEngineTypeSafe {
			return infraerrors.BadRequest("INVALID_KEY_URL_ENGINE", "仅 TypeSafe 支持逐 Key URL")
		}
		known := map[string]bool{}
		for _, key := range p.APIKeys {
			known[scopedModerationKeyHash(key, engine)] = true
		}
		if p.APIKeyBaseURLs == nil {
			p.APIKeyBaseURLs = map[string]string{}
		}
		for _, change := range input.KeyURLUpdates {
			if !known[change.KeyHash] {
				return infraerrors.New(409, "MODERATION_KEY_CHANGED", "Key 已变更，请刷新后重试")
			}
			old := p.BaseURL
			if base := p.APIKeyBaseURLs[change.KeyHash]; base != "" {
				old = base
			}
			if change.ExpectedBaseURL == nil || strings.TrimRight(strings.TrimSpace(*change.ExpectedBaseURL), "/") != strings.TrimRight(strings.TrimSpace(old), "/") {
				return infraerrors.New(409, "MODERATION_KEY_URL_CHANGED", "该 Key URL 已变更，请刷新后重试")
			}
			base := strings.TrimRight(strings.TrimSpace(change.BaseURL), "/")
			if base == "" {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "Key URL 不能为空")
			}
			if _, err := typesafe.SystemOneURL(base); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "Key URL 无效")
			}
			p.APIKeyBaseURLs[change.KeyHash] = base
		}
	}
	// Remove orphan overrides when credentials are explicitly removed/replaced.
	for hash := range p.APIKeyBaseURLs {
		found := false
		for _, key := range p.APIKeys {
			if scopedModerationKeyHash(key, engine) == hash {
				found = true
				break
			}
		}
		if !found {
			delete(p.APIKeyBaseURLs, hash)
		}
	}
	if err := validateNestedModerationEndpoint(engine, p); err != nil {
		return err
	}
	check := cloneContentModerationConfig(cfg)
	check.Engine = engine
	check.applyEngineProfile(p)
	if err := s.validateConfig(ctx, check, engine); err != nil {
		return err
	}
	p = &ContentModerationEngineConfig{BaseURL: check.BaseURL, BaseURLs: append([]string(nil), check.BaseURLs...), Model: check.Model, ProxyID: check.ProxyID, APIKeys: check.APIKeys, APIKeyBaseURLs: check.APIKeyBaseURLs, PausedKeyHashes: append([]string(nil), p.PausedKeyHashes...), TimeoutMS: check.TimeoutMS, RetryCount: check.RetryCount, Thresholds: check.Thresholds, CategorySettings: check.CategorySettings}
	if slot, nested := cfg.nestedEngineConfig(engine); nested {
		*slot = p
	} else {
		cfg.applyEngineProfile(p)
	}
	return nil
}

func validateNestedModerationEndpoint(engine string, p *ContentModerationEngineConfig) error {
	var join func(string) (string, error)
	switch engine {
	case ContentModerationEngineTypeSafe:
		join = typesafe.SystemOneURL
	case ContentModerationEngineDecisions:
		join = openaidecisions.DecisionsURL
	default:
		return nil
	}
	urls := p.BaseURLs
	if len(urls) == 0 {
		urls = []string{p.BaseURL}
	}
	for _, raw := range urls {
		if _, err := join(raw); err != nil {
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "审计 Base URL 无效")
		}
	}
	return nil
}

func (s *ContentModerationService) engineConfigView(cfg *ContentModerationConfig) *ContentModerationConfigView {
	view := s.configView(cfg.effectiveEngine(cfg.Engine))
	view.EngineConfigs = map[string]*ContentModerationConfigView{}
	for _, engine := range []string{ContentModerationEngineOpenAI, ContentModerationEngineTypeSafe, ContentModerationEngineDecisions} {
		view.EngineConfigs[engine] = s.configView(cfg.effectiveEngine(engine))
	}
	return view
}

func scopedModerationKeyHash(key string, engine ...string) string {
	if len(engine) > 0 {
		switch engine[0] {
		case ContentModerationEngineTypeSafe:
			return moderationAPIKeyHash("typesafe\x00" + key)
		case ContentModerationEngineDecisions:
			return moderationAPIKeyHash("decisions\x00" + key)
		}
	}
	return moderationAPIKeyHash(key)
}

func (cfg *ContentModerationConfig) keyBaseURL(key string) string {
	if cfg.Engine == ContentModerationEngineTypeSafe {
		if base := cfg.APIKeyBaseURLs[scopedModerationKeyHash(key, cfg.Engine)]; base != "" {
			return base
		}
	}
	return cfg.BaseURL
}

func (s *ContentModerationService) configAPIKeyStatuses(cfg *ContentModerationConfig) []ContentModerationAPIKeyStatus {
	keys := cfg.apiKeys()
	rows := s.apiKeyStatuses(keys, cfg.Engine)
	if cfg.Engine == ContentModerationEngineTypeSafe {
		for i, key := range keys {
			rows[i].BaseURL = cfg.keyBaseURL(key)
		}
	}
	return rows
}

var ErrContentModerationConfigConflict = infraerrors.New(409, "MODERATION_CONFIG_CHANGED", "配置已被其他操作修改，请刷新后重试")

func cloneModerationKeyURLs(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
