// Package apiv1 SaaS 自有接口（/api/v1/*）— memory 模块 handler。
package apiv1

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm"
	"ykt.dev/aisaas/internal/tenantm/memory"
	"ykt.dev/aisaas/internal/tenantm/persona"
)

// MemoryHandler /api/v1/memories 接口 handler。
type MemoryHandler struct {
	Svc              *memory.Service
	Quota            *quota.Guard
	Meter            *metering.Recorder
	Worker           *memory.Worker
	DeviceTenantSvc  *tenantm.DeviceTenantService
	PersonaBindSvc   *persona.Service
}

// writeMemoryAudit 记录 memory 模块审计日志。
func (h *MemoryHandler) writeMemoryAudit(c *gin.Context, action, result string, detail map[string]any) {
	tid, _ := tenant.FromSafe(c.Request.Context())
	actor, _ := auth.ActorFrom(c.Request.Context())
	audit.Record(c.Request.Context(), audit.AuditEntry{
		TenantID:     tid,
		ActorType:    actor.Type,
		ActorID:      strconv.FormatInt(actor.ID, 10),
		ActorIP:      c.ClientIP(),
		Action:       action,
		ResourceType: "memory",
		Result:       result,
		ActionDetail: detail,
	})
}

// verifyDeviceOwnership 验证设备是否属于当前租户（C-6: IDOR 防护）。
// 返回 false 表示验证失败（设备不属于该租户）。
func (h *MemoryHandler) verifyDeviceOwnership(c *gin.Context, deviceID string) bool {
	tid, ok := tenant.FromSafe(c.Request.Context())
	if !ok {
		return false
	}
	// 优先用 device_tenant 表校验（设备独立开户）
	if h.DeviceTenantSvc != nil {
		deviceTenant, err := h.DeviceTenantSvc.GetByDevice(c.Request.Context(), deviceID)
		if err == nil {
			return deviceTenant.ID == tid
		}
	}
	// Fallback：用 persona_bind 表校验（设备仅绑 persona，未独立开户）
	if h.PersonaBindSvc != nil {
		bind, err := h.PersonaBindSvc.GetBindByDevice(c.Request.Context(), deviceID)
		if err == nil && bind != nil {
			return bind.TenantID == tid
		}
	}
	return false
}

// AppendMessage POST /api/v1/memories/:deviceId/messages
// 写入短期会话消息。
func (h *MemoryHandler) AppendMessage(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	var req memory.WriteMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}
	if req.Role != "user" && req.Role != "assistant" && req.Role != "system" {
		web.Abort(c, errs.New(errs.InvalidParam, "role 必须为 user/assistant/system"))
		return
	}

	// 配额预扣（估算 token 数）
	estimated := int64(len(req.Content) / 3)
	if estimated < 1 {
		estimated = 1
	}
	if estimated > 2048 {
		estimated = 2048
	}
	if !h.Quota.PrecheckDim(c.Request.Context(), "llm_tokens_in", estimated) {
		h.writeMemoryAudit(c, "memory.append", "failure", map[string]any{"reason": "quota_exceeded"})
		web.Abort(c, errs.New(errs.QuotaExceeded))
		return
	}

	msg, shouldExtract, err := h.Svc.AppendMessage(c.Request.Context(), &req)
	if err != nil {
		h.writeMemoryAudit(c, "memory.append", "failure", map[string]any{"error": err.Error()})
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	// 审计日志
	h.writeMemoryAudit(c, "memory.append", "success", map[string]any{
		"msgId":    msg.ID,
		"role":     msg.Role,
		"deviceId": deviceID,
	})

	// 自动触发抽取（ADR-3 决策：aisaas 内部自动触发）
	if shouldExtract {
		tid, _ := tenant.FromSafe(c.Request.Context())
		// C-5: 获取最近 10 条累积消息
		recentMsgs, err := h.Svc.GetRecentMessages(c.Request.Context(), deviceID, 10)
		if err != nil {
			slog.Warn("get recent messages failed for extract", "deviceId", deviceID, "err", err)
			recentMsgs = []memory.MessageItem{{Role: req.Role, Content: req.Content}}
		}
		task := &memory.ExtractTask{
			TaskID:       uuid.NewString(),
			TenantID:     tid,
			DeviceID:     deviceID,
			Messages:     recentMsgs,
			ExtractTypes: []string{"entity", "preference", "event"},
			CreatedAt:    time.Now(),
		}
		if err := h.Worker.PublishExtractTask(c.Request.Context(), task); err != nil {
			// 发布失败不阻塞，仅记录日志
			_ = err
		}
	}

	web.OK(c, &memory.WriteMessageResp{
		ID:        msg.ID,
		SessionID: fmt.Sprintf("sess_%d", msg.SessionID),
		CreatedAt: msg.CreateTime,
	})
}

// ListMessages GET /api/v1/memories/:deviceId/messages
// 列出短期会话消息（游标分页，按时间倒序）。
func (h *MemoryHandler) ListMessages(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	// C-6: 验证设备归属租户
	if !h.verifyDeviceOwnership(c, deviceID) {
		web.Abort(c, errs.New(errs.Forbidden, "设备不属于当前租户"))
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v >= 1 && v <= 200 {
			limit = v
		}
	}

	var cursor int64
	if cs := c.Query("cursor"); cs != "" {
		decoded, err := base64.StdEncoding.DecodeString(cs)
		if err == nil {
			if v, err := strconv.ParseInt(string(decoded), 10, 64); err == nil {
				cursor = v
			}
		}
	}

	dos, hasMore, err := h.Svc.ListMessages(c.Request.Context(), deviceID, limit, cursor)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	h.writeMemoryAudit(c, "memory.read", "success", map[string]any{
		"deviceId": deviceID,
		"limit":    limit,
		"count":    len(dos),
	})

	items := memory.BuildMessagesForList(dos)
	resp := &memory.ListMessagesResp{
		Items:   items,
		HasMore: hasMore,
	}
	if hasMore && len(items) > 0 {
		lastID := items[len(items)-1].ID
		resp.NextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(lastID, 10)))
	}

	web.OK(c, resp)
}

// GetGraph GET /api/v1/memories/:deviceId
// 拉取长期记忆图谱。
func (h *MemoryHandler) GetGraph(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	// C-6: 验证设备归属租户
	if !h.verifyDeviceOwnership(c, deviceID) {
		web.Abort(c, errs.New(errs.Forbidden, "设备不属于当前租户"))
		return
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v >= 1 && v <= 100 {
			limit = v
		}
	}
	dimension := c.Query("dimension")

	resp, err := h.Svc.GetGraph(c.Request.Context(), deviceID, limit, dimension)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	h.writeMemoryAudit(c, "memory.read", "success", map[string]any{
		"deviceId":  deviceID,
		"dimension": dimension,
		"entities":  len(resp.Entities),
	})

	web.OK(c, resp)
}

// ExtractAsync POST /api/v1/memories/:deviceId/extract
// 异步抽取实体。
func (h *MemoryHandler) ExtractAsync(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	var req memory.ExtractReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	// 预估配额消耗（按消息数估算 token）
	estimated := int64(len(req.Messages) * 100)
	if estimated > 4096 {
		estimated = 4096
	}
	if !h.Quota.PrecheckDim(c.Request.Context(), "llm_tokens_in", estimated) {
		h.writeMemoryAudit(c, "memory.extract", "failure", map[string]any{"reason": "quota_exceeded"})
		web.Abort(c, errs.New(errs.QuotaExceeded))
		return
	}

	tid, _ := tenant.FromSafe(c.Request.Context())
	task := &memory.ExtractTask{
		TaskID:       uuid.NewString(),
		TenantID:     tid,
		DeviceID:     deviceID,
		SessionID:    req.SessionID,
		Messages:     req.Messages,
		ExtractTypes: req.ExtractTypes,
		CreatedAt:    time.Now(),
	}

	if err := h.Worker.PublishExtractTask(c.Request.Context(), task); err != nil {
		h.writeMemoryAudit(c, "memory.extract", "failure", map[string]any{"error": err.Error()})
		web.Abort(c, errs.Wrap(errs.Internal, fmt.Errorf("publish extract task: %w", err)))
		return
	}

	h.writeMemoryAudit(c, "memory.extract", "success", map[string]any{
		"taskId":    task.TaskID,
		"deviceId":  deviceID,
		"msgCount":  len(req.Messages),
		"types":     req.ExtractTypes,
	})

	web.OK(c, &memory.ExtractResp{
		TaskID:            task.TaskID,
		Status:            string(memory.TaskPending),
		EstimatedEntities: len(req.Messages),
		CreatedAt:         task.CreatedAt,
	})
}

// SummarizeAsync POST /api/v1/memories/:deviceId/summarize
// 异步摘要聚合。
func (h *MemoryHandler) SummarizeAsync(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	var req memory.SummarizeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	// 默认摘要类型
	if len(req.SummarizeTypes) == 0 {
		req.SummarizeTypes = []string{"conversation", "topic"}
	}

	// 配额预扣
	estimated := int64(2000)
	if !h.Quota.PrecheckDim(c.Request.Context(), "llm_tokens_in", estimated) {
		h.writeMemoryAudit(c, "memory.summarize", "failure", map[string]any{"reason": "quota_exceeded"})
		web.Abort(c, errs.New(errs.QuotaExceeded))
		return
	}

	tid, _ := tenant.FromSafe(c.Request.Context())
	task := &memory.SummarizeTask{
		TaskID:         uuid.NewString(),
		TenantID:       tid,
		DeviceID:       deviceID,
		SessionID:      req.SessionID,
		SummarizeTypes: req.SummarizeTypes,
		TopicFocus:     req.TopicFocus,
		CreatedAt:      time.Now(),
	}

	if err := h.Worker.PublishSummarizeTask(c.Request.Context(), task); err != nil {
		h.writeMemoryAudit(c, "memory.summarize", "failure", map[string]any{"error": err.Error()})
		web.Abort(c, errs.Wrap(errs.Internal, fmt.Errorf("publish summarize task: %w", err)))
		return
	}

	h.writeMemoryAudit(c, "memory.summarize", "success", map[string]any{
		"taskId":   task.TaskID,
		"deviceId": deviceID,
		"types":    req.SummarizeTypes,
	})

	web.OK(c, &memory.SummarizeResp{
		TaskID:    task.TaskID,
		Status:    string(memory.TaskPending),
		CreatedAt: task.CreatedAt,
	})
}