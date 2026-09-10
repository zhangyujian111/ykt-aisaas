//go:build integration
// +build integration

// Package integration P0 阶段端到端集成测试（Fix 1-7）。
// 运行：go test -tags=integration -v ./internal/integration/...
package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/portal"
	"ykt.dev/aisaas/internal/server"
	tenantm "ykt.dev/aisaas/internal/tenantm"
	"ykt.dev/aisaas/internal/tenantm/apikey"
)

// testEnv 集成测试环境（真实 Redis + MySQL via testcontainers）。
type testEnv struct {
	t             *testing.T
	redisC        testcontainers.Container
	pgC           testcontainers.Container
	rdb           *redisx.Client
	db            *gorm.DB
	server        *httptest.Server
	meter         *metering.Recorder
	internalToken string
	rs256Pub     *rsa.PublicKey
	rs256Priv    *rsa.PrivateKey
	cleanup       func()
}

// setupTestEnv 启动 testcontainers（Redis + PostgreSQL），装配 ykt-aisaas 服务。
func setupTestEnv(t *testing.T) *testEnv {
	ctx := context.Background()
	env := &testEnv{t: t}

	// ---- Redis ----
	redisC, err := redis.RunContainer(ctx,
		testcontainers.WithImage("redis:7-alpine"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379")),
	)
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	env.redisC = redisC
	redisAddr, err := redisC.Address(ctx)
	if err != nil {
		t.Fatalf("redis addr: %v", err)
	}

	// ---- PostgreSQL (模拟 MySQL 行为，gorm MySQL 不支持 testcontainers) ----
	// 注意：ykt-aisaas 迁移脚本为 MySQL 语法，此处用 PostgreSQL 简化测试。
	// 真实集成测试应使用 MySQL 容器。
	pgC, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("ykt_aisaas"),
		postgres.WithUsername("aisaas"),
		postgres.WithPassword("aisaas"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432")),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	env.pgC = pgC
	pgAddr, err := pgC.Address(ctx)
	if err != nil {
		t.Fatalf("postgres addr: %v", err)
	}

	// ---- Redis Client ----
	env.rdb = redisx.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	if env.rdb == nil {
		t.Fatalf("redis connect failed")
	}

	// ---- PostgreSQL GORM DB ----
	// 使用 gorm postgres driver（简化测试；迁移脚本需调整为 PostgreSQL 语法）
	dsn := fmt.Sprintf("host=%s user=aisaas password=aisaas dbname=ykt_aisaas port=5432 sslmode=disable", extractHost(pgAddr))
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{NoLowerCase: true, SingularTable: true},
	})
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	env.db = db

	// ---- 运行 migrations ----
	if err := runMigrations(ctx, db); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// ---- RSA Key Pair（JWT RS256 测试用）----
	env.rs256Priv, env.rs256Pub, err = generateRSAKeyPair()
	if err != nil {
		t.Fatalf("generate RSA: %v", err)
	}

	// ---- 装配 ykt-aisaas 服务 ----
	env.internalToken = "test-internal-token-" + uuid.NewString()[:8]

	cfg := &config.Config{
		Server: config.Server{
			Port:          0, // random port
			InternalToken: env.internalToken,
		},
		Crypto: config.Crypto{
			AESKey: "dev-aes-key-32-bytes-1234567890a",
		},
		Quota: config.Quota{
			Precheck: true,
		},
		Metering: config.Metering{
			BatchSize:     10,
			FlushSec:      1,
			UseStream:     true,
			StreamMaxLen:  1000,
			ConsumerGroup: "test-metering-group",
			DLQMaxRetries: 3,
		},
		Audit: config.Audit{
			StreamName:    "aisaas:audit:stream",
			BatchSize:     10,
			FlushInterval: 1 * time.Second,
			StreamMaxLen:  1000,
			Region:        "test",
		},
		Redis: config.Redis{
			Addr: redisAddr,
		},
		MySQL: config.MySQL{
			DSN: dsn,
		},
	}

	// 初始化各个服务
	billingSvc := billing.NewService(db)
	registry := llm.NewRegistry(db, cfg.Crypto.AESKey)
	chatSvc := llm.NewChatService(registry)
	guard := quota.NewGuard(env.rdb, cfg.Quota.Precheck)
	meterRec, err := metering.NewRecorder(env.rdb, db, cfg.Metering)
	if err != nil {
		t.Fatalf("metering recorder: %v", err)
	}
	env.meter = meterRec
	deviceTenantSvc := tenantm.NewDeviceTenantService(db, env.rdb)
	portalSvc := portal.NewService(db, billingSvc, env.rdb, env.rs256Pub, env.rs256Priv)
	quotaLoader := billing.NewQuotaLoader(db, env.rdb)

	authSvc := auth.NewService(db, env.rdb, func(ctx context.Context, tid int64) (int8, error) {
		return 1, nil // 租户状态正常
	})

	keySvc := apikey.NewService(apikey.NewRepo(db), authSvc, ids.Next)

	auditRec := audit.NewRecorder(env.rdb, db, cfg.Audit)

	router := server.NewRouter(&server.Deps{
		Cfg: cfg, AuthSvc: authSvc, ChatSvc: chatSvc, Registry: registry,
		Quota: guard, Meter: meterRec, APIKeySvc: keySvc,
		BillingSvc: billingSvc, QuotaLoader: quotaLoader,
		DeviceTenant: deviceTenantSvc, PortalSvc: portalSvc,
		AuditRec: auditRec,
	})

	srv := &httptest.Server{
		Listener: nil,
		Config:   &http.Server{Handler: router},
	}
	srv.Start()
	env.server = srv

	// ---- Cleanup ----
	env.cleanup = func() {
		srv.Close()
		meterRec.Close()
		auditRec.Stop()
		redisC.Terminate(ctx)
		pgC.Terminate(ctx)
	}

	return env
}

