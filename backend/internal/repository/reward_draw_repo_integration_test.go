//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestRewardDrawWalletEmptyCollectionCountIsZero(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-empty-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
	})

	wallet, err := NewRewardDrawRepository(integrationEntClient, integrationDB).GetWallet(ctx, user.ID)
	require.NoError(t, err)
	require.Zero(t, wallet.DiscoveredCombinations)
	require.Zero(t, wallet.AvailableComponents)
}

func TestRewardDrawOpeningMigrationStartsAtBeijingCutoff(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-cutoff-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{Name: fmt.Sprintf("reward-cutoff-%d", time.Now().UnixNano())})
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: user.ID})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", account.ID)
		require.NoError(t, err)
	})

	cutoff := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, request_id, model,
			input_tokens, output_tokens, total_cost, actual_cost, created_at
		) VALUES
			($1, $2, $3, 'reward-cutoff-before', 'cutoff-test', 1, 1, 0.75, 0.75, $4),
			($1, $2, $3, 'reward-cutoff-at', 'cutoff-test', 1, 1, 0.5000004, 0.5000004, $5),
			($1, $2, $3, 'reward-cutoff-after', 'cutoff-test', 1, 1, 0.5000004, 0.5000004, $6)
	`, user.ID, apiKey.ID, account.ID, cutoff.Add(-time.Microsecond), cutoff, cutoff.Add(time.Microsecond))
	require.NoError(t, err)

	migrationSQL, err := dbmigrations.FS.ReadFile("196_reward_draw.sql")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var tickets, remainder, lifetimeConsumption, lifetimeTickets int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT ticket_balance, consumption_remainder_units,
			lifetime_consumption_units, lifetime_tickets
		FROM reward_draw_wallets WHERE user_id = $1
	`, user.ID).Scan(&tickets, &remainder, &lifetimeConsumption, &lifetimeTickets))
	require.EqualValues(t, 1, tickets)
	require.Zero(t, remainder)
	require.EqualValues(t, 1_000_000, lifetimeConsumption)
	require.EqualValues(t, 1, lifetimeTickets)

	var ledgerRows int
	var ledgerUnits int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(consumption_units), 0)
		FROM reward_draw_ledger
		WHERE user_id = $1 AND source_type = 'migration'
		  AND source_key = 'reward-draw-cutoff-20260810-v1'
	`, user.ID).Scan(&ledgerRows, &ledgerUnits))
	require.Equal(t, 1, ledgerRows)
	require.EqualValues(t, 1_000_000, ledgerUnits)
}

func TestRewardDrawAccrualCarriesFractionAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-accrual-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
	})

	for _, input := range []struct {
		key  string
		cost float64
	}{{"first", 0.4}, {"first", 0.4}, {"second", 0.7}} {
		tx, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		require.NoError(t, creditRewardDrawTickets(ctx, tx, user.ID, "usage", input.key, input.cost))
		require.NoError(t, tx.Commit())
	}

	var tickets, remainder, lifetime int64
	var ledgerCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT ticket_balance, consumption_remainder_units, lifetime_consumption_units
		FROM reward_draw_wallets WHERE user_id = $1
	`, user.ID).Scan(&tickets, &remainder, &lifetime))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reward_draw_ledger WHERE user_id = $1 AND source_type = 'usage'
	`, user.ID).Scan(&ledgerCount))
	require.EqualValues(t, 1, tickets)
	require.EqualValues(t, 100_000, remainder)
	require.EqualValues(t, 1_100_000, lifetime)
	require.Equal(t, 2, ledgerCount)
}

func TestRewardDrawPostDeployOnlyMigrationRemovesOpeningConsumption(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-post-deploy-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
	})

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (
			user_id, ticket_balance, consumption_remainder_units,
			lifetime_consumption_units, lifetime_tickets, lifetime_draws
		) VALUES ($1, 2, 100000, 3100000, 3, 1)
	`, user.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO reward_draw_ledger (
			user_id, source_type, source_key, ticket_delta, ticket_after, consumption_units
		) VALUES
			($1, 'migration', 'compute-cottage-opening-v1', 2, 2, 2400000),
			($1, 'usage', 'post-deploy-usage', 1, 3, 700000),
			($1, 'draw', 'post-deploy-draw', -1, 2, 0)
	`, user.ID)
	require.NoError(t, err)

	migrationSQL, err := dbmigrations.FS.ReadFile("200_reward_draw_post_deploy_only.sql")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var tickets, remainder, lifetimeConsumption, lifetimeTickets int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT ticket_balance, consumption_remainder_units,
			lifetime_consumption_units, lifetime_tickets
		FROM reward_draw_wallets WHERE user_id = $1
	`, user.ID).Scan(&tickets, &remainder, &lifetimeConsumption, &lifetimeTickets))
	require.Zero(t, tickets)
	require.EqualValues(t, 700000, remainder)
	require.EqualValues(t, 700000, lifetimeConsumption)
	require.Zero(t, lifetimeTickets)

	var openingRows, resetRows int
	var resetDelta, resetAfter int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reward_draw_ledger
		WHERE user_id = $1 AND source_type = 'migration' AND source_key = 'compute-cottage-opening-v1'
	`, user.ID).Scan(&openingRows))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(ticket_delta), 0), COALESCE(MIN(ticket_after), 0)
		FROM reward_draw_ledger
		WHERE user_id = $1 AND source_type = 'migration' AND source_key = 'reward-draw-post-deploy-only-v1'
	`, user.ID).Scan(&resetRows, &resetDelta, &resetAfter))
	require.Zero(t, openingRows)
	require.Equal(t, 1, resetRows)
	require.EqualValues(t, -2, resetDelta)
	require.Zero(t, resetAfter)
}

