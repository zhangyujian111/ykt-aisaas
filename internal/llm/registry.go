// Package llm 模型注册表与 Chat 服务。
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	pcrypto "ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// ---- model_registry DO ----

// ModelDO ykt_aisaas_model_registry（tenantId IS NULL = 全局模型）。
// 表在租户豁免名单（跨租户共享读），查询显式带租户条件。
type ModelDO struct {
	ID               int64     `gorm:"column:id;primaryKey"`
	TenantID         *int64    `gorm:"column:tenantId"`
	ModelID          string    `gorm:"column:modelId"`
	Provider         string    `gorm:"column:provider"`
	BaseURL          string    `gorm:"column:baseUrl"`
	APIKeyEnc        string    `gorm:"column:apiKeyEnc"`
	UpstreamModel    string    `gorm:"column:upstreamModel"`
	Type             string    `gorm:"column:type"` // chat/embedding/tts/asr
	Modality         string    `gorm:"column:modality"`
	ContextLength    int       `gorm:"column:contextLength"`
	PriceInputCents  float64   `gorm:"column:priceInputCents"`
	PriceOutputCents float64   `gorm:"column:priceOutputCents"`
	IsDefault        int8      `gorm:"column:isDefault"`
	Status           int8      `gorm:"column:status"`
	CreateTime       time.Time `gorm:"column:createTime"`
	UpdateTime       time.Time `gorm:"column:updateTime"`
}

func (ModelDO) TableName() string { return "ykt_aisaas_model_registry" }

// Resolved 路由结果：上游客户端 + 实际模型名。
type Resolved struct {
	ModelID       string
	UpstreamModel string
	Provider      string
	Type          string
	Client        *openaiclient.Client
}

// Registry 模型注册表：DB + 进程内缓存（TTL 5 分钟）。
type Registry struct {
	db     *gorm.DB
	aesKey []byte

	mu    sync.RWMutex
	cache map[string]cacheEntry // key: tenantID|modelId
}

type cacheEntry struct {
	model  *ModelDO
	expire time.Time
}

// NewRegistry 构造。
func NewRegistry(db *gorm.DB, aesKey string) *Registry {
	return &Registry{db: db, aesKey: []byte(aesKey), cache: map[string]cacheEntry{}}
}

// Resolve 按（租户, 模型ID）解析 chat 模型（兼容入口）。
func (r *Registry) Resolve(ctx context.Context, modelID string) (*Resolved, error) {
	return r.ResolveFor(ctx, modelID, "chat")
}

// ResolveFor 按类型解析（chat/embedding/tts/asr）。tenantId=0 表示只要全局模型。
// 优先级：租户私有 > 全局共享。
func (r *Registry) ResolveFor(ctx context.Context, modelID, wantType string) (*Resolved, error) {
	tid, _ := tenant.FromSafe(ctx)
	do, err := r.load(ctx, tid, modelID)
	if err != nil {
		return nil, err
	}
	if do.Type != wantType {
		return nil, errs.New(errs.ModelNotFound, "模型 "+modelID+" 类型为 "+do.Type+"，需要 "+wantType)
	}
	if do.Status != 1 {
		return nil, errs.New(errs.ModelNotFound)
	}
	key, err := pcrypto.Decrypt(do.APIKeyEnc, r.aesKey)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("decrypt upstream key: %w", err))
	}
	return &Resolved{
		ModelID:       do.ModelID,
		UpstreamModel: do.UpstreamModel,
		Provider:      do.Provider,
		Type:          do.Type,
		Client:        openaiclient.New(do.BaseURL, string(key)),
	}, nil
}

func (r *Registry) load(ctx context.Context, tid int64, modelID string) (*ModelDO, error) {
	ck := fmt.Sprintf("%d|%s", tid, modelID)
	r.mu.RLock()
	if e, ok := r.cache[ck]; ok && time.Now().Before(e.expire) {
		r.mu.RUnlock()
		return e.model, nil
	}
	r.mu.RUnlock()

	var rows []*ModelDO
	// 显式租户条件（该表在豁免名单）：私有优先，全局兜底
	sess := r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(ModelDO{}.TableName()).
		Where("modelId = ? AND isDeleted = 0", modelID)
	if tid > 0 {
		sess = sess.Where("tenantId = ? OR tenantId IS NULL", tid).
			Order("tenantId DESC") // 租户私有优先（非 NULL 排前）
	} else {
		sess = sess.Where("tenantId IS NULL")
	}
	if err := sess.Limit(1).Find(&rows).Error; err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	if len(rows) == 0 {
		return nil, errs.New(errs.ModelNotFound)
	}
	do := rows[0]

	r.mu.Lock()
	r.cache[ck] = cacheEntry{model: do, expire: time.Now().Add(5 * time.Minute)}
	r.mu.Unlock()
	return do, nil
}

// ListChatModels 列出租户可用 chat 模型（/v1/models）。
func (r *Registry) ListChatModels(ctx context.Context) ([]*ModelDO, error) {
	tid, _ := tenant.FromSafe(ctx)
	var rows []*ModelDO
	sess := r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(ModelDO{}.TableName()).
		Where("type = ? AND status = ? AND isDeleted = 0", "chat", 1)
	if tid > 0 {
		sess = sess.Where("tenantId = ? OR tenantId IS NULL", tid)
	} else {
		sess = sess.Where("tenantId IS NULL")
	}
	if err := sess.Find(&rows).Error; err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return rows, nil
}

// Price 返回模型每千 token 单价（分）。未注册/缓存未命中返回 0（不计费）。
// Pricer 接口实现，供 metering 计价。
func (r *Registry) Price(modelID string) (in, out float64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.cache {
		if e.model.ModelID == modelID {
			return e.model.PriceInputCents, e.model.PriceOutputCents
		}
	}
	return 0, 0
}

// DefaultModelID 取默认 chat 模型。
func (r *Registry) DefaultModelID(ctx context.Context) string {
	tid, _ := tenant.FromSafe(ctx)
	var rows []*ModelDO
	sess := r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(ModelDO{}.TableName()).
		Where("type = ? AND status = ? AND isDeleted = 0", "chat", 1)
	if tid > 0 {
		sess = sess.Where("tenantId = ? OR tenantId IS NULL", tid)
	} else {
		sess = sess.Where("tenantId IS NULL")
	}
	_ = sess.Order("isDefault DESC").Limit(1).Find(&rows)
	if len(rows) == 0 {
		return ""
	}
	return rows[0].ModelID
}

// ---- ChatService ----

// ChatService 封装平台侧 chat 编排（当前版本：模型路由 + 直通；Persona/RAG/MCP 后续模块接入）。
type ChatService struct {
	Registry *Registry
}

// NewChatService 构造。
func NewChatService(r *Registry) *ChatService { return &ChatService{Registry: r} }

// UpstreamRequest 构造剥离平台扩展后的上游请求。
func (s *ChatService) UpstreamRequest(resolved *Resolved, msgs []openaiclient.Message, temp *float64, maxTok *int, tools []openaiclient.Tool) *openaiclient.ChatRequest {
	return &openaiclient.ChatRequest{
		Model:       resolved.UpstreamModel,
		Messages:    msgs,
		Temperature: temp,
		MaxTokens:   maxTok,
		Tools:       tools,
	}
}

var _ = slog.Info
var _ = json.Marshal
var _ = strings.TrimSpace
