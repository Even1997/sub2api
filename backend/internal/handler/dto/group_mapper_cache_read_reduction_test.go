package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperExposesCacheReadReductionOnlyToAdmins(t *testing.T) {
	group := &service.Group{
		ID: 7, Name: "cache-adjusted", Platform: service.PlatformOpenAI,
		Status: service.StatusActive, CacheReadReductionPercent: 37.5,
		CacheReadReductionRules: []service.CacheReadReductionRule{{Model: "model", Percent: 5}},
	}

	userFields := marshalToMap(t, GroupFromService(group))
	require.NotContains(t, userFields, "cache_read_reduction_percent")
	require.NotContains(t, userFields, "cache_read_reduction_rules")

	admin := GroupFromServiceAdmin(group)
	require.Equal(t, 37.5, admin.CacheReadReductionPercent)
	require.Equal(t, group.CacheReadReductionRules, admin.CacheReadReductionRules)
	adminFields := marshalToMap(t, admin)
	require.Equal(t, 37.5, adminFields["cache_read_reduction_percent"])
}
