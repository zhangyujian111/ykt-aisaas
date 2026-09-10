# P0 安全修复清单 — ykt-aisaas V2

> 基于 Q6 鉴权群讨论 + Q8 事务群讨论结论  
> 版本：V2.0 | 日期：2026-09-02 | 状态：待实现

---

## 目录

- [Fix 1：Portal JWT 强制 RS256（Q6.3 决策）](#fix-1portal-jwt-强制-rs256q63-决策)
- [Fix 2：API Key 轮换接口（Q6 决策）](#fix-2api-key-轮换接口q6-决策)
- [Fix 3：审计日志强制写入（Q6 决策）](#fix-3审计日志强制写入q6-决策)
- [Fix 4：Metering 改 Redis Stream（MUST Q8 决策）](#fix-4metering-改-redis-streammust-q8-决策)
- [Fix 5：配额退款接口（MUST Q8 决策）](#fix-5配额退款接口must-q8-决策)
- [Fix 6：quota_snapshot Lua 原子借记（Q8 子议题决策）](#fix-6quota_snapshot-lua-原子借记q8-子议题决策)
- [Fix 7：API Key 紧急撤销（Q6 决策）](#fix-7api-key-紧急撤销q6-决策)
- [跨项验收 Checklist](#跨项验收-checklist)

---

## Fix 1：Portal JWT 强制 RS256（Q6.3 决策）

### 1.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/portal/portal.go:65` | `jwtKey []byte` 明文存储，无 KMS 密钥管理 |
| `internal/portal/portal.go:114` | 使用 `jwt.SigningMethodHS256`，存在密钥泄露 + 算法混淆攻击风险 |
| `internal/portal/portal.go:122-141` | `ParseToken` 仅校验 HMAC，无法防止算法混淆 |

**风险等级**：CRITICAL — HS256 密钥泄露可导致攻击者伪造任意用户身份。

### 1.2 修复点

#### 1.2.1 Service 结构体重构

**文件**：`internal/portal/portal.go`  
**行号**：62-70  
**修改内容**：

```go
// 原结构
type Service struct {
    db      *gorm.DB
    billing *billing.Service
    jwtKey  []byte
}

// 新结构
type Service struct {
    db           *gorm.DB
    billing      *billing.Service
    jwtPublicKey  *rsa.PublicKey   // 新增：公钥验签
    jwtPrivateKey *rsa.PrivateKey  // 新增：私钥签发（仅 Login 使用）
}
```

**构造函数签名变更**：

```go
// 原签名
func NewService(db *gorm.DB, b *billing.Service, jwtKey string) *Service

// 新签名
func NewService(db *gorm.DB, b *billing.Service, jwtPublicKey *rsa.PublicKey, jwtPrivateKey *rsa.PrivateKey) *Service
```

#### 1.2.2 Login 签发改用 RS256

**文件**：`internal/portal/portal.go`  
**行号**：110-118  
**修改内容**：

```go
// 原代码（行 110-118）
claims := jwt.MapClaims{
    "uid": do.ID, "username": do.Username,
    "exp": time.Now().Add(72 * time.Hour).Unix(),
}
t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtKey)

// 新代码
claims := jwt.MapClaims{
    "uid": do.ID, "username": do.Username,
    "exp": time.Now().Add(15 * time.Minute).Unix(),  // 缩短至 15min
    "ref": do.ID % 10000,  // 用于 refresh token 关联
}
// 新增：refresh_token 7d 有效期
refreshClaims := jwt.MapClaims{
    "uid": do.ID, "type": "refresh",
    "exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
}
accessToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.jwtPrivateKey)
refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, refreshClaims).SignedString(s.jwtPrivateKey)
// 返回双 token
```

#### 1.2.3 ParseToken 强制校验 RSA

**文件**：`internal/portal/portal.go`  
**行号**：121-141  
**修改内容**：

```go
// 原代码（行 121-141）
func (s *Service) ParseToken(tokenStr string) (int64, error) {
    tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
        if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("bad alg")
        }
        return s.jwtKey, nil
    })
    // ...
}

// 新代码
func (s *Service) ParseToken(tokenStr string) (int64, error) {
    tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
        if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
        }
        return s.jwtPublicKey, nil
    })
    // ...
}
```

#### 1.2.4 KMS 私钥加载接口（新增）

**文件**：`internal/platform/crypto/kms.go`（新建）  
**职责**：
- 从 KMS 服务加载 RSA 私钥/公钥（支持多 region 独立 KMS key）
- 配置示例：`kms://region-cn-north-1/alias/aisaas-jwt-key`

**接口签名**：

```go
type KMSKeyLoader interface {
    LoadRSAPrivateKey(ctx context.Context, keyID string) (*rsa.PrivateKey, error)
    LoadRSAPublicKey(ctx context.Context, keyID string) (*rsa.PublicKey, error)
}
```

#### 1.2.5 暴力破解防护（新增）

**文件**：`internal/platform/auth/ratelimit.go`（新建或扩展）  
**规则**：
- 同一 IP/用户名 5 次登录失败 → 15min 冷却
- Redis Key：`aisaas:portal:login:fail:{ipOrUsername}`
- TTL：15min

### 1.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | HS256/none 算法 token 被拒绝（返回 `unexpected signing method`） | 单元测试：构造 HS256/none token，验证 ParseToken 报错 |
| 2 | 私钥从 KMS 加载，配置不存明文 | 确认 `portal.NewService` 接收 `*rsa.PrivateKey`，无 `string jwtKey` 参数 |
| 3 | token expiry 改 15min + refresh_token 7d | 检查 Login 返回双 token，验证 exp claim |
| 4 | 5 次失败 → 15min 冷却 | 单元测试：连续 5 次失败后第 6 次被拒绝 |

### 1.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 保留原 `jwtKey []byte` 字段 + `jwt.SigningMethodHS256` 代码路径（注释标注 "V2 rollback"） |
| 2 | 配置开关 `portal.jwtAlgorithm: "HS256" \| "RS256"` |
| 3 | 回滚时将配置改为 `HS256`，重启服务即可恢复 |

### 1.5 依赖关系

- **依赖**：Fix 3（审计日志）— Login 成功/失败需写审计日志
- **被依赖**：无

---

## Fix 2：API Key 轮换接口（Q6 决策）

### 2.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/tenantm/apikey/apikey.go:117-144` | 仅有 Create/List/Disable/Delete，缺 Rotate/Revoke |
| `internal/server/apiv1/apikey.go` | 同上 |

**轮换需求**：24h / 10K 请求触发，宽限期 5min。

### 2.2 修复点

#### 2.2.1 Rotate 接口（新增）

**文件**：`internal/tenantm/apikey/apikey.go`  
**新增 Service 方法**：

```go
// Rotate 轮换 API Key。
// 1. 生成新 Key（同 scope/IP 白名单）
// 2. 旧 Key expiresAt = now + 5min（宽限期）
// 3. 新 Key 立即生效
// 返回：新 Key 明文（仅此一次）
func (s *Service) Rotate(ctx context.Context, id int64) (*CreateResp, error)

// RevokeByInternal 紧急撤销（无需 tenant 上下文，内部通道专用）。
// 1. UpdateStatus(0)
// 2. Redis Del 缓存（立即失效）
// 3. 写审计日志
func (s *Service) RevokeByInternal(ctx context.Context, keyID int64) error
```

#### 2.2.2 Rotate Handler（新增）

**文件**：`internal/server/internalapi/rotate.go`（新建）  
**路由**：`POST /internal/api/v1/apikeys/:id/rotate`  
**路由**：`POST /internal/api/v1/apikeys/:id/revoke`  

**实现逻辑**：

```go
// Rotate
func (h *Handler) RotateAPIKey(c *gin.Context) {
    id, ok := parseIDParam(c, "id")
    if !ok { return }
    resp, err := h.KeySvc.Rotate(c.Request.Context(), id)
    if err != nil { web.Abort(c, err); return }
    web.OK(c, resp)
}

// Revoke
func (h *Handler) RevokeAPIKey(c *gin.Context) {
    id, ok := parseIDParam(c, "id")
    if !ok { return }
    err := h.KeySvc.RevokeByInternal(c.Request.Context(), id)
    if err != nil { web.Abort(c, err); return }
    web.OK(c, gin.H{"revoked": true})
}
```

#### 2.2.3 缓存立即失效

**文件**：`internal/platform/auth/apikey.go`  
**行号**：151-157  
**修改**：确保 `InvalidateCache` 被 Rotate/Revoke 调用。

### 2.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | Rotate 返回新明文 Key（仅此一次） | 集成测试：调用 Rotate，验证返回 `apiKey` 字段非空 |
| 2 | 5min 宽限期内旧 Key 仍可使用 | 单元测试：Rotate 后直接用旧 Key 验签成功 |
| 3 | Revoke 后 0~5s 内旧 Key 失效（Redis Del） | 单元测试：Revoke 后查 Redis 缓存为空 |
| 4 | 全部操作写 audit_log | 检查 audit_log 表记录（见 Fix 3） |

### 2.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 临时启用旧 Key 缓存 TTL（30min → 5min） |
| 2 | 禁止新 Rotate 调用（返回 503） |
| 3 | 数据库手动将 `expiresAt` 恢复为 NULL（永久） |

### 2.5 依赖关系

- **依赖**：Fix 3（审计日志）— Rotate/Revoke 需写审计日志
- **被依赖**：无

---

## Fix 3：审计日志强制写入（Q6 决策）

### 3.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/platform/auth/apikey.go:94-129` | Validate 无审计日志，auth.fail 事故溯源困难 |
| `internal/platform/quota/quota.go:52-66` | PrecheckDim 无审计日志，配额超支无法追溯 |
| `internal/tenantm/apikey/apikey.go:117-144` | Create/Rotate/Revoke 无审计日志 |

### 3.2 修复点

#### 3.2.1 Audit Entry 结构体（新增）

**文件**：`internal/platform/audit/audit.go`（新建）  
**结构体定义**：

```go
type AuditEntry struct {
    EventID       string            // UUID v7
    TenantID      int64             // 租户 ID（0 = 系统级）
    ActorType     string            // apikey/device/user/internal/system
    ActorID       string            // 操作者 ID
    ActorIP       string            // 来源 IP
    Action        string            // auth.success | auth.fail | quota.check | apikey.create | apikey.rotate | apikey.revoke
    ActionDetail  map[string]any    // 扩展字段（脱敏）
    ResourceType  string            // apikey | quota | tenant | session
    ResourceID    string            // 资源 ID
    Result        string            // success | failure | partial
    ErrorCode     string            // 错误码
    ErrorMessage  string            // 错误消息（敏感字段 [REDACTED]）
    EventTime     time.Time         // 事件时间
    TraceID       string            // OpenTelemetry trace ID
    SpanID        string            // OpenTelemetry span ID
    ParentSpanID  string            // OpenTelemetry parent span ID
    Region        string            // Region 标识
}

func Record(ctx context.Context, entry AuditEntry)
```

#### 3.2.2 写入策略

**存储路径**：
1. 同步写 Redis Stream：`aisaas:audit:stream`（MAXLEN 100000）
2. 后台 consumer 批量落库 `ykt_aisaas_audit_log`

**写入失败处理**：
- 不阻塞业务路径
- 仅 `slog.Warn("audit write failed", "err", err)`
- 单独重试队列（见 DLQ 机制参考 Fix 4）

#### 3.2.3 关键路径埋点

| 文件 | 函数 | 事件 |
|------|------|------|
| `internal/platform/auth/apikey.go` | Middleware | `auth.success`（成功后） |
| `internal/platform/auth/apikey.go` | Middleware | `auth.fail`（失败原因：InvalidAPIKey/IPNotAllowed/ScopeForbidden） |
| `internal/platform/auth/apikey.go` | Validate | `auth.fail`（详细错误） |
| `internal/platform/quota/quota.go` | PrecheckDim | `quota.check`（抽样 1/100） |
| `internal/tenantm/apikey/apikey.go` | Create | `apikey.create` |
| `internal/tenantm/apikey/apikey.go` | Rotate | `apikey.rotate` |
| `internal/tenantm/apikey/apikey.go` | RevokeByInternal | `apikey.revoke` |
| `internal/server/internalapi/rotate.go` | IssueAPIKey | `apikey.create` |

#### 3.2.4 脱敏规则

**脱敏字段**：password、apiKey、token、creditCard、ssn  
**脱敏方式**：值为 `[REDACTED]`

### 3.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | 关键路径全埋点 | 代码审查：确认 8 个埋点全部存在 |
| 2 | 错误消息含敏感字段时自动 [REDACTED] | 单元测试：传入 `password=secret`，验证日志为 `password=[REDACTED]` |
| 3 | 写入失败不阻塞业务路径 | 断 Redis 后 API 正常响应 |
| 4 | audit_log 表 schema + 分区策略 | 由 database-optimizer 提供（不在本修复范围内） |

### 3.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 注释掉 `audit.Record()` 调用 |
| 2 | 保留 Stream 写入（避免数据丢失） |
| 3 | consumer 降级为 dry-run 模式（写日志不落库） |

### 3.5 依赖关系

- **依赖**：无
- **被依赖**：Fix 1（JWT）、Fix 2（Rotate/Revoke）— 依赖本修复记录审计日志

---

## Fix 4：Metering 改 Redis Stream（MUST Q8 决策）

### 4.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/platform/metering/metering.go:95` | `make(chan Record, cfg.BatchSize*8)` 内存 channel，有界队列满则 drop |
| `internal/platform/metering/metering.go:140-144` | channel 满时 `slog.Warn` + drop，不阻塞业务但不可靠 |

**生产事故**：高并发时 metering channel 满，大量记录丢弃，计费数据丢失。

### 4.2 修复点

#### 4.2.1 Record 改用 XADD

**文件**：`internal/platform/metering/metering.go`  
**行号**：134-145  
**修改内容**：

```go
// 原代码
func (r *Recorder) Record(ctx context.Context, rec Record) {
    // Redis 实时计数
    ym := time.Now().Format("200601")
    _ = r.rdb.IncrBy(ctx, redisx.KeyQuotaUsed(rec.TenantID, rec.Dimension, ym), rec.Amount).Err()

    select {
    case r.ch <- rec:
    default:
        slog.Warn("metering channel full, record dropped", "tenantId", rec.TenantID, "dim", rec.Dimension)
    }
}

// 新代码
func (r *Recorder) Record(ctx context.Context, rec Record) {
    // Redis 实时计数
    ym := time.Now().Format("200601")
    _ = r.rdb.IncrBy(ctx, redisx.KeyQuotaUsed(rec.TenantID, rec.Dimension, ym), rec.Amount).Err()

    // XADD 到 Redis Stream
    err := r.rdb.XAdd(ctx, &redis.Message{
        Stream: "aisaas:metering:stream",
        Values: recordToMap(rec),
    }).Err()
    if err != nil {
        // Fallback：写内存 channel（10s 重试）
        r.fallbackChannel <- rec
        slog.Warn("metering xadd failed, fallback to channel", "err", err)
    }
}
```

#### 4.2.2 Worker 改用 XREADGROUP

**文件**：`internal/platform/metering/metering.go`  
**行号**：158-221  
**修改内容**：

```go
func (r *Recorder) worker() {
    defer r.wg.Done()
    // 使用 XREADGROUP consumer group
    for {
        // 1. XREADGROUP 阻塞读取（block 3s）
        streams, err := r.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
            Group:    "aisaas-metering-consumers",
            Consumer: fmt.Sprintf("consumer-%d", os.Getpid()),
            Streams:  []string{"aisaas:metering:stream", ">"},
            Count:    int64(r.cfg.BatchSize),
            Block:    3 * time.Second,
        }).Result()
        if err != nil {
            if err == redis.Nil { continue }
            slog.Error("xreadgroup error", "err", err)
            continue
        }
        // 2. 处理消息
        for _, stream := range streams {
            for _, msg := range stream.Messages {
                rec := mapToRecord(msg.Values)
                r.processRecord(rec)
                // 3. XACK 确认
                r.rdb.XAck(ctx, "aisaas:metering:stream", "aisaas-metering-consumers", msg.ID)
            }
        }
    }
}
```

#### 4.2.3 DLQ 机制（新增）

**文件**：`internal/platform/metering/dlq.go`（新建）  
**DLQ 表**：`ykt_aisaas_metering_dlq`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint | 主键 |
| task_id | varchar(64) | XADD 消息 ID |
| payload | text | JSON 序列化 Record |
| retry_count | int | 重试次数 |
| last_error | text | 最后一次错误 |
| created_at | datetime | 创建时间 |

**DLQ Worker 逻辑**：
1. `XREADGROUP` 读取 `aisaas:metering:stream`
2. 落库失败 → `XADD aisaas:metering:dlq *`
3. DLQ 重试 3 次 → 仍失败 → P0 告警 + 人工介入

#### 4.2.4 Config 新增配置

**文件**：`internal/platform/config/config.go`  
**新增字段**：

```go
type MeteringConfig struct {
    // ... 现有字段 ...
    StreamMaxLen     int64  // 默认 100000
    ConsumerGroup    string // 默认 "aisaas-metering-consumers"
    DLQMaxRetries    int    // 默认 3
}
```

### 4.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | Redis Stream `aisaas:metering:stream` 持续接收 | `redis-cli XLEN aisaas:metering:stream` > 0 |
| 2 | XADD 失败时业务不阻塞（fallback 到 channel） | 断 Redis，API 正常响应 |
| 3 | DLQ 表记录所有失败 | 主动制造落库失败，验证 DLQ 表 |
| 4 | drop_count Prometheus 指标 = 0 | 压测后检查指标 |
| 5 | 现有 API 签名不变 | Record / RecordWithCtx / Close 签名与原接口一致 |

### 4.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 配置 `metering.useStream: false` |
| 2 | 代码自动降级为原 channel 模式 |
| 3 | Redis Stream 数据由后台任务迁移回 channel |

### 4.5 依赖关系

- **依赖**：无
- **被依赖**：无

---

## Fix 5：配额退款接口（MUST Q8 决策）

### 5.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/platform/redisx/redisx.go:109-112` | `QuotaRollback` 存在但业务层未使用 |
| `internal/server/v1/chat.go:128-134` | chat 失败时仅记录失败用量，未退款 |

**业务问题**：chat 失败时预扣配额不退还，租户受损。

### 5.2 修复点

#### 5.2.1 QuotaRefundReq 结构体（新增）

**文件**：`internal/server/internalapi/params.go`  
**新增**：

```go
type QuotaRefundReq struct {
    TenantID  int64  `json:"tenantId" binding:"required"`
    Dimension string `json:"dimension" binding:"required"`
    Estimated int64  `json:"estimated" binding:"required,min=0"`   // 预扣量
    Actual    int64  `json:"actual" binding:"required,min=0"`      // 实际用量
    RequestID string `json:"requestId" binding:"required"`         // 溯源
}
```

#### 5.2.2 QuotaRefund Handler（新增）

**文件**：`internal/server/internalapi/quota_refund.go`（新建）  
**路由**：`POST /internal/api/v1/quota/refund`  

**逻辑**：

```go
func (h *Handler) RefundQuota(c *gin.Context) {
    var req QuotaRefundReq
    if err := c.ShouldBindJSON(&req); err != nil {
        web.Abort(c, errs.New(errs.InvalidJSON, err.Error())); return
    }
    refund := req.Estimated - req.Actual
    if refund <= 0 {
        web.OK(c, gin.H{"refund": 0, "note": "no refund needed"}) // estimated <= actual 不退款
        return
    }
    // DecrBy Redis used（原子）
    if err := h.QuotaSvc.Refund(c.Request.Context(), req.TenantID, req.Dimension, refund); err != nil {
        web.Abort(c, err); return
    }
    // 写退款记录到 usage_detail（costCents 调整为实际 cost）
    // 写 audit_log
    web.OK(c, gin.H{"refund": refund})
}
```

#### 5.2.3 调用方改造

**文件**：`internal/server/v1/chat.go`  
**行号**：128-134（失败分支）  
**修改内容**：

```go
// 原代码
resp, err := resolved.Client.Complete(ctx, upReq)
if err != nil {
    h.Meter.RecordWithCtx(ctx, metering.Record{
        BizType: metering.BizLLM, Dimension: metering.DimLLMTokensIn,
        ModelID: resolved.ModelID, Status: 0, RequestID: web.RequestID(c),
    })
    web.AbortOpenAI(c, errs.Wrap(errs.ProviderError, err))
    return
}

// 新代码
resp, err := resolved.Client.Complete(ctx, upReq)
estimated := quota.EstimateChat(body.Messages)
if err != nil {
    // 退款估算差额
    h.handleChatRefund(ctx, c, resolved, estimated, 0)
    h.Meter.RecordWithCtx(ctx, metering.Record{
        BizType: metering.BizLLM, Dimension: metering.DimLLMTokensIn,
        ModelID: resolved.ModelID, Status: 0, RequestID: web.RequestID(c),
    })
    web.AbortOpenAI(c, errs.Wrap(errs.ProviderError, err))
    return
}

// 成功后按实际用量退款
actual := int(resp.Usage.PromptTokens)
h.handleChatRefund(ctx, c, resolved, estimated, actual)
```

**新增方法**：

```go
func (h *ChatHandler) handleChatRefund(ctx context.Context, c *gin.Context, resolved *llm.Resolved, estimated, actual int) {
    if estimated <= actual {
        return
    }
    refund := estimated - actual
    tid, _ := tenant.FromSafe(ctx)
    if tid == 0 { return }
    // 调用退款接口（异步，不阻塞响应）
    go func() {
        bg := context.Background()
        _ = h.QuotaSvc.Refund(bg, tid, redisx.DimLLMTokensIn, int64(refund))
        // 写 audit_log（异步）
    }()
}
```

### 5.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | estimated > actual 时退款差额到 Redis used | 测试：Precheck 扣 100，实际用 60，退款 40，验证 Redis used |
| 2 | estimated < actual 时不退款（不退成负数） | 测试：Precheck 扣 60，实际用 100，不退款 |
| 3 | 退款记录写 usage_detail | 检查数据库：costCents = actual cost |
| 4 | 退款操作写 audit_log | 检查 audit_log 表：action = quota.refund |

### 5.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 注释掉 `handleChatRefund` 调用 |
| 2 | 数据库手动补偿 Redis used 值 |

### 5.5 依赖关系

- **依赖**：无
- **被依赖**：无

---

## Fix 6：quota_snapshot Lua 原子借记（Q8 子议题决策）

### 6.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/platform/redisx/redisx.go:66-83` | `luaQuotaDeduct` 只扣设备配额，无 session 快照 |
| `internal/platform/redisx/redisx.go` | 缺少 session 粒度的配额追踪 |

**问题**：session 创建时可能并发超支，且无法追踪单个 session 的配额使用。

### 6.2 修复点

#### 6.2.1 QuotaSnapshotDeduct Lua 脚本（新增）

**文件**：`internal/platform/redisx/redisx.go`  
**新增常量**：

```go
const luaQuotaSnapshotDeduct = `
-- KEYS[1] = device_quota_used      -- 设备已用计数
-- KEYS[2] = device_quota_limit     -- 设备限额快照
-- KEYS[3] = session_quota_key      -- session 配额快照 Hash
-- ARGV[1] = amount                 -- 本次预扣量
-- ARGV[2] = ttl                    -- TTL（秒）
-- ARGV[3] = session_id            -- session ID
-- 返回：{remainingDevice, snapshotValue} 或 -1（配额不足）或 -2（limit 未加载）

local limit = tonumber(redis.call('GET', KEYS[2]) or '-1')
if limit < 0 then
  return -2  -- limit 未加载：允许放行（配额未配置=不限）
end
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
if used + amount > limit then
  return -1  -- 配额不足
end
redis.call('INCRBY', KEYS[1], amount)
redis.call('EXPIRE', KEYS[1], ARGV[2])
redis.call('HSET', KEYS[3], 'initial', amount, 'remaining', amount, 'device_used_after', used + amount)
redis.call('EXPIRE', KEYS[3], ARGV[2])
return {limit - used - amount, amount}
`
```

#### 6.2.2 QuotaSnapshotDeduct 方法（新增）

**文件**：`internal/platform/redisx/redisx.go`  
**新增方法**：

```go
// QuotaSnapshotDeduct 原子扣减设备配额 + 写入 session 快照。
// 返回：(remainingDevice, snapshotValue, error)
// -2 = limit 未加载（不限量）；-1 = 配额不足
func (c *Client) QuotaSnapshotDeduct(ctx context.Context, tenantID int64, dim, sessionID string, amount int64) (int64, int64, error) {
    ym := time.Now().Format("200601")
    usedKey := KeyQuotaUsed(tenantID, dim, ym)
    limitKey := KeyQuotaLimit(tenantID, dim, ym)
    sessionKey := KeySessionQuota(sessionID, dim)
    res, err := quotaSnapshotDeduct.Run(ctx, c.Client,
        []string{usedKey, limitKey, sessionKey},
        amount, int64((35*24*time.Hour)/time.Second), sessionID,
    ).Slice()
    if err != nil {
        return 0, 0, fmt.Errorf("quota snapshot lua: %w", err)
    }
    remaining, _ := res[0].(int64)
    snapshot, _ := res[1].(int64)
    if remaining == -2 { return 0, 0, nil }
    if remaining == -1 { return 0, 0, ErrQuotaExceeded }
    return remaining, snapshot, nil
}

// KeySessionQuota session 配额快照 Key。
func KeySessionQuota(sessionID, dim string) string {
    return fmt.Sprintf("aisaas:session:%s:quota:%s", sessionID, dim)
}
```

#### 6.2.3 QuotaSnapshotRefund 方法（新增）

**文件**：`internal/platform/redisx/redisx.go`  
**新增方法**：

```go
// QuotaSnapshotRefund session 结束时退差额。
// 1. 读取 session 快照 initial 值
// 2. DecrBy device_quota_used(initial - actualUsed)
// 3. Del session_quota_key
func (c *Client) QuotaSnapshotRefund(ctx context.Context, tenantID int64, dim, sessionID string, actualUsed int64) error {
    sessionKey := KeySessionQuota(sessionID, dim)
    initial, err := c.HGet(ctx, sessionKey, "initial").Int64()
    if err != nil {
        return nil // session 不存在（可能已过期）
    }
    refund := initial - actualUsed
    if refund <= 0 {
        return nil
    }
    ym := time.Now().Format("200601")
    c.DecrBy(ctx, KeyQuotaUsed(tenantID, dim, ym), refund)
    c.Del(ctx, sessionKey)
    return nil
}
```

### 6.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | Lua 脚本 KEYS + ARGV 数量与注释一致 | 代码审查：3 KEYS / 3 ARGV |
| 2 | 单元测试：扣减成功 | 验证 remaining = limit - used - amount |
| 3 | 单元测试：配额不足返回 -1 | 扣减到 limit 边界，验证返回 |
| 4 | 单元测试：Redis 故障返回 error | mock Redis 错误，验证 error |
| 5 | 单元测试：session_id 重复不冲突 | 同一 session_id 调用两次，验证互不覆盖 |
| 6 | 并发测试（Go race detector） | `go test -race`，无 data race |
| 7 | 与现有 QuotaDeduct 不冲突 | 独立调用，功能正常 |

### 6.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 注释掉 `QuotaSnapshotDeduct` 调用 |
| 2 | 改回使用原 `QuotaDeduct` |
| 3 | 数据库手动补偿 session 快照缺失的退款 |

### 6.5 依赖关系

- **依赖**：无
- **被依赖**：无

---

## Fix 7：API Key 紧急撤销（Q6 决策）

### 7.1 当前问题

| 位置 | 问题 |
|------|------|
| `internal/tenantm/apikey/apikey.go:164-178` | `Disable` + `Delete` 需要 tenant 上下文 |
| `internal/tenantm/apikey/apikey.go:117-144` | 无 `RevokeByInternal` 实现 |

**业务场景**：安全事件时需立即撤销 API Key，但现有接口需要 tenant 上下文，操作繁琐。

### 7.2 修复点

#### 7.2.1 RevokeByInternal 实现

**文件**：`internal/tenantm/apikey/apikey.go`  
**行号**：172-178 附近  
**新增方法**：

```go
// RevokeByInternal 紧急撤销（内部通道专用，无需 tenant 上下文）。
// 1. 按 ID 全局查询（不依赖 tenant 隔离）
// 2. UpdateStatus(0)
// 3. Redis Del 缓存
// 4. 写审计日志（actor = internal）
func (s *Service) RevokeByInternal(ctx context.Context, keyID int64) error {
    // 1. 全局查询（不走 repo 租户过滤）
    var do DO
    err := s.repo.db.WithContext(ctx).
        Table("ykt_aisaas_apikey").
        Where("id = ? AND isDeleted = 0", keyID).
        Take(&do).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return errs.New(errs.ResourceNotFound, "API Key 不存在")
    }
    if err != nil {
        return errs.Wrap(errs.Internal, err)
    }
    // 2. UpdateStatus(0)
    if err := s.repo.UpdateStatus(ctx, keyID, 0); err != nil {
        return errs.Wrap(errs.Internal, err)
    }
    // 3. Redis Del 缓存（立即失效）
    hash := do.APIKeyHash
    s.auth.InvalidateCache(ctx, hash)
    // 4. 写审计日志
    audit.Record(ctx, audit.AuditEntry{
        ActorType:    "internal",
        ActorID:      "0",
        Action:       "apikey.revoke",
        ResourceType: "apikey",
        ResourceID:   strconv.FormatInt(keyID, 10),
        Result:       "success",
    })
    return nil
}
```

#### 7.2.2 Revoke Handler（复用 Fix 2）

**文件**：`internal/server/internalapi/rotate.go`  
**路由**：`POST /internal/api/v1/apikeys/:id/revoke`（已在 Fix 2 定义）

### 7.3 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| 1 | revoke 后 0~5s 内 Redis 缓存失效 | 调用 Revoke 后，`redis-cli GET aisaas:apikey:idx:{hash}` 返回 nil |
| 2 | 不需要 tenant 上下文即可执行 | 模拟无 tenant context 调用，成功执行 |
| 3 | 操作写 audit_log（actor=internal） | 检查 audit_log 表记录 |

### 7.4 回滚方案

| 步骤 | 操作 |
|------|------|
| 1 | 数据库手动 `UPDATE ykt_aisaas_apikey SET status = 1 WHERE id = ?` |
| 2 | Redis 缓存由 `InvalidateCache` 自动恢复（下次 Validate 时重建） |

### 7.5 依赖关系

- **依赖**：Fix 3（审计日志）
- **被依赖**：无

---

## 跨项验收 Checklist

### 通用要求

| # | 检查项 | Fix 关联 |
|---|--------|---------|
| 1 | 所有新增文件/方法有单元测试覆盖 | 全部 |
| 2 | 所有新增接口有 OpenAPI 文档（由 api-platform-engineer 提供） | Fix 2, Fix 5 |
| 3 | 所有新增 Redis Key 符合 Key 规范（在 redisx.go 注释） | Fix 4, Fix 6 |
| 4 | 所有配置项写入 config.yaml（无硬编码） | Fix 1, Fix 4 |
| 5 | 所有新增 ErrorCode 已在 errs.go 注册 | 全部 |
| 6 | 所有新增表/索引有 migration（由 database-optimizer 提供） | Fix 3, Fix 4 |
| 7 | 回滚方案已验证（至少覆盖代码路径回滚） | 全部 |

### Fix 1 - JWT RS256

- [ ] HS256 token 被 ParseToken 拒绝
- [ ] none 算法 token 被 ParseToken 拒绝
- [ ] RS256 私钥从 KMS 加载（非配置文件明文）
- [ ] RS256 公钥从 KMS 加载
- [ ] token expiry = 15min
- [ ] refresh_token expiry = 7d
- [ ] 5 次登录失败 → 15min 冷却
- [ ] Login 成功写 audit_log
- [ ] Login 失败写 audit_log

### Fix 2 - API Key Rotate/Revoke

- [ ] Rotate 返回新明文 Key
- [ ] Rotate 旧 Key 5min 宽限期
- [ ] Revoke 后 Redis 缓存立即失效
- [ ] Rotate 写 audit_log
- [ ] Revoke 写 audit_log

### Fix 3 - 审计日志

- [ ] 8 个关键路径埋点全部存在
- [ ] 敏感字段脱敏 [REDACTED]
- [ ] 写入失败不阻塞业务
- [ ] audit_log 表分区策略（365d 保留）

### Fix 4 - Metering Redis Stream

- [ ] XADD 替代 channel
- [ ] XREADGROUP 替代 channel 消费
- [ ] DLQ 机制：落库失败 → DLQ 表
- [ ] DLQ 重试 3 次 → P0 告警
- [ ] drop_count = 0
- [ ] API 签名不变

### Fix 5 - Quota Refund

- [ ] estimated > actual 退款差额
- [ ] estimated <= actual 不退款
- [ ] 退款记录写 usage_detail
- [ ] 退款写 audit_log
- [ ] chat 失败时调用退款

### Fix 6 - quota_snapshot Lua

- [ ] KEYS = 3, ARGV = 3
- [ ] 扣减成功返回 remaining + snapshot
- [ ] 配额不足返回 -1
- [ ] limit 未加载返回 -2
- [ ] 并发测试通过（race detector）
- [ ] QuotaSnapshotRefund 正确

### Fix 7 - API Key 紧急撤销

- [ ] RevokeByInternal 无需 tenant 上下文
- [ ] Redis 缓存立即失效
- [ ] 写 audit_log（actor=internal）

---

## 附录

### A. 文件变更摘要

| 操作 | 文件路径 |
|------|---------|
| 修改 | `internal/portal/portal.go` |
| 修改 | `internal/tenantm/apikey/apikey.go` |
| 修改 | `internal/platform/auth/apikey.go` |
| 修改 | `internal/platform/quota/quota.go` |
| 修改 | `internal/platform/redisx/redisx.go` |
| 修改 | `internal/platform/metering/metering.go` |
| 修改 | `internal/server/router.go` |
| 修改 | `internal/server/internalapi/params.go` |
| 修改 | `internal/server/v1/chat.go` |
| 新建 | `internal/platform/crypto/kms.go` |
| 新建 | `internal/server/internalapi/rotate.go` |
| 新建 | `internal/server/internalapi/quota_refund.go` |
| 新建 | `internal/platform/audit/audit.go` |
| 新建 | `internal/platform/metering/dlq.go` |

### B. 术语表

| 术语 | 定义 |
|------|------|
| RS256 | RSA Signature with SHA-256（非对称签名算法） |
| HS256 | HMAC with SHA-256（对称签名算法） |
| XADD | Redis Stream 追加命令 |
| XREADGROUP | Redis Stream 消费者组读取命令 |
| DLQ | Dead Letter Queue（死信队列） |
| KMS | Key Management Service（密钥管理服务） |
| UUID v7 | 时间有序 UUID（基于时间戳 + 随机） |

### C. 参考资料

- Q6 鉴权群讨论结论（security-audit 团队）
- Q8 事务群讨论结论（software-architect-review 团队）
- OWASP Top 10 2021
- NIST SSDF
- ykt-aisaas 现有实现文档