func TestRewardDrawPoolDrawCombineAndRedemptionRecovery(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-combine-%d@example.com", time.Now().UnixNano()), Balance: 7.25, Concurrency: 1,
	})
	notes := fmt.Sprintf("reward_draw:user=%d", user.ID)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM redeem_codes WHERE notes = $1", notes)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO reward_draw_wallets (user_id, ticket_balance, lifetime_tickets)
		VALUES ($1, 3, 3)
	`, user.ID)
	require.NoError(t, err)

	repo := NewRewardDrawRepository(integrationEntClient, integrationDB)
	commands := []*service.RewardDrawCommand{
		rewardDrawComponentCommand(user.ID, "draw-breed-001", "breed", "绒云羊", 400, false),
		rewardDrawComponentCommand(user.ID, "draw-profession-001", "profession", "牧场学徒", 300, false),
		rewardDrawComponentCommand(user.ID, "draw-treasure-001", "treasure", "棉花羊铃", 300, false),
	}
	for _, command := range commands {
		batch, recovered, err := repo.Draw(ctx, command)
		require.NoError(t, err)
		require.False(t, recovered)
		require.Equal(t, command.PoolID, batch.PoolID)
		require.Len(t, batch.Components, 1)
	}
	batch, recovered, err := repo.Draw(ctx, commands[0])
	require.NoError(t, err)
	require.True(t, recovered)
	require.Equal(t, "breed", batch.PoolID)
	conflict := *commands[0]
	conflict.PoolID = "profession"
	conflict.Components = []service.RewardDrawComponent{{Position: 0, PoolID: "profession", TierID: "code", Name: "牧场学徒", Rarity: "N", WoolValue: 300}}
	_, _, err = repo.Draw(ctx, &conflict)
	require.ErrorIs(t, err, service.ErrRewardDrawRequestConflict)

	componentPage, err := repo.ListComponents(ctx, user.ID, true, 100, 0)
	require.NoError(t, err)
	require.Len(t, componentPage.Items, 3)
	componentIDs := make([]int64, 0, 3)
	for _, component := range componentPage.Items {
		componentIDs = append(componentIDs, component.ID)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	cards := make(chan *service.RewardDrawCombineResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := repo.Combine(ctx, user.ID, componentIDs, 1)
			cards <- result
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(cards)
	successes := 0
	conflicts := 0
	var composed *service.RewardDrawCard
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, service.ErrRewardDrawComponentsUnavailable):
			conflicts++
		default:
			t.Fatalf("unexpected combine error: %v", err)
		}
	}
	for result := range cards {
		if result != nil {
			require.Len(t, result.Cards, 1)
			composed = &result.Cards[0]
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.NotNil(t, composed)
	require.EqualValues(t, 1500, composed.WoolValue)

	redemption, err := repo.Redeem(ctx, user.ID, nil, []int64{composed.ID}, false)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, redemption.Status)
	require.Zero(t, redemption.ComponentCount)
	require.Equal(t, 1, redemption.CardCount)
	require.InDelta(t, 1.5, redemption.Reward, 1e-9)
	require.Len(t, redemption.RedeemCode, 32)

	redemptionPage, err := repo.ListRedemptions(ctx, user.ID, 30, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, redemptionPage.Total)
	require.Len(t, redemptionPage.Items, 1)
	require.Equal(t, redemption.RedeemCode, redemptionPage.Items[0].RedeemCode)
	require.Equal(t, service.StatusUnused, redemptionPage.Items[0].Status)

	standardRedeemService := service.NewRedeemService(
		NewRedeemCodeRepository(integrationEntClient),
		NewUserRepository(integrationEntClient, integrationDB),
		nil, nil, nil, integrationEntClient, nil, nil,
	)
	usedCode, err := standardRedeemService.Redeem(ctx, user.ID, redemption.RedeemCode)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, usedCode.Status)
	updatedUser, err := NewUserRepository(integrationEntClient, integrationDB).GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.InDelta(t, 8.75, updatedUser.Balance, 1e-9)
	redemptionPage, err = repo.ListRedemptions(ctx, user.ID, 30, 0)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, redemptionPage.Items[0].Status)

	var availableComponents, finishedCards, redemptions int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_components WHERE user_id = $1 AND combined_at IS NULL AND redeemed_at IS NULL`, user.ID).Scan(&availableComponents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_cards WHERE user_id = $1`, user.ID).Scan(&finishedCards))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_redemptions WHERE user_id = $1`, user.ID).Scan(&redemptions))
	require.Zero(t, availableComponents)
	require.Equal(t, 1, finishedCards)
	require.Equal(t, 1, redemptions)
}

