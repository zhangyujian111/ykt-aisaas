# P0 安全修复实施顺序 — ykt-aisaas V2

> 基于 Q6 鉴权群讨论 + Q8 事务群讨论结论  
> 版本：V2.0 | 日期：2026-09-02 | 状态：待实现

---

## 1. 依赖关系图

```
Fix 1 (JWT RS256) ───────────────────────────────────────────────────────────┐
    │                                                                         │
    │  ├─ Fix 3 (审计日志)  ←──────────────────────────────────────────────┐  │
    │  │    │                                                              │  │
    │  │    │                                                              │  │
Fix 2 (Rotate/Revoke) ───────────────────────────────────────────────────────┼──┤
    │                                                               │        │  │
    │                                                               │        │  │
Fix 4 (Metering Stream) ──────────────────────────────────────────── Fix 5 ─┤  │
    │                                                               │  (Quota│  │
    │                                                               │  Refund)│  │
Fix 6 (quota_snapshot Lua) ──────────────────────────────────────────────────┘  │
    │                                                                         │
    │                                                                         │
Fix 7 (Revoke Immediate) ─────────────────────────────────────────────────────┘
```

### 依赖关系说明

| 修复项 | 依赖 | 被依赖 | 依赖类型 |
|--------|------|--------|---------|
| Fix 1 (JWT RS256) | - | Fix 3 | Fix 3 需要记录 Login 审计日志 |
| Fix 2 (Rotate/Revoke) | Fix 3 | - | Rotate/Revoke 需要写审计日志 |
| Fix 3 (审计日志) | - | Fix 1, Fix 2, Fix 7 | 基础设施，为其他修复提供审计能力 |
| Fix 4 (Metering Stream) | - | - | 独立，无依赖 |
| Fix 5 (Quota Refund) | - | - | 独立，无依赖 |
| Fix 6 (quota_snapshot Lua) | - | - | 独立，无依赖 |
| Fix 7 (Revoke Immediate) | Fix 3 | - | RevokeByInternal 需要写审计日志 |

---

## 2. 实施阶段划分

### 阶段 1：基础设施（Fix 3 审计日志）

**目的**：为后续所有修复提供审计日志基础设施

**修复项**：Fix 3（审计日志强制写入）

**产出**：
- `internal/platform/audit/audit.go` — AuditEntry 结构体 + Record 函数
- Redis Stream 配置
- 8 个关键路径埋点

**验收里程碑**：
- [ ] `audit.Record()` 可正常调用
- [ ] Redis Stream `aisaas:audit:stream` 正常接收
- [ ] consumer 批量落库 `ykt_aisaas_audit_log`（由 database-optimizer 提供 schema）

---

### 阶段 2：认证安全（Fix 1 + Fix 7）

**目的**：修复 JWT 算法问题 + API Key 紧急撤销能力

**修复项**：
- Fix 1（JWT RS256）— 依赖 Fix 3 记录 Login 审计
- Fix 7（API Key 紧急撤销）— 依赖 Fix 3 记录 Revoke 审计

**并行策略**：
- Fix 1 和 Fix 7 可并行开发（无相互依赖）
- 均依赖 Fix 3 的 audit infrastructure

**Fix 1 产出**：
- `internal/portal/portal.go` — RS256 签发/验签
- `internal/platform/crypto/kms.go` — KMS 密钥加载接口
- JWT 配置：`access_token: 15min`，`refresh_token: 7d`
- 暴力破解防护：5 次失败 → 15min 冷却

**Fix 7 产出**：
- `internal/tenantm/apikey/apikey.go` — `RevokeByInternal` 方法
- Redis 缓存立即失效

**验收里程碑**：
- [ ] HS256/none token 被 ParseToken 拒绝
- [ ] 私钥从 KMS 加载（非明文配置）
- [ ] Revoke 后 Redis 缓存 0~5s 内失效
- [ ] Login/Revoke 均写 audit_log

---

