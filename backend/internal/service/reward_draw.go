package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

const (
	RewardDrawConsumptionUnitScale int64 = 1_000_000
	RewardDrawWoolPerCredit        int64 = 1_000
	RewardDrawRewardUnitPerWool    int64 = 1_000
	RewardDrawMinimumRewardUnits   int64 = RewardDrawRewardUnitPerWool
	RewardDrawProbabilityScale           = 1_000_000
	RewardDrawPoolVersion                = 3
	RewardDrawMaxCount                   = 100
	RewardDrawMaxCombineQuantity         = 500
	RewardDrawRawPayoutCap               = 0.0101
	RewardDrawPayoutCap                  = 0.0152
	RewardDrawJackpotWool          int64 = 100_000
)

var (
	ErrRewardDrawInvalidRequest        = errors.New("invalid reward draw request")
	ErrRewardDrawInsufficientTickets   = errors.New("insufficient sheep bells")
	ErrRewardDrawRequestConflict       = errors.New("reward draw request conflicts with an existing draw")
	ErrRewardDrawComponentsUnavailable = errors.New("reward components are unavailable")
	ErrRewardDrawInvalidCombination    = errors.New("select one unused component from each pool")
	ErrRewardDrawCardsUnavailable      = errors.New("reward cards are unavailable")
	ErrRewardDrawNothingToRedeem       = errors.New("no reward cards to redeem")
	ErrRewardDrawBelowMinimum          = errors.New("reward draw redemption requires at least $1")
	rewardDrawRequestKeyPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$`)
)

type RewardDrawPoolItem struct {
	TierID      string  `json:"tier_id"`
	Name        string  `json:"name"`
	Rarity      string  `json:"rarity"`
	Probability float64 `json:"probability"`
	Wool        int64   `json:"wool"`
	IsJackpot   bool    `json:"is_jackpot"`
}

type RewardDrawPool struct {
	ID    string               `json:"id"`
	Name  string               `json:"name"`
	Items []RewardDrawPoolItem `json:"items"`
}

type RewardDrawRecipe struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Wool int64  `json:"wool"`
}

type RewardDrawWallet struct {
	UserID                    int64   `json:"user_id"`
	TicketBalance             int64   `json:"ticket_balance"`
	ConsumptionRemainderUnits int64   `json:"consumption_remainder_units"`
	LifetimeConsumptionUnits  int64   `json:"lifetime_consumption_units"`
	LifetimeTickets           int64   `json:"lifetime_tickets"`
	LifetimeDraws             int64   `json:"lifetime_draws"`
	WoolRedeemed              int64   `json:"wool_redeemed"`
	RewardUnitsRedeemed       int64   `json:"reward_units_redeemed"`
	AvailableWool             int64   `json:"available_wool"`
	AvailableCards            int64   `json:"available_cards"`
	AvailableComponents       int64   `json:"available_components"`
	DiscoveredCombinations    int64   `json:"discovered_combinations"`
	UserBalance               float64 `json:"user_balance"`
	ConsumptionProgress       float64 `json:"consumption_progress"`
	AvailableReward           float64 `json:"available_reward"`
	RedeemedReward            float64 `json:"redeemed_reward"`
}

type RewardDrawComponent struct {
	ID         int64      `json:"id"`
	BatchID    int64      `json:"batch_id"`
	Position   int        `json:"position"`
	PoolID     string     `json:"pool_id"`
	TierID     string     `json:"tier_id"`
	Name       string     `json:"name"`
	Rarity     string     `json:"rarity"`
	WoolValue  int64      `json:"wool_value"`
	IsJackpot  bool       `json:"is_jackpot"`
	StackCount int64      `json:"stack_count,omitempty"`
	CombinedAt *time.Time `json:"combined_at,omitempty"`
	RedeemedAt *time.Time `json:"redeemed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type RewardDrawCard struct {
	ID                    int64      `json:"id"`
	BatchID               int64      `json:"batch_id,omitempty"`
	Position              int        `json:"position"`
	BreedID               string     `json:"breed_id"`
	BreedName             string     `json:"breed_name"`
	ProfessionID          string     `json:"profession_id"`
	ProfessionName        string     `json:"profession_name"`
	TreasureID            string     `json:"treasure_id"`
	TreasureName          string     `json:"treasure_name"`
	BreedComponentID      int64      `json:"breed_component_id,omitempty"`
	ProfessionComponentID int64      `json:"profession_component_id,omitempty"`
	TreasureComponentID   int64      `json:"treasure_component_id,omitempty"`
	RecipeID              string     `json:"recipe_id"`
	Title                 string     `json:"title"`
	WoolValue             int64      `json:"wool_value"`
	IsJackpot             bool       `json:"is_jackpot"`
	RedeemedAt            *time.Time `json:"redeemed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type RewardDrawCombineResult struct {
	Cards     []RewardDrawCard `json:"cards"`
	Quantity  int              `json:"quantity"`
	RecipeID  string           `json:"recipe_id"`
	WoolTotal int64            `json:"wool_total"`
}

type RewardDrawBatch struct {
	ID          int64                 `json:"id"`
	RequestKey  string                `json:"request_key"`
	DrawCount   int                   `json:"draw_count"`
	PoolVersion int                   `json:"pool_version"`
	PoolID      string                `json:"pool_id"`
	TotalWool   int64                 `json:"total_wool"`
	CreatedAt   time.Time             `json:"created_at"`
	Components  []RewardDrawComponent `json:"components"`
	Cards       []RewardDrawCard      `json:"cards,omitempty"`
}

type RewardDrawCollectionEntry struct {
	BreedID         string    `json:"breed_id"`
	BreedName       string    `json:"breed_name"`
	ProfessionID    string    `json:"profession_id"`
	ProfessionName  string    `json:"profession_name"`
	TreasureID      string    `json:"treasure_id"`
	TreasureName    string    `json:"treasure_name"`
	RecipeID        string    `json:"recipe_id"`
	Title           string    `json:"title"`
	WoolValue       int64     `json:"wool_value"`
	DiscoveredCount int64     `json:"discovered_count"`
	AvailableCount  int64     `json:"available_count"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
}

