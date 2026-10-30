//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMergeV2181ImageCountBillingAuthCacheRoundTrip(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		original := &Group{ID: 42, ImageCountBilling: enabled}
		restored := apiKeyAuthGroupSnapshotToGroup(apiKeyAuthGroupSnapshotFromGroup(original))
		require.Equal(t, enabled, restored.ImageCountBilling)
	}
}

func TestMergeV2181RepeatedClockObservationsStayOrdered(t *testing.T) {
	now := time.Now()
	previous := nextCodexUsageObservation(now)
	for range 100 {
		next := nextCodexUsageObservation(now)
		require.True(t, next.After(previous))
		previous = next
	}
}

func TestMergeV2181TeamsSnapshotOwnsScoringMetadata(t *testing.T) {
	cfg := DefaultPrioritySchedulingConfig()
	cfg.Teams.Enabled, cfg.Teams.WindowSource = true, "explicit"
	cfg.GroupIDs, cfg.Models = []int64{11}, []string{"gpt-test"}
	now := time.Now().Truncate(time.Second)
	item := priorityCandidate(1, .1, 0)
	item.account.Type = AccountTypeOAuth
	item.account.Credentials = map[string]any{"plan_type": "team", "access_token": "must-not-retain"}
	item.account.Extra["priority_teams_window_start"] = now.Format(time.RFC3339)
	input := newPrioritySnapshotInput(OpenAIAccountScheduleRequest{}, cfg, []openAIAccountCandidateScore{item}, now)
	cfg.GroupIDs[0], cfg.Models[0] = 12, "changed"
	item.account.Credentials["plan_type"] = "free"
	item.account.Extra["priority_teams_window_start"] = "changed"
	require.Equal(t, []int64{11}, input.config.GroupIDs)
	require.Equal(t, []string{"gpt-test"}, input.config.Models)
	require.Equal(t, map[string]any{"plan_type": "team"}, input.candidates[0].account.Credentials)
	windows := priorityTeamsWindows(input.config.Teams, input.candidates)
	require.Len(t, windows, 1)
	require.True(t, now.Equal(*windows[0].Start))
}

func TestMergeV2181PrearmSnapshotCannotOverwriteNewerUsage(t *testing.T) {
	account := issue578Account()
	repo := &issue578Repo{accounts: map[int64]*Account{account.ID: account}}
	throttle := newAccountWriteThrottle(40 * time.Millisecond)
	t.Cleanup(throttle.close)
	svc := &OpenAIGatewayService{accountRepo: repo, codexSnapshotThrottle: throttle}
	now := time.Now().UTC()
	update := func(percent float64, at time.Time) map[string]any {
		return map[string]any{"codex_5h_used_percent": percent, "codex_usage_updated_at": at.Format(time.RFC3339Nano)}
	}
	svc.processCodexQuotaOverdraftUsageSnapshot(context.Background(), account.ID, now, update(89, now))
	issue578WaitUsage(t, repo, account.ID, 89)
	svc.processCodexQuotaOverdraftUsageSnapshot(context.Background(), account.ID, now, update(100, now.Add(time.Second)))
	issue578WaitUsage(t, repo, account.ID, 100)
	svc.processCodexQuotaOverdraftUsageSnapshot(context.Background(), account.ID, now, update(95, now.Add(-time.Second)))
	require.Eventually(t, func() bool {
		throttle.mu.Lock()
		defer throttle.mu.Unlock()
		return len(throttle.pending) == 0
	}, time.Second, time.Millisecond)
	current, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 100.0, current.Extra["codex_5h_used_percent"])
}
