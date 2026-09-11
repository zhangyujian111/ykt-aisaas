// Package internalapi — OSS 配置 CRUD handler（运营/超管用）。
//
// 对应 Java 版 OssConfigView / 字段对齐 Java config.oss 类型。
// 路由组 /internal/api/v1 已加 InternalMiddleware，调用者必须持有 X-Internal-Token。
package internalapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/ids"
)

// OssConfigHandler /internal/api/v1/oss — OSS 配置 CRUD。
type OssConfigHandler struct {
	DB     *gorm.DB
	AESKey string
}

type ossRow struct {
	ID          int64  `json:"id"`
	TenantID    *int64 `json:"tenantId"`
	Provider    string `json:"provider"`
	ConfigName  string `json:"configName"`
	ConfigDesc  string `json:"configDesc"`
	Endpoint    string `json:"endpoint"`
	Bucket      string `json:"bucket"`
	AccessKey   string `json:"accessKey"`
	Secret      string `json:"secret"` // 解密后的明文（仅 Internal API 返回）
	Region      string `json:"region"`
	PathPrefix  string `json:"pathPrefix"`
	IsDefault   bool   `json:"isDefault"`
	Status      int    `json:"status"`
}

// List GET /internal/api/v1/oss?status=1
func (h *OssConfigHandler) List(c *gin.Context) {
	q := c.Request.URL.Query()
	tx := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_oss_config").
		Where("isDeleted = 0")
	if s := q.Get("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			tx = tx.Where("status = ?", v)
		}
	}
	var rows []map[string]any
	if err := tx.Order("isDefault DESC, id DESC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	out := make([]ossRow, 0, len(rows))
	for _, r := range rows {
		var tid *int64
		if v, ok := r["tenantId"]; ok && v != nil {
			if n, ok := v.(int64); ok && n != 0 {
				tid = &n
			}
		}
		secEnc, _ := r["secretEnc"].(string)
		var plain string
		if secEnc != "" {
			if pt, err := crypto.Decrypt(secEnc, []byte(h.AESKey)); err == nil {
				plain = string(pt)
			}
		}
		out = append(out, ossRow{
			ID:         toInt64(r["id"]),
			TenantID:   tid,
			Provider:   toString(r["provider"]),
			ConfigName: toString(r["configName"]),
			ConfigDesc: toString(r["configDesc"]),
			Endpoint:   toString(r["endpoint"]),
			Bucket:     toString(r["bucket"]),
			AccessKey:  toString(r["accessKey"]),
			Secret:     plain,
			Region:     toString(r["region"]),
			PathPrefix: toString(r["pathPrefix"]),
			IsDefault:  toInt(r["isDefault"]) == 1,
			Status:     toInt(r["status"]),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out, "message": "success"})
}

type ossCreateReq struct {
	TenantID   *int64 `json:"tenantId"`
	Provider   string `json:"provider"`
	ConfigName string `json:"configName"`
	ConfigDesc string `json:"configDesc"`
	Endpoint   string `json:"endpoint"`
	Bucket     string `json:"bucket"`
	AccessKey  string `json:"accessKey"`
	Secret     string `json:"secret"`
	Region     string `json:"region"`
	PathPrefix string `json:"pathPrefix"`
	IsDefault  bool   `json:"isDefault"`
	Status     int    `json:"status"`
}

// Create POST /internal/api/v1/oss
func (h *OssConfigHandler) Create(c *gin.Context) {
	var req ossCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid JSON: " + err.Error()})
		return
	}
	if req.ConfigName == "" || req.Endpoint == "" || req.Bucket == "" || req.AccessKey == "" || req.Secret == "" {
		c.JSON(http.StatusOK, gin.H{"code": 40003, "message": "configName/endpoint/bucket/accessKey/secret 必填"})
		return
	}
	if req.Provider == "" {
		req.Provider = "aliyun"
	}
	if req.Status == 0 {
		req.Status = 1
	}
	secEnc, err := crypto.Encrypt([]byte(req.Secret), []byte(h.AESKey))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": "encrypt secret: " + err.Error()})
		return
	}
	row := map[string]any{
		"id":         ids.Next(),
		"tenantId":   req.TenantID,
		"provider":   req.Provider,
		"configName": req.ConfigName,
		"configDesc": req.ConfigDesc,
		"endpoint":   req.Endpoint,
		"bucket":     req.Bucket,
		"accessKey":  req.AccessKey,
		"secretEnc":  secEnc,
		"region":     req.Region,
		"pathPrefix": req.PathPrefix,
		"isDefault":  boolToInt(req.IsDefault),
		"status":     req.Status,
	}
	if err := h.DB.WithContext(c.Request.Context()).Table("ykt_aisaas_oss_config").Create(row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": row})
}

// Update PUT /internal/api/v1/oss/:id
func (h *OssConfigHandler) Update(c *gin.Context) {
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
		case "secret":
			if s, ok := v.(string); ok && s != "" {
				enc, err := crypto.Encrypt([]byte(s), []byte(h.AESKey))
				if err != nil {
					c.JSON(http.StatusOK, gin.H{"code": 50001, "message": "encrypt secret: " + err.Error()})
					return
				}
				updates["secretEnc"] = enc
			}
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
		Table("ykt_aisaas_oss_config").
		Where("id = ? AND isDeleted = 0", id).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}

// Delete DELETE /internal/api/v1/oss/:id（软删）
func (h *OssConfigHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 40002, "message": "invalid id"})
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Table("ykt_aisaas_oss_config").
		Where("id = ?", id).
		Update("isDeleted", 1).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 50001, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success"})
}
