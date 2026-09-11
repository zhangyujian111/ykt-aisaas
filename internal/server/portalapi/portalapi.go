// Package portalapi 用户门户接口（/portal/api/v1/*，JWT 鉴权）。
package portalapi

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/portal"
)

// Handler 门户。
type Handler struct{ Svc *portal.Service }

// Register POST /portal/api/v1/auth/register。
func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	u, err := h.Svc.Register(c.Request.Context(), req.Username, req.Password, req.Nickname)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"id": u.ID, "username": u.Username, "nickname": u.Nickname})
}

// Login POST /portal/api/v1/auth/login。
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	resp, err := h.Svc.Login(c, req.Username, req.Password)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{
		"accessToken":  resp.AccessToken,
		"refreshToken": resp.RefreshToken,
		"tokenType":    resp.TokenType,
		"expiresIn":    resp.ExpiresIn,
		"user":         gin.H{"id": resp.User.ID, "username": resp.User.Username, "nickname": resp.User.Nickname},
	})
}

// JWTMiddleware Bearer JWT → userId 注入。
func (h *Handler) JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if raw == "" {
			web.Abort(c, errs.New(errs.TokenInvalid, "缺少 token"))
			return
		}
		uid, err := h.Svc.ParseToken(raw)
		if err != nil {
			web.Abort(c, err)
			return
		}
		c.Set("uid", uid)
		c.Next()
	}
}

func uid(c *gin.Context) int64 {
	v, _ := c.Get("uid")
	n, _ := v.(int64)
	return n
}

// MyDevices GET /portal/api/v1/devices。
func (h *Handler) MyDevices(c *gin.Context) {
	devs, err := h.Svc.MyDevices(c.Request.Context(), uid(c))
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, devs)
}

// BindDevice POST /portal/api/v1/devices/bind。
func (h *Handler) BindDevice(c *gin.Context) {
	var req struct {
		BindCode string `json:"bindCode" binding:"required"`
		BindName string `json:"bindName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	b, err := h.Svc.BindDevice(c.Request.Context(), uid(c), req.BindCode, req.BindName)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, b)
}

// Plans GET /portal/api/v1/plans。
func (h *Handler) Plans(c *gin.Context) {
	plans, err := h.Svc.Plans(c.Request.Context())
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, plans)
}

// Subscribe POST /portal/api/v1/devices/:tenantId/subscribe。
func (h *Handler) Subscribe(c *gin.Context) {
	tid, err := strconv.ParseInt(c.Param("tenantId"), 10, 64)
	if err != nil || tid <= 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid tenantId"))
		return
	}
	var req struct {
		PlanID int64 `json:"planId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	if err := h.Svc.Subscribe(c.Request.Context(), uid(c), tid, req.PlanID); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"tenantId": tid, "planId": req.PlanID, "subscribed": true})
}

// CreateOrder POST /portal/api/v1/devices/:tenantId/recharge。
func (h *Handler) CreateOrder(c *gin.Context) {
	tid, err := strconv.ParseInt(c.Param("tenantId"), 10, 64)
	if err != nil || tid <= 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid tenantId"))
		return
	}
	var req struct {
		AmountCents int64 `json:"amountCents" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	o, err := h.Svc.CreateOrder(c.Request.Context(), uid(c), tid, req.AmountCents)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, o)
}

// PayOrderMock POST /portal/api/v1/orders/:id/pay-mock（模拟支付确认）。
func (h *Handler) PayOrderMock(c *gin.Context) {
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid id"))
		return
	}
	o, err := h.Svc.PayOrderMock(c.Request.Context(), uid(c), oid)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, o)
}

// MyOrders GET /portal/api/v1/orders。
func (h *Handler) MyOrders(c *gin.Context) {
	orders, err := h.Svc.MyOrders(c.Request.Context(), uid(c))
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, orders)
}

// Me GET /portal/api/v1/me。
func (h *Handler) Me(c *gin.Context) {
	u, err := h.Svc.GetUser(c.Request.Context(), uid(c))
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{
		"id":       u.ID,
		"username": u.Username,
		"nickname": u.Nickname,
		"phone":    u.Phone,
	})
}

// ChangePassword POST /portal/api/v1/auth/change-password。
func (h *Handler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"oldPassword" binding:"required"`
		NewPassword string `json:"newPassword" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	if err := h.Svc.ChangePassword(c.Request.Context(), uid(c), req.OldPassword, req.NewPassword); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"changed": true})
}