### 阶段 3：API Key 管理增强（Fix 2）

**目的**：提供 Rotate/Revoke 接口

**修复项**：Fix 2（API Key 轮换接口）

**前置条件**：Fix 3 完成（Rotate/Revoke 需写审计日志）

**产出**：
- `internal/server/internalapi/rotate.go` — Rotate + Revoke Handler
- `POST /internal/api/v1/apikeys/:id/rotate`
- `POST /internal/api/v1/apikeys/:id/revoke`
- 5min 宽限期机制

**验收里程碑**：
- [ ] Rotate 返回新明文 Key
- [ ] 5min 宽限期内旧 Key 仍可用
- [ ] Revoke 后 0~5s 内旧 Key 失效
- [ ] Rotate/Revoke 写 audit_log

---

### 阶段 4：Metering 可靠性（Fix 4）

**目的**：消除 channel drop 风险

**修复项**：Fix 4（Metering 改 Redis Stream）

**前置条件**：无

**产出**：
- `internal/platform/metering/metering.go` — XADD/XREADGROUP
- `internal/platform/metering/dlq.go` — DLQ Worker
- `ykt_aisaas_metering_dlq` 表（由 database-optimizer 提供 schema）
- Prometheus `drop_count` 指标

**验收里程碑**：
- [ ] Redis Stream `aisaas:metering:stream` 持续接收
- [ ] XADD 失败 fallback 到 channel（10s 重试）
- [ ] DLQ 表记录所有失败
- [ ] `drop_count = 0`

---

### 阶段 5：Quota 准确性（Fix 5 + Fix 6）

**目的**：修复配额超支 + 退款问题

**修复项**：
- Fix 5（配额退款接口）
- Fix 6（quota_snapshot Lua 原子借记）

**并行策略**：
- Fix 5 和 Fix 6 可并行开发（无相互依赖）

**Fix 5 产出**：
- `internal/server/internalapi/quota_refund.go` — Refund Handler
- `POST /internal/api/v1/quota/refund`
- `internal/server/v1/chat.go` — 失败时调用退款

**Fix 6 产出**：
- `internal/platform/redisx/redisx.go` — `luaQuotaSnapshotDeduct` + `QuotaSnapshotDeduct` + `QuotaSnapshotRefund`

**验收里程碑**：
- [ ] estimated > actual 退款差额到 Redis used
- [ ] estimated <= actual 不退款
- [ ] Lua KEYS=3, ARGV=3 与注释一致
- [ ] 并发测试通过（race detector）

---

## 3. 实施甘特图

```
Week 1:  Fix 3 (审计日志基础设施) ─────────────────────────────────────────────
          │
Week 2:  ├─ Fix 1 (JWT RS256) ───────────────────────────────────────────┐
          │                                                                │
Week 2:  └─ Fix 7 (Revoke Immediate) ────────────────────────────────────┼──
                                                                         │
Week 3:  Fix 2 (Rotate/Revoke) ───────────────────────────────────────────┤
          │                                                                 │
Week 3:  ├─ Fix 4 (Metering Stream) ──────────────────────────────────────┤
          │                                                                 │
Week 4:  ├─ Fix 5 (Quota Refund) ────────────────────────────────────────┤
          │                                                                 │
Week 4:  └─ Fix 6 (quota_snapshot Lua) ───────────────────────────────────┘
```

---

## 4. 验收流程

### 4.1 代码评审（Code Review）

每项 Fix 完成后，由 `code-reviewer` 进行评审，验收标准见 `P0-fixes.md`。

### 4.2 门禁检查（Reality Check）

所有 7 项 Fix 完成后，由 `reality-checker` 进行生产就绪认证：

| 检查项 | 标准 |
|--------|------|
| 单元测试覆盖率 | ≥ 80% |
| 所有验收标准通过 | 7 项 × 验收标准 100% |
| 无高危安全漏洞 | CVSS < 7.0 |
| 性能回归 | P99 < 基线 + 20% |
| 回滚方案验证 | 已演练 |