// extractHost 从 testcontainers 地址中提取 host
func extractHost(addr string) string {
	// testcontainers 返回格式: 192.168.1.1:5432 或 [::1]:5432
	parts := strings.Split(addr, ":")
	if len(parts) >= 2 {
		port := parts[len(parts)-1]
		host := strings.Join(parts[:len(parts)-1], ":")
		return fmt.Sprintf("%s:%s", host, port)
	}
	return addr
}

// runMigrations 执行数据库 migrations（简化版，仅创建核心表）。
func runMigrations(ctx context.Context, db *gorm.DB) error {
	tables := []string{
		// 租户主表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_tenant (
			id BIGINT PRIMARY KEY, code VARCHAR(64) UNIQUE, name VARCHAR(128),
			tenantType VARCHAR(32) DEFAULT 'NORMAL', deviceId VARCHAR(128) DEFAULT '',
			bindCode VARCHAR(64) DEFAULT '', status TINYINT DEFAULT 1,
			planId BIGINT DEFAULT 0, expireTime DATETIME DEFAULT NULL,
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP, isDeleted TINYINT DEFAULT 0
		)`,
		// API Key 表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_apikey (
			id BIGINT PRIMARY KEY, tenantId BIGINT NOT NULL,
			name VARCHAR(64) NOT NULL, apiKeyHash VARCHAR(128) NOT NULL,
			keyPrefix VARCHAR(16) NOT NULL, scope VARCHAR(512) DEFAULT '[]',
			ipWhitelist VARCHAR(1024) DEFAULT '[]', expiresAt DATETIME DEFAULT NULL,
			lastUsedAt DATETIME DEFAULT NULL, lastUsedIp VARCHAR(64) DEFAULT '',
			status TINYINT DEFAULT 1, createdBy BIGINT DEFAULT 0,
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP, updateTime DATETIME DEFAULT CURRENT_TIMESTAMP,
			isDeleted TINYINT DEFAULT 0
		)`,
		// 用户表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_user (
			id BIGINT PRIMARY KEY, username VARCHAR(64) UNIQUE, password VARCHAR(256),
			nickname VARCHAR(128) DEFAULT '', phone VARCHAR(32) DEFAULT '',
			status TINYINT DEFAULT 1, lastLoginTime DATETIME DEFAULT NULL,
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP, isDeleted TINYINT DEFAULT 0
		)`,
		// 用户设备绑定表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_user_device (
			id BIGINT PRIMARY KEY, userId BIGINT NOT NULL, deviceId VARCHAR(128) NOT NULL,
			tenantId BIGINT NOT NULL, bindName VARCHAR(128) DEFAULT '',
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 配额表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_quota (
			id BIGINT PRIMARY KEY, tenantId BIGINT NOT NULL,
			periodStart DATE NOT NULL, periodEnd DATE NOT NULL,
			dimension VARCHAR(32) NOT NULL, limitValue BIGINT DEFAULT 0,
			usedValue BIGINT DEFAULT 0, overagePolicy VARCHAR(16) DEFAULT 'reject',
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP, updateTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 计量明细表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_usage_detail (
			id BIGINT PRIMARY KEY, tenantId BIGINT NOT NULL, apiKeyId BIGINT DEFAULT 0,
			bizType VARCHAR(32) NOT NULL, dimension VARCHAR(32) NOT NULL,
			amount BIGINT NOT NULL, costCents BIGINT DEFAULT 0,
			modelId VARCHAR(64) DEFAULT '', requestId VARCHAR(64) DEFAULT '',
			status TINYINT DEFAULT 1, createTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 审计日志表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_audit_log (
			eventId VARCHAR(36) PRIMARY KEY, tenantId BIGINT NOT NULL,
			actorType VARCHAR(16) NOT NULL, actorId VARCHAR(128) NOT NULL,
			actorIp VARCHAR(45) DEFAULT NULL, action VARCHAR(64) NOT NULL,
			actionDetail JSON DEFAULT '{}', resourceType VARCHAR(32) DEFAULT NULL,
			resourceId VARCHAR(128) DEFAULT NULL, result VARCHAR(16) NOT NULL,
			errorCode VARCHAR(32) DEFAULT NULL, errorMessage VARCHAR(512) DEFAULT NULL,
			eventTime DATETIME(6) NOT NULL, traceId VARCHAR(64) DEFAULT NULL,
			spanId VARCHAR(32) DEFAULT NULL, parentSpanId VARCHAR(32) DEFAULT NULL,
			region VARCHAR(32) DEFAULT NULL, extra JSON DEFAULT '{}'
		)`,
		// 计量死信队列表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_metering_dlq (
			id BIGSERIAL PRIMARY KEY, taskId VARCHAR(64) NOT NULL,
			payload TEXT NOT NULL, retryCount INT DEFAULT 0,
			lastError VARCHAR(512) DEFAULT '', createdAt DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 余额表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_balance (
			id BIGINT PRIMARY KEY, tenantId BIGINT UNIQUE NOT NULL,
			balanceCents BIGINT DEFAULT 0, frozenCents BIGINT DEFAULT 0,
			totalRecharged BIGINT DEFAULT 0, totalConsumed BIGINT DEFAULT 0,
			version INT DEFAULT 0, updateTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 充值订单表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_recharge_order (
			id BIGINT PRIMARY KEY, orderNo VARCHAR(64) UNIQUE NOT NULL,
			tenantId BIGINT NOT NULL, userId BIGINT NOT NULL,
			amountCents BIGINT NOT NULL, payMethod VARCHAR(32) DEFAULT '',
			status TINYINT DEFAULT 0, paidTime DATETIME DEFAULT NULL,
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 套餐表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_plan (
			id BIGINT PRIMARY KEY, code VARCHAR(64) UNIQUE NOT NULL,
			name VARCHAR(128) NOT NULL, quotas TEXT DEFAULT '{}',
			priceMonthly DECIMAL(10,2) DEFAULT 0, status TINYINT DEFAULT 1,
			createTime DATETIME DEFAULT CURRENT_TIMESTAMP, isDeleted TINYINT DEFAULT 0
		)`,
		// 内部租户配置表
		`CREATE TABLE IF NOT EXISTS ykt_aisaas_internal_tenant_config (
			id BIGINT PRIMARY KEY, tenantId BIGINT UNIQUE NOT NULL,
			tenantType VARCHAR(32) NOT NULL, isUnlimited TINYINT DEFAULT 1,
			rateLimitOverride VARCHAR(512) DEFAULT '{}', internalApiKey VARCHAR(64) NOT NULL,
			webhookUrl VARCHAR(512) DEFAULT NULL, metadata VARCHAR(1024) DEFAULT '{}',
			status TINYINT DEFAULT 1, createTime DATETIME DEFAULT CURRENT_TIMESTAMP,
			updateTime DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, sql := range tables {
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}

// generateRSAKeyPair 生成 RSA 2048 密钥对（用于 JWT RS256 测试）。
func generateRSAKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	return priv, &priv.PublicKey, nil
}

// RS256PublicKeyPEM 获取 RSA Public Key PEM（用于直接验证 JWT）。
func (e *testEnv) RS256PublicKeyPEM() string {
	pubASN1, err := x509.MarshalPKIXPublicKey(e.rs256Pub)
	if err != nil {
		return ""
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	})
	return string(pubPEM)
}

// doRequest 通用 HTTP 请求封装。
func doRequest(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string, body any) (*http.Response, []byte) {
	var bodyReader *strings.Reader
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(bodyBytes))
	} else {
		bodyReader = strings.NewReader("")
	}

	req, err := http.NewRequest(method, srv.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

// newAPIKey 创建 API Key 并返回 plaintext + keyID。
func newAPIKey(t *testing.T, env *testEnv, tenantID int64, scopes []string) (plaintext string, keyID int64) {
	reqBody := map[string]any{
		"name":   "test-key-" + uuid.NewString()[:8],
		"scope":  scopes,
	}
	resp, body := doRequest(t, env.server, "POST",
		fmt.Sprintf("/internal/api/v1/tenants/%d/apikeys", tenantID),
		map[string]string{
			"X-Internal-Token": env.internalToken,
		},
		reqBody,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create apikey failed: %s", string(body))
	}

	var result struct {
		Data struct {
			ID      int64  `json:"id"`
			APIKey  string `json:"apiKey"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data.APIKey, result.Data.ID
}

// mustParseUUIDv7 验证 UUID v7（版本位 = 7）。
func mustParseUUIDv7(t *testing.T, s string) {
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("invalid UUID: %s, err: %v", s, err)
	}
	version := int(id.Version())
	if version != 7 {
		t.Errorf("expected UUID v7, got v%d: %s", version, s)
	}
}

// createTestTenant 创建测试租户。
func createTestTenant(t *testing.T, env *testEnv) int64 {
	tid := int64(ids.Next())
	err := env.db.Exec(`INSERT INTO ykt_aisaas_tenant (id, code, name, tenantType, status)
		VALUES (?, ?, ?, 'NORMAL', 1) ON DUPLICATE KEY UPDATE id=id`,
		tid, "test-tenant-"+uuid.NewString()[:8], "Test Tenant").Error
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	// 创建配额记录
	err = env.db.Exec(`INSERT INTO ykt_aisaas_quota
		(id, tenantId, periodStart, periodEnd, dimension, limitValue)
		VALUES (?, ?, CURRENT_DATE, LAST_DAY(CURRENT_DATE) + INTERVAL 1 DAY, 'llm_tokens_in', 1000000)`,
		int64(ids.Next()), tid).Error
	if err != nil {
		t.Fatalf("create quota: %v", err)
	}
	// 设置 Redis 配额限制
	ym := time.Now().Format("200601")
	env.rdb.Set(context.Background(), redisx.KeyQuotaLimit(tid, "llm_tokens_in", ym), 1000000, 35*24*time.Hour)
	return tid
}

// createTestPortalUser 创建门户测试用户（bcrypt 哈希后的密码）。
func createTestPortalUser(t *testing.T, env *testEnv, username, password string) *portal.UserDO {
	// 简化：直接插入 bcrypt hash
	// bcrypt hash for "password123": $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.R8xTLT1KCWmhqkBCRC
	hash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.R8xTLT1KCWmhqkBCRC"
	u := &portal.UserDO{
		ID:       ids.Next(),
		Username: username,
		Password: hash,
		Nickname: username,
		Status:   1,
	}
	err := env.db.Exec(`INSERT INTO ykt_aisaas_user (id, username, password, nickname, status)
		VALUES (?, ?, ?, ?, 1)`,
		u.ID, u.Username, u.Password, u.Nickname).Error
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// loginPortalUser 登录门户用户并返回 tokens。
func loginPortalUser(t *testing.T, env *testEnv, username, password string) (accessToken, refreshToken string) {
	resp, body := doRequest(t, env.server, "POST", "/portal/api/v1/auth/login", nil, map[string]any{
		"username": username,
		"password": password,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %s", string(body))
	}

	var result struct {
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data.AccessToken, result.Data.RefreshToken
}

// getAPIKeyList 获取租户的 API Key 列表。
func getAPIKeyList(t *testing.T, env *testEnv, token string) []map[string]any {
	resp, body := doRequest(t, env.server, "GET", "/api/v1/apikeys",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list apikeys failed: %s", string(body))
	}

	var result struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data
}

// revokeAPIKeyByInternal 通过内部接口撤销 API Key。
func revokeAPIKeyByInternal(t *testing.T, env *testEnv, keyID int64) {
	resp, body := doRequest(t, env.server, "POST",
		fmt.Sprintf("/internal/api/v1/apikeys/%d/revoke", keyID),
		map[string]string{"X-Internal-Token": env.internalToken}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke apikey failed: %s", string(body))
	}
}

// waitForAuditLog 等待审计日志写入。
func waitForAuditLog(t *testing.T, env *testEnv, action string, timeout time.Duration) *audit.AuditEntry {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for audit log: action=%s", action)
		default:
			var entries []audit.AuditEntry
			env.db.Session(&gorm.Session{SkipHooks: true}).
				Table("ykt_aisaas_audit_log").
				Where("action = ?", action).
				Find(&entries)
			if len(entries) > 0 {
				return &entries[0]
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// waitForMeteringRecords 等待计量记录写入。
func waitForMeteringRecords(t *testing.T, env *testEnv, tenantID int64, expected int, timeout time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for metering records")
		default:
			var count int64
			env.db.Session(&gorm.Session{SkipHooks: true}).
				Table("ykt_aisaas_usage_detail").
				Where("tenantId = ?", tenantID).
				Count(&count)
			if int(count) >= expected {
				return int(count)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// assertResponseStatus 断言响应状态码。
func assertResponseStatus(t *testing.T, resp *http.Response, expected int) {
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}
