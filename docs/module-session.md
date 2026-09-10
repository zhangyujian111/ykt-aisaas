# Session 模块（V2 P1）

> **版本**：V1.0 | **日期**：2026-09-02 | **决策**：Q8 quota_snapshot

## 概述

会话生命周期管理 + 配额预扣/退款，基于 Q8 决策的 `quota_snapshot` Lua 原子借记机制。

## 架构

```
xiaozhi-server-go 调用流程：
  对话前 → POST /api/v1/sessions/{deviceId}  (CreateSession)
            └─ quota_snapshot Lua 原子借记设备配额
            └─ 写入 ykt_aisaas_session

  对话后 → POST /api/v1/sessions/{sessionId}/end  (EndSession)
            └─ 按实际 token 用量退差额
            └─ 更新 session.endTime + actualCost

  查询   → GET /api/v1/sessions/{deviceId}/history  (GetHistory)
            └─ 拉取最近 N 轮对话消息
```

## 接口

| 方法 | 路径 | 用途 |
|------|------|------|
| POST | `/api/v1/sessions/{deviceId}` | 创建会话 + 配额预扣 |
| GET | `/api/v1/sessions/{deviceId}/history?limit=N` | 列出历史会话消息 |
| POST | `/api/v1/sessions/{sessionId}/end` | 结束会话 + 实际用量退款 |

## 配额机制

### 创建会话时（配额预扣）

1. 调用 `redisx.QuotaSnapshotDeduct(tenantID, dim, sessionID, quotaInitial)`
2. Lua 原子操作：检查设备配额 → 扣减 → 写入 session 快照
3. 配额不足返回 `402 Payment Required` + 剩余配额

### 结束会话时（配额退款）

1. 调用 `redisx.QuotaSnapshotRefund(tenantID, dim, sessionID, actualUsed)`
2. 读取 session 快照 `initial` 值 → 计算 `refund = initial - actualUsed`
3. `DecrBy` 设备配额 → `Del` session 快照 key

### 失败补偿

| 场景 | 处理 |
|------|------|
| Lua 脚本执行失败（Redis 断连） | 返回 503，调用方重试 |
| session 创建成功但 Redis key 丢失 | 后台对账任务补扣差额 |
| 退款时 session key 已过期 | 跳过退款，不阻塞 |
| 入库失败（DB 故障） | 回滚 Redis 配额 |

## 会话状态机

```
1 创建中 → 2 活跃 → 3 已结束 → 4 已结算
```

- **创建中（1）**：初始状态，配额已借记但对话未开始
- **活跃（2）**：对话中，`lastMessageTime` 持续更新
- **已结束（3）**：用户主动关闭（status=failed）
- **已结算（4）**：配额清算完成（status=success）

## 超时清理

- 会话超时：24h 无消息自动结束
- 清理策略：定时任务（每小时）扫描 `status IN (1,2) AND lastMessageTime < NOW() - 24h`
- 超时 session 配额自动退款

## 文件清单

| 文件 | 职责 |
|------|------|
| `internal/tenantm/session/session.go` | Repo + Service + Handler 结构体 |
| `internal/tenantm/session/lifecycle.go` | Create / End 会话生命周期 |
| `internal/tenantm/session/quota.go` | quota_snapshot 集成封装 |
| `internal/tenantm/session/history.go` | 历史查询 |
| `internal/tenantm/session/handler.go` | HTTP handler（3 个接口） |
| `internal/tenantm/session/types.go` | DO + API 请求/响应类型 |
| `internal/tenantm/session/config.go` | SessionConfig |
| `internal/server/router.go` | 路由注册（修改） |

## 依赖

- **P0 复用**：`redisx.QuotaSnapshotDeduct`、`redisx.QuotaSnapshotRefund`、`quota.Refund`
- **数据库**：`ykt_aisaas_session`（000009 migration）、`ykt_aisaas_session_message`（000008 migration）
- **鉴权**：API Key Bearer（`auth.Middleware`，自动注入 tenant context）