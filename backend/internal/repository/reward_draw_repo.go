package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type rewardDrawRepository struct {
	db *sql.DB
}

func NewRewardDrawRepository(_ *dbent.Client, sqlDB *sql.DB) service.RewardDrawRepository {
	return &rewardDrawRepository{db: sqlDB}
}

func (r *rewardDrawRepository) GetWallet(ctx context.Context, userID int64) (*service.RewardDrawWallet, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, userID); err != nil {
		return nil, err
	}
	var wallet service.RewardDrawWallet
	err := r.db.QueryRowContext(ctx, `
		SELECT w.user_id, w.ticket_balance, w.consumption_remainder_units,
			w.lifetime_consumption_units, w.lifetime_tickets, w.lifetime_draws,
			w.wool_redeemed, w.reward_units_redeemed, u.balance,
			COALESCE((SELECT SUM(c.wool_value) FROM reward_draw_cards c WHERE c.user_id = w.user_id AND c.redeemed_at IS NULL), 0)
				+ COALESCE((SELECT SUM(c.wool_value) FROM reward_draw_components c WHERE c.user_id = w.user_id AND c.combined_at IS NULL AND c.redeemed_at IS NULL), 0),
			(SELECT COUNT(*) FROM reward_draw_cards c WHERE c.user_id = w.user_id AND c.redeemed_at IS NULL),
			(SELECT COUNT(*) FROM reward_draw_components c WHERE c.user_id = w.user_id AND c.combined_at IS NULL AND c.redeemed_at IS NULL),
			(SELECT COUNT(DISTINCT (c.breed_id, c.profession_id, c.treasure_id)) FROM reward_draw_cards c WHERE c.user_id = w.user_id)
		FROM reward_draw_wallets w
		JOIN users u ON u.id = w.user_id AND u.deleted_at IS NULL
		WHERE w.user_id = $1
	`, userID).Scan(
		&wallet.UserID,
		&wallet.TicketBalance,
		&wallet.ConsumptionRemainderUnits,
		&wallet.LifetimeConsumptionUnits,
		&wallet.LifetimeTickets,
		&wallet.LifetimeDraws,
		&wallet.WoolRedeemed,
		&wallet.RewardUnitsRedeemed,
		&wallet.UserBalance,
		&wallet.AvailableWool,
		&wallet.AvailableCards,
		&wallet.AvailableComponents,
		&wallet.DiscoveredCombinations,
	)
	return &wallet, err
}

func (r *rewardDrawRepository) ListCollection(ctx context.Context, userID int64) ([]service.RewardDrawCollectionEntry, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT breed_id, breed_name, profession_id, profession_name,
			treasure_id, treasure_name, recipe_id, title, MAX(wool_value),
			COUNT(*), COUNT(*) FILTER (WHERE redeemed_at IS NULL), MIN(created_at)
		FROM reward_draw_cards
		WHERE user_id = $1
		GROUP BY breed_id, breed_name, profession_id, profession_name,
			treasure_id, treasure_name, recipe_id, title
		ORDER BY MIN(created_at) DESC, title ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.RewardDrawCollectionEntry, 0)
	for rows.Next() {
		var item service.RewardDrawCollectionEntry
		if err := rows.Scan(
			&item.BreedID, &item.BreedName, &item.ProfessionID, &item.ProfessionName,
			&item.TreasureID, &item.TreasureName, &item.RecipeID, &item.Title,
			&item.WoolValue, &item.DiscoveredCount, &item.AvailableCount, &item.FirstSeenAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *rewardDrawRepository) ListComponents(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*service.RewardDrawComponentPage, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	limit, offset = rewardDrawPage(limit, offset, 300)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reward_draw_components
		WHERE user_id = $1 AND (NOT $2 OR (combined_at IS NULL AND redeemed_at IS NULL))
	`, userID, availableOnly).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, batch_id, position, pool_id, tier_id, name, rarity,
			wool_value, is_jackpot, combined_at, redeemed_at,
			COUNT(*) FILTER (WHERE combined_at IS NULL AND redeemed_at IS NULL)
				OVER (PARTITION BY pool_id, tier_id), created_at
		FROM reward_draw_components
		WHERE user_id = $1 AND (NOT $2 OR (combined_at IS NULL AND redeemed_at IS NULL))
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, userID, availableOnly, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.RewardDrawComponent, 0, limit)
	for rows.Next() {
		var component service.RewardDrawComponent
		if err := rows.Scan(
			&component.ID, &component.BatchID, &component.Position, &component.PoolID,
			&component.TierID, &component.Name, &component.Rarity, &component.WoolValue,
			&component.IsJackpot, &component.CombinedAt, &component.RedeemedAt,
			&component.StackCount, &component.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, component)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.RewardDrawComponentPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *rewardDrawRepository) ListCards(ctx context.Context, userID int64, availableOnly bool, limit, offset int) (*service.RewardDrawCardPage, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	limit, offset = rewardDrawPage(limit, offset, 300)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reward_draw_cards
		WHERE user_id = $1 AND (NOT $2 OR redeemed_at IS NULL)
	`, userID, availableOnly).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, COALESCE(batch_id, 0), position, breed_id, breed_name, profession_id,
			profession_name, treasure_id, treasure_name, COALESCE(breed_component_id, 0),
			COALESCE(profession_component_id, 0), COALESCE(treasure_component_id, 0),
			recipe_id, title, wool_value, is_jackpot, redeemed_at, created_at
		FROM reward_draw_cards
		WHERE user_id = $1 AND (NOT $2 OR redeemed_at IS NULL)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, userID, availableOnly, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.RewardDrawCard, 0, limit)
	for rows.Next() {
		card, err := scanRewardDrawCard(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.RewardDrawCardPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *rewardDrawRepository) ListHistory(ctx context.Context, userID int64, limit, offset int) (*service.RewardDrawHistoryPage, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	limit, offset = rewardDrawPage(limit, offset, 30)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_batches WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_key, draw_count, pool_version, pool_id, total_wool, created_at
		FROM reward_draw_batches
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	items := make([]service.RewardDrawBatch, 0, limit)
	batchIndex := make(map[int64]int, limit)
	batchIDs := make([]int64, 0, limit)
	for rows.Next() {
		var batch service.RewardDrawBatch
		if err := rows.Scan(&batch.ID, &batch.RequestKey, &batch.DrawCount, &batch.PoolVersion, &batch.PoolID, &batch.TotalWool, &batch.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		batch.Components = make([]service.RewardDrawComponent, 0, batch.DrawCount)
		batch.Cards = make([]service.RewardDrawCard, 0)
		batchIndex[batch.ID] = len(items)
		batchIDs = append(batchIDs, batch.ID)
		items = append(items, batch)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(batchIDs) > 0 {
		if err := r.loadHistoryComponents(ctx, items, batchIndex, batchIDs); err != nil {
			return nil, err
		}
		if err := r.loadHistoryCards(ctx, items, batchIndex, batchIDs); err != nil {
			return nil, err
		}
	}
	return &service.RewardDrawHistoryPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *rewardDrawRepository) ListRedemptions(ctx context.Context, userID int64, limit, offset int) (*service.RewardDrawRedemptionPage, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	limit, offset = rewardDrawPage(limit, offset, 30)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_redemptions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.redeem_code_id, c.code, r.component_count, r.card_count, r.wool, r.reward_units,
			c.status, r.created_at
		FROM reward_draw_redemptions r
		JOIN redeem_codes c ON c.id = r.redeem_code_id
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.RewardDrawRedemption, 0, limit)
	for rows.Next() {
		var item service.RewardDrawRedemption
		var rewardUnits int64
		if err := rows.Scan(&item.ID, &item.RedeemCodeID, &item.RedeemCode, &item.ComponentCount, &item.CardCount, &item.Wool, &rewardUnits, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Reward = float64(rewardUnits) / float64(service.RewardDrawConsumptionUnitScale)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.RewardDrawRedemptionPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *rewardDrawRepository) Draw(ctx context.Context, command *service.RewardDrawCommand) (_ *service.RewardDrawBatch, recovered bool, err error) {
	if r == nil || r.db == nil || command == nil || command.UserID <= 0 || len(command.Components) < 1 || len(command.Components) > service.RewardDrawMaxCount {
		return nil, false, service.ErrRewardDrawInvalidRequest
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, command.UserID); err != nil {
		return nil, false, err
	}
	var ticketBalance int64
	if err := tx.QueryRowContext(ctx, `
		SELECT ticket_balance FROM reward_draw_wallets WHERE user_id = $1 FOR UPDATE
	`, command.UserID).Scan(&ticketBalance); err != nil {
		return nil, false, err
	}
	existing, err := loadRewardDrawBatchTx(ctx, tx, command.UserID, command.RequestKey)
	if err == nil {
		if existing.DrawCount != len(command.Components) || existing.PoolVersion != command.PoolVersion || existing.PoolID != command.PoolID {
			return nil, false, service.ErrRewardDrawRequestConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		tx = nil
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if ticketBalance < int64(len(command.Components)) {
		return nil, false, service.ErrRewardDrawInsufficientTickets
	}
	totalWool := int64(0)
	for _, component := range command.Components {
		if component.WoolValue <= 0 || component.PoolID != command.PoolID {
			return nil, false, service.ErrRewardDrawInvalidRequest
		}
		totalWool += component.WoolValue
	}
	var batch service.RewardDrawBatch
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO reward_draw_batches (user_id, request_key, draw_count, pool_version, pool_id, total_wool)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, request_key, draw_count, pool_version, pool_id, total_wool, created_at
	`, command.UserID, command.RequestKey, len(command.Components), command.PoolVersion, command.PoolID, totalWool).Scan(
		&batch.ID, &batch.RequestKey, &batch.DrawCount, &batch.PoolVersion, &batch.PoolID, &batch.TotalWool, &batch.CreatedAt,
	); err != nil {
		return nil, false, err
	}
	batch.Components = make([]service.RewardDrawComponent, 0, len(command.Components))
	for _, component := range command.Components {
		component.BatchID = batch.ID
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO reward_draw_components (
				batch_id, user_id, position, pool_id, tier_id, name, rarity, wool_value, is_jackpot
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id, created_at
		`, batch.ID, command.UserID, component.Position, component.PoolID, component.TierID,
			component.Name, component.Rarity, component.WoolValue, component.IsJackpot).Scan(&component.ID, &component.CreatedAt); err != nil {
			return nil, false, err
		}
		batch.Components = append(batch.Components, component)
	}
	ticketBalance -= int64(len(command.Components))
	if _, err := tx.ExecContext(ctx, `
		UPDATE reward_draw_wallets
		SET ticket_balance = $2, lifetime_draws = lifetime_draws + $3, updated_at = NOW()
		WHERE user_id = $1
	`, command.UserID, ticketBalance, len(command.Components)); err != nil {
		return nil, false, err
	}
	metadata, _ := json.Marshal(map[string]any{"batch_id": batch.ID, "pool_id": batch.PoolID, "component_wool": totalWool})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_ledger (
			user_id, source_type, source_key, ticket_delta, ticket_after, metadata
		) VALUES ($1, 'draw', $2, $3, $4, $5::jsonb)
	`, command.UserID, command.RequestKey, -len(command.Components), ticketBalance, string(metadata)); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	tx = nil
	return &batch, false, nil
}

func (r *rewardDrawRepository) Combine(ctx context.Context, userID int64, componentIDs []int64, quantity int) (_ *service.RewardDrawCombineResult, err error) {
	if r == nil || r.db == nil || userID <= 0 || len(componentIDs) != 3 || quantity <= 0 || quantity > service.RewardDrawMaxCombineQuantity {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, batch_id, position, pool_id, tier_id, name, rarity,
			wool_value, is_jackpot, combined_at, redeemed_at, created_at
		FROM reward_draw_components
		WHERE user_id = $1 AND id = ANY($2) AND combined_at IS NULL AND redeemed_at IS NULL
		ORDER BY id
	`, userID, pq.Array(componentIDs))
	if err != nil {
		return nil, err
	}
	selectedComponents := make([]service.RewardDrawComponent, 0, 3)
	for rows.Next() {
		component, scanErr := scanRewardDrawComponent(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		selectedComponents = append(selectedComponents, component)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(selectedComponents) != 3 {
		return nil, service.ErrRewardDrawComponentsUnavailable
	}
	if _, err := service.ComposeRewardDrawCard(selectedComponents); err != nil {
		return nil, err
	}
	selectedByPool := make(map[string]service.RewardDrawComponent, 3)
	for _, component := range selectedComponents {
		if _, exists := selectedByPool[component.PoolID]; exists {
			return nil, service.ErrRewardDrawInvalidCombination
		}
		selectedByPool[component.PoolID] = component
	}
	poolIDs := []string{"breed", "profession", "treasure"}
	stackComponents := make(map[string][]service.RewardDrawComponent, 3)
	for _, poolID := range poolIDs {
		selected, ok := selectedByPool[poolID]
		if !ok {
			return nil, service.ErrRewardDrawInvalidCombination
		}
		stackRows, err := tx.QueryContext(ctx, `
			SELECT id, batch_id, position, pool_id, tier_id, name, rarity,
				wool_value, is_jackpot, combined_at, redeemed_at, created_at
			FROM reward_draw_components
			WHERE user_id = $1 AND pool_id = $2 AND tier_id = $3
				AND combined_at IS NULL AND redeemed_at IS NULL
			ORDER BY id LIMIT $4 FOR UPDATE
		`, userID, poolID, selected.TierID, quantity)
		if err != nil {
			return nil, err
		}
		stack := make([]service.RewardDrawComponent, 0)
		for stackRows.Next() {
			component, scanErr := scanRewardDrawComponent(stackRows)
			if scanErr != nil {
				_ = stackRows.Close()
				return nil, scanErr
			}
			stack = append(stack, component)
		}
		if err := stackRows.Close(); err != nil {
			return nil, err
		}
		if len(stack) != quantity {
			return nil, service.ErrRewardDrawComponentsUnavailable
		}
		stackComponents[poolID] = stack
	}

	cards := make([]service.RewardDrawCard, 0, quantity)
	consumedIDs := make([]int64, 0, quantity*3)
	cardIDs := make([]int64, 0, quantity)
	var woolTotal int64
	for index := 0; index < quantity; index++ {
		components := []service.RewardDrawComponent{
			stackComponents["breed"][index],
			stackComponents["profession"][index],
			stackComponents["treasure"][index],
		}
		card, err := service.ComposeRewardDrawCard(components)
		if err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO reward_draw_cards (
				batch_id, user_id, position, breed_id, breed_name, profession_id,
				profession_name, treasure_id, treasure_name, breed_component_id,
				profession_component_id, treasure_component_id, recipe_id, title,
				wool_value, is_jackpot
			) VALUES (NULL, $1, 0, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING id, created_at
		`, userID, card.BreedID, card.BreedName, card.ProfessionID, card.ProfessionName,
			card.TreasureID, card.TreasureName, card.BreedComponentID, card.ProfessionComponentID,
			card.TreasureComponentID, card.RecipeID, card.Title, card.WoolValue, card.IsJackpot).Scan(&card.ID, &card.CreatedAt); err != nil {
			return nil, err
		}
		cards = append(cards, card)
		cardIDs = append(cardIDs, card.ID)
		woolTotal += card.WoolValue
		for _, component := range components {
			consumedIDs = append(consumedIDs, component.ID)
		}
	}
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE reward_draw_components SET combined_at = NOW()
		WHERE user_id = $1 AND id = ANY($2) AND combined_at IS NULL AND redeemed_at IS NULL
	`, userID, pq.Array(consumedIDs))
	if err != nil {
		return nil, err
	}
	updated, err := updateResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if updated != int64(len(consumedIDs)) {
		return nil, service.ErrRewardDrawComponentsUnavailable
	}
	var ticketBalance int64
	if err := tx.QueryRowContext(ctx, `SELECT ticket_balance FROM reward_draw_wallets WHERE user_id = $1`, userID).Scan(&ticketBalance); err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(map[string]any{
		"card_ids": cardIDs, "component_ids": consumedIDs, "quantity": quantity, "recipe_id": cards[0].RecipeID,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_ledger (user_id, source_type, source_key, ticket_after, wool_delta, metadata)
		VALUES ($1, 'combine', $2, $3, $4, $5::jsonb)
	`, userID, fmt.Sprint(cards[0].ID), ticketBalance, woolTotal, string(metadata)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return &service.RewardDrawCombineResult{
		Cards: cards, Quantity: quantity, RecipeID: cards[0].RecipeID, WoolTotal: woolTotal,
	}, nil
}

func (r *rewardDrawRepository) Redeem(ctx context.Context, userID int64, componentIDs, cardIDs []int64, redeemAll bool) (_ *service.RewardDrawRedemption, err error) {
	if r == nil || r.db == nil || userID <= 0 || redeemAll == (len(componentIDs)+len(cardIDs) > 0) {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	componentQuery := `
		SELECT id, wool_value FROM reward_draw_components
		WHERE user_id = $1 AND combined_at IS NULL AND redeemed_at IS NULL
	`
	componentArgs := []any{userID}
	if !redeemAll {
		componentQuery += ` AND id = ANY($2)`
		componentArgs = append(componentArgs, pq.Array(componentIDs))
	}
	componentQuery += ` ORDER BY id FOR UPDATE`
	componentRows, err := tx.QueryContext(ctx, componentQuery, componentArgs...)
	if err != nil {
		return nil, err
	}
	redeemedComponentIDs := make([]int64, 0, len(componentIDs))
	totalWool := int64(0)
	for componentRows.Next() {
		var id, wool int64
		if err := componentRows.Scan(&id, &wool); err != nil {
			_ = componentRows.Close()
			return nil, err
		}
		if wool > math.MaxInt64-totalWool {
			_ = componentRows.Close()
			return nil, service.ErrRewardDrawInvalidRequest
		}
		redeemedComponentIDs = append(redeemedComponentIDs, id)
		totalWool += wool
	}
	if err := componentRows.Close(); err != nil {
		return nil, err
	}

	cardQuery := `
		SELECT id, wool_value FROM reward_draw_cards
		WHERE user_id = $1 AND redeemed_at IS NULL
	`
	cardArgs := []any{userID}
	if !redeemAll {
		cardQuery += ` AND id = ANY($2)`
		cardArgs = append(cardArgs, pq.Array(cardIDs))
	}
	cardQuery += ` ORDER BY id FOR UPDATE`
	cardRows, err := tx.QueryContext(ctx, cardQuery, cardArgs...)
	if err != nil {
		return nil, err
	}
	redeemedCardIDs := make([]int64, 0, len(cardIDs))
	for cardRows.Next() {
		var id, wool int64
		if err := cardRows.Scan(&id, &wool); err != nil {
			_ = cardRows.Close()
			return nil, err
		}
		if wool > math.MaxInt64-totalWool {
			_ = cardRows.Close()
			return nil, service.ErrRewardDrawInvalidRequest
		}
		redeemedCardIDs = append(redeemedCardIDs, id)
		totalWool += wool
	}
	if err := cardRows.Close(); err != nil {
		return nil, err
	}
	if len(redeemedComponentIDs)+len(redeemedCardIDs) == 0 {
		return nil, service.ErrRewardDrawNothingToRedeem
	}
	if !redeemAll && len(redeemedComponentIDs) != len(componentIDs) {
		return nil, service.ErrRewardDrawComponentsUnavailable
	}
	if !redeemAll && len(redeemedCardIDs) != len(cardIDs) {
		return nil, service.ErrRewardDrawCardsUnavailable
	}
	if totalWool > math.MaxInt64/service.RewardDrawRewardUnitPerWool {
		return nil, service.ErrRewardDrawInvalidRequest
	}
	rewardUnits := totalWool * service.RewardDrawRewardUnitPerWool
	if rewardUnits < service.RewardDrawMinimumRewardUnits {
		return nil, service.ErrRewardDrawBelowMinimum
	}
	rewardAmount := decimal.NewFromInt(rewardUnits).Div(decimal.NewFromInt(service.RewardDrawConsumptionUnitScale))
	redeemCode, err := service.GenerateRedeemCode()
	if err != nil {
		return nil, err
	}
	var redemption service.RewardDrawRedemption
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO redeem_codes (code, type, value, status, notes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, redeemCode, service.RedeemTypeBalance, rewardAmount.String(), service.StatusUnused,
		fmt.Sprintf("reward_draw:user=%d", userID)).Scan(&redemption.RedeemCodeID); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO reward_draw_redemptions (user_id, component_count, card_count, wool, reward_units, redeem_code_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, component_count, card_count, wool, created_at
	`, userID, len(redeemedComponentIDs), len(redeemedCardIDs), totalWool, rewardUnits, redemption.RedeemCodeID).Scan(
		&redemption.ID, &redemption.ComponentCount, &redemption.CardCount, &redemption.Wool, &redemption.CreatedAt,
	); err != nil {
		return nil, err
	}
	componentUpdate, err := tx.ExecContext(ctx, `
		UPDATE reward_draw_components
		SET redemption_id = $2, redeemed_at = NOW()
		WHERE user_id = $1 AND id = ANY($3) AND combined_at IS NULL AND redeemed_at IS NULL
	`, userID, redemption.ID, pq.Array(redeemedComponentIDs))
	if err != nil {
		return nil, err
	}
	componentUpdated, err := componentUpdate.RowsAffected()
	if err != nil {
		return nil, err
	}
	if componentUpdated != int64(len(redeemedComponentIDs)) {
		return nil, service.ErrRewardDrawComponentsUnavailable
	}
	cardUpdate, err := tx.ExecContext(ctx, `
		UPDATE reward_draw_cards
		SET redemption_id = $2, redeemed_at = NOW()
		WHERE user_id = $1 AND id = ANY($3) AND redeemed_at IS NULL
	`, userID, redemption.ID, pq.Array(redeemedCardIDs))
	if err != nil {
		return nil, err
	}
	cardUpdated, err := cardUpdate.RowsAffected()
	if err != nil {
		return nil, err
	}
	if cardUpdated != int64(len(redeemedCardIDs)) {
		return nil, service.ErrRewardDrawCardsUnavailable
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, userID); err != nil {
		return nil, err
	}
	var ticketBalance int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE reward_draw_wallets
		SET wool_redeemed = wool_redeemed + $2,
			reward_units_redeemed = reward_units_redeemed + $3,
			updated_at = NOW()
		WHERE user_id = $1
		RETURNING ticket_balance
	`, userID, totalWool, rewardUnits).Scan(&ticketBalance); err != nil {
		return nil, err
	}
	redemption.RedeemCode = redeemCode
	redemption.Reward, _ = rewardAmount.Float64()
	redemption.Status = service.StatusUnused
	metadata, _ := json.Marshal(map[string]any{
		"redemption_id": redemption.ID, "redeem_code_id": redemption.RedeemCodeID,
		"component_count": len(redeemedComponentIDs), "card_count": len(redeemedCardIDs),
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_ledger (
			user_id, source_type, source_key, ticket_after, wool_delta, reward_units, metadata
		) VALUES ($1, 'redeem', $2, $3, $4, $5, $6::jsonb)
	`, userID, fmt.Sprint(redemption.ID), ticketBalance, -totalWool, rewardUnits, string(metadata)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return &redemption, nil
}

func (r *rewardDrawRepository) loadHistoryComponents(ctx context.Context, batches []service.RewardDrawBatch, index map[int64]int, batchIDs []int64) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, batch_id, position, pool_id, tier_id, name, rarity,
			wool_value, is_jackpot, combined_at, redeemed_at, created_at
		FROM reward_draw_components WHERE batch_id = ANY($1)
		ORDER BY batch_id, position
	`, pq.Array(batchIDs))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		component, err := scanRewardDrawComponent(rows)
		if err != nil {
			return err
		}
		batches[index[component.BatchID]].Components = append(batches[index[component.BatchID]].Components, component)
	}
	return rows.Err()
}

func (r *rewardDrawRepository) loadHistoryCards(ctx context.Context, batches []service.RewardDrawBatch, index map[int64]int, batchIDs []int64) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, COALESCE(batch_id, 0), position, breed_id, breed_name, profession_id,
			profession_name, treasure_id, treasure_name, COALESCE(breed_component_id, 0),
			COALESCE(profession_component_id, 0), COALESCE(treasure_component_id, 0),
			recipe_id, title, wool_value, is_jackpot, redeemed_at, created_at
		FROM reward_draw_cards WHERE batch_id = ANY($1)
		ORDER BY batch_id, position
	`, pq.Array(batchIDs))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		card, err := scanRewardDrawCard(rows)
		if err != nil {
			return err
		}
		batches[index[card.BatchID]].Cards = append(batches[index[card.BatchID]].Cards, card)
	}
	return rows.Err()
}

func creditRewardDrawTickets(ctx context.Context, tx *sql.Tx, userID int64, sourceType, sourceKey string, actualCost float64) error {
	if tx == nil || userID <= 0 || strings.TrimSpace(sourceKey) == "" || actualCost <= 0 {
		return nil
	}
	actualUnits, err := rewardDrawAmountToUnits(actualCost)
	if err != nil {
		return err
	}
	if actualUnits == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, userID); err != nil {
		return err
	}
	var ticketBalance, remainder int64
	if err := tx.QueryRowContext(ctx, `
		SELECT ticket_balance, consumption_remainder_units
		FROM reward_draw_wallets WHERE user_id = $1 FOR UPDATE
	`, userID).Scan(&ticketBalance, &remainder); err != nil {
		return err
	}
	var ledgerID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO reward_draw_ledger (user_id, source_type, source_key, consumption_units)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, source_type, source_key) DO NOTHING
		RETURNING id
	`, userID, sourceType, sourceKey, actualUnits).Scan(&ledgerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	total := remainder + actualUnits
	tickets := total / service.RewardDrawConsumptionUnitScale
	remainder = total % service.RewardDrawConsumptionUnitScale
	ticketBalance += tickets
	if _, err := tx.ExecContext(ctx, `
		UPDATE reward_draw_wallets
		SET ticket_balance = $2,
			consumption_remainder_units = $3,
			lifetime_consumption_units = lifetime_consumption_units + $4,
			lifetime_tickets = lifetime_tickets + $5,
			updated_at = NOW()
		WHERE user_id = $1
	`, userID, ticketBalance, remainder, actualUnits, tickets); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE reward_draw_ledger
		SET ticket_delta = $2, ticket_after = $3
		WHERE id = $1
	`, ledgerID, tickets, ticketBalance)
	return err
}

func loadRewardDrawBatchTx(ctx context.Context, tx *sql.Tx, userID int64, requestKey string) (*service.RewardDrawBatch, error) {
	var batch service.RewardDrawBatch
	if err := tx.QueryRowContext(ctx, `
		SELECT id, request_key, draw_count, pool_version, pool_id, total_wool, created_at
		FROM reward_draw_batches WHERE user_id = $1 AND request_key = $2
	`, userID, requestKey).Scan(
		&batch.ID, &batch.RequestKey, &batch.DrawCount, &batch.PoolVersion, &batch.PoolID, &batch.TotalWool, &batch.CreatedAt,
	); err != nil {
		return nil, err
	}
	componentRows, err := tx.QueryContext(ctx, `
		SELECT id, batch_id, position, pool_id, tier_id, name, rarity,
			wool_value, is_jackpot, combined_at, redeemed_at, created_at
		FROM reward_draw_components WHERE batch_id = $1 ORDER BY position ASC
	`, batch.ID)
	if err != nil {
		return nil, err
	}
	batch.Components = make([]service.RewardDrawComponent, 0, batch.DrawCount)
	for componentRows.Next() {
		component, err := scanRewardDrawComponent(componentRows)
		if err != nil {
			_ = componentRows.Close()
			return nil, err
		}
		batch.Components = append(batch.Components, component)
	}
	if err := componentRows.Close(); err != nil {
		return nil, err
	}
	cardRows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(batch_id, 0), position, breed_id, breed_name, profession_id,
			profession_name, treasure_id, treasure_name, COALESCE(breed_component_id, 0),
			COALESCE(profession_component_id, 0), COALESCE(treasure_component_id, 0),
			recipe_id, title, wool_value, is_jackpot, redeemed_at, created_at
		FROM reward_draw_cards WHERE batch_id = $1 ORDER BY position ASC
	`, batch.ID)
	if err != nil {
		return nil, err
	}
	batch.Cards = make([]service.RewardDrawCard, 0)
	for cardRows.Next() {
		card, err := scanRewardDrawCard(cardRows)
		if err != nil {
			_ = cardRows.Close()
			return nil, err
		}
		batch.Cards = append(batch.Cards, card)
	}
	if err := cardRows.Close(); err != nil {
		return nil, err
	}
	return &batch, nil
}

type rewardDrawScanner interface {
	Scan(dest ...any) error
}

func scanRewardDrawComponent(scanner rewardDrawScanner) (service.RewardDrawComponent, error) {
	var component service.RewardDrawComponent
	var combinedAt, redeemedAt sql.NullTime
	if err := scanner.Scan(
		&component.ID, &component.BatchID, &component.Position, &component.PoolID,
		&component.TierID, &component.Name, &component.Rarity, &component.WoolValue,
		&component.IsJackpot, &combinedAt, &redeemedAt, &component.CreatedAt,
	); err != nil {
		return service.RewardDrawComponent{}, err
	}
	if combinedAt.Valid {
		component.CombinedAt = &combinedAt.Time
	}
	if redeemedAt.Valid {
		component.RedeemedAt = &redeemedAt.Time
	}
	return component, nil
}

func scanRewardDrawCard(scanner rewardDrawScanner) (service.RewardDrawCard, error) {
	var card service.RewardDrawCard
	var redeemedAt sql.NullTime
	if err := scanner.Scan(
		&card.ID, &card.BatchID, &card.Position, &card.BreedID, &card.BreedName,
		&card.ProfessionID, &card.ProfessionName, &card.TreasureID, &card.TreasureName,
		&card.BreedComponentID, &card.ProfessionComponentID, &card.TreasureComponentID,
		&card.RecipeID, &card.Title, &card.WoolValue, &card.IsJackpot, &redeemedAt, &card.CreatedAt,
	); err != nil {
		return service.RewardDrawCard{}, err
	}
	if redeemedAt.Valid {
		card.RedeemedAt = &redeemedAt.Time
	}
	return card, nil
}

func rewardDrawPage(limit, offset, fallback int) (int, int) {
	if limit < 1 || limit > 500 {
		limit = fallback
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func rewardDrawAmountToUnits(amount float64) (int64, error) {
	if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, errors.New("reward draw amount is invalid")
	}
	value := decimal.NewFromFloat(amount).Mul(decimal.NewFromInt(service.RewardDrawConsumptionUnitScale)).Round(0)
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, errors.New("reward draw amount is out of range")
	}
	return value.IntPart(), nil
}

var _ service.RewardDrawRepository = (*rewardDrawRepository)(nil)
