// Package persona 人设模块：Persona CRUD + 设备绑定 + SystemPrompt 生成。
package persona

import (
	"encoding/json"
	"log/slog"
	"time"

	"ykt.dev/aisaas/internal/platform/database"
)

// ============================================================
// 辅助类型（对齐 OpenAPI）
// ============================================================

// VoicePreference 对齐 OpenAPI voicePreference。
type VoicePreference struct {
	Voice string  `json:"voice"`
	Speed float64 `json:"speed"`
	Pitch float64 `json:"pitch"`
}

// RelationshipStage 对齐 OpenAPI relationshipStages 元素。
type RelationshipStage struct {
	Stage     string `json:"stage"`
	Prompt    string `json:"prompt"`
	Threshold int    `json:"threshold"`
}

// ============================================================
// DO — 数据库模型（与 ykt_aisaas_persona / ykt_aisaas_persona_bind 对齐）
// ============================================================

// PersonaDO ykt_aisaas_persona。
type PersonaDO struct {
	database.BaseDO
	Code                   string          `gorm:"column:code" json:"code"`
	Name                   string          `gorm:"column:name" json:"name"`
	Description            string          `gorm:"column:description" json:"description"`
	SystemPrompt           string          `gorm:"column:systemPrompt" json:"systemPrompt"`
	PersonalityTraits      json.RawMessage `gorm:"column:personalityTraits" json:"personalityTraits"`
	VoicePreference        string          `gorm:"column:voicePreference" json:"voicePreference"`
	RelationshipStages     json.RawMessage `gorm:"column:relationshipStages" json:"relationshipStages"`
	DefaultModelID         string          `gorm:"column:defaultModelId" json:"defaultModelId"`
	DefaultKnowledgeBaseIDs json.RawMessage `gorm:"column:defaultKnowledgeBaseIds" json:"defaultKnowledgeBaseIds"`
	Temperature            float64         `gorm:"column:temperature" json:"temperature"`
	TopP                   float64         `gorm:"column:topP" json:"topP"`
	MaxTokens              int             `gorm:"column:maxTokens" json:"maxTokens"`
	PresencePenalty        float64         `gorm:"column:presencePenalty" json:"presencePenalty"`
	FrequencyPenalty       float64         `gorm:"column:frequencyPenalty" json:"frequencyPenalty"`
	Status                 int8            `gorm:"column:status" json:"status"`
	Tags                   json.RawMessage `gorm:"column:tags" json:"tags"`
	IsDeleted              int8            `gorm:"column:isDeleted" json:"-"`
}

func (PersonaDO) TableName() string { return "ykt_aisaas_persona" }

// PersonaBindDO ykt_aisaas_persona_bind。
type PersonaBindDO struct {
	database.BaseDO
	DeviceID      string          `gorm:"column:deviceId" json:"deviceId"`
	PersonaID     int64           `gorm:"column:personaId" json:"personaId"`
	BindType      string          `gorm:"column:bindType" json:"bindType"`
	Priority      int             `gorm:"column:priority" json:"priority"`
	BindCondition json.RawMessage `gorm:"column:bindCondition" json:"bindCondition"`
	Status        int8            `gorm:"column:status" json:"status"`
	IsDeleted     int8            `gorm:"column:isDeleted" json:"-"`
}

func (PersonaBindDO) TableName() string { return "ykt_aisaas_persona_bind" }

// ============================================================
// API 请求/响应类型
// ============================================================

// PersonaResp 对齐 OpenAPI Persona schema。
type PersonaResp struct {
	ID                   int64             `json:"id"`
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Description          string            `json:"description,omitempty"`
	SystemPrompt         string            `json:"systemPrompt"`
	PersonalityTraits    json.RawMessage   `json:"personalityTraits,omitempty"`
	VoicePreference      *VoicePreference  `json:"voicePreference,omitempty"`
	RelationshipStages   []RelationshipStage `json:"relationshipStages,omitempty"`
	DefaultModelID       string            `json:"defaultModelId,omitempty"`
	DefaultKnowledgeBaseIDs []string        `json:"defaultKnowledgeBaseIds,omitempty"`
	Temperature          *float64          `json:"temperature,omitempty"`
	TopP                 *float64          `json:"topP,omitempty"`
	MaxTokens            *int              `json:"maxTokens,omitempty"`
	PresencePenalty      *float64          `json:"presencePenalty,omitempty"`
	FrequencyPenalty     *float64          `json:"frequencyPenalty,omitempty"`
	Tags                 []string          `json:"tags,omitempty"`
	Skills               []PersonaSkillResp `json:"skills,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
	CreatedAt            string            `json:"createdAt"`
	UpdatedAt            string            `json:"updatedAt"`
}

// PersonaSkillResp 对齐 OpenAPI PersonaSkill。
type PersonaSkillResp struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// PersonaBindResp 对齐 OpenAPI PersonaBind。
type PersonaBindResp struct {
	BindID    int64  `json:"bindId"`
	PersonaID int64  `json:"personaId"`
	DeviceID  string `json:"deviceId"`
	IsDefault bool   `json:"isDefault"`
	BoundAt   string `json:"boundAt"`
}

// PersonaWithBindResp 对齐 OpenAPI PersonaWithBind（Persona + PersonaBind）。
type PersonaWithBindResp struct {
	*PersonaResp
	PersonaBind *PersonaBindResp `json:"persona_bind,omitempty"`
}

// ListPersonasResp 对齐 OpenAPI ListPersonasResponse。
type ListPersonasResp struct {
	Items   []*PersonaWithBindResp `json:"items"`
	HasMore bool                   `json:"hasMore"`
}

// CreatePersonaReq 创建 Persona（admin 用，不暴露 API）。
type CreatePersonaReq struct {
	Code                   string          `json:"code" binding:"required,max=64"`
	Name                   string          `json:"name" binding:"required,max=128"`
	Description            string          `json:"description"`
	SystemPrompt           string          `json:"systemPrompt" binding:"required"`
	PersonalityTraits      json.RawMessage `json:"personalityTraits"`
	VoicePreference        string          `json:"voicePreference"`
	RelationshipStages     json.RawMessage `json:"relationshipStages"`
	DefaultModelID         string          `json:"defaultModelId"`
	DefaultKnowledgeBaseIDs json.RawMessage `json:"defaultKnowledgeBaseIds"`
	Temperature            *float64        `json:"temperature"`
	TopP                   *float64        `json:"topP"`
	MaxTokens              *int            `json:"maxTokens"`
	PresencePenalty        *float64        `json:"presencePenalty"`
	FrequencyPenalty       *float64        `json:"frequencyPenalty"`
	Tags                   json.RawMessage `json:"tags"`
}

// UpdatePersonaReq 更新 Persona（admin 用，不暴露 API）。
type UpdatePersonaReq struct {
	ID                     int64           `json:"id" binding:"required"`
	Name                   *string         `json:"name"`
	Description            *string         `json:"description"`
	SystemPrompt           *string         `json:"systemPrompt"`
	PersonalityTraits      json.RawMessage `json:"personalityTraits"`
	VoicePreference        *string         `json:"voicePreference"`
	RelationshipStages     json.RawMessage `json:"relationshipStages"`
	DefaultModelID         *string         `json:"defaultModelId"`
	DefaultKnowledgeBaseIDs json.RawMessage `json:"defaultKnowledgeBaseIds"`
	Temperature            *float64        `json:"temperature"`
	TopP                   *float64        `json:"topP"`
	MaxTokens              *int            `json:"maxTokens"`
	PresencePenalty        *float64        `json:"presencePenalty"`
	FrequencyPenalty       *float64        `json:"frequencyPenalty"`
	Tags                   json.RawMessage `json:"tags"`
	Status                 *int8           `json:"status"`
}

// BindReq 绑定设备到 Persona。
type BindReq struct {
	DeviceID      string          `json:"deviceId" binding:"required,max=128"`
	PersonaID     int64           `json:"personaId" binding:"required"`
	BindType      string          `json:"bindType"`      // default / strict
	Priority      int             `json:"priority"`
	BindCondition json.RawMessage `json:"bindCondition"`
}

// UnbindReq 解绑设备。
type UnbindReq struct {
	DeviceID string `json:"deviceId" binding:"required,max=128"`
}

// ============================================================
// DO → API Resp 转换
// ============================================================

// ToResp 将 PersonaDO 转为 API 响应。
func (d *PersonaDO) ToResp() *PersonaResp {
	r := &PersonaResp{
		ID:                     d.ID,
		Name:                   d.Name,
		Version:                "1.0",
		Description:            d.Description,
		SystemPrompt:           d.SystemPrompt,
		PersonalityTraits:      d.PersonalityTraits,
		VoicePreference:        parseVoicePreference(d.VoicePreference),
		RelationshipStages:     parseRelationshipStages(d.RelationshipStages),
		DefaultModelID:         d.DefaultModelID,
		DefaultKnowledgeBaseIDs: parseStringSlice(d.DefaultKnowledgeBaseIDs),
		Temperature:            parseFloat64Ptr(d.Temperature),
		TopP:                   parseFloat64Ptr(d.TopP),
		MaxTokens:              parseIntPtr(d.MaxTokens),
		PresencePenalty:        parseFloat64Ptr(d.PresencePenalty),
		FrequencyPenalty:       parseFloat64Ptr(d.FrequencyPenalty),
		Tags:                   parseStringSlice(d.Tags),
		Skills:                 []PersonaSkillResp{},
		Metadata:               map[string]any{"code": d.Code, "status": d.Status},
		CreatedAt:              d.CreateTime.Format(time.RFC3339),
		UpdatedAt:              d.UpdateTime.Format(time.RFC3339),
	}
	return r
}

// ToBindResp 将 PersonaBindDO 转为 API 响应。
func (d *PersonaBindDO) ToBindResp() *PersonaBindResp {
	return &PersonaBindResp{
		BindID:    d.ID,
		PersonaID: d.PersonaID,
		DeviceID:  d.DeviceID,
		IsDefault: d.BindType == "default",
		BoundAt:   d.CreateTime.Format(time.RFC3339),
	}
}

// defaultJSON 返回空 []byte 的默认值。
func defaultJSON(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("{}")
	}
	return v
}

// ============================================================
// JSON 解析辅助函数（DO → Resp）
// ============================================================

// parseVoicePreference 解析 voicePreference 字符串为 *VoicePreference。
func parseVoicePreference(v string) *VoicePreference {
	if v == "" {
		return nil
	}
	// v 是 voice_library.id，直接构造对象
	// speed/pitch 使用默认值
	return &VoicePreference{
		Voice: v,
		Speed: 1.0,
		Pitch: 1.0,
	}
}

// parseRelationshipStages 解析 relationshipStages JSON 为 []RelationshipStage。
func parseRelationshipStages(v json.RawMessage) []RelationshipStage {
	if len(v) == 0 {
		return nil
	}
	var stages []RelationshipStage
	if err := json.Unmarshal(v, &stages); err != nil {
		slog.Warn("parse relationship stages failed", "error", err)
		return nil
	}
	return stages
}

// parseStringSlice 解析 JSON 为 []string。
func parseStringSlice(v json.RawMessage) []string {
	if len(v) == 0 {
		return nil
	}
	var slice []string
	if err := json.Unmarshal(v, &slice); err != nil {
		slog.Warn("parse string slice failed", "error", err)
		return nil
	}
	return slice
}

// parseFloat64Ptr 将 float64 转为指针（DB NULL 时返回 nil）。
func parseFloat64Ptr(v float64) *float64 {
	// GORM 对 DECIMAL 类型处理：0 是有效值，NULL 需要用 *float64
	// 此处假设 DB 非 NULL，直接返回指针
	return &v
}

// parseIntPtr 将 int 转为指针（DB NULL 时返回 nil）。
func parseIntPtr(v int) *int {
	return &v
}