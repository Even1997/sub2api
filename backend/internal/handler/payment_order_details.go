package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *PaymentHandler) UpdateOrderRemark(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	var req struct {
		Remark *string `json:"remark" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "A remark string is required")
		return
	}
	if err := h.paymentService.UpdateOrderRemark(c.Request.Context(), id, subject.UserID, *req.Remark); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"remark": *req.Remark})
}

func (h *PaymentHandler) ExportMyOrders(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	start, startErr := time.Parse(time.RFC3339, c.Query("start_time"))
	end, endErr := time.Parse(time.RFC3339, c.Query("end_time"))
	if startErr != nil || endErr != nil {
		response.BadRequest(c, "start_time and end_time must be RFC3339 timestamps")
		return
	}
	orders, err := h.paymentService.ExportUserOrders(c.Request.Context(), subject.UserID, start, end, c.Query("status"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sanitizePaymentOrdersForResponse(orders))
}
