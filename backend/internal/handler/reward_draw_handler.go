package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type RewardDrawHandler struct {
	service *service.RewardDrawService
}

func NewRewardDrawHandler(rewardDrawService *service.RewardDrawService) *RewardDrawHandler {
	return &RewardDrawHandler{service: rewardDrawService}
}

func (h *RewardDrawHandler) GetOverview(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	overview, err := h.service.GetOverview(c.Request.Context(), userID)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *RewardDrawHandler) Draw(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	var request struct {
		Count      int    `json:"count" binding:"required,min=1,max=100"`
		PoolID     string `json:"pool_id" binding:"required,oneof=breed profession treasure"`
		RequestKey string `json:"request_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, service.ErrRewardDrawInvalidRequest.Error())
		return
	}
	result, err := h.service.Draw(c.Request.Context(), userID, request.Count, request.PoolID, request.RequestKey)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *RewardDrawHandler) ListComponents(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "300"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	availableOnly := !strings.EqualFold(c.DefaultQuery("available_only", "true"), "false")
	page, err := h.service.ListComponents(c.Request.Context(), userID, availableOnly, limit, offset)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *RewardDrawHandler) Combine(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	var request struct {
		ComponentIDs []int64 `json:"component_ids" binding:"required,len=3"`
		Quantity     int     `json:"quantity" binding:"omitempty,min=1,max=500"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, service.ErrRewardDrawInvalidRequest.Error())
		return
	}
	if request.Quantity == 0 {
		request.Quantity = 1
	}
	result, err := h.service.Combine(c.Request.Context(), userID, request.ComponentIDs, request.Quantity)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *RewardDrawHandler) ListCards(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	availableOnly := !strings.EqualFold(c.DefaultQuery("available_only", "true"), "false")
	page, err := h.service.ListCards(c.Request.Context(), userID, availableOnly, limit, offset)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *RewardDrawHandler) ListHistory(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	page, err := h.service.ListHistory(c.Request.Context(), userID, limit, offset)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *RewardDrawHandler) ListRedemptions(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	page, err := h.service.ListRedemptions(c.Request.Context(), userID, limit, offset)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *RewardDrawHandler) Redeem(c *gin.Context) {
	userID, ok := rewardDrawUserID(c)
	if !ok {
		return
	}
	var request struct {
		ComponentIDs []int64 `json:"component_ids"`
		CardIDs      []int64 `json:"card_ids"`
		All          bool    `json:"all"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, service.ErrRewardDrawInvalidRequest.Error())
		return
	}
	redemption, err := h.service.Redeem(c.Request.Context(), userID, request.ComponentIDs, request.CardIDs, request.All)
	if err != nil {
		writeRewardDrawError(c, err)
		return
	}
	response.Success(c, redemption)
}

func writeRewardDrawError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRewardDrawInvalidRequest):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrRewardDrawInsufficientTickets):
		response.Error(c, http.StatusPaymentRequired, err.Error())
	case errors.Is(err, service.ErrRewardDrawRequestConflict), errors.Is(err, service.ErrRewardDrawComponentsUnavailable), errors.Is(err, service.ErrRewardDrawInvalidCombination), errors.Is(err, service.ErrRewardDrawCardsUnavailable), errors.Is(err, service.ErrRewardDrawNothingToRedeem), errors.Is(err, service.ErrRewardDrawBelowMinimum):
		response.Error(c, http.StatusConflict, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "Reward draw request failed")
	}
}

func rewardDrawUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}
