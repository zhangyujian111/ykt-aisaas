package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/errs"
)

// History 按设备查询会话历史（含消息列表）。
//
// 流程：
//  1. 按 deviceId 查询活跃 session 列表
//  2. 按 session 查询最近 N 条消息（按 createTime DESC）
//  3. 返回 messages + hasMore + nextCursor
func (s *Service) History(ctx context.Context, deviceID string, limit int, cursor string) (*SessionHistoryResponse, error) {
	tid, err := extractTenant(ctx)
	if err != nil {
		return nil, err
	}

	if limit <= 0 || limit > s.cfg.MaxHistoryLimit {
		limit = s.cfg.MaxHistoryLimit
	}

	// 1. 查询活跃 session
	sessions, err := s.repo.ListByDevice(ctx, deviceID, 5)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("list sessions: %w", err))
	}
	if len(sessions) == 0 {
		writeAudit(ctx, "session.history", deviceID, "success", tid,
			map[string]any{"deviceId": deviceID, "count": 0})
		return &SessionHistoryResponse{
			SessionID: "",
			Items:     []*SessionMessage{},
			HasMore:   false,
		}, nil
	}

	// 2. 取第一个活跃 session 的消息
	primarySession := sessions[0]
	sessionID := formatSessionID(primarySession.ID)

	// 3. 解析 cursor
	var cursorID int64
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			cursorID, _ = strconv.ParseInt(string(decoded), 10, 64)
		}
	}

	// 4. 查询消息（跳过租户插件，走显式条件）
	var msgs []*SessionMessageDO
	query := s.repo.DB.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(SessionMessageDO{}.TableName()).
		Where("sessionId = ? AND tenantId = ?", primarySession.ID, tid)

	if cursorID > 0 {
		query = query.Where("id < ?", cursorID)
	}

	err = query.Order("id DESC").
		Limit(limit + 1).
		Find(&msgs).Error
	if err != nil {
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("query messages: %w", err))
	}

	// 5. 构建响应
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	items := make([]*SessionMessage, len(msgs))
	for i, m := range msgs {
		items[i] = &SessionMessage{
			ID:        m.ID,
			Role:      m.Role,
			Content:   m.Content,
			Metadata:  m.Metadata,
			CreatedAt: m.CreateTime.UTC().Format(time.RFC3339),
		}
	}

	var nextCursor string
	if hasMore && len(msgs) > 0 {
		lastID := msgs[len(msgs)-1].ID
		nextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(lastID, 10)))
	}

	// 6. 审计日志
	writeAudit(ctx, "session.history", sessionID, "success", tid,
		map[string]any{"deviceId": deviceID, "count": len(items), "hasMore": hasMore})

	return &SessionHistoryResponse{
		SessionID:  sessionID,
		Items:      items,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}