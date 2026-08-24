package service

import (
	"context"
	"errors"
	"math"
	"testing"
)

type rewardDrawRepositoryStub struct {
	command             *RewardDrawCommand
	combineComponentIDs []int64
	combineQuantity     int
}

func (r *rewardDrawRepositoryStub) GetWallet(context.Context, int64) (*RewardDrawWallet, error) {
	return &RewardDrawWallet{}, nil
}

func (r *rewardDrawRepositoryStub) ListCollection(context.Context, int64) ([]RewardDrawCollectionEntry, error) {
	return nil, nil
}

func (r *rewardDrawRepositoryStub) ListComponents(context.Context, int64, bool, int, int) (*RewardDrawComponentPage, error) {
	return &RewardDrawComponentPage{}, nil
}

func (r *rewardDrawRepositoryStub) ListCards(context.Context, int64, bool, int, int) (*RewardDrawCardPage, error) {
	return &RewardDrawCardPage{}, nil
}

func (r *rewardDrawRepositoryStub) ListHistory(context.Context, int64, int, int) (*RewardDrawHistoryPage, error) {
	return &RewardDrawHistoryPage{}, nil
}

func (r *rewardDrawRepositoryStub) ListRedemptions(context.Context, int64, int, int) (*RewardDrawRedemptionPage, error) {
	return &RewardDrawRedemptionPage{}, nil
}

func (r *rewardDrawRepositoryStub) Draw(_ context.Context, command *RewardDrawCommand) (*RewardDrawBatch, bool, error) {
	r.command = command
	return &RewardDrawBatch{
		DrawCount: len(command.Components), PoolID: command.PoolID, Components: command.Components,
	}, false, nil
}

func (r *rewardDrawRepositoryStub) Combine(_ context.Context, _ int64, componentIDs []int64, quantity int) (*RewardDrawCombineResult, error) {
	r.combineComponentIDs = append([]int64(nil), componentIDs...)
	r.combineQuantity = quantity
	return &RewardDrawCombineResult{}, nil
}

func (r *rewardDrawRepositoryStub) Redeem(context.Context, int64, []int64, []int64, bool) (*RewardDrawRedemption, error) {
	return &RewardDrawRedemption{}, nil
}

func TestRewardDrawEconomyMatchesOneAndOnePointFivePercentTargets(t *testing.T) {
	if err := ValidateRewardDrawEconomy(); err != nil {
		t.Fatalf("ValidateRewardDrawEconomy() error = %v", err)
	}
	const wantRawWool = 10.009993
	if got := rewardDrawExpectedWool(); math.Abs(got-wantRawWool) > 1e-12 {
		t.Fatalf("rewardDrawExpectedWool() = %.12f, want %.12f", got, wantRawWool)
	}
	const wantCombinedWool = 15.123322666666667
	if got := rewardDrawExpectedCombinedWool(); math.Abs(got-wantCombinedWool) > 1e-12 {
		t.Fatalf("rewardDrawExpectedCombinedWool() = %.12f, want %.12f", got, wantCombinedWool)
	}
	if payout := rewardDrawExpectedWool() / float64(RewardDrawWoolPerCredit); payout < 0.01 || payout > RewardDrawRawPayoutCap {
		t.Fatalf("raw payout %.8f is outside target", payout)
	}
	if payout := rewardDrawExpectedCombinedWool() / float64(RewardDrawWoolPerCredit); payout < 0.015 || payout > RewardDrawPayoutCap {
		t.Fatalf("combined payout %.8f is outside target", payout)
	}
}

func TestRewardDrawUsesOnlySelectedPool(t *testing.T) {
	repository := &rewardDrawRepositoryStub{}
	svc := NewRewardDrawService(repository)
	svc.randomIntn = func(int) (int, error) { return 0, nil }

	result, err := svc.Draw(context.Background(), 7, 10, "profession", "draw-test-001")
	if err != nil {
		t.Fatalf("Draw() error = %v", err)
	}
	if result.Batch.DrawCount != 10 || len(result.Batch.Components) != 10 {
		t.Fatalf("Draw() batch = %#v", result.Batch)
	}
	for _, component := range result.Batch.Components {
		if component.PoolID != "profession" || component.Name != "牧场学徒" || component.WoolValue != 7 {
			t.Fatalf("Draw() component = %#v", component)
		}
	}
	if repository.command.PoolID != "profession" || repository.command.PoolVersion != RewardDrawPoolVersion {
		t.Fatalf("command = %#v", repository.command)
	}
}

func TestRewardDrawMiracleUsesOneInMillionSlot(t *testing.T) {
	svc := NewRewardDrawService(&rewardDrawRepositoryStub{})
	svc.randomIntn = func(max int) (int, error) { return max - 1, nil }

	result, err := svc.Draw(context.Background(), 7, 1, "treasure", "draw-miracle-001")
	if err != nil {
		t.Fatalf("Draw() error = %v", err)
	}
	component := result.Batch.Components[0]
	if !component.IsJackpot || component.TierID != "miracle" || component.WoolValue != RewardDrawJackpotWool {
		t.Fatalf("Draw() component = %#v", component)
	}
}

