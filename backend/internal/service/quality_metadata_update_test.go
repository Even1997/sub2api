package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func qualityMetadataUpdateFixture(t *testing.T, enabled bool) (*ScheduledTestService, *templatePlanRepo, *ScheduledTestPlan) {
	t.Helper()
	stored := multiModelQualityPlan(t, `["gpt-6-astra"]`)
	stored.ID, stored.AccountID, stored.Enabled = 1, 42, enabled
	stored.PelicanConfig.QuestionKind = "state_probe"
	stored.PelicanConfig.TestChannel = "account"
	stored.ModelID = "gpt-6-astra"
	repo := newTemplatePlanRepo()
	repo.plans[stored.ID] = stored
	candidate := *stored
	candidate.PelicanConfig = cloneQualityPelicanConfig(stored.PelicanConfig)
	svc := NewScheduledTestService(repo, nil)
	svc.accountTests = &AccountTestService{accountRepo: &qualitySupportAccounts{account: &Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
	}}}
	return svc, repo, &candidate
}

func TestQualityPlanUpdateMetadataDoesNotFetchCatalog(t *testing.T) {
	for _, change := range []string{"rest", "schedule", "retention", "action", "disable"} {
		t.Run(change, func(t *testing.T) {
			svc, repo, plan := qualityMetadataUpdateFixture(t, true)
			svc.qualityModels = func(context.Context, int64) ([]string, error) {
				t.Error("unchanged model selection must not fetch the upstream catalog")
				return nil, errors.New("Cloudflare origin unavailable")
			}
			switch change {
			case "rest":
				plan.PelicanConfig.Quality.RetestRestMinutes = 1440
			case "schedule":
				plan.CronExpression = "0 * * * *"
			case "retention":
				plan.MaxResults = 200
			case "action":
				plan.PelicanConfig.Quality.AutoRestore = !plan.PelicanConfig.Quality.AutoRestore
			case "disable":
				plan.Enabled = false
			}
			updated, err := svc.UpdatePlan(context.Background(), plan)
			require.NoError(t, err)
			require.Equal(t, updated, repo.plans[plan.ID])
			require.Equal(t, plan.PelicanConfig.Quality.RetestRestMinutes, updated.PelicanConfig.Quality.RetestRestMinutes)
			require.Equal(t, []string{"gpt-6-astra"}, updated.PelicanConfig.ModelIDs)
		})
	}
}

func TestQualityPlanUpdateSelectionStillRequiresCatalog(t *testing.T) {
	for _, change := range []string{"enable", "model", "models", "channel", "question"} {
		t.Run(change, func(t *testing.T) {
			svc, repo, plan := qualityMetadataUpdateFixture(t, change != "enable")
			before := repo.plans[plan.ID]
			calls := 0
			svc.qualityModels = func(context.Context, int64) ([]string, error) {
				calls++
				return nil, errors.New("catalog unavailable")
			}
			switch change {
			case "enable":
				plan.Enabled = true
			case "model":
				plan.ModelID = "gpt-6-sol"
				plan.PelicanConfig.ModelIDs = nil
			case "models":
				plan.PelicanConfig.ModelIDs = []string{"gpt-6-sol"}
			case "channel":
				plan.PelicanConfig.TestChannel = "bps"
				plan.PelicanConfig.QuestionKind = "candy"
				plan.PelicanConfig.Quality.Action = QualityActionObserveOnly
			case "question":
				plan.PelicanConfig.QuestionKind = "random_reasoning"
			}
			_, err := svc.UpdatePlan(context.Background(), plan)
			require.ErrorContains(t, err, "catalog unavailable")
			require.Equal(t, 1, calls)
			require.Same(t, before, repo.plans[plan.ID])
		})
	}
}

func TestQualityPlanMetadataUpdateStillRejectsAccountModelRestrictions(t *testing.T) {
	for _, restriction := range []string{"mapping", "observed_rejection"} {
		t.Run(restriction, func(t *testing.T) {
			svc, repo, plan := qualityMetadataUpdateFixture(t, true)
			before := repo.plans[plan.ID]
			account, err := svc.accountTests.accountRepo.GetByID(context.Background(), plan.AccountID)
			require.NoError(t, err)
			if restriction == "mapping" {
				account.Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
			} else {
				account.Extra = map[string]any{qualityUnsupportedModelKey("gpt-6-astra"): "gpt-6-astra"}
			}
			svc.qualityModels = func(context.Context, int64) ([]string, error) {
				t.Fatal("local restrictions must reject without upstream discovery")
				return nil, nil
			}
			plan.PelicanConfig.Quality.RetestRestMinutes = 1440
			_, err = svc.UpdatePlan(context.Background(), plan)
			require.Error(t, err)
			require.Same(t, before, repo.plans[plan.ID])
		})
	}
}

func TestQualityPlanMetadataUpdateRejectsInvalidRest(t *testing.T) {
	for _, minutes := range []int{-1, 10081} {
		svc, repo, plan := qualityMetadataUpdateFixture(t, true)
		before := repo.plans[plan.ID]
		svc.qualityModels = func(context.Context, int64) ([]string, error) {
			t.Fatal("invalid settings must fail before upstream discovery")
			return nil, nil
		}
		plan.PelicanConfig.Quality.RetestRestMinutes = minutes
		_, err := svc.UpdatePlan(context.Background(), plan)
		require.ErrorContains(t, err, "retest rest")
		require.Same(t, before, repo.plans[plan.ID])
	}
}