type RewardDrawComponentPage struct {
	Items  []RewardDrawComponent `json:"items"`
	Total  int64                 `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

type RewardDrawCardPage struct {
	Items  []RewardDrawCard `json:"items"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type RewardDrawHistoryPage struct {
	Items  []RewardDrawBatch `json:"items"`
	Total  int64             `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

type RewardDrawOverview struct {
	PoolVersion           int                         `json:"pool_version"`
	Wallet                *RewardDrawWallet           `json:"wallet"`
	Pools                 []RewardDrawPool            `json:"pools"`
	Recipes               []RewardDrawRecipe          `json:"recipes"`
	Collection            []RewardDrawCollectionEntry `json:"collection"`
	TotalCombinations     int                         `json:"total_combinations"`
	WoolPerCredit         int64                       `json:"wool_per_credit"`
	ExpectedRewardPerDraw float64                     `json:"expected_reward_per_draw"`
	TheoreticalPayoutRate float64                     `json:"theoretical_payout_rate"`
	ConsumptionPerDraw    float64                     `json:"consumption_per_draw"`
	MaximumDrawCount      int                         `json:"maximum_draw_count"`
	MinimumRedeemReward   float64                     `json:"minimum_redemption_reward"`
}

type RewardDrawCommand struct {
	UserID      int64
	RequestKey  string
	PoolVersion int
	PoolID      string
	Components  []RewardDrawComponent
}

type RewardDrawResult struct {
	Batch     *RewardDrawBatch `json:"batch"`
	Recovered bool             `json:"recovered"`
}

type RewardDrawRedemption struct {
	ID             int64      `json:"id"`
	RedeemCodeID   int64      `json:"redeem_code_id"`
	RedeemCode     string     `json:"redeem_code"`
	ComponentCount int        `json:"component_count"`
	CardCount      int        `json:"card_count"`
	Wool           int64      `json:"wool"`
	Reward         float64    `json:"reward"`
	Status         string     `json:"status"`
	UsedAt         *time.Time `json:"used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type RewardDrawRedemptionPage struct {
	Items  []RewardDrawRedemption `json:"items"`
	Total  int64                  `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type RewardDrawRepository interface {
	GetWallet(ctx context.Context, userID int64) (*RewardDrawWallet, error)
	ListCollection(ctx context.Context, userID int64) ([]RewardDrawCollectionEntry, error)
	ListComponents(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*RewardDrawComponentPage, error)
	ListCards(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*RewardDrawCardPage, error)
	ListHistory(ctx context.Context, userID int64, limit, offset int) (*RewardDrawHistoryPage, error)
	ListRedemptions(ctx context.Context, userID int64, limit, offset int) (*RewardDrawRedemptionPage, error)
	Draw(ctx context.Context, command *RewardDrawCommand) (*RewardDrawBatch, bool, error)
	Combine(ctx context.Context, userID int64, componentIDs []int64, quantity int) (*RewardDrawCombineResult, error)
	Redeem(ctx context.Context, userID int64, componentIDs, cardIDs []int64, redeemAll bool) (*RewardDrawRedemption, error)
}

type rewardDrawTier struct {
	id             string
	rarity         string
	probability    int
	wool           int64
	recipeName     string
	breedName      string
	professionName string
	treasureName   string
	isJackpot      bool
}

var rewardDrawTiers = []rewardDrawTier{
	{id: "code", rarity: "N", probability: 399_999, wool: 7, recipeName: "绒云咩", breedName: "绒云羊", professionName: "牧场学徒", treasureName: "棉花羊铃"},
	{id: "reasoning", rarity: "R", probability: 270_000, wool: 9, recipeName: "侦探咩", breedName: "墨角羊", professionName: "羊羊侦探", treasureName: "线索放大镜"},
	{id: "speed", rarity: "SR", probability: 180_000, wool: 11, recipeName: "极速咩", breedName: "风驰羊", professionName: "云端骑手", treasureName: "涡轮羊铃"},
	{id: "inspiration", rarity: "SSR", probability: 100_000, wool: 13, recipeName: "灵感咩", breedName: "彩云羊", professionName: "灵感画师", treasureName: "星光画笔"},
	{id: "wisdom", rarity: "UR", probability: 30_000, wool: 20, recipeName: "智慧咩", breedName: "月桂羊", professionName: "典籍学者", treasureName: "智慧书卷"},
	{id: "fortune", rarity: "SP", probability: 20_000, wool: 40, recipeName: "招财咩", breedName: "金绒羊", professionName: "招财管家", treasureName: "金色羊铃"},
	{id: "miracle", rarity: "MIRACLE", probability: 1, wool: RewardDrawJackpotWool, recipeName: "奇迹咩", breedName: "奇迹星羊", professionName: "奇迹召唤师", treasureName: "百元奇迹铃", isJackpot: true},
}

type RewardDrawService struct {
	repository RewardDrawRepository
	randomIntn func(int) (int, error)
}

func NewRewardDrawService(repository RewardDrawRepository) *RewardDrawService {
	if err := ValidateRewardDrawEconomy(); err != nil {
		panic(err)
	}
	return &RewardDrawService{repository: repository, randomIntn: cryptoRewardDrawIntn}
}

func (s *RewardDrawService) GetOverview(ctx context.Context, userID int64) (*RewardDrawOverview, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrRewardDrawInvalidRequest
	}
	wallet, err := s.repository.GetWallet(ctx, userID)
	if err != nil {
		return nil, err
	}
	collection, err := s.repository.ListCollection(ctx, userID)
	if err != nil {
		return nil, err
	}
	wallet.ConsumptionProgress = float64(wallet.ConsumptionRemainderUnits) / float64(RewardDrawConsumptionUnitScale)
	wallet.AvailableReward = float64(wallet.AvailableWool) / float64(RewardDrawWoolPerCredit)
	wallet.RedeemedReward = float64(wallet.RewardUnitsRedeemed) / float64(RewardDrawConsumptionUnitScale)
	expectedWool := rewardDrawExpectedWool()
	combinedWool := rewardDrawExpectedCombinedWool()
	return &RewardDrawOverview{
		PoolVersion:           RewardDrawPoolVersion,
		Wallet:                wallet,
		Pools:                 rewardDrawPublicPools(),
		Recipes:               rewardDrawPublicRecipes(),
		Collection:            collection,
		TotalCombinations:     len(rewardDrawTiers),
		WoolPerCredit:         RewardDrawWoolPerCredit,
		ExpectedRewardPerDraw: expectedWool / float64(RewardDrawWoolPerCredit),
		TheoreticalPayoutRate: combinedWool / float64(RewardDrawWoolPerCredit),
		ConsumptionPerDraw:    1,
		MaximumDrawCount:      RewardDrawMaxCount,
		MinimumRedeemReward:   float64(RewardDrawMinimumRewardUnits) / float64(RewardDrawConsumptionUnitScale),
	}, nil
}

func (s *RewardDrawService) Draw(ctx context.Context, userID int64, count int, poolID, requestKey string) (*RewardDrawResult, error) {
	requestKey = strings.TrimSpace(requestKey)
	if s == nil || s.repository == nil || userID <= 0 || count < 1 || count > RewardDrawMaxCount || !rewardDrawPoolValid(poolID) || !rewardDrawRequestKeyPattern.MatchString(requestKey) {
		return nil, ErrRewardDrawInvalidRequest
	}
	components := make([]RewardDrawComponent, count)
	for index := range components {
		component, err := s.drawComponent(poolID, index)
		if err != nil {
			return nil, err
		}
		components[index] = component
	}
	batch, recovered, err := s.repository.Draw(ctx, &RewardDrawCommand{
		UserID: userID, RequestKey: requestKey, PoolVersion: RewardDrawPoolVersion, PoolID: poolID, Components: components,
	})
	if err != nil {
		return nil, err
	}
	return &RewardDrawResult{Batch: batch, Recovered: recovered}, nil
}

func (s *RewardDrawService) ListComponents(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*RewardDrawComponentPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrRewardDrawInvalidRequest
	}
	return s.repository.ListComponents(ctx, userID, availableOnly, limit, offset)
}

func (s *RewardDrawService) ListCards(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*RewardDrawCardPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrRewardDrawInvalidRequest
	}
	return s.repository.ListCards(ctx, userID, availableOnly, limit, offset)
}

func (s *RewardDrawService) ListHistory(ctx context.Context, userID int64, limit, offset int) (*RewardDrawHistoryPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrRewardDrawInvalidRequest
	}
	return s.repository.ListHistory(ctx, userID, limit, offset)
}

func (s *RewardDrawService) ListRedemptions(ctx context.Context, userID int64, limit, offset int) (*RewardDrawRedemptionPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrRewardDrawInvalidRequest
	}
	return s.repository.ListRedemptions(ctx, userID, limit, offset)
}

func (s *RewardDrawService) Combine(ctx context.Context, userID int64, componentIDs []int64, quantity int) (*RewardDrawCombineResult, error) {
	if s == nil || s.repository == nil || userID <= 0 || len(componentIDs) != 3 || quantity <= 0 || quantity > RewardDrawMaxCombineQuantity {
		return nil, ErrRewardDrawInvalidRequest
	}
	seen := make(map[int64]struct{}, 3)
	for _, id := range componentIDs {
		if id <= 0 {
			return nil, ErrRewardDrawInvalidRequest
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 3 {
		return nil, ErrRewardDrawInvalidCombination
	}
	return s.repository.Combine(ctx, userID, componentIDs, quantity)
}

func (s *RewardDrawService) Redeem(ctx context.Context, userID int64, componentIDs, cardIDs []int64, redeemAll bool) (*RewardDrawRedemption, error) {
	hasIDs := len(componentIDs)+len(cardIDs) > 0
	if s == nil || s.repository == nil || userID <= 0 || redeemAll == hasIDs || len(componentIDs)+len(cardIDs) > 500 {
		return nil, ErrRewardDrawInvalidRequest
	}
	uniqueComponents, ok := uniqueRewardDrawIDs(componentIDs)
	if !ok {
		return nil, ErrRewardDrawInvalidRequest
	}
	uniqueCards, ok := uniqueRewardDrawIDs(cardIDs)
	if !ok || (!redeemAll && len(uniqueComponents)+len(uniqueCards) == 0) {
		return nil, ErrRewardDrawInvalidRequest
	}
	return s.repository.Redeem(ctx, userID, uniqueComponents, uniqueCards, redeemAll)
}

func uniqueRewardDrawIDs(ids []int64) ([]int64, bool) {
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, false
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique, true
}

func (s *RewardDrawService) drawComponent(poolID string, position int) (RewardDrawComponent, error) {
	tier, err := s.pickTier()
	if err != nil {
		return RewardDrawComponent{}, err
	}
	return RewardDrawComponent{
		Position: position, PoolID: poolID, TierID: tier.id, Name: rewardDrawTierName(poolID, tier),
		Rarity: tier.rarity, WoolValue: tier.wool, IsJackpot: tier.isJackpot,
	}, nil
}

func (s *RewardDrawService) pickTier() (rewardDrawTier, error) {
	value, err := s.randomIntn(RewardDrawProbabilityScale)
	if err != nil {
		return rewardDrawTier{}, err
	}
	cumulative := 0
	for _, tier := range rewardDrawTiers {
		cumulative += tier.probability
		if value < cumulative {
			return tier, nil
		}
	}
	return rewardDrawTier{}, errors.New("reward draw probability table is invalid")
}

func ComposeRewardDrawCard(components []RewardDrawComponent) (RewardDrawCard, error) {
	if len(components) != 3 {
		return RewardDrawCard{}, ErrRewardDrawInvalidCombination
	}
	byPool := make(map[string]RewardDrawComponent, 3)
	for _, component := range components {
		if !rewardDrawPoolValid(component.PoolID) || component.WoolValue <= 0 {
			return RewardDrawCard{}, ErrRewardDrawInvalidCombination
		}
		tier, ok := rewardDrawTierByID(component.TierID)
		if !ok || tier.isJackpot != component.IsJackpot {
			return RewardDrawCard{}, ErrRewardDrawInvalidCombination
		}
		if _, exists := byPool[component.PoolID]; exists {
			return RewardDrawCard{}, ErrRewardDrawInvalidCombination
		}
		byPool[component.PoolID] = component
	}
	breed, breedOK := byPool["breed"]
	profession, professionOK := byPool["profession"]
	treasure, treasureOK := byPool["treasure"]
	if !breedOK || !professionOK || !treasureOK {
		return RewardDrawCard{}, ErrRewardDrawInvalidCombination
	}
	card := RewardDrawCard{
		BreedID: breed.TierID, BreedName: breed.Name, BreedComponentID: breed.ID,
		ProfessionID: profession.TierID, ProfessionName: profession.Name, ProfessionComponentID: profession.ID,
		TreasureID: treasure.TierID, TreasureName: treasure.Name, TreasureComponentID: treasure.ID,
		Title:     fmt.Sprintf("%s · %s · %s", breed.Name, profession.Name, treasure.Name),
		WoolValue: breed.WoolValue + profession.WoolValue + treasure.WoolValue,
	}
	if breed.IsJackpot || profession.IsJackpot || treasure.IsJackpot {
		card.RecipeID = "miracle"
		card.Title = "奇迹咩"
		card.WoolValue = RewardDrawJackpotWool
		card.IsJackpot = true
	} else {
		if breed.TierID != profession.TierID || profession.TierID != treasure.TierID {
			return RewardDrawCard{}, ErrRewardDrawInvalidCombination
		}
		tier, ok := rewardDrawTierByID(breed.TierID)
		if !ok || tier.isJackpot {
			return RewardDrawCard{}, ErrRewardDrawInvalidCombination
		}
		card.RecipeID = tier.id
		card.Title = tier.recipeName
		card.WoolValue = rewardDrawCombinedWool(card.WoolValue)
	}
	return card, nil
}

func ValidateRewardDrawEconomy() error {
	probability := 0
	jackpots := 0
	for _, tier := range rewardDrawTiers {
		if tier.probability <= 0 || tier.wool <= 0 || tier.id == "" || tier.rarity == "" {
			return errors.New("reward draw tier is invalid")
		}
		if tier.isJackpot {
			jackpots++
		}
		probability += tier.probability
	}
	if probability != RewardDrawProbabilityScale || jackpots != 1 {
		return fmt.Errorf("reward draw probability table is invalid: total=%d jackpots=%d", probability, jackpots)
	}
	rawPayout := rewardDrawExpectedWool() / float64(RewardDrawWoolPerCredit)
	if rawPayout > RewardDrawRawPayoutCap {
		return fmt.Errorf("reward draw raw payout %.8f exceeds %.8f", rawPayout, RewardDrawRawPayoutCap)
	}
	combinedPayout := rewardDrawExpectedCombinedWool() / float64(RewardDrawWoolPerCredit)
	if combinedPayout > RewardDrawPayoutCap {
		return fmt.Errorf("reward draw combined payout %.8f exceeds %.8f", combinedPayout, RewardDrawPayoutCap)
	}
	return nil
}

func rewardDrawExpectedWool() float64 {
	expected := 0.0
	for _, tier := range rewardDrawTiers {
		expected += float64(tier.probability) / float64(RewardDrawProbabilityScale) * float64(tier.wool)
	}
	return expected
}

func rewardDrawExpectedCombinedWool() float64 {
	expected := 0.0
	for _, tier := range rewardDrawTiers {
		probability := float64(tier.probability) / float64(RewardDrawProbabilityScale)
		if tier.isJackpot {
			expected += probability * float64(RewardDrawJackpotWool)
			continue
		}
		expected += probability * float64(rewardDrawCombinedWool(tier.wool*3)) / 3
	}
	return expected
}

func rewardDrawCombinedWool(componentWool int64) int64 {
	return (componentWool*3 + 1) / 2
}

func rewardDrawPublicPools() []RewardDrawPool {
	pools := []RewardDrawPool{{ID: "breed", Name: "羊种池"}, {ID: "profession", Name: "职业池"}, {ID: "treasure", Name: "宝物池"}}
	for poolIndex := range pools {
		pools[poolIndex].Items = make([]RewardDrawPoolItem, 0, len(rewardDrawTiers))
		for _, tier := range rewardDrawTiers {
			pools[poolIndex].Items = append(pools[poolIndex].Items, RewardDrawPoolItem{
				TierID: tier.id, Name: rewardDrawTierName(pools[poolIndex].ID, tier), Rarity: tier.rarity,
				Probability: float64(tier.probability) / float64(RewardDrawProbabilityScale), Wool: tier.wool, IsJackpot: tier.isJackpot,
			})
		}
	}
	return pools
}

func rewardDrawPublicRecipes() []RewardDrawRecipe {
	recipes := make([]RewardDrawRecipe, 0, len(rewardDrawTiers))
	for _, tier := range rewardDrawTiers {
		wool := rewardDrawCombinedWool(tier.wool * 3)
		if tier.isJackpot {
			wool = RewardDrawJackpotWool
		}
		recipes = append(recipes, RewardDrawRecipe{ID: tier.id, Name: tier.recipeName, Wool: wool})
	}
	return recipes
}

func rewardDrawPoolValid(poolID string) bool {
	return poolID == "breed" || poolID == "profession" || poolID == "treasure"
}

func rewardDrawTierName(poolID string, tier rewardDrawTier) string {
	switch poolID {
	case "profession":
		return tier.professionName
	case "treasure":
		return tier.treasureName
	default:
		return tier.breedName
	}
}

func rewardDrawTierByID(id string) (rewardDrawTier, bool) {
	for _, tier := range rewardDrawTiers {
		if tier.id == id {
			return tier, true
		}
	}
	return rewardDrawTier{}, false
}

func cryptoRewardDrawIntn(max int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}
