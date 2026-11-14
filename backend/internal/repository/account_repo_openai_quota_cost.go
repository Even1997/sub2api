package repository

import (
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func prepareOpenAIQuotaCostSamples(updates map[string]any) map[string]any {
	stamp, ok := updates["codex_usage_updated_at"].(string)
	observedAt, err := time.Parse(time.RFC3339, stamp)
	if !ok || err != nil || observedAt.After(time.Now()) {
		return updates
	}
	versions := service.CodexUsageFieldVersions(updates, observedAt)
	if len(versions) == 0 {
		return updates
	}
	updates = maps.Clone(updates)
	updates[service.CodexUsageFieldVersionsKey] = versions
	for _, window := range []string{service.OpenAIQuotaWindow5h, service.OpenAIQuotaWindow7d} {
		prefix := "codex_" + window + "_"
		if _, exists := updates[prefix+"used_percent"]; !exists {
			continue
		}
		version := versions[prefix+"used_percent"].(string)
		ns, _ := strconv.ParseInt(version, 10, 64)
		at := time.Unix(0, ns)
		if at.After(time.Now()) {
			continue
		}
		updates[prefix+"observed_at"] = at.UTC().Format(time.RFC3339Nano)
		versions[prefix+"observed_at"] = version
		if sample := service.NewOpenAIQuotaCostSample(updates, window); sample != nil {
			updates[prefix+"cost_sample"] = sample
			versions[prefix+"cost_sample"] = version
		}
	}
	return updates
}

// Retain samples atomically with the existing snapshot write. A reset or tiny
// reading leaves them intact; a late response cannot overwrite a newer sample.
func openAIQuotaCostSamplesSQL(base string, updates map[string]any) string {
	if _, ok := updates["codex_usage_updated_at"]; !ok {
		return base
	}
	for _, window := range []string{service.OpenAIQuotaWindow5h, service.OpenAIQuotaWindow7d} {
		observedKey := "codex_" + window + "_observed_at"
		if _, exists := updates[observedKey]; !exists {
			// A partial 5h response must not make the cached 7d reading newer.
			base = fmt.Sprintf(`(%s) || jsonb_build_object('%s', COALESCE(extra->'%s', extra->'codex_usage_updated_at'))`, base, observedKey, observedKey)
		}
		key := "codex_" + window + "_cost_sample"
		if _, ok := updates[key].(*service.OpenAIQuotaCostSample); !ok {
			continue
		}
		base = fmt.Sprintf(`(%s) || jsonb_build_object('%s', CASE
			WHEN platform = 'openai' AND type = 'oauth'
			AND COALESCE(extra->'%s'->'sampled_at', '0'::jsonb) <= $1::jsonb->'%s'->'sampled_at'
			THEN ($1::jsonb->'%s') || jsonb_build_object(
				'plan_type', lower(btrim(COALESCE(credentials->>'plan_type', ''))),
				'rate_multiplier', CASE WHEN rate_multiplier < 0 THEN 1 ELSE COALESCE(rate_multiplier, 1) END)
			ELSE extra->'%s' END)`, base, key, key, key, key, key)
	}
	return base
}
