package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// Create 创建会话（含配额快照借记）。
//
// 流程：
//  1. 生成 session ID
//  2. 调 redisx.QuotaSnapshotDeduct 原子借记设备配额 + 写入 session 快照
//  3. INSERT session 记录
//  4. 写审计日志
//  5. 返回 session 信息
func (s *Service) Create(ctx context.Context, deviceID string, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	tid, err := extractTenant(ctx)
	if err != nil {
		return nil, err
	}
	if deviceID == "" || len(deviceID) > 128 {
		return nil, errs.New(errs.InvalidParam, "deviceId 无效")
	}
	if !validDimension(req.Dimension) {
		return nil, errs.New(errs.InvalidParam, "无效的配额维度: "+req.Dimension)
	}

	// 1. 生成 session ID
	sessionID := genSessionID()
	sessionIDStr := formatSessionID(sessionID)

	// 2. 配额快照借记（原子 Lua）
	quotaInitial := req.QuotaInitial
	if quotaInitial <= 0 {
		quotaInitial = s.cfg.DefaultQuotaInitial
	}
	remaining, _, err := s.rdb.QuotaSnapshotDeduct(ctx, tid, req.Dimension, sessionIDStr, quotaInitial)
	if err != nil {
		if errors.Is(err, redisx.ErrQuotaExceeded) {
			writeAudit(ctx, "session.create", sessionIDStr, "failure", tid,
				map[string]any{"reason": "quota_exceeded", "dimension": req.Dimension, "quotaInitial": quotaInitial})
			return nil, errs.New(errs.QuotaExceeded, "设备配额不足")
		}
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("quota snapshot deduct: %w", err))
	}

	// 3. 构建配额快照 JSON
	limit, used := s.rdb.QuotaRemaining(ctx, tid, req.Dimension)
	snapshotJSON := buildQuotaSnapshotJSON(tid, limit, used, req.Dimension, quotaInitial)

	// 4. 构建 DO 并入库
	now := time.Now()
	do := &SessionDO{
		DeviceID:        deviceID,
		SessionType:     defaultString(req.SessionType, SessionTypeChat),
		Title:           req.Title,
		PersonaID:       req.PersonaID,
		ModelID:         req.ModelID,
		QuotaSnapshot:   snapshotJSON,
		QuotaDimension:  req.Dimension,
		QuotaInitial:    quotaInitial,
		QuotaUsed:       0,
		Status:          StatusActive,
		Metadata:        defaultString(req.Metadata, "{}"),
		StartTime:       &now,
		LastMessageTime: &now,
	}
	do.ID = sessionID
	do.TenantID = tid
	if err := s.repo.Create(ctx, do); err != nil {
		// 入库失败，回滚配额
		_ = s.rdb.QuotaSnapshotRefund(ctx, tid, req.Dimension, sessionIDStr, 0)
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("create session: %w", err))
	}

	// 5. 审计日志
	writeAudit(ctx, "session.create", sessionIDStr, "success", tid,
		map[string]any{
			"deviceId":     deviceID,
			"dimension":    req.Dimension,
			"quotaInitial": quotaInitial,
			"remaining":    remaining,
		})

	return &CreateSessionResponse{
		SessionID:      sessionIDStr,
		DeviceID:       deviceID,
		Dimension:      req.Dimension,
		QuotaInitial:   quotaInitial,
		QuotaRemaining: remaining,
		QuotaSnapshot: &QuotaSnapshot{
			TenantID:             tid,
			DeviceQuotaLimit:     limit,
			DeviceQuotaUsed:      used,
			DeviceQuotaRemaining: remaining,
			SnapshotTime:         now.UTC().Format(time.RFC3339),
		},
		CreatedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

// End 结算会话实际消耗（含配额退款）。
//
// 流程：
//  1. 查询 session 记录
//  2. 计算实际用量（按主维度）
//  3. 调 redisx.QuotaSnapshotRefund 退差额
//  4. UPDATE session SET actualCost, status, endTime
//  5. 写审计日志
func (s *Service) End(ctx context.Context, sessionID int64, req *EndSessionRequest) (*EndSessionResponse, error) {
	tid, err := extractTenant(ctx)
	if err != nil {
		return nil, err
	}

	// 1. 查询 session
	do, err := s.repo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if do.Status != StatusActive && do.Status != StatusCreating {
		return nil, errs.New(errs.Conflict, "会话已结束或已结算")
	}

	// 2. 计算实际用量（按主维度）
	actualUsed := extractDimUsage(req.ActualCost, do.QuotaDimension)
	quotaInitial := do.QuotaInitial

	// 3. 退差额
	sessionIDStr := formatSessionID(sessionID)
	if err := s.rdb.QuotaSnapshotRefund(ctx, tid, do.QuotaDimension, sessionIDStr, actualUsed); err != nil {
		// 退款失败不阻塞，记录日志
		// session key 可能已过期，跳过即可
	}

	// 4. 构建 actualCost JSON
	actualCostJSON, _ := json.Marshal(req.ActualCost)

	// 5. 确定结束状态
	endStatus := int8(StatusSettled)
	if req.Status == "failed" {
		endStatus = StatusEnded
	}

	// 6. 更新 DB
	if err := s.repo.UpdateEnd(ctx, sessionID, string(actualCostJSON), endStatus, actualUsed); err != nil {
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("update session end: %w", err))
	}

	// 7. 计算退款量
	refunded := quotaInitial - actualUsed
	if actualUsed > quotaInitial {
		slog.Warn("session quota overused",
			"sessionID", sessionIDStr,
			"tenantID", tid,
			"deviceID", do.DeviceID,
			"dimension", do.QuotaDimension,
			"quotaInitial", quotaInitial,
			"actualUsed", actualUsed,
			"overuse", actualUsed-quotaInitial)
	}
	if refunded < 0 {
		refunded = 0
	}

	// 8. 审计日志
	writeAudit(ctx, "session.end", sessionIDStr, "success", tid,
		map[string]any{
			"deviceId":      do.DeviceID,
			"dimension":     do.QuotaDimension,
			"quotaInitial":  quotaInitial,
			"actualUsed":    actualUsed,
			"quotaRefunded": refunded,
			"status":        req.Status,
		})

	now := time.Now().UTC().Format(time.RFC3339)
	return &EndSessionResponse{
		SessionID: sessionIDStr,
		QuotaUsed: &QuotaUsage{
			LLMTokensIn:  pickDim(req.ActualCost, "llm_tokens_in"),
			LLMTokensOut: pickDim(req.ActualCost, "llm_tokens_out"),
			TTSChars:     pickDim(req.ActualCost, "tts_chars"),
			ASRSeconds:   pickDim(req.ActualCost, "asr_seconds"),
		},
		QuotaRefunded: buildRefundUsage(do.QuotaDimension, refunded),
		EndedAt:       now,
	}, nil
}

