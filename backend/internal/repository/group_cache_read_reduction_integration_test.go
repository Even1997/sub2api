//go:build integration

package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

func (s *GroupRepoSuite) TestCacheReadReductionRulesRoundTrip() {
	start := 80.0
	rules := []service.CacheReadReductionRule{{Model: "gpt-6.1-sol", Percent: 5, StartHitRatePercent: &start}}
	group := &service.Group{Name: "cache-rules", Platform: service.PlatformOpenAI, RateMultiplier: 0.15,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
		CacheReadReductionRules: rules}
	s.Require().NoError(s.repo.Create(s.ctx, group))
	loaded, err := s.repo.GetByID(s.ctx, group.ID)
	s.Require().NoError(err)
	s.Equal(rules, loaded.CacheReadReductionRules)
	loaded.CacheReadReductionRules = []service.CacheReadReductionRule{}
	s.Require().NoError(s.repo.Update(s.ctx, loaded))
	loaded, err = s.repo.GetByID(s.ctx, group.ID)
	s.Require().NoError(err)
	s.Empty(loaded.CacheReadReductionRules)
}
