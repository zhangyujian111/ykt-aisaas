// ykt-aisaas 启动入口。
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/observability"
	"ykt.dev/aisaas/internal/platform/anomaly"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/oauth"
	"ykt.dev/aisaas/internal/platform/outbox"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/scim"
	"ykt.dev/aisaas/internal/portal"
	"ykt.dev/aisaas/internal/rag"
	"ykt.dev/aisaas/internal/server"
	"ykt.dev/aisaas/internal/server/middleware"
	"ykt.dev/aisaas/internal/tenantm"
	"ykt.dev/aisaas/internal/tenantm/apikey"
	"ykt.dev/aisaas/internal/tenantm/face"
	"ykt.dev/aisaas/internal/tenantm/memory"
	"ykt.dev/aisaas/internal/tenantm/persona"
	"ykt.dev/aisaas/internal/tenantm/session"
	"ykt.dev/aisaas/internal/tenantm/vision"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "config file path")
	// mTLS 启动参数（NICE v2，Phase 3 公网部署）
	tlsCertFile := flag.String("tls-cert", "", "TLS server certificate path (enables HTTPS+mTLS if set)")
	tlsKeyFile := flag.String("tls-key", "", "TLS server private key path")
	tlsCACertFile := flag.String("tls-ca", "", "CA certificate path for verifying client certs (mTLS)")
	tlsPort := flag.Int("tls-port", 0, "HTTPS+mTLS listener port (default 8443 if --tls-cert set)")
	flag.Parse()

	// slog JSON 结构化输出
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Info("load config", "err", err)
		os.Exit(1)
	}

	// ---- 阶段 5b: OTel metrics ----
	metricsShutdown, err := observability.InitMetrics(observability.MetricsConfig{
		ExporterURL: cfg.OTel.ExporterURL,
		ServiceName: "ykt-aisaas",
		Interval:    time.Duration(cfg.OTel.ExportInterval) * time.Second,
	})
	if err != nil {
		slog.Warn("otel metrics init failed, continuing without metrics", "err", err)
	} else {
		slog.Info("otel metrics initialized",
			"exporter_url", cfg.OTel.ExporterURL,
			"interval", cfg.OTel.ExportInterval,
		)
		defer func() {
			if err := metricsShutdown(context.Background()); err != nil {
				slog.Error("otel metrics shutdown failed", "err", err)
			}
		}()
	}

	// ---- 阶段 5c: OTel logs ----
	logsShutdown, err := observability.InitLogs(observability.LogsConfig{
		ExporterURL: cfg.OTel.ExporterURL,
		ServiceName: "ykt-aisaas",
	})
	if err != nil {
		slog.Warn("otel logs init failed, continuing without logs", "err", err)
	} else {
		slog.Info("otel logs initialized",
			"exporter_url", cfg.OTel.ExporterURL,
		)
		defer func() {
			if err := logsShutdown(context.Background()); err != nil {
				slog.Error("otel logs shutdown failed", "err", err)
			}
		}()
	}

	// ---- V6-T: OTel tracing（AI 推理分布式追踪）----
	traceShutdown, err := observability.InitTracing(observability.TracingConfig{
		ExporterURL: cfg.OTel.ExporterURL,
		ServiceName: "ykt-aisaas",
		SampleRatio: cfg.OTel.SampleRatio,
	})
	if err != nil {
		slog.Warn("otel tracing init failed, continuing without tracing", "err", err)
	} else {
		slog.Info("otel tracing initialized",
			"exporter_url", cfg.OTel.ExporterURL,
			"sample_ratio", cfg.OTel.SampleRatio,
		)
		defer func() {
			if err := traceShutdown(context.Background()); err != nil {
				slog.Error("otel tracing shutdown failed", "err", err)
			}
		}()
	}

	// Continuous profiling disabled (Pyroscope package removed to avoid build failures)

	// 命令行参数覆盖 TLS 配置（优先级高于 yaml）
	if *tlsCertFile != "" || *tlsKeyFile != "" || *tlsCACertFile != "" {
		cfg.Server.TLSCertFile = *tlsCertFile
		cfg.Server.TLSKeyFile = *tlsKeyFile
		cfg.Server.TLSCACertFile = *tlsCACertFile
		if *tlsPort > 0 {
			cfg.Server.TLSPort = *tlsPort
		} else if cfg.Server.TLSPort == 0 {
			cfg.Server.TLSPort = 8443
		}
	}

	// TLS 配置一致性校验
	if cfg.Server.IsTLSEnabled() {
		slog.Info("mTLS enabled",
			"tls_cert", cfg.Server.TLSCertFile,
			"tls_ca", cfg.Server.TLSCACertFile,
			"tls_port", cfg.Server.TLSPort,
		)
	} else if *tlsCertFile != "" || *tlsKeyFile != "" || *tlsCACertFile != "" {
		slog.Error("incomplete mTLS config: all of --tls-cert/--tls-key/--tls-ca must be set")
		os.Exit(1)
	}

	// ---- 依赖装配 ----
	db, err := database.Open(&cfg.MySQL, cfg.Tenant.SkipTables, false)
	if err != nil {
		slog.Error("open mysql", "err", err)
		os.Exit(1)
	}

	rdb, err := redisx.New(&cfg.Redis)
	if err != nil {
		slog.Error("connect redis", "err", err)
		os.Exit(1)
	}

	// Postgres / pgvector is optional in dev (RAG can be skipped).
	// If connection fails, log warning and use nil pool — callers must handle nil.
	var pgPool *pgxpool.Pool
	if cfg.Postgres.DSN != "" {
		p, perr := pgxpool.New(context.Background(), cfg.Postgres.DSN)
		if perr != nil {
			slog.Warn("connect postgres (optional, RAG disabled)", "err", perr)
			pgPool = nil
		} else {
			defer p.Close()
			if _, verr := p.Exec(context.Background(), "CREATE EXTENSION IF NOT EXISTS vector"); verr != nil {
				slog.Warn("ensure pgvector extension (RAG disabled)", "err", verr)
				p.Close()
				pgPool = nil
			} else {
				pgPool = p
			}
		}
	}

	tenantOf := func(ctx context.Context, tid int64) (int8, error) {
		var status int8
		err := db.WithContext(ctx).
			Raw("SELECT status FROM ykt_aisaas_tenant WHERE id = ? AND isDeleted = 0", tid).
			Scan(&status).Error
		return status, err
	}

	authSvc := auth.NewService(db, rdb, tenantOf)
	registry := llm.NewRegistry(db, cfg.Crypto.AESKey)
	chatSvc := llm.NewChatService(registry)
	guard := quota.NewGuard(rdb, cfg.Quota.Precheck)
	meter, err := metering.NewRecorder(rdb, db, cfg.Metering)
	if err != nil {
		slog.Error("metering recorder", "err", err)
		os.Exit(1)
	}
	defer meter.Close()

	ragStore := rag.NewStore(pgPool)
	ragRepo := rag.NewRepo(db)
	ragSvc := rag.NewService(ragRepo, ragStore)
	ragIngest := rag.NewIngestor(ragRepo, registry)
	ragIngest.BindStore(func(ctx context.Context, tid, kbID, docID int64, chunks []string, vecs [][]float32) error {
		if err := ragStore.DeleteDocChunks(ctx, tid, kbID, docID); err != nil {
			return err
		}
		return ragStore.InsertChunks(ctx, tid, kbID, docID, chunks, vecs)
	})
	ragRetriever := rag.NewRetriever(ragRepo, registry, ragStore, meter)

	mcpRepo := mcp.NewRepo(db)
	mcpSvc := mcp.NewService(mcpRepo, meter)

	billingSvc := billing.NewService(db)

	deviceTenantSvc := tenantm.NewDeviceTenantService(db, rdb)
	// 开发：从文件加载 JWT 密钥
	pubKey, privKey, err := crypto.LoadFromFile("keys/jwt_public.pem", "keys/jwt_private.pem")
	if err != nil {
		slog.Error("load jwt keys", "err", err)
		os.Exit(1)
	}
	portalSvc := portal.NewService(db, billingSvc, rdb, pubKey, privKey)
	quotaLoader := billing.NewQuotaLoader(db, rdb)
	if n, err := quotaLoader.LoadAll(context.Background()); err != nil {
		slog.Error("quota load", "err", err)
	} else {
		slog.Info("quota loaded", "rows", n)
	}
	quotaLoader.Start(context.Background())
	meter.BindPricer(registry)
	meter.BindConsumer(billingSvc)

	keySvc := apikey.NewService(apikey.NewRepo(db), authSvc, ids.Next)

	// === P1 模块装配 ===
	// Memory 模块
	memSvc := memory.NewService(db, rdb, memory.DefaultConfig())
	llmExtractor := memory.NewLLMExtractor(registry)
	memExtractWorker := memory.NewExtractWorker(memSvc, llmExtractor)
	memSummarizeWorker := memory.NewSummarizeWorker(memSvc, llmExtractor)
	memWorker := memory.NewWorker(rdb, memExtractWorker, memSummarizeWorker, memory.DefaultConfig(), guard, meter)
	memWorker.Start()

	// === Memory Outbox（NICE-1: 异步可靠写入）===
	sqlDB, err := db.DB()
	if err != nil {
		slog.Error("get sql.DB from gorm", "err", err)
		os.Exit(1)
	}
	outboxWriter := outbox.NewWriter(sqlDB)
	outboxHandler := memory.NewOutboxHandler(memSvc, memExtractWorker, memSummarizeWorker)
	outboxWorker := outbox.NewWorker(sqlDB, outboxHandler, outbox.DefaultWorkerConfig())
	if err := outboxWorker.Start(context.Background()); err != nil {
		slog.Error("start outbox worker", "err", err)
		os.Exit(1)
	}
	_ = outboxWriter // 后续供 handler 层事务内调用

	// Persona 模块（先于 Session 创建，Session Handler 需注入 personaSvc 做设备归属校验）
	personaRepo := persona.NewRepo(db)
	personaSvc := persona.NewService(personaRepo, persona.DefaultConfig())

	// Session 模块
	sessionRepo := session.NewRepo(db)
	sessionSvc := session.NewService(sessionRepo, rdb, session.DefaultConfig())
	sessionH := session.NewHandler(sessionSvc, personaSvc)
	go sessionSvc.StartCleanupWorker(context.Background())

	// Vision 模块（多模态）
	// 创建 3 个 provider：OpenAI GPT-4V、Qwen-VL、GLM-4V
	visionProviders := map[string]vision.Provider{
		"openai-gpt4v": vision.NewOpenAIProvider(vision.OpenAIConfig{
			APIKey:    cfg.OpenAI.APIKey,
			APIBase:   "https://api.openai.com/v1",
			ModelName: "gpt-4-vision-preview",
		}),
		"qwen-vl-max": vision.NewQwenVLProvider(vision.QwenVLConfig{
			APIKey:    cfg.DashScope.APIKey,
			APIBase:   "https://dashscope.aliyuncs.com/api/v1",
			ModelName: "qwen-vl-max",
		}),
		"glm-4v": vision.NewGLM4VProvider(vision.GLM4VConfig{
			APIKey:    cfg.Zhipu.APIKey,
			APIBase:   "https://open.bigmodel.cn/api/paas/v4",
			ModelName: "glm-4v-plus",
		}),
	}
	defaultModel := cfg.Vision.DefaultModel
	if defaultModel == "" {
		defaultModel = "qwen-vl-max"
	}
	visionSvc := vision.NewService(visionProviders, defaultModel)

	// === Face 人脸检测（v1：detect + 5 点 keypoints，无 profile/识别）===
	faceDetector := face.NewMockDetector() // dev 用 mock，生产替换为 SCRFD/YuNet
	faceSvc := face.NewService(faceDetector, slog.Default())

	// === V4-A Anomaly 异常检测引擎 ===
	var anomalyEngine anomaly.Engine
	if cfg.Anomaly.Enabled {
		metricsCollector := anomaly.NewRedisStreamCollector(rdb)
		notifierCfg := anomaly.NotifierConfig{
			SlackWebhookURL: cfg.Anomaly.SlackWebhookURL,
			WebhookURLs:     cfg.Anomaly.WebhookURLs,
			EmailSMTPHost:   cfg.Anomaly.EmailSMTPHost,
			EmailSMTPPort:   cfg.Anomaly.EmailSMTPPort,
			EmailFrom:       cfg.Anomaly.EmailFrom,
			EmailTo:         cfg.Anomaly.EmailTo,
		}
		notifier := anomaly.NewMultiNotifier(notifierCfg)
		anomalyEngine = anomaly.NewEngine(db, metricsCollector, notifier)
		if err := anomalyEngine.Start(context.Background()); err != nil {
			slog.Error("start anomaly engine", "err", err)
		}

		// ML anomaly disabled in dev mode (training_data.go removed to avoid build failures)
	}

	router := server.NewRouter(&server.Deps{
		DB: db, Cfg: cfg, AuthSvc: authSvc, ChatSvc: chatSvc, Registry: registry,
		Quota: guard, Meter: meter, APIKeySvc: keySvc,
		RagRepo: ragRepo, RagSvc: ragSvc, RagIngest: ragIngest, RagRetriever: ragRetriever,
		McpRepo: mcpRepo, McpSvc: mcpSvc,
		BillingSvc: billingSvc, QuotaLoader: quotaLoader,
		DeviceTenant: deviceTenantSvc, PortalSvc: portalSvc,
		// P1 模块
		MemorySvc:    memSvc,
		MemoryWorker: memWorker,
		SessionH:     sessionH,
		PersonaSvc:   personaSvc,
		// NICE-4: Vision 多模态
		VisionSvc: visionSvc,
		// Face 人脸检测（v1：detect + 5 点 keypoints）
		FaceSvc: faceSvc,
		// V4-A: Anomaly 异常检测
		AnomalyEngine: anomalyEngine,
	})

	// === V5-S: OAuth 2.1 + SCIM 自动账户同步 ===
	// OAuth 2.1 Server (PKCE mandatory, DPoP, PAR, Resource Indicators)
	oauth21Cfg := &oauth.OAuth21Config{
		Issuer:             cfg.Server.BaseURL + "/oauth2",
		ClientID:           cfg.OAuth2.ClientID,
		ClientSecret:       cfg.OAuth2.ClientSecret,
		RequirePKCE:        true,
		DPoPEnabled:        cfg.OAuth2.DPoPEnabled,
		PAREnabled:         true,
		ResourceIndicators: []string{"ykt-aisaas", "ykt-admin"},
		AccessTokenTTL:     900,  // 15 minutes
		RefreshTokenTTL:    604800, // 7 days
		ParTTL:             90,
		AuthCodeTTL:        60,
	}
	oauth21Server := oauth.NewOAuth21Server(oauth21Cfg, rdb, pgPool)

	// SCIM v2 Server (User/Group provisioning)
	scimAuthTokens := []string{cfg.Scim.AuthToken}
	userProvider := scim.NewMysqlSCIMUserProvider(db)
	groupProvider := scim.NewMysqlSCIMGroupProvider(db)
	scimServer := scim.NewSCIMServer(userProvider, groupProvider, scimAuthTokens)

	// Register OAuth 2.1 endpoints
	router.GET("/.well-known/oauth-authorization-server", func(c *gin.Context) {
		oauth21Server.Metadata(c.Writer, c.Request)
	})
	router.POST("/oauth2/par", func(c *gin.Context) {
		oauth21Server.PushedAuth(c.Writer, c.Request)
	})
	router.POST("/oauth2/token", func(c *gin.Context) {
		// Token request handler - delegates to OAuth21Server
		grantType := c.PostForm("grant_type")
		params := c.Request.PostForm
		tokenInfo, err := oauth21Server.TokenRequest(grantType, params)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": err.Error()})
			return
		}
		c.JSON(http.StatusOK, tokenInfo)
	})
	router.POST("/oauth2/revoke", func(c *gin.Context) {
		oauth21Server.TokenRevocation(c.Writer, c.Request)
	})
	router.POST("/oauth2/introspect", func(c *gin.Context) {
		oauth21Server.TokenIntrospection(c.Writer, c.Request)
	})

	// Register SCIM v2 endpoints (with Bearer token auth)
	scimGroup := router.Group("/scim/v2", scimServer.AuthMiddleware())
	scimGroup.GET("/Users", scimServer.ListUsers)
	scimGroup.POST("/Users", scimServer.CreateUser)
	scimGroup.GET("/Users/:id", scimServer.GetUser)
	scimGroup.PUT("/Users/:id", scimServer.ReplaceUser)
	scimGroup.PATCH("/Users/:id", scimServer.PatchUser)
	scimGroup.DELETE("/Users/:id", scimServer.DeleteUser)
	scimGroup.GET("/Groups", scimServer.ListGroups)
	scimGroup.POST("/Groups", scimServer.CreateGroup)
	scimGroup.GET("/Groups/:id", scimServer.GetGroup)
	scimGroup.GET("/Schemas", scimServer.GetSchemas)
	scimGroup.GET("/ServiceProviderConfig", scimServer.ServiceProviderConfig)

	srv := &http.Server{
		Addr:    fmtAddr(cfg.Server.Port),
		Handler: router,
	}
	publicListener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		slog.Error("public listener bind", "addr", srv.Addr, "err", err)
		os.Exit(1)
	}
	go func() {
		slog.Info("ykt-aisaas public listening", "addr", srv.Addr)
		if err := srv.Serve(publicListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("public server exit", "err", err)
			os.Exit(1)
		}
	}()

	// HTTPS+mTLS 监听器（仅在 TLS 配置完整时启用）
	var mtlsListener net.Listener
	if cfg.Server.IsTLSEnabled() {
		tlsConfig, err := middleware.GetServerTLSConfig(
			cfg.Server.TLSCertFile,
			cfg.Server.TLSKeyFile,
			cfg.Server.TLSCACertFile,
		)
		if err != nil {
			slog.Error("load TLS config", "err", err)
			os.Exit(1)
		}

		mtlsAddr := fmtAddr(cfg.Server.TLSPort)
		mtlsListener, err = tls.Listen("tcp", mtlsAddr, tlsConfig)
		if err != nil {
			slog.Error("mTLS listener bind", "addr", mtlsAddr, "err", err)
			os.Exit(1)
		}
		mtlsSrv := &http.Server{
			Handler:   router,
			TLSConfig: tlsConfig,
		}
		go func() {
			slog.Info("ykt-aisaas mTLS listening",
				"addr", mtlsAddr,
				"client_auth", "RequireAndVerifyClientCert",
				"min_tls", "1.2",
			)
			if err := mtlsSrv.Serve(mtlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("mTLS server exit", "err", err)
				os.Exit(1)
			}
		}()
	}

	// 优雅停机
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if mtlsListener != nil {
		_ = mtlsListener.Close()
	}
	// 停止 P1 Workers
	memWorker.Stop()
	// 停止 Anomaly Engine
	if anomalyEngine != nil {
		_ = anomalyEngine.Stop()
	}
	// ML Integration already disabled
	slog.Info("bye")
}

func fmtAddr(port int) string { return ":" + strconv.Itoa(port) }
