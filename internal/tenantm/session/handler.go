package session

import (
	"time"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// CreateSession POST /api/v1/sessions/:deviceId
// 创建会话并借记配额。租户隔离：先校验设备归属当前租户。
func (h *Handler) CreateSession(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errBadRequest("deviceId 不能为空"))
		return
	}

	tid, err := extractTenant(c.Request.Context())
	if err != nil {
		web.Abort(c, err)
		return
	}

	if h.PersonaBindSvc != nil {
		bind, bindErr := h.PersonaBindSvc.GetBindByDevice(c.Request.Context(), deviceID)
		if bindErr != nil || bind == nil {
			web.Abort(c, errs.New(errs.ResourceNotFound, "设备未绑定"))
			return
		}
		if bind.TenantID != tid {
			web.Abort(c, errs.New(errs.Forbidden, "设备不在你的租户下"))
			return
		}
	}

	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}

	resp, err := h.Svc.Create(c.Request.Context(), deviceID, &req)
	if err != nil {
		web.Abort(c, err)
		return
	}

	web.OK(c, resp)
}

// GetHistory GET /api/v1/sessions/:deviceId/history
// 拉取会话历史消息。
func (h *Handler) GetHistory(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errBadRequest("deviceId 不能为空"))
		return
	}

	var req GetHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		web.Abort(c, err)
		return
	}

	resp, err := h.Svc.History(c.Request.Context(), deviceID, req.Limit, req.Cursor)
	if err != nil {
		web.Abort(c, err)
		return
	}

	web.OK(c, resp)
}

// EndSession POST /api/v1/sessions/:sessionId/end
// 结算会话实际消耗并退款。
func (h *Handler) EndSession(c *gin.Context) {
	sessionIDStr := c.Param("sessionId")
	sessionID, err := parseSessionID(sessionIDStr)
	if err != nil {
		web.Abort(c, errBadRequest("无效的 sessionId"))
		return
	}

	var req EndSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}

	resp, err := h.Svc.End(c.Request.Context(), sessionID, &req)
	if err != nil {
		web.Abort(c, err)
		return
	}

	web.OK(c, resp)
}

// ListSessionsByDevice GET /api/v1/sessions/:deviceId
// 按设备 ID 查询最近 50 条会话摘要（id/deviceId/userId/roundCount/startTime/lastActive/status）。
// 租户隔离：先校验设备归属当前租户，否则 403。
func (h *Handler) ListSessionsByDevice(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errBadRequest("deviceId 不能为空"))
		return
	}

	tid, err := extractTenant(c.Request.Context())
	if err != nil {
		web.Abort(c, err)
		return
	}

	if h.PersonaBindSvc != nil {
		bind, bindErr := h.PersonaBindSvc.GetBindByDevice(c.Request.Context(), deviceID)
		if bindErr != nil || bind == nil {
			web.Abort(c, errs.New(errs.ResourceNotFound, "设备未绑定"))
			return
		}
		if bind.TenantID != tid {
			web.Abort(c, errs.New(errs.Forbidden, "设备不在你的租户下"))
			return
		}
	}

	sessions, err := h.Svc.ListByDevice(c.Request.Context(), deviceID, 50)
	if err != nil {
		web.Abort(c, err)
		return
	}

	items := make([]SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, SessionSummary{
			ID:            s.ID,
			DeviceID:      s.DeviceID,
			PersonaID:     s.PersonaID,
			RoundCount:    s.MessageCount,
			StartTime:     s.StartTime,
			LastActive:    s.LastMessageTime,
			Status:        s.Status,
			QuotaUsed:     s.QuotaUsed,
			QuotaDimension: s.QuotaDimension,
		})
	}

	web.OK(c, items)
}

// SessionSummary 会话摘要（API 响应）。
type SessionSummary struct {
	ID            int64      `json:"id"`
	DeviceID      string     `json:"deviceId"`
	PersonaID     *int64     `json:"personaId,omitempty"`
	RoundCount    int        `json:"roundCount"`
	StartTime     *time.Time `json:"startTime,omitempty"`
	LastActive    *time.Time `json:"lastActive,omitempty"`
	Status        int8       `json:"status"`
	QuotaUsed     int64      `json:"quotaUsed"`
	QuotaDimension string    `json:"quotaDimension"`
}

// ---- 错误辅助 ----

func errBadRequest(msg string) error {
	return errs.New(errs.InvalidParam, msg)
}