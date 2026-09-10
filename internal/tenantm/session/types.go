// Package session 会话生命周期管理 + 配额预扣/退款（Q8 决策 quota_snapshot）。
package session

import (
	"time"

	"ykt.dev/aisaas/internal/platform/database"
)

// SessionDO ykt_aisaas_session（GORM 模型）。
type SessionDO struct {
	database.BaseDO
	DeviceID        string     `gorm:"column:deviceId" json:"deviceId"`
	SessionType     string     `gorm:"column:sessionType" json:"sessionType"`
	Title           string     `gorm:"column:title" json:"title"`
	PersonaID       *int64     `gorm:"column:personaId" json:"personaId"`
	ExternalUserID  string     `gorm:"column:externalUserId" json:"externalUserId"`
	ModelID         string     `gorm:"column:modelId" json:"modelId"`
	QuotaSnapshot   string     `gorm:"column:quotaSnapshot" json:"quotaSnapshot"`
	QuotaDimension  string     `gorm:"column:quotaDimension" json:"quotaDimension"`
	QuotaInitial    int64      `gorm:"column:quotaInitial" json:"quotaInitial"`
	QuotaUsed       int64      `gorm:"column:quotaUsed" json:"quotaUsed"`
	ActualCost      string     `gorm:"column:actualCost" json:"actualCost"`
	MessageCount    int        `gorm:"column:messageCount" json:"messageCount"`
	TotalTokensIn   int64      `gorm:"column:totalTokensIn" json:"totalTokensIn"`
	TotalTokensOut  int64      `gorm:"column:totalTokensOut" json:"totalTokensOut"`
	Status          int8       `gorm:"column:status" json:"status"`
	Metadata        string     `gorm:"column:metadata" json:"metadata"`
	StartTime       *time.Time `gorm:"column:startTime" json:"startTime"`
	EndTime         *time.Time `gorm:"column:endTime" json:"endTime"`
	LastMessageTime *time.Time `gorm:"column:lastMessageTime" json:"lastMessageTime"`
	IsDeleted       int8       `gorm:"column:isDeleted" json:"-"`
}

// TableName 表名。
func (SessionDO) TableName() string { return "ykt_aisaas_session" }

// SessionMessageDO ykt_aisaas_session_message（短期会话消息）。
type SessionMessageDO struct {
	ID              int64     `gorm:"column:id;primaryKey" json:"id"`
	TenantID        int64     `gorm:"column:tenantId" json:"tenantId"`
	SessionID       int64     `gorm:"column:sessionId" json:"sessionId"`
	MessageIndex    int       `gorm:"column:messageIndex" json:"messageIndex"`
	Role            string    `gorm:"column:role" json:"role"`
	Content         string    `gorm:"column:content" json:"content"`
	TokensIn        int       `gorm:"column:tokensIn" json:"tokensIn"`
	TokensOut       int       `gorm:"column:tokensOut" json:"tokensOut"`
	Metadata        string    `gorm:"column:metadata" json:"metadata"`
	SourceType      string    `gorm:"column:sourceType" json:"sourceType"`
	SourceMessageID *int64    `gorm:"column:sourceMessageId" json:"sourceMessageId"`
	CreateTime      time.Time `gorm:"column:createTime;autoCreateTime" json:"createTime"`
}

// TableName 表名。
func (SessionMessageDO) TableName() string { return "ykt_aisaas_session_message" }

// ---- 会话状态常量 ----

const (
	StatusCreating = 1 // 创建中
	StatusActive   = 2 // 活跃（对话中）
	StatusEnded    = 3 // 已结束
	StatusSettled  = 4 // 已结算
)

// ---- 会话类型常量 ----

const (
	SessionTypeChat  = "chat"
	SessionTypeVoice = "voice"
	SessionTypeVideo = "video"
	SessionTypeRAG   = "rag"
)

// ---- API 请求/响应 ----

// CreateSessionRequest POST /api/v1/sessions/:deviceId 请求体。
type CreateSessionRequest struct {
	Dimension    string `json:"dimension" binding:"required,oneof=llm_tokens_in llm_tokens_out tts_chars asr_seconds"`
	QuotaInitial int64  `json:"quotaInitial" binding:"required,min=1"`
	PersonaID    *int64 `json:"personaId"`
	Title        string `json:"title"`
	SessionType  string `json:"sessionType"`
	ModelID      string `json:"modelId"`
	Metadata     string `json:"metadata"`
}

// CreateSessionResponse 创建会话响应。
type CreateSessionResponse struct {
	SessionID       string          `json:"sessionId"`
	DeviceID        string          `json:"deviceId"`
	Dimension       string          `json:"dimension"`
	QuotaInitial    int64           `json:"quotaInitial"`
	QuotaRemaining  int64           `json:"quotaRemaining"`
	QuotaSnapshot   *QuotaSnapshot  `json:"quotaSnapshot"`
	CreatedAt       string          `json:"createdAt"`
}

// QuotaSnapshot 设备配额快照。
type QuotaSnapshot struct {
	TenantID             int64  `json:"tenantId"`
	DeviceQuotaLimit     int64  `json:"deviceQuotaLimit"`
	DeviceQuotaUsed      int64  `json:"deviceQuotaUsed"`
	DeviceQuotaRemaining int64  `json:"deviceQuotaRemaining"`
	SnapshotTime         string `json:"snapshotTime"`
}

// EndSessionRequest POST /api/v1/sessions/:sessionId/end 请求体。
type EndSessionRequest struct {
	ActualCost *QuotaUsage `json:"actualCost" binding:"required"`
	Status     string      `json:"status" binding:"required,oneof=success failed"`
}

// EndSessionResponse 结束会话响应。
type EndSessionResponse struct {
	SessionID     string     `json:"sessionId"`
	QuotaUsed     *QuotaUsage `json:"quotaUsed"`
	QuotaRefunded *QuotaUsage `json:"quotaRefunded"`
	EndedAt       string     `json:"endedAt"`
}

// QuotaUsage 配额使用量（多维度）。
type QuotaUsage struct {
	LLMTokensIn  int64 `json:"llm_tokens_in,omitempty"`
	LLMTokensOut int64 `json:"llm_tokens_out,omitempty"`
	TTSChars     int64 `json:"tts_chars,omitempty"`
	ASRSeconds   int64 `json:"asr_seconds,omitempty"`
}

// GetHistoryRequest 历史查询参数。
type GetHistoryRequest struct {
	Limit  int    `form:"limit"`
	Cursor string `form:"cursor"`
}

// SessionHistoryResponse 会话历史响应。
type SessionHistoryResponse struct {
	SessionID  string             `json:"sessionId"`
	Items      []*SessionMessage  `json:"items"`
	HasMore    bool               `json:"hasMore"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// SessionMessage 会话消息（API 响应）。
type SessionMessage struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata,omitempty"`
	CreatedAt string `json:"createdAt"`
}