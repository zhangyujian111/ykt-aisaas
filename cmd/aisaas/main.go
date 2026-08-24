// ykt-aisaas 启动入口。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/rag"
	"ykt.dev/aisaas/internal/server"
	"ykt.dev/aisaas/internal/tenantm/apikey"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "config file path")
	flag.Parse()

	// slog JSON 结构化输出
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("load config", "err", err)
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

	pgPool, err := pgxpool.New(context.Background(), cfg.Postgres.DSN)
	if err != nil {
		slog.Error("connect postgres", "err", err)
		os.Exit(1)
	}
	defer pgPool.Close()
	if _, err := pgPool.Exec(context.Background(), "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		slog.Error("ensure pgvector extension", "err", err)
		os.Exit(1)
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

	router := server.NewRouter(&server.Deps{
		Cfg: cfg, AuthSvc: authSvc, ChatSvc: chatSvc, Registry: registry,
		Quota: guard, Meter: meter, APIKeySvc: keySvc,
		RagRepo: ragRepo, RagSvc: ragSvc, RagIngest: ragIngest, RagRetriever: ragRetriever,
		McpRepo: mcpRepo, McpSvc: mcpSvc,
		BillingSvc: billingSvc, QuotaLoader: quotaLoader,
	})

	srv := &http.Server{
		Addr:    fmtAddr(cfg.Server.Port),
		Handler: router,
	}
	go func() {
		slog.Info("ykt-aisaas listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server exit", "err", err)
			os.Exit(1)
		}
	}()

	// 优雅停机
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	slog.Info("bye")
}

func fmtAddr(port int) string { return ":" + strconv.Itoa(port) }
