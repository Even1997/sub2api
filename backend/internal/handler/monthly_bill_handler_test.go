package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMonthlyBillHandlerOwnershipAndManualGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	h := NewMonthlyBillHandler(service.NewMonthlyBillService(db))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	r.GET("/bills", h.List)
	r.GET("/bills/:month/:version", h.Get)
	r.POST("/bills", h.Generate)
	// Client-supplied owner IDs never reach SQL. Listing is read-only.
	mock.ExpectQuery(`SELECT version, generated_at FROM user_monthly_bills WHERE user_id=\$1`).WithArgs(int64(7), "2026-01").WillReturnRows(sqlmock.NewRows([]string{"version", "generated_at"}).AddRow(1, time.Now()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/bills?month=2026-01&user_id=999", nil))
	require.Equal(t, 200, w.Code)
	mock.ExpectQuery(`SELECT snapshot FROM user_monthly_bills WHERE user_id=\$1`).WithArgs(int64(7), "2026-01", 1).WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow(`{"month":"2026-01","user_id":7,"version":1}`))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/bills/2026-01/1?user_id=999", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"user_id":7`)
	for _, body := range []string{`{"month":"bad"}`, `{"month":"2999-01"}`, `{"month":"2026-01","base_version":-1}`, `{"user_id":999}`} {
		w = httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/bills", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, 400, w.Code)
	}
	unauth := gin.New()
	unauth.GET("/bills", h.List)
	unauth.GET("/bills/:month/:version", h.Get)
	unauth.POST("/bills", h.Generate)
	for _, path := range []string{"/bills", "/bills/2026-01/1"} {
		w = httptest.NewRecorder()
		unauth.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 401, w.Code)
	}
	w = httptest.NewRecorder()
	unauth.ServeHTTP(w, httptest.NewRequest("POST", "/bills", strings.NewReader(`{"month":"2026-01"}`)))
	require.Equal(t, 401, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
