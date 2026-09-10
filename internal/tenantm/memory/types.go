// Package memory 三层记忆模型：短期消息 + 摘要 + 长期图谱。
package memory

import (
	"time"

	"ykt.dev/aisaas/internal/platform/database"
)

// ---- 数据库 DO ----

// SessionMessageDO ykt_aisaas_session_message 短期会话消息。
type SessionMessageDO struct {
	database.BaseDO
	SessionID      int64     `gorm:"column:sessionId" json:"sessionId"`
	MessageIndex   int       `gorm:"column:messageIndex" json:"messageIndex"`
	Role           string    `gorm:"column:role" json:"role"`
	Content        string    `gorm:"column:content" json:"content"`
	TokensIn       int       `gorm:"column:tokensIn" json:"tokensIn"`
	TokensOut      int       `gorm:"column:tokensOut" json:"tokensOut"`
	Metadata       string    `gorm:"column:metadata;type:json" json:"metadata"`
	SourceType     string    `gorm:"column:sourceType" json:"sourceType"`
	SourceMsgID    int64     `gorm:"column:sourceMessageId" json:"sourceMessageId"`
	IsDeleted      int8      `gorm:"column:isDeleted" json:"-"`
}

func (SessionMessageDO) TableName() string { return "ykt_aisaas_session_message" }

// MemoryDO ykt_aisaas_memory 长期记忆实体。
type MemoryDO struct {
	database.BaseDO
	DeviceID        string    `gorm:"column:deviceId" json:"deviceId"`
	EntityType      string    `gorm:"column:entityType" json:"entityType"`
	EntityKey       string    `gorm:"column:entityKey" json:"entityKey"`
	Content         string    `gorm:"column:content;type:json" json:"content"`
	Importance      float64   `gorm:"column:importance" json:"importance"`
	SourceMsgID     int64     `gorm:"column:sourceMessageId" json:"sourceMessageId"`
	Version         int       `gorm:"column:version" json:"version"`
	Status          int8      `gorm:"column:status" json:"status"`
	LastAccessTime  *time.Time `gorm:"column:lastAccessTime" json:"lastAccessTime"`
	IsDeleted       int8      `gorm:"column:isDeleted" json:"-"`
}

func (MemoryDO) TableName() string { return "ykt_aisaas_memory" }

// MemoryRelationDO ykt_aisaas_memory_relation 记忆实体关系。
type MemoryRelationDO struct {
	database.BaseDO
	SourceEntityID int64  `gorm:"column:sourceEntityId" json:"sourceEntityId"`
	RelationType   string `gorm:"column:relationType" json:"relationType"`
	TargetEntityID int64  `gorm:"column:targetEntityId" json:"targetEntityId"`
	Weight         float64 `gorm:"column:weight" json:"weight"`
	Properties     string `gorm:"column:properties;type:json" json:"properties"`
	SourceMsgID    int64  `gorm:"column:sourceMessageId" json:"sourceMessageId"`
	Version        int    `gorm:"column:version" json:"version"`
	Status         int8   `gorm:"column:status" json:"status"`
	IsDeleted      int8   `gorm:"column:isDeleted" json:"-"`
}

func (MemoryRelationDO) TableName() string { return "ykt_aisaas_memory_relation" }

// ---- 请求/响应类型（对齐 OpenAPI YAML）----

// WriteMessageReq POST /api/v1/memories/{deviceId}/messages 请求体。
type WriteMessageReq struct {
	SessionID string            `json:"sessionId"`
	Role      string            `json:"role" binding:"required"`
	Content   string            `json:"content" binding:"required"`
	Metadata  *WriteMessageMeta `json:"metadata"`
}

// WriteMessageMeta 消息元数据。
type WriteMessageMeta struct {
	Model     string `json:"model"`
	Tokens    int    `json:"tokens"`
	LatencyMs int    `json:"latencyMs"`
}