func TestRewardDrawMiracleCombinationIsFixedAtOneHundredDollars(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-miracle-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO reward_draw_wallets (user_id, ticket_balance, lifetime_tickets) VALUES ($1, 3, 3)`, user.ID)
	require.NoError(t, err)

	repo := NewRewardDrawRepository(integrationEntClient, integrationDB)
	commands := []*service.RewardDrawCommand{
		rewardDrawComponentCommand(user.ID, "miracle-breed-001", "breed", "奇迹星羊", service.RewardDrawJackpotWool, true),
		rewardDrawComponentCommand(user.ID, "miracle-profession-001", "profession", "牧场学徒", 14, false),
		rewardDrawComponentCommand(user.ID, "miracle-treasure-001", "treasure", "棉花羊铃", 14, false),
	}
	for _, command := range commands {
		_, _, err := repo.Draw(ctx, command)
		require.NoError(t, err)
	}
	page, err := repo.ListComponents(ctx, user.ID, true, 100, 0)
	require.NoError(t, err)
	ids := make([]int64, 0, 3)
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	result, err := repo.Combine(ctx, user.ID, ids, 1)
	require.NoError(t, err)
	require.Len(t, result.Cards, 1)
	card := result.Cards[0]
	require.True(t, card.IsJackpot)
	require.Equal(t, "miracle", card.RecipeID)
	require.EqualValues(t, service.RewardDrawJackpotWool, card.WoolValue)
}

func TestRewardDrawBatchCombineUsesSmallestSelectedStack(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-batch-combine-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO reward_draw_wallets (user_id, ticket_balance, lifetime_tickets) VALUES ($1, 9, 9)`, user.ID)
	require.NoError(t, err)

	repo := NewRewardDrawRepository(integrationEntClient, integrationDB)
	poolCounts := map[string]int{"breed": 3, "profession": 2, "treasure": 4}
	poolNames := map[string]string{"breed": "绒云羊", "profession": "牧场学徒", "treasure": "棉花羊铃"}
	for _, poolID := range []string{"breed", "profession", "treasure"} {
		for index := 0; index < poolCounts[poolID]; index++ {
			command := rewardDrawComponentCommand(user.ID, fmt.Sprintf("batch-%s-%d", poolID, index), poolID, poolNames[poolID], 7, false)
			_, _, err := repo.Draw(ctx, command)
			require.NoError(t, err)
		}
	}

	page, err := repo.ListComponents(ctx, user.ID, true, 1, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.EqualValues(t, 4, page.Items[0].StackCount)

	page, err = repo.ListComponents(ctx, user.ID, true, 100, 0)
	require.NoError(t, err)
	selectedIDs := make([]int64, 0, 3)
	seenPools := make(map[string]bool, 3)
	for _, component := range page.Items {
		require.EqualValues(t, poolCounts[component.PoolID], component.StackCount)
		if !seenPools[component.PoolID] {
			selectedIDs = append(selectedIDs, component.ID)
			seenPools[component.PoolID] = true
		}
	}
	require.Len(t, selectedIDs, 3)

	_, err = repo.Combine(ctx, user.ID, selectedIDs, 3)
	require.ErrorIs(t, err, service.ErrRewardDrawComponentsUnavailable)
	var availableComponents, finishedCards int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_components WHERE user_id = $1 AND combined_at IS NULL AND redeemed_at IS NULL`, user.ID).Scan(&availableComponents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_cards WHERE user_id = $1`, user.ID).Scan(&finishedCards))
	require.Equal(t, 9, availableComponents)
	require.Zero(t, finishedCards)

	start := make(chan struct{})
	results := make(chan *service.RewardDrawCombineResult, 2)
	combineErrors := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := repo.Combine(ctx, user.ID, selectedIDs, 2)
			results <- result
			combineErrors <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(combineErrors)
	var result *service.RewardDrawCombineResult
	successes, conflicts := 0, 0
	for err := range combineErrors {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, service.ErrRewardDrawComponentsUnavailable):
			conflicts++
		default:
			t.Fatalf("unexpected batch combine error: %v", err)
		}
	}
	for candidate := range results {
		if candidate != nil {
			result = candidate
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.NotNil(t, result)
	require.Equal(t, 2, result.Quantity)
	require.Equal(t, "code", result.RecipeID)
	require.EqualValues(t, 64, result.WoolTotal)
	require.Len(t, result.Cards, 2)
	for _, card := range result.Cards {
		require.Equal(t, "code", card.RecipeID)
		require.EqualValues(t, 32, card.WoolValue)
	}
	for poolID, want := range map[string]int{"breed": 1, "profession": 0, "treasure": 2} {
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_components WHERE user_id = $1 AND pool_id = $2 AND combined_at IS NULL AND redeemed_at IS NULL`, user.ID, poolID).Scan(&count))
		require.Equal(t, want, count, poolID)
	}
}

func TestRewardDrawSingleComponentAndMaximumRedemption(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-minimum-%d@example.com", time.Now().UnixNano()), Balance: 3.5, Concurrency: 1,
	})
	notes := fmt.Sprintf("reward_draw:user=%d", user.ID)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM redeem_codes WHERE notes = $1", notes)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO reward_draw_wallets (user_id, ticket_balance, lifetime_tickets) VALUES ($1, 3, 3)`, user.ID)
	require.NoError(t, err)

	repo := NewRewardDrawRepository(integrationEntClient, integrationDB)
	for _, command := range []*service.RewardDrawCommand{
		rewardDrawComponentCommand(user.ID, "minimum-breed-001", "breed", "绒云羊", 14, false),
		rewardDrawComponentCommand(user.ID, "minimum-profession-001", "profession", "牧场学徒", 14, false),
		rewardDrawComponentCommand(user.ID, "minimum-treasure-001", "treasure", "棉花羊铃", 14, false),
	} {
		_, _, err := repo.Draw(ctx, command)
		require.NoError(t, err)
	}
	page, err := repo.ListComponents(ctx, user.ID, true, 100, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	single := page.Items[0]
	redemption, err := repo.Redeem(ctx, user.ID, []int64{single.ID}, nil, false)
	require.NoError(t, err)
	require.Equal(t, 1, redemption.ComponentCount)
	require.Zero(t, redemption.CardCount)
	require.Equal(t, single.WoolValue, redemption.Wool)
	require.InDelta(t, float64(single.WoolValue)/1000, redemption.Reward, 1e-9)

	maximum, err := repo.Redeem(ctx, user.ID, nil, nil, true)
	require.NoError(t, err)
	require.Equal(t, 2, maximum.ComponentCount)
	require.Zero(t, maximum.CardCount)
	require.InDelta(t, 0.028, maximum.Reward, 1e-9)

	var redeemedComponents, redemptions, redeemCodes int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_components WHERE user_id = $1 AND redeemed_at IS NOT NULL`, user.ID).Scan(&redeemedComponents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_draw_redemptions WHERE user_id = $1`, user.ID).Scan(&redemptions))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM redeem_codes WHERE notes = $1`, notes).Scan(&redeemCodes))
	require.Equal(t, 3, redeemedComponents)
	require.Equal(t, 2, redemptions)
	require.Equal(t, 2, redeemCodes)
}

func TestRewardDrawMaximumRedemptionIncludesComponentsAndCombinedCards(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("reward-maximum-%d@example.com", time.Now().UnixNano()), Concurrency: 1,
	})
	notes := fmt.Sprintf("reward_draw:user=%d", user.ID)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM redeem_codes WHERE notes = $1", notes)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO reward_draw_wallets (user_id, ticket_balance, lifetime_tickets) VALUES ($1, 1, 1)`, user.ID)
	require.NoError(t, err)

	repo := NewRewardDrawRepository(integrationEntClient, integrationDB)
	_, _, err = repo.Draw(ctx, rewardDrawComponentCommand(user.ID, "maximum-component-001", "breed", "绒云羊", 7, false))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO reward_draw_cards (
			batch_id, user_id, position, breed_id, breed_name, profession_id,
			profession_name, treasure_id, treasure_name, recipe_id, title, wool_value
		) VALUES
			(NULL, $1, 0, 'code', '绒云羊', 'code', '牧场学徒', 'code', '棉花羊铃', 'code', '绒云咩', 32),
			(NULL, $1, 0, 'reasoning', '墨角羊', 'reasoning', '羊羊侦探', 'reasoning', '线索放大镜', 'reasoning', '侦探咩', 41)
	`, user.ID)
	require.NoError(t, err)

	redemption, err := repo.Redeem(ctx, user.ID, nil, nil, true)
	require.NoError(t, err)
	require.Equal(t, 1, redemption.ComponentCount)
	require.Equal(t, 2, redemption.CardCount)
	require.EqualValues(t, 80, redemption.Wool)
	require.InDelta(t, 0.08, redemption.Reward, 1e-9)

	wallet, err := repo.GetWallet(ctx, user.ID)
	require.NoError(t, err)
	require.Zero(t, wallet.AvailableComponents)
	require.Zero(t, wallet.AvailableCards)
	require.Zero(t, wallet.AvailableWool)
}

func rewardDrawComponentCommand(userID int64, requestKey, poolID, name string, wool int64, jackpot bool) *service.RewardDrawCommand {
	tierID := "code"
	rarity := "N"
	if jackpot {
		tierID = "miracle"
		rarity = "MIRACLE"
	}
	return &service.RewardDrawCommand{
		UserID: userID, RequestKey: requestKey, PoolVersion: service.RewardDrawPoolVersion, PoolID: poolID,
		Components: []service.RewardDrawComponent{{
			Position: 0, PoolID: poolID, TierID: tierID, Name: name, Rarity: rarity, WoolValue: wool, IsJackpot: jackpot,
		}},
	}
}