func TestComposeRewardDrawCardMatchesAndSupportsMiracle(t *testing.T) {
	components := []RewardDrawComponent{
		{ID: 1, PoolID: "breed", TierID: "speed", Name: "风驰羊", WoolValue: 11},
		{ID: 2, PoolID: "profession", TierID: "speed", Name: "云端骑手", WoolValue: 11},
		{ID: 3, PoolID: "treasure", TierID: "speed", Name: "涡轮羊铃", WoolValue: 11},
	}
	card, err := ComposeRewardDrawCard(components)
	if err != nil {
		t.Fatalf("ComposeRewardDrawCard() error = %v", err)
	}
	if card.RecipeID != "speed" || card.Title != "极速咩" || card.WoolValue != 50 {
		t.Fatalf("matching card = %#v", card)
	}

	components[2] = RewardDrawComponent{ID: 4, PoolID: "treasure", TierID: "miracle", Name: "百元奇迹铃", WoolValue: RewardDrawJackpotWool, IsJackpot: true}
	card, err = ComposeRewardDrawCard(components)
	if err != nil {
		t.Fatalf("ComposeRewardDrawCard() miracle error = %v", err)
	}
	if !card.IsJackpot || card.RecipeID != "miracle" || card.WoolValue != RewardDrawJackpotWool {
		t.Fatalf("miracle card = %#v", card)
	}
}

func TestComposeRewardDrawCardRejectsMixedNormalTiers(t *testing.T) {
	components := []RewardDrawComponent{
		{ID: 1, PoolID: "breed", TierID: "code", Name: "绒云羊", WoolValue: 7},
		{ID: 2, PoolID: "profession", TierID: "reasoning", Name: "羊羊侦探", WoolValue: 9},
		{ID: 3, PoolID: "treasure", TierID: "code", Name: "棉花羊铃", WoolValue: 7},
	}
	if _, err := ComposeRewardDrawCard(components); !errors.Is(err, ErrRewardDrawInvalidCombination) {
		t.Fatalf("ComposeRewardDrawCard() error = %v", err)
	}
}

func TestRewardDrawRejectsInvalidPoolAndOversizedBatch(t *testing.T) {
	svc := NewRewardDrawService(&rewardDrawRepositoryStub{})
	for _, input := range []struct {
		count int
		pool  string
	}{{1, "legacy"}, {101, "breed"}} {
		if _, err := svc.Draw(context.Background(), 7, input.count, input.pool, "draw-test-invalid"); err != ErrRewardDrawInvalidRequest {
			t.Fatalf("Draw(%d, %q) error = %v", input.count, input.pool, err)
		}
	}
}

func TestRewardDrawCombineValidatesAndForwardsQuantity(t *testing.T) {
	repository := &rewardDrawRepositoryStub{}
	svc := NewRewardDrawService(repository)
	ids := []int64{11, 12, 13}

	if _, err := svc.Combine(context.Background(), 7, ids, 5); err != nil {
		t.Fatalf("Combine() error = %v", err)
	}
	if repository.combineQuantity != 5 || len(repository.combineComponentIDs) != 3 {
		t.Fatalf("combine request = ids %v quantity %d", repository.combineComponentIDs, repository.combineQuantity)
	}
	for _, input := range []struct {
		ids      []int64
		quantity int
	}{{ids, 0}, {ids, RewardDrawMaxCombineQuantity + 1}, {[]int64{11, 11, 13}, 1}} {
		if _, err := svc.Combine(context.Background(), 7, input.ids, input.quantity); err == nil {
			t.Fatalf("Combine(%v, %d) unexpectedly succeeded", input.ids, input.quantity)
		}
	}
}

func TestRewardDrawOverviewPublishesRedemptionMinimum(t *testing.T) {
	overview, err := NewRewardDrawService(&rewardDrawRepositoryStub{}).GetOverview(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.MinimumRedeemReward != 0.001 || overview.PoolVersion != 3 || overview.TotalCombinations != 7 {
		t.Fatalf("overview = %#v", overview)
	}
	if math.Abs(overview.ExpectedRewardPerDraw-0.010009993) > 1e-12 || math.Abs(overview.TheoreticalPayoutRate-0.015123322666666667) > 1e-12 {
		t.Fatalf("overview rates = raw %.12f combined %.12f", overview.ExpectedRewardPerDraw, overview.TheoreticalPayoutRate)
	}
	if len(overview.Recipes) != 7 || overview.Recipes[0].Wool != 32 || overview.Recipes[6].Wool != RewardDrawJackpotWool {
		t.Fatalf("overview recipes = %#v", overview.Recipes)
	}
}
