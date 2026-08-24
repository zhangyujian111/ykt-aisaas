// Package server 路由装配。
package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/asr"
	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/rag"
	"ykt.dev/aisaas/internal/server/apiv1"
	"ykt.dev/aisaas/internal/server/internalapi"
	v1 "ykt.dev/aisaas/internal/server/v1"
	"ykt.dev/aisaas/internal/tenantm/apikey"
	"ykt.dev/aisaas/internal/tts"
)

// Deps 装配依赖。
type Deps struct {
	Cfg          *config.Config
	AuthSvc      *auth.Service
	ChatSvc      *llm.ChatService
	Registry     *llm.Registry
	Quota        *quota.Guard
	Meter        *metering.Recorder
	APIKeySvc    *apikey.Service
	RagRepo      *rag.Repo
	RagSvc       *rag.Service
	RagIngest    *rag.Ingestor
	RagRetriever *rag.Retriever
	McpRepo      *mcp.Repo
	McpSvc       *mcp.Service
	BillingSvc   *billing.Service
	QuotaLoader  *billing.QuotaLoader
}

// NewRouter 装配全部路由。
func NewRouter(d *Deps) *gin.Engine {
	r := web.NewEngine()
	r.Use(web.RequestIDMiddleware())

	// ---- 公共 ----
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "ykt-aisaas", "version": "0.1.0"})
	})

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

	// ---- 内部超级租户（X-Internal-Token + loopback）----
	intg := r.Group("/internal/api/v1", auth.InternalMiddleware(d.Cfg.Server.InternalToken))
	intg.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pong": true, "internal": true})
	})
	intH := &internalapi.Handler{KeySvc: d.APIKeySvc, Billing: d.BillingSvc}
	intg.POST("/tenants/:tenantId/apikeys", intH.IssueAPIKey)
	intg.POST("/tenants/:tenantId/recharge", intH.Recharge)
	// device-sessions / bulk-import 后续模块接入

	// ---- 404 ----
	r.NoRoute(func(c *gin.Context) {
		web.Abort(c, fmt.Errorf("route not found: %s %s", c.Request.Method, c.Request.URL.Path))
	})
	return r
}