### 4.3 灰度发布

| 阶段 | 策略 |
|------|------|
| Stage 1 | 10% 流量，7 项 Fix 全开 |
| Stage 2 | 50% 流量，观察 24h |
| Stage 3 | 100% 流量 |

**回滚触发条件**：
- 错误率上升 > 0.1%
- P99 延迟上升 > 50%
- 任意 Fix 验收标准失败

---

## 5. 并行开发分组

### 并行组 A（Week 1-2）

| 修复项 | 开发者 | 产出 |
|--------|--------|------|
| Fix 3 | Backend-1 | `internal/platform/audit/audit.go` |
| Fix 1 | Backend-2 | `internal/portal/portal.go` |
| Fix 7 | Backend-2 | `internal/tenantm/apikey/apikey.go`（RevokeByInternal） |

### 并行组 B（Week 3-4）

| 修复项 | 开发者 | 产出 |
|--------|--------|------|
| Fix 2 | Backend-1 | `internal/server/internalapi/rotate.go` |
| Fix 4 | Backend-3 | `internal/platform/metering/metering.go` |
| Fix 5 | Backend-1 | `internal/server/internalapi/quota_refund.go` |
| Fix 6 | Backend-3 | `internal/platform/redisx/redisx.go`（Lua 脚本） |

---

## 6. 风险与缓解

| 风险 | 等级 | 缓解措施 |
|------|------|---------|
| Fix 1 JWT RS256 切换导致所有现有 token 失效 | HIGH | 配置开关支持回退 HS256；旧 token 设置过期时间 > 迁移窗口 |
| Fix 4 Metering Stream XADD 性能问题 | MEDIUM | 先压测；fallback channel 保障；batch size 可配置 |
| Fix 6 Lua 脚本 bug 导致配额计算错误 | HIGH | 充分单元测试 + race detector；上线后监控异常退款 |
| Fix 3 审计日志写入影响业务延迟 | LOW | 异步写；失败不阻塞；batch 合并 |

---

## 7. 配置开关清单

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `portal.jwtAlgorithm` | string | "RS256" | HS256 回退开关 |
| `portal.tokenExpiry` | duration | 15min | access_token 有效期 |
| `portal.refreshTokenExpiry` | duration | 7d | refresh_token 有效期 |
| `portal.loginMaxFail` | int | 5 | 登录失败次数上限 |
| `portal.loginCooldown` | duration | 15min | 登录失败冷却时间 |
| `metering.useStream` | bool | true | 是否使用 Redis Stream |
| `metering.streamMaxLen` | int64 | 100000 | Stream 最大长度 |
| `metering.dlqMaxRetries` | int | 3 | DLQ 最大重试次数 |

---

## 8. 附录

### A. 关键文件路径

| 类别 | 文件路径 |
|------|---------|
| Portal JWT | `internal/portal/portal.go` |
| API Key Service | `internal/tenantm/apikey/apikey.go` |
| Auth Middleware | `internal/platform/auth/apikey.go` |
| Quota | `internal/platform/redisx/redisx.go` |
| Metering | `internal/platform/metering/metering.go` |
| Audit | `internal/platform/audit/audit.go` |
| Router | `internal/server/router.go` |
| Chat Handler | `internal/server/v1/chat.go` |

### B. 外部依赖

| 依赖 | 负责方 | 交付物 |
|------|--------|--------|
| KMS 接口实现 | cloud-security-architect | `internal/platform/crypto/kms.go` 实现 |
| audit_log 表 schema | database-optimizer | `ykt_aisaas_audit_log` 分区策略 |
| metering_dlq 表 schema | database-optimizer | `ykt_aisaas_metering_dlq` schema |
| Prometheus 指标集成 | devops-automator | `drop_count` 等指标接入 |

### C. 参考文档

- `P0-fixes.md` — 详细修复规范
- Q6 鉴权群讨论结论
- Q8 事务群讨论结论
