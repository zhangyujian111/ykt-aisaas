// Package server 路由装配。
package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/asr"
	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/platform/anomaly"
	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/portal"
	"ykt.dev/aisaas/internal/rag"
	"ykt.dev/aisaas/internal/server/apiv1"
	"ykt.dev/aisaas/internal/server/internalapi"
	"ykt.dev/aisaas/internal/server/middleware"
	"ykt.dev/aisaas/internal/server/portalapi"
	v1 "ykt.dev/aisaas/internal/server/v1"
	"ykt.dev/aisaas/internal/tenantm"
	"ykt.dev/aisaas/internal/tenantm/apikey"
	"ykt.dev/aisaas/internal/tenantm/face"
	"ykt.dev/aisaas/internal/tenantm/memory"
	"ykt.dev/aisaas/internal/tenantm/persona"
	"ykt.dev/aisaas/internal/tenantm/session"
	"ykt.dev/aisaas/internal/tenantm/vision"
	"ykt.dev/aisaas/internal/tts"
)

// Deps 装配依赖。
type Deps struct {
	DB           *gorm.DB
	Cfg          *config.Config
	AuthSvc      *auth.Service
	ChatSvc      *llm.ChatService
	Registry     *llm.Registry
	Quota        *quota.Guard
	Meter        *metering.Recorder
	APIKeySvc    *apikey.Service
	AuditRec     *audit.Recorder
	RagRepo      *rag.Repo
	RagSvc       *rag.Service
	RagIngest    *rag.Ingestor
	RagRetriever *rag.Retriever
	McpRepo      *mcp.Repo
	McpSvc       *mcp.Service
	BillingSvc   *billing.Service
	QuotaLoader  *billing.QuotaLoader
	DeviceTenant *tenantm.DeviceTenantService
	PortalSvc    *portal.Service
	MemorySvc    *memory.Service
	MemoryWorker *memory.Worker
	SessionH     *session.Handler
	PersonaSvc   *persona.Service
	VisionSvc    vision.Service
	FaceSvc      face.Service
	AnomalyEngine anomaly.Engine
}

// NewRouter 装配全部路由。
func NewRouter(d *Deps) *gin.Engine {
	r := web.NewEngine()
	r.Use(web.RequestIDMiddleware())
	r.Use(middleware.MetricsMiddleware())

	// ---- 公共 ----
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "ykt-aisaas", "version": "0.1.0"})
	})

	// ---- mTLS 防御纵深 ----
	// 当 TLS 配置启用时，对全部 /internal/* 路由强制客户端证书校验。
	// 这一层在 TLS 握手层（ClientAuth=RequireAndVerifyClientCert）之上，
	// 是失败闭合的：缺证书 / 证书非本 CA 签发 → 直接 401。
	// 注意：仅 TLS 启用时才挂载，开发模式（HTTP）下走原有的 token+loopback 鉴权。
	if d.Cfg.Server.IsTLSEnabled() {
		allowedSANs := []string{
			"xiaozhi-server-go",
			"xiaozhi-server-go.default.svc.cluster.local",
			"ykt-admin",
			"ykt-admin.default.svc.cluster.local",
		}
		r.Group("/internal", middleware.MTLSRequired(d.Cfg.Server.TLSCACertFile, allowedSANs))
	}

	// ---- OpenAI 兼容（API Key 鉴权）----
	v1g := r.Group("/v1", auth.Middleware(d.AuthSvc, ""))
	chatH := v1.NewChatHandler(d.ChatSvc, d.Quota, d.Meter, d.RagRetriever, d.McpSvc)
	v1g.POST("/chat/completions", chatH.Completions)
	v1g.GET("/models", (&v1.ModelsHandler{Registry: d.Registry}).Models)

	// OpenAI 兼容音频接口（xiaozhi-server TTS/ASR 依赖）
	audioH := &v1.AudioHandler{
		Tts:   tts.New(d.Registry),
		Asr:   asr.New(d.Registry),
		Quota: d.Quota, Meter: d.Meter,
	}
	v1g.POST("/audio/speech", auth.Middleware(d.AuthSvc, "tts"), audioH.Speech)
	v1g.POST("/audio/transcriptions", auth.Middleware(d.AuthSvc, "asr"), audioH.Transcriptions)

	// ---- SaaS 自有（API Key 程序访问；浏览器登录态后续接 JWT）----
	apig := r.Group("/api/v1", auth.Middleware(d.AuthSvc, ""))
	keyH := &apiv1.APIKeyHandler{Svc: d.APIKeySvc}
	billH := &apiv1.BillingHandler{Svc: d.BillingSvc, Rdb: d.Quota.Rdb}
	apig.GET("/billing/balance", billH.Balance)
	apig.GET("/billing/transactions", billH.Transactions)
	apig.GET("/usage/overview", billH.UsageOverview)

	mcpH := &apiv1.MCPHandler{Repo: d.McpRepo, Svc: d.McpSvc}
	apig.POST("/mcp/tools", mcpH.Create)
	apig.GET("/mcp/tools", mcpH.List)
	apig.POST("/mcp/tools/bind", mcpH.Bind)
	apig.POST("/mcp/tools/test", mcpH.Test)

	kbH := &apiv1.KBHandler{Svc: d.RagSvc, Ingest: d.RagIngest, Repo: d.RagRepo, Retriever: d.RagRetriever}
	apig.POST("/knowledge-bases", kbH.Create)
	apig.GET("/knowledge-bases", kbH.List)
	apig.DELETE("/knowledge-bases/:id", kbH.Delete)
	apig.GET("/knowledge-bases/:id/documents", kbH.Docs)
	apig.POST("/knowledge-bases/:id/documents", kbH.UploadDoc)
	apig.POST("/knowledge-bases/:id/search", kbH.Search)

	apig.POST("/apikeys", keyH.Create)
	apig.GET("/apikeys", keyH.List)
	apig.PATCH("/apikeys/:id/status", keyH.Disable)
	apig.DELETE("/apikeys/:id", keyH.Delete)

	// persona 模块路由
	if d.PersonaSvc != nil {
		personaH := &apiv1.PersonaHandler{Svc: d.PersonaSvc, Audit: d.AuditRec}
		apig.GET("/personas/:id", personaH.GetPersona)
		apig.GET("/personas", personaH.GetPersonaByDevice)
	}

	// memory 模块路由（device-scoped）
	memH := &apiv1.MemoryHandler{Svc: d.MemorySvc, Quota: d.Quota, Meter: d.Meter, Worker: d.MemoryWorker, DeviceTenantSvc: d.DeviceTenant, PersonaBindSvc: d.PersonaSvc}
	memoryGroup := apig.Group("/memories/:deviceId")
	{
		memoryGroup.POST("/messages", memH.AppendMessage)
		memoryGroup.GET("/messages", memH.ListMessages)
		memoryGroup.GET("", memH.GetGraph)
		memoryGroup.POST("/extract", memH.ExtractAsync)
		memoryGroup.POST("/summarize", memH.SummarizeAsync)
	}

	// Session 会话（配额快照借记/退款）
	if d.SessionH != nil {
		apig.GET("/sessions/:deviceId", d.SessionH.ListSessionsByDevice)
		apig.POST("/sessions/:deviceId", d.SessionH.CreateSession)
		apig.GET("/sessions/:deviceId/history", d.SessionH.GetHistory)
		apig.POST("/sessions/:deviceId/:sessionId/end", d.SessionH.EndSession)
	}

	// Vision 视觉理解（多模态）
	if d.VisionSvc != nil {
		visionH := &apiv1.VisionHandler{Svc: d.VisionSvc, Quota: d.Quota, Meter: d.Meter}
		apig.POST("/vision/analyze", visionH.Analyze)
		apig.POST("/vision/compare", visionH.Compare)
		apig.POST("/vision/video", visionH.AnalyzeVideo)
	}

	// Anomaly 异常检测规则引擎（V4-A）
	if d.AnomalyEngine != nil {
		anomalyH := &apiv1.AnomalyHandler{Engine: d.AnomalyEngine}
		apig.GET("/anomaly/rules", anomalyH.ListRules)
		apig.GET("/anomaly/rules/:id", anomalyH.GetRule)
		apig.POST("/anomaly/rules", anomalyH.CreateRule)
		apig.PUT("/anomaly/rules/:id", anomalyH.UpdateRule)
		apig.DELETE("/anomaly/rules/:id", anomalyH.DeleteRule)
		apig.GET("/anomaly/events", anomalyH.ListEvents)
	}

	// ---- 内部超级租户（X-Internal-Token + loopback）----
	intg := r.Group("/internal/api/v1", auth.InternalMiddleware(d.Cfg.Server.InternalToken))
	intg.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pong": true, "internal": true})
	})
	intH := &internalapi.Handler{KeySvc: d.APIKeySvc, Billing: d.BillingSvc, QuotaSvc: d.Quota}
	intg.POST("/tenants/:tenantId/apikeys", intH.IssueAPIKey)
	intg.POST("/tenants/:tenantId/recharge", intH.Recharge)
	intg.POST("/apikeys/:id/rotate", intH.RotateAPIKey)
	intg.POST("/apikeys/:id/revoke", intH.RevokeAPIKey)
	intg.POST("/quota/refund", intH.RefundQuota)

	// 模型注册表 CRUD（运营/超管用 X-Internal-Token）
	modelH := &internalapi.ModelRegistryHandler{DB: d.DB, AESKey: d.Cfg.Crypto.AESKey}
	intg.GET("/models", modelH.List)
	intg.POST("/models", modelH.Create)
	intg.PUT("/models/:id", modelH.Update)
	intg.DELETE("/models/:id", modelH.Delete)
	intg.POST("/models/test", modelH.Test)

	// xiaozhi-server 设备即租户通道：X-Device-Id 自动开户 + 独立计量计费
	devg := r.Group("/internal/xiaozhi/v1", auth.InternalDeviceMiddleware(d.Cfg.Server.InternalToken, d.DeviceTenant))
	devChat := v1.NewChatHandler(d.ChatSvc, d.Quota, d.Meter, d.RagRetriever, d.McpSvc)
	devg.POST("/chat/completions", devChat.Completions)
	devAudio := &v1.AudioHandler{Tts: tts.New(d.Registry), Asr: asr.New(d.Registry), Quota: d.Quota, Meter: d.Meter}
	devg.POST("/audio/speech", devAudio.Speech)
	devg.POST("/audio/transcriptions", devAudio.Transcriptions)

	// Face 人脸检测（v1：detect + 5 点 keypoints，无 profile/识别）
	if d.FaceSvc != nil {
		faceH := &internalapi.FaceHandler{Svc: d.FaceSvc, Quota: d.Quota, Meter: d.Meter}
		devg.POST("/face/detect", faceH.Detect)
	}

	// 用户门户（JWT）
	portalH := &portalapi.Handler{Svc: d.PortalSvc}
	pg := r.Group("/portal/api/v1")
	pg.POST("/auth/register", portalH.Register)
	pg.POST("/auth/login", portalH.Login)
	authed := pg.Group("", portalH.JWTMiddleware())
	authed.GET("/devices", portalH.MyDevices)
	authed.POST("/devices/bind", portalH.BindDevice)
	authed.GET("/plans", portalH.Plans)
	authed.POST("/devices/:tenantId/subscribe", portalH.Subscribe)
	authed.POST("/devices/:tenantId/recharge", portalH.CreateOrder)
	authed.POST("/orders/:id/pay-mock", portalH.PayOrderMock)
	authed.GET("/orders", portalH.MyOrders)
	// device-sessions / bulk-import 后续模块接入

	// ---- 404 ----
	r.NoRoute(func(c *gin.Context) {
		web.Abort(c, errs.New(errs.ResourceNotFound, fmt.Sprintf("route not found: %s %s", c.Request.Method, c.Request.URL.Path)))
	})
	r.NoMethod(func(c *gin.Context) {
		web.Abort(c, errs.New(errs.ResourceNotFound, fmt.Sprintf("method not allowed: %s %s", c.Request.Method, c.Request.URL.Path)))
	})
	return r
}