// WriteMessageResp 写入响应。
type WriteMessageResp struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"sessionId"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListMessagesResp 消息列表响应。
type ListMessagesResp struct {
	Items      []*MemoryMessage `json:"items"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// MemoryMessage 单条消息。
type MemoryMessage struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"sessionId"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// MemoryGraphResp 图谱响应。
type MemoryGraphResp struct {
	DeviceID    string              `json:"deviceId"`
	Entities    []*MemoryEntity     `json:"entities"`
	Preferences []*MemoryPreference `json:"preferences"`
	Events      []*MemoryEvent      `json:"events"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

// MemoryEntity 实体。
type MemoryEntity struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Confidence float64        `json:"confidence"`
	Mentions   []string       `json:"mentions,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// MemoryPreference 偏好。
type MemoryPreference struct {
	Dimension      string `json:"dimension"`
	Value          string `json:"value"`
	Confidence     float64 `json:"confidence"`
	SourceMessages []int64 `json:"sourceMessages,omitempty"`
}

// MemoryEvent 事件。
type MemoryEvent struct {
	ID           string         `json:"id,omitempty"`
	EventType    string         `json:"eventType"`
	EventTime    time.Time      `json:"eventTime"`
	Description  string         `json:"description"`
	Participants []string       `json:"participants,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// ExtractReq POST /api/v1/memories/{deviceId}/extract 请求体。
type ExtractReq struct {
	SessionID    string        `json:"sessionId"`
	Messages     []MessageItem `json:"messages" binding:"required,min=1"`
	ExtractTypes []string      `json:"extractTypes" binding:"required,min=1"`
}

// MessageItem 抽取请求中的消息项。
type MessageItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ExtractResp 抽取任务响应。
type ExtractResp struct {
	TaskID            string    `json:"taskId"`
	Status            string    `json:"status"`
	EstimatedEntities int       `json:"estimatedEntities"`
	CreatedAt         time.Time `json:"createdAt"`
}

// SummarizeReq POST /api/v1/memories/{deviceId}/summarize 请求体。
type SummarizeReq struct {
	SessionID      string   `json:"sessionId"`
	SummarizeTypes []string `json:"summarizeTypes"`
	TopicFocus     string   `json:"topicFocus"`
}

// SummarizeResp 摘要任务响应。
type SummarizeResp struct {
	TaskID    string    `json:"taskId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---- 任务类型 ----

// TaskState 异步任务状态。
type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
)

// ExtractTask 抽取任务 payload。
type ExtractTask struct {
	TaskID       string        `json:"taskId"`
	TenantID     int64         `json:"tenantId"`
	DeviceID     string        `json:"deviceId"`
	SessionID    string        `json:"sessionId"`
	Messages     []MessageItem `json:"messages"`
	ExtractTypes []string      `json:"extractTypes"`
	CreatedAt    time.Time     `json:"createdAt"`
}

// SummarizeTask 摘要任务 payload。
type SummarizeTask struct {
	TaskID         string    `json:"taskId"`
	TenantID       int64     `json:"tenantId"`
	DeviceID       string    `json:"deviceId"`
	SessionID      string    `json:"sessionId"`
	SummarizeTypes []string  `json:"summarizeTypes"`
	TopicFocus     string    `json:"topicFocus"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ---- LLM 抽取结果 ----

// ExtractResult LLM 实体抽取结果。
type ExtractResult struct {
	Entities  []*ExtractedEntity  `json:"entities"`
	Relations []*ExtractedRelation `json:"relations"`
}

// ExtractedEntity 抽取到的实体。
type ExtractedEntity struct {
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
	Importance float64        `json:"importance"`
}

// ExtractedRelation 抽取到的关系。
type ExtractedRelation struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
}

// SummarizeResult LLM 摘要结果。
type SummarizeResult struct {
	Summary       string   `json:"summary"`
	Topics        []string `json:"topics"`
	KeyEvents     []string `json:"keyEvents"`
	Importance    float64  `json:"importance"`
}