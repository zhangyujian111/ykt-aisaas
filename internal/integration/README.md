# ykt-aisaas P0 阶段集成测试

## 概述

本目录包含 ykt-aisaas V2 P0 阶段（7 项 Fix）的端到端集成测试，覆盖核心安全场景。

## 测试覆盖

| Fix | 场景 | 测试用例 |
|-----|------|---------|
| Fix 1 | JWT RS256 | `TestPortal_Login_RS256_Token`, `TestPortal_ParseToken_RejectsHS256`, `TestPortal_ParseToken_RejectsNone`, `TestPortal_LoginRateLimit_5Failures_Locks15Min` |
| Fix 2+7 | API Key Rotate/Revoke | `TestAPIKey_Rotate_Success_5MinGracePeriod`, `TestAPIKey_RevokeByInternal_BypassesTenant`, `TestAPIKey_Rotate_NotLeakOldKey` |
| Fix 3 | Audit Log | `TestAudit_Record_RedisStream_ThenConsumerWrite`, `TestAudit_RedactValue_Comprehensive`, `TestAudit_SamplingRate_1Percent_QuotaCheck` |
| Fix 4 | Metering Stream | `TestMetering_XADD_Success_NoChannelBlock`, `TestMetering_XREADGROUP_Consumes`, `TestMetering_DLQ_OnFailure`, `TestMetering_FallbackChannel_XADDFailure` |
| Fix 5 | Quota Refund | `TestQuota_Refund_ActualTokens`, `TestChat_RefundOnFailure`, `TestChat_RefundOnCancel_Stream`, `TestChat_RefundAsync_NonBlocking` |
| Fix 6 | Quota Snapshot Lua | `TestQuotaSnapshot_Deduct_Atomic`, `TestQuotaSnapshot_Refund_ActualUsed`, `TestQuotaSnapshot_ConcurrentSafety` |
| E2E | End-to-End | `TestIntegration_End2End_XiaozhiBoot`, `TestIntegration_QuotaFlow_E2E`, `TestIntegration_MeteringAndAudit_E2E` |

## 测试运行

### 前置条件

- Go 1.21+
- Docker（用于启动 testcontainers）

### 运行所有测试

```bash
cd ykt-aisaas
go test -tags=integration -v ./internal/integration/...
```

### 运行特定测试

```bash
# 运行 Fix 1 测试
go test -tags=integration -v -run TestPortal_ ./internal/integration/

# 运行 Fix 6 并发安全测试
go test -tags=integration -v -run TestQuotaSnapshot_ConcurrentSafety ./internal/integration/
```

### 运行 E2E 测试

```bash
go test -tags=integration -v -run TestIntegration_ ./internal/integration/
```

## 测试架构

### 技术栈

- **框架**: Go 标准 `testing` + `testcontainers-go`
- **容器**: Redis 7 + PostgreSQL 15（模拟 MySQL）
- **真实依赖**: 无 Mock，所有外部依赖使用真实容器

### 文件结构

```
internal/integration/
├── helper_test.go              # 测试环境 setup + 工具函数
├── p0_fix1_jwt_test.go        # Fix 1: JWT RS256
├── p0_fix2_apikey_test.go     # Fix 2+7: API Key Rotate/Revoke
├── p0_fix3_audit_test.go     # Fix 3: Audit Log
├── p0_fix4_metering_test.go   # Fix 4: Metering Stream
├── p0_fix5_quota_test.go      # Fix 5: Quota Refund
├── p0_fix6_snapshot_test.go   # Fix 6: Quota Snapshot Lua
├── p0_e2e_test.go             # 端到端流程测试
└── README.md                   # 本文件
```

### testEnv 结构

```go
type testEnv struct {
    t             *testing.T
    redisC        testcontainers.Container  // Redis 容器
    pgC           testcontainers.Container  // PostgreSQL 容器
    rdb           *redisx.Client           // Redis 客户端
    db            *gorm.DB                 // PostgreSQL GORM 客户端
    server        *httptest.Server         // ykt-aisaas HTTP 服务
    meter         *metering.Recorder       // 计量记录器
    internalToken string                   // 内部 API Token
    rs256Pub     *rsa.PublicKey           // JWT RS256 公钥
    rs256Priv    *rsa.PrivateKey          // JWT RS256 私钥
    cleanup       func()                   // 清理函数
}
```

## 关键测试场景

### JWT RS256 (Fix 1)

- **Token 生成**: 使用 RSA 2048 私钥签发 RS256 JWT
- **Token 验证**: 只接受 RS256，拒绝 HS256/none
- **登录限流**: 5 次失败后锁定 15 分钟

### API Key Rotate/Revoke (Fix 2+7)

- **轮换**: 旧 Key 5 分钟宽限期，新 Key 立即生效
- **撤销**: 内部 API 绕过租户隔离，立即失效 + Redis 缓存清除
- **审计**: apikey.rotate / apikey.revoke 写入审计日志

### Audit Log (Fix 3)

- **写入路径**: Record() → Redis Stream → Consumer → DB
- **ID 生成**: UUID v7（时间有序）
- **脱敏**: password / apiKey / token / secret 等字段自动脱敏
- **采样**: quota.check 1% 采样

### Metering Stream (Fix 4)

- **写入**: XADD 到 Redis Stream
- **消费**: XREADGROUP 批量消费 → DB
- **DLQ**: 落库失败 → 3 次重试 → DLQ 表
- **Fallback**: XADD 失败 → channel → 10s 后重试

### Quota Refund (Fix 5)

- **预扣**: 估算 token → QuotaDeduct
- **退款**: 差额退款 → Redis used 计数器减少
- **异步**: handleChatRefund 异步执行，不阻塞响应

### Quota Snapshot Lua (Fix 6)

- **原子性**: Lua 脚本保证并发安全
- **防超卖**: used + amount > limit 时拒绝
- **快照**: session 记录 initial 值，用于退款计算

## 注意事项

1. **测试隔离**: 每个测试创建独立的 testEnv，测试间无共享状态
2. **容器资源**: 测试启动 Redis 和 PostgreSQL 容器，确保有足够资源
3. **运行时间**: 完整测试套件约需 3-5 分钟（包含容器启动）
4. **跳过场景**: 部分测试（如 SSE 流取消）需要完整的上游 mock，标记为 Skip

## 故障排查

### 容器启动失败

```bash
# 检查 Docker 是否运行
docker ps

# 手动拉取镜像
docker pull redis:7-alpine
docker pull postgres:15-alpine
```

### 测试超时

```bash
# 增加超时时间
go test -tags=integration -v -timeout 10m ./internal/integration/...
```

### 查看容器日志

```bash
# 查看 Redis 容器日志
docker logs <redis-container-id>

# 查看 PostgreSQL 容器日志
docker logs <postgres-container-id>
```

## 持续集成

建议在 CI 中运行测试：

```yaml
# .github/workflows/integration.yml
- name: Run Integration Tests
  run: |
    cd ykt-aisaas
    go test -tags=integration -v -race ./internal/integration/...
```
