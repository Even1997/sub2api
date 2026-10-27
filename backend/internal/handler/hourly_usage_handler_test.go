package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type hourlyUsageRepoStub struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	start   time.Time
	calls   int
}

func (r *hourlyUsageRepoStub) GetHourlyUsage(_ context.Context, start, _ time.Time, _ string, f usagestats.UsageLogFilters) (*usagestats.HourlyUsageReport, error) {
	r.filters = f
	r.start = start
	r.calls++
	return &usagestats.HourlyUsageReport{}, nil
}

func TestHourlyUsageHandlerOwnershipAndBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &hourlyUsageRepoStub{}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	r.GET("/hourly", h.HourlySummary)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/hourly?start_date=2026-01-01&end_date=2026-01-01&user_id=99&timezone=America/New_York&model=gpt-5&billing_type=1", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(7), repo.filters.UserID)
	require.Equal(t, "gpt-5", repo.filters.Model)
	require.Equal(t, int8(1), *repo.filters.BillingType)
	require.Equal(t, 16, repo.start.UTC().Hour())
	for _, query := range []string{
		"start_date=2026-01-01&end_date=2026-02-10",
		"start_date=2024-01-01&end_date=2026-01-01&granularity=day",
		"start_date=2026-01-02&end_date=2026-01-01",
		"granularity=invalid", "start_date=bad", "model_provider=invalid", "billing_mode=invalid",
	} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/hourly?"+query, nil))
		require.Equal(t, 400, w.Code, query)
	}
	require.Equal(t, 1, repo.calls)
	unauth := gin.New()
	unauth.GET("/hourly", h.HourlySummary)
	w = httptest.NewRecorder()
	unauth.ServeHTTP(w, httptest.NewRequest("GET", "/hourly", nil))
	require.Equal(t, 401, w.Code)
}
