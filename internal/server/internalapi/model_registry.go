package internalapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
)

// ModelRegistryHandler 模型注册表 CRUD（运营/超管用）。
// 调用者必须持有 X-Internal-Token（路由组已加 InternalMiddleware）。
// 所有写操作都需要 AES 加密 apiKeyEnc（前端传明文 + crypto.aesKey，后端加密落库）。
type ModelRegistryHandler struct {
	DB     *gorm.DB
	AESKey string
}

// modelRow 内部表示（apiKeyEnc 用明文 apiKey 表示传输给前端）。
type modelRow struct {
	ID              int64   `json:"id"`
	TenantID        *int64  `json:"tenantId"` // nil = 全局共享
	ModelID         string  `json:"modelId"`
	Provider        string  `json:"provider"`
	BaseURL         string  `json:"baseUrl"`
	APIKey          string  `json:"apiKey"` // 解密后的明文（仅 Internal API 返回）
	UpstreamModel   string  `json:"upstreamModel"`
	Modality        string  `json:"modality"`
	Type            string  `json:"type"`
	ContextLength   int     `json:"contextLength"`
	PriceInputCents float64 `json:"priceInputCents"`
	PriceOutputCents float64 `json:"priceOutputCents"`
	IsStream        bool    `json:"isStream"`
	IsDefault       bool    `json:"isDefault"`
	Capabilities    string  `json:"capabilities"`
	Status          int     `json:"status"`
}

// List GET /internal/api/v1/models?type=chat&status=1
func (h *ModelRegistryHandler) List(c *gin.Context) {
	type filter struct {
		Type   string
		Status *int
	}
	q := c.Request.URL.Query()
	tx := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_model_registry").
		Where("isDeleted = 0")
	if t := q.Get("type"); t != "" {
		tx = tx.Where("type = ?", t)
	}
	if s := q.Get("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			tx = tx.Where("status = ?", v)
		}
	}
	var rows []map[string]any
	if err := tx.Order("type, id").Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	out := make([]modelRow, 0, len(rows))
	for _, r := range rows {
		apiKeyEnc, _ := r["apiKeyEnc"].(string)
		var plain string
		if apiKeyEnc != "" {
			if pt, err := crypto.Decrypt(apiKeyEnc, []byte(h.AESKey)); err == nil {
				plain = string(pt)
			}
		}
		tenantID, _ := r["tenantId"].(int64)
		var tid *int64
		if v, ok := r["tenantId"]; ok && v != nil {
			if n, ok := v.(int64); ok && n != 0 {
				tid = &n
			}
		}
		_ = tenantID
		out = append(out, modelRow{
			ID:               toInt64(r["id"]),
			TenantID:         tid,
			ModelID:          toString(r["modelId"]),
			Provider:         toString(r["provider"]),
			BaseURL:          toString(r["baseUrl"]),
			APIKey:           plain,
			UpstreamModel:    toString(r["upstreamModel"]),
			Modality:         toString(r["modality"]),
			Type:             toString(r["type"]),
			ContextLength:    toInt(r["contextLength"]),
			PriceInputCents:  toFloat(r["priceInputCents"]),
			PriceOutputCents: toFloat(r["priceOutputCents"]),
			IsStream:         toInt(r["isStream"]) == 1,
			IsDefault:        toInt(r["isDefault"]) == 1,
			Capabilities:     toString(r["capabilities"]),
			Status:           toInt(r["status"]),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out, "message": "success"})
}

type createReq struct {
	TenantID         *int64  `json:"tenantId"`
	ModelID          string  `json:"modelId"`
	Provider         string  `json:"provider"`
	BaseURL          string  `json:"baseUrl"`
	APIKey           string  `json:"apiKey"`
	UpstreamModel    string  `json:"upstreamModel"`
	Modality         string  `json:"modality"`
	Type             string  `json:"type"`
	ContextLength    int     `json:"contextLength"`
	PriceInputCents  float64 `json:"priceInputCents"`
	PriceOutputCents float64 `json:"priceOutputCents"`
	IsStream         bool    `json:"isStream"`
	IsDefault        bool    `json:"isDefault"`
	Capabilities     string  `json:"capabilities"`
}

// Create POST /internal/api/v1/models
func (h *ModelRegistryHandler) Create(c *gin.Context) {
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid JSON: " + err.Error()})
		return
	}
	if req.ModelID == "" || req.Provider == "" || req.BaseURL == "" || req.APIKey == "" || req.Type == "" {
		c.JSON(http.StatusOK, gin.H{"code": 40003, "message": "modelId/provider/baseUrl/apiKey/type 必填"})
		return
	}
	enc, err := crypto.Encrypt([]byte(req.APIKey), []byte(h.AESKey))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": "encrypt apiKey: " + err.Error()})
		return
	}
	modality := req.Modality
	if modality == "" {
		modality = `["text"]`
	}
	row := map[string]any{
		"id":               ids.Next(),
		"tenantId":         req.TenantID,
		"modelId":          req.ModelID,
		"provider":         req.Provider,
		"baseUrl":          req.BaseURL,
		"apiKeyEnc":        enc,
		"upstreamModel":    firstNonEmpty(req.UpstreamModel, req.ModelID),
		"modality":         modality,
		"type":             req.Type,
		"contextLength":    req.ContextLength,
		"priceInputCents":  req.PriceInputCents,
		"priceOutputCents": req.PriceOutputCents,
		"isStream":         boolToInt(req.IsStream),
		"isDefault":        boolToInt(req.IsDefault),
		"capabilities":     firstNonEmpty(req.Capabilities, "{}"),
		"status":           1,
	}
	if err := h.DB.WithContext(c.Request.Context()).Table("ykt_aisaas_model_registry").Create(row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": row})
}

// Update PUT /internal/api/v1/models/:id
func (h *ModelRegistryHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid id"})
		return
	}
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid JSON: " + err.Error()})
		return
	}
	updates := map[string]any{}
	for k, v := range req {
		switch k {
		case "id", "createTime", "updateTime", "isDeleted":
			continue
		case "apiKey":
			if s, ok := v.(string); ok && s != "" {
				enc, err := crypto.Encrypt([]byte(s), []byte(h.AESKey))
				if err != nil {
					c.JSON(http.StatusOK, gin.H{"code": 50001, "message": "encrypt apiKey: " + err.Error()})
					return
				}
				updates["apiKeyEnc"] = enc
			}
		case "isStream", "isDefault":
			if bv, ok := v.(bool); ok {
				updates[k] = boolToInt(bv)
			}
		default:
			updates[k] = v
		}
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "no fields"})
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_model_registry").
		Where("id = ? AND isDeleted = 0", id).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// Delete DELETE /internal/api/v1/models/:id (软删)
func (h *ModelRegistryHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid id"})
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_model_registry").
		Where("id = ?", id).
		Update("isDeleted", 1).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// Test POST /internal/api/v1/models/test { baseUrl, apiKey, upstreamModel, type } — 透传小测试
func (h *ModelRegistryHandler) Test(c *gin.Context) {
	var req struct {
		BaseURL       string `json:"baseUrl"`
		APIKey        string `json:"apiKey"`
		UpstreamModel string `json:"upstreamModel"`
		Type          string `json:"type"` // chat / asr / tts
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid JSON: " + err.Error()})
		return
	}
	if req.BaseURL == "" || req.APIKey == "" || req.UpstreamModel == "" {
		c.JSON(http.StatusOK, gin.H{"code": 40003, "message": "baseUrl/apiKey/upstreamModel 必填"})
		return
	}
	switch req.Type {
	case "chat":
		// 简单 chat /v1/chat/completions 测试
		body := []byte(`{"model":"` + req.UpstreamModel + `","messages":[{"role":"user","content":"ping"}],"stream":false,"max_tokens":8}`)
		proxy, perr := newUpstreamProxy(req.BaseURL, req.APIKey, "/chat/completions", body)
		if perr != nil {
			c.JSON(http.StatusOK, gin.H{"code": 50001, "message": perr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": proxy, "message": "success"})
	default:
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "未实现的测试类型: " + req.Type})
	}
}

// ---- helpers ----

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

func toInt(v any) int {
	return int(toInt64(v))
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	return 0
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// 避免 unused import 警告
var _ = errs.New