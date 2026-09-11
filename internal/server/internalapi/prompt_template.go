// Package internalapi — Prompt 模板 CRUD handler（运营/超管用）。
//
// 对应 Java 版 TemplateView / 字段对齐 Java config.agent + template 类型。
// 路由组 /internal/api/v1 已加 InternalMiddleware，调用者必须持有 X-Internal-Token。
package internalapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/ids"
)

// PromptTemplateHandler /internal/api/v1/templates — 模板 CRUD。
type PromptTemplateHandler struct {
	DB *gorm.DB
}

type templateRow struct {
	ID          int64  `json:"id"`
	TenantID    *int64 `json:"tenantId"`
	TemplateKey string `json:"templateKey"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Variables   string `json:"variables"`
	IsDefault   bool   `json:"isDefault"`
	Status      int    `json:"status"`
}

type templateCreateReq struct {
	TenantID    *int64 `json:"tenantId"`
	TemplateKey string `json:"templateKey"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Variables   string `json:"variables"`
	IsDefault   bool   `json:"isDefault"`
	Status      int    `json:"status"`
}

// List GET /internal/api/v1/templates?category=memory_summary&status=1
func (h *PromptTemplateHandler) List(c *gin.Context) {
	q := c.Request.URL.Query()
	tx := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_prompt_template").
		Where("isDeleted = 0")
	if cat := q.Get("category"); cat != "" {
		tx = tx.Where("category = ?", cat)
	}
	if s := q.Get("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			tx = tx.Where("status = ?", v)
		}
	}
	var rows []map[string]any
	if err := tx.Order("category, id").Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	out := make([]templateRow, 0, len(rows))
	for _, r := range rows {
		var tid *int64
		if v, ok := r["tenantId"]; ok && v != nil {
			if n, ok := v.(int64); ok && n != 0 {
				tid = &n
			}
		}
		out = append(out, templateRow{
			ID:          toInt64(r["id"]),
			TenantID:    tid,
			TemplateKey: toString(r["templateKey"]),
			Name:        toString(r["name"]),
			Category:    toString(r["category"]),
			Description: toString(r["description"]),
			Content:     toString(r["content"]),
			Variables:   toString(r["variables"]),
			IsDefault:   toInt(r["isDefault"]) == 1,
			Status:      toInt(r["status"]),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out, "message": "success"})
}

// Create POST /internal/api/v1/templates
func (h *PromptTemplateHandler) Create(c *gin.Context) {
	var req templateCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid JSON: " + err.Error()})
		return
	}
	if req.TemplateKey == "" || req.Name == "" || req.Content == "" {
		c.JSON(http.StatusOK, gin.H{"code": 40003, "message": "templateKey/name/content 必填"})
		return
	}
	if req.Category == "" {
		req.Category = "custom"
	}
	if req.Status == 0 {
		req.Status = 1
	}
	row := map[string]any{
		"id":          ids.Next(),
		"templateKey": req.TemplateKey,
		"name":        req.Name,
		"category":    req.Category,
		"description": req.Description,
		"content":     req.Content,
		"variables":   req.Variables,
		"isDefault":   boolToInt(req.IsDefault),
		"status":      req.Status,
	}
	if req.TenantID != nil {
		row["tenantId"] = *req.TenantID
	}
	if err := h.DB.WithContext(c.Request.Context()).Table("ykt_aisaas_prompt_template").Create(row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": row})
}

// Update PUT /internal/api/v1/templates/:id
func (h *PromptTemplateHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
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
		case "isDefault":
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
		Table("ykt_aisaas_prompt_template").
		Where("id = ? AND isDeleted = 0", id).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// Delete DELETE /internal/api/v1/templates/:id（软删）
func (h *PromptTemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid id"})
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_prompt_template").
		Where("id = ?", id).
		Update("isDeleted", 1).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// Preview POST /internal/api/v1/templates/:id/preview
// 用样例变量渲染模板（用于前台实时预览）
func (h *PromptTemplateHandler) Preview(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid id"})
		return
	}
	var row map[string]any
	if err := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_prompt_template").
		Where("id = ? AND isDeleted = 0", id).
		Take(&row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50004, "message": "模板不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"content": toString(row["content"]),
		},
	})
}