// ---- 内部辅助 ----

// extractDimUsage 从 QuotaUsage 中提取指定维度的用量。
func extractDimUsage(usage *QuotaUsage, dim string) int64 {
	if usage == nil {
		return 0
	}
	switch dim {
	case redisx.DimLLMTokensIn:
		return usage.LLMTokensIn
	case redisx.DimLLMTokensOut:
		return usage.LLMTokensOut
	case redisx.DimTTSChars:
		return usage.TTSChars
	case redisx.DimASRSeconds:
		return usage.ASRSeconds
	}
	return 0
}

// pickDim 安全提取维度值。
func pickDim(usage *QuotaUsage, dim string) int64 {
	if usage == nil {
		return 0
	}
	switch dim {
	case "llm_tokens_in":
		return usage.LLMTokensIn
	case "llm_tokens_out":
		return usage.LLMTokensOut
	case "tts_chars":
		return usage.TTSChars
	case "asr_seconds":
		return usage.ASRSeconds
	}
	return 0
}

// buildRefundUsage 构建退款 QuotaUsage（仅主维度有值）。
func buildRefundUsage(dim string, amount int64) *QuotaUsage {
	u := &QuotaUsage{}
	switch dim {
	case redisx.DimLLMTokensIn:
		u.LLMTokensIn = amount
	case redisx.DimLLMTokensOut:
		u.LLMTokensOut = amount
	case redisx.DimTTSChars:
		u.TTSChars = amount
	case redisx.DimASRSeconds:
		u.ASRSeconds = amount
	}
	return u
}

// defaultString 空字符串取默认值。
func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// buildQuotaSnapshotJSON 构建配额快照 JSON 字符串。
func buildQuotaSnapshotJSON(tid, limit, used int64, dim string, quotaInitial int64) string {
	snap := map[string]any{
		"tenantId":      tid,
		"dimension":     dim,
		"limit":         limit,
		"used_before":   used - quotaInitial,
		"used_after":    used,
		"remaining":     limit - used,
		"quota_initial": quotaInitial,
	}
	b, _ := json.Marshal(snap)
	return string(b)
}