# YKT 工作区 Go 化迁移计划（ykt-admin + xiaozhi-server）

> 版本：v2.1（2026-08-24）
> 前置：[GOLANG-IMPLEMENTATION.md](./GOLANG-IMPLEMENTATION.md)（ykt-aisaas v2.0-GO，W1-W12）
> 状态：待评审
> 基准日：W1 = 2026-08-31（aisaas 开发启动周）

---

## 0. 三句话结论

1. **ykt-aisaas**：正在 Go 化（v2.0-GO 已定），是全工作区 Go 能力的"训练场"。
2. **ykt-admin**：实测仅 **3,670 行 Java**（骨架期）——不值得"迁移"，直接**用 Go 重建**，1 人 × 3 周完事，安排在 aisaas 收尾窗口（W11-W14）。
3. **xiaozhi-server**：**大概率不需要独立转 Go**。它的并发核心（dialogue 模块 7,841 行）在 aisaas 集成完成后，可被 aisaas 的 Go 实时网关（通路 B，协议同源）**吸收替代**；剩余部分是内部管理 CRUD，可长期保持 Java，零风险。只有量级触发器命中时才启动（最早 2027-Q1）。

---

## 1. 现状盘点（2026-08-24 实测）

### 1.1 代码规模（find 实测，排除 target）

| 项目 | 模块 | 文件数 | 行数 | 状态 |
|---|---|---|---|---|
| **ykt-admin** | common | 12 | 394 | 骨架 |
| | framework | 19 | 716 | 骨架 |
| | system-api | 22 | 461 | 骨架 |
| | monitor（5 维指标+告警） | 51 | 1,793 | **早期** |
| | server | 5 | 306 | 骨架 |
| | **合计** | **109** | **3,670** | ⭐ 未成型 |
| **xiaozhi-server** | common | 128 | 6,864 | 生产 |
| | service | 155 | 9,832 | 生产 |
| | **ai（将被删除）** | 109 | **12,612** | ⚠️ aisaas 上线后删 |
| | **dialogue（并发核心）** | 73 | **7,841** | 生产 |
| | server（HTTP API） | 56 | 5,933 | 生产 |
| | **合计** | **521** | **43,082** | 成熟 |

### 1.2 关键判断

| 判断 | 依据 |
|---|---|
| ykt-admin 是"重建"不是"迁移" | 3,670 行、无成型前端、5 维指标仅落地一部分；重建比翻译快且干净 |
| xiaozhi-ai 不参与任何迁移 | 12,612 行在 v2.0 计划 W13 就要删除——**任何包含它的迁移方案都是浪费** |
| xiaozhi-dialogue 是唯一"有必要讨论 Go 化"的部分 | 7,841 行 WebSocket 并发核心；且 aisaas `realtime/` 模块本就是它的 Go 移植（协议同源、VAD/Opus/流水线已写） |
| xiaozhi service+server（1.58 万行）保持 Java | 内部管理 CRUD，无并发压力，无客户 SLA；重写零业务收益，纯风险 |

### 1.3 依赖关系（迁移顺序约束）

```
xiaozhi-server ──(W7 接入)──► ykt-aisaas ──(W13 删 xiaozhi-ai)──► xiaozhi 瘦身完成
      │                                                        │
      │              ykt-admin ──(读 xiaozhi 库/调 HTTP API)───┘
      └──(W11+ 增加对 aisaas 的监控)── ykt-admin-go
```

**硬约束**：aisaas MVP（W1-10）期间，ykt-admin 和 xiaozhi-server 只修 bug。三个系统同时动 = 团队（3 后端）过载 + 生产事故温床。

---

## 2. 总路线图

```
2026                     │ W1──W10 │ W11─W14 │ W15─W20 │ W21+（触发器制）
─────────────────────────┼─────────┼─────────┼─────────┼──────────────────
Phase 0：aisaas MVP      │ ███████ │ ▓▓(缓冲)│         │
  其余系统冻结(bugfix)    │ ─────── │         │         │
Phase 1：ykt-admin 重建  │         │ ███████ │         │
  + platform 库抽取      │         │         │         │
Phase 2：融合期          │         │   ▓▓░░░ │ ███████ │
  ykt-admin-go 接管监控  │         │         │  (含对 aisaas/xiaozhi)
  xiaozhi 瘦身收尾       │         │         │         │
Phase 3：xiaozhi 决策点  │         │         │         │ 触发器命中才启动
  首选 B 吸收 / 备选 A 重写│         │         │         │ (最早 2027-Q1)
─────────────────────────┴─────────┴─────────┴─────────┴──────────────────
里程碑                    MVP 上线   ykt-admin  全工作区    xiaozhi 并发核心
                         (W10)     Go 切换(W14) Go 化(W20)  (可选, 触发制)
```

---

## 3. Phase 0（W1-W10）：冻结 + aisaas MVP

**原则**：此阶段对 ykt-admin / xiaozhi-server **只做三件事**——线上 bug 修复、aisaas 集成改造（已排定的 W7 切换）、安全补丁。**不做任何 Go 相关动作**。

| 事项 | 说明 |
|---|---|
| aisaas 开发 | 按 [GOLANG-IMPLEMENTATION.md §8](./GOLANG-IMPLEMENTATION.md) 执行（W1-10） |
| xiaozhi 接入 aisaas | W5-W7（Persona 改造 + 数据迁移 + 生产切换），v2.0 已排定 |
| ykt-admin | 冻结。若必须加监控小功能，加在 Java 上但**严格限 500 行以内**（避免沉没成本变大） |
| 产出 | aisaas MVP 上线（W10）；团队获得 10 周 Go 生产经验（这是后续所有阶段的隐性前提） |

**退出标准**：aisaas MVP 上线且 P95 < 3s 达标；xiaozhi-aissaas 链路稳定运行 ≥ 2 周。

---

## 4. Phase 1（W11-W14）：ykt-admin 用 Go 重建

### 4.1 为什么是"重建"而不是"迁移"

- 3,670 行骨架，翻译不如按 Go 惯例重写（GORM/gin/slog 模式与 MyBatis-Plus/Sa-Token 无一一对应）
- 顺带修正 Java 版的结构债：5 维指标体系只落地了一部分，Go 版按 [METRICS-DESIGN.md] 完整实现
- 新增的 aisaas 监控（本阶段需求）直接用 Go 写，避免 Java 版继续生长

### 4.2 任务分解（1 名后端，W11-W13 开发 + W14 切换）

| 周 | 任务 | 产出/验证 |
|---|---|---|
| W11 前半 | **platform 库抽取**：aisaas `internal/platform` → `ykt.dev/platform` 共享模块（go.work：config/logger/redisx/web/errs/gorm 装配） | aisaas 改引共享库，回归测试绿 |
| W11 后半 | ykt-admin-go 脚手架：gin + gorm 双数据源（ykt 主库 + xiaozhi 只读库）+ jwt 登录 + 操作审计中间件 | /healthz + 登录可用 |
| W12 | system 域重建：用户/角色/菜单/字典/操作日志（对应 ykt-admin-system-api，461 行） | CRUD + 权限中间件 ✅ |
| W12-W13 | **monitor 域重建（重点）**：5 维指标（连接/会话/动作/延迟/离线）+ 告警规则 + 大盘；告警表达式用 `expr-lang/expr`（若 Java 版 Aviator 已存规则，写 Aviator→expr 转换器 + 双引擎金测对拍） | 指标与 Java 版对账一致 ✅ |
| W13 | 数据迁移：`init.sql`/`V2__metrics.sql` 生成 golang-migrate baseline；对接 xiaozhi 只读库 + 订阅 Redis `xiaozhi:events:*` | 指标采集链路通 ✅ |
| W14 | **并行运行 + 切换**：Go 版只读挂同一 MySQL 跑 3 天，大盘数据与 Java 版比对；Nginx 切流量；Java 版冷备 2 周 | 切换完成，下线 Java 版 |

### 4.3 ykt-admin-go 项目结构（复用 aisaas 模式）

```
ykt-admin-go/
├── cmd/admin/main.go
├── internal/
│   ├── system/          # 用户/角色/菜单/字典/审计
│   ├── monitor/         # ⭐ metrics{connection,dialog,action,latency,offline} + alert + dashboard
│   ├── xiaozhi/         # 只读数据访问（sys_device/sys_message 只 SELECT）+ HTTP 客户端 + 事件订阅
│   └── aisaasmon/       # ⭐ 新增：监控 aisaas（Prometheus 抓取 + usage_detail 只读聚合）
├── migrations/
└── web/                 # （可选 P1）Vue 运维控制台，或暂用 Knife4j 风格 swag 页面
```

### 4.4 技术映射速查

| Java（ykt-admin） | Go（ykt-admin-go） |
|---|---|
| Sa-Token JWT | ykt.dev/platform/auth（jwt + RequirePerm 中间件） |
| MyBatis-Plus + PageHelper | GORM + platform 分页封装 |
| Dynamic Datasource（master/xiaozhi 只读） | 两个 `*gorm.DB` 实例显式注入（`xiaozhi.*Repo` 只拿只读库） |
| Aviator 告警表达式 | expr-lang/expr + Aviator 转换器（如有存量规则） |
| Redisson（锁/限流） | redsync + redis_rate（platform 已有） |
| Flyway | golang-migrate（baseline 现有库） |
| @AuditLog 切面 | gin 审计中间件（显式） |
| Knife4j | swaggo |

**退出标准**：Go 版大盘与 Java 版连续 3 天数据一致（误差 < 1%）；告警规则全部通过金测；Nginx 切换后 2 周无 P1 事故。

---

## 5. Phase 2（W15-W20）：融合期

| 周 | 任务 |
|---|---|
| W13（交叉） | xiaozhi-server 完成 aisaas 观察期 → **删除 xiaozhi-ai 模块**（-12,612 行），xiaozhi 瘦身为 3.05 万行 |
| W15-W16 | ykt-admin-go 补 aisaas 监控：抓取 aisaas Prometheus 指标 + 只读聚合 `ykt_aisaas.usage_detail`，形成"设备↔AI 调用"关联视图 |
| W17-W18 | xiaozhi-server 按需小改（仅限：为 aisaas 吸收 dialogue 做准备的接口暴露，见 Phase 3）；无则跳过 |
| W19-W20 | 全工作区运维收口：统一 Go 构建链（make/Docker/systemd）、统一日志规范（slog JSON）、值班手册、全链路压测复演 |

**产出**：工作区进入"**2 个 Go 服务（aisaas、admin-go）+ 1 个 Java 服务（xiaozhi 瘦身版）**"的稳定形态。这个形态本身是**可长期维持的终态之一**。

---

## 6. Phase 3（W21+ / 2027-Q1 起）：xiaozhi-server 决策点（触发器制）

### 6.1 触发器（满足任一才立项，否则永久停留在 Phase 2 形态）

| # | 触发器 | 量化标准 |
|---|---|---|
| T1 | 设备并发压垮 JVM | 单节点持续 > 3 万 WebSocket 长连接，或 GC 停顿导致音频链路 P95 > 3s 且无法调参解决 |
| T2 | 双语言运维成本实锤 | ≥ 2 次线上事故因 Java/Go 分裂（排障跳转/工具链不统一）直接延误 > 1 小时 |
| T3 | 团队形态 | aisaas + admin-go 生产运行 ≥ 3 个月，值班全由团队自主 cover，且季度排期有 ≥ 2 人 × 6 周余量 |

### 6.2 命中触发器后的两条路径

#### ✅ 首选 · 路径 B：吸收（不重写 xiaozhi）

**原理**：aisaas `realtime/`（通路 B）本就是 xiaozhi-dialogue 的 Go 移植——协议（hello/listen/iot/abort/goodbye/mcp + Opus 帧）、VAD、Opus codec、SessionRegistry 全部已在 Go 侧存在。把设备流量入口从 xiaozhi-dialogue（:8092）切到 aisaas 实时网关即可。

```
改造前（Phase 2 形态）：
  设备 ──WS──► xiaozhi-dialogue(:8092, Java) ──通路B──► aisaas(realtime, Go)

改造后（路径 B）：
  设备 ──WS──► aisaas 实时网关(Go)   ← 设备 URL 不变（Nginx /ws/xiaozhi/v1/ 改上游）
                 │
                 ├─ 消息/状态回写 ──HTTP──► xiaozhi-server(HTTP API, Java 保留)
                 └─ 后续可渐进合并入 ykt-admin-go
```

| 步骤 | 内容 | 工时 |
|---|---|---|
| B1 | aisaas realtime 补齐 dialogue 遗留职责：消息落库回写（调 xiaozhi HTTP API，**禁止直写其库**）、sys_device 状态同步、companion/播放控制 | 2-3 周 |
| B2 | 设备协议兼容验证：实验室固件矩阵（v2.2.6 及存量 v1）× 全消息类型；线上抓真实流量录制回放金测 | 1-2 周 |
| B3 | Nginx 按 deviceId 哈希灰度切流：5% → 25% → 50% → 100%，每档观察 ≥ 3 天 | 3-4 周（日历时间） |
| B4 | 下线 xiaozhi-dialogue 模块（-7,841 行）；xiaozhi-server 仅剩 HTTP 管理 API（1.58 万行，长期 Java 或并入 admin-go，另行评估） | 1 周 |
| 合计 | | **约 7-10 周（含灰度日历时间，人力 2 人）** |

#### 备选 · 路径 A：独立重写 xiaozhi-dialogue 为 Go 服务

仅在路径 B 被否决时使用（否决理由通常是：不接受 aisaas 与设备入口耦合）。

| 项 | 说明 |
|---|---|
| 做法 | 新建 `xiaozhi-dialogue-go`，复用 aisaas realtime 的 protocol/session/audio 包（拷贝或抽共享库），业务逻辑（消息持久化/摘要/设备状态/companion）从 Java 移植 |
| 工时 | 6-8 周 × 2 人 + 灰度 3-4 周 |
| 灰度 | 与 B3 相同的 Nginx deviceId 哈希切流 |
| 劣势 | 与路径 B 相比：多一份协议实现要长期维护；aisaas 与 dialogue-go 之间仍需通路 B 一跳 |

### 6.3 红线（两条路径通用）

- **固件兼容是硬约束**：ESP32 v1/v2 分区表不兼容、存量设备无法 OTA 升级 → Go 侧协议必须**字节级兼容**全部存量固件；以 B2 的录制回放金测为准出条件
- 切流期间 Java 版 dialogue 保留热备，Nginx 可 30 秒内回切上游

---

## 7. 容量与人员（3 后端 + 1 前端）

| 阶段 | 人力安排 |
|---|---|
| Phase 0（W1-10） | 3 后端全在 aisaas；前端做 aisaas Web 控制台 |
| Phase 1（W11-14） | 1 后端重建 ykt-admin-go；1 后端 aisaas 生产维护 + 缓冲期收尾；1 后端休整/技术债（或支援 admin-go） |
| Phase 2（W15-20） | 2 后端融合期任务；1 后端值班 + 小需求 |
| Phase 3 | 触发器命中后单独立项排期（若届时只有 3 人，需冻结其他需求或补人） |

---

## 8. 风险登记册

| 风险 | 概率 | 影响 | 对策 |
|---|---|---|---|
| aisaas MVP 延期挤压 Phase 1 | 中 | 全链顺延 | Phase 1 仅 1 人 3 周，可整体右移；W11-12 缓冲期是设计好的容错 |
| ykt-admin 重建期间 Java 版出线上问题 | 低 | 分心 | Java 版冻结 + 只修 P1/P2；重建窗口短（3 周） |
| Aviator 告警规则语义差异 | 低（存量规则少） | 告警误报/漏报 | 双引擎金测对拍全部存量规则，不过的规则人工改写后才切换 |
| 重建期间丢失 ykt-admin 未文档化的隐性逻辑 | 中 | 监控断档 | 重建前逐模块读代码列清单；并行运行 3 天对账是硬门槛 |
| 路径 B 吸收时设备协议回归 | 中 | **设备不可用（严重）** | B2 录制回放金测 + 实验室固件矩阵 + 哈希灰度 + 30 秒回切 |
| 团队 Go 疲劳（连续 6 个月两个系统重写） | 中 | 质量下滑/离职 | Phase 2 刻意留白（W17-18 按需）；Phase 3 触发器 T3 明确要求排期余量 |
| 三系统同时变更叠加 | 低（已被计划排除） | 生产事故 | 本计划的核心设计就是串行 + 冻结；任何并行诉求需评审会特批 |

---

## 9. 阶段门（Go/No-Go）

| 门 | 时点 | 通过条件 |
|---|---|---|
| G1 | W10 末 | aisaas MVP 上线、P95<3s、xiaozhi 链路稳定 2 周 → 放行 Phase 1 |
| G2 | W14 末 | ykt-admin-go 数据对账 <1% 差异、告警金测全绿、切换无 P1 → 放行 Phase 2 |
| G3 | W20 末 | 融合期任务完成、运维收口、值班自主 → 进入"长期稳定形态"，**默认停止** |
| G4 | 触发器评审（每季度一次） | T1/T2/T3 任一命中才立项 Phase 3；评审结论记录在案 |

---

## 10. 明确不做的事（Anti-Scope）

- ❌ 不把 ESP32 固件纳入 Go 化讨论（C++/ESP-IDF，与后端语言无关）
- ❌ 不重写 xiaozhi-service / xiaozhi-server 的 HTTP 管理部分（1.58 万行内部 CRUD，重写零收益）
- ❌ 不为"全 Go 洁癖"服务——Java 孤岛通过 HTTP/MySQL/Redis 集成，长期共存是合法终态
- ❌ Phase 0 期间不给 Java 版 ykt-admin 加 >500 行新功能
- ❌ 不在 aisaas MVP 窗口内启动任何迁移编码

---

## 11. 待评审

- [ ] W1 基准日确认（2026-08-31 还是顺延一周）
- [ ] ykt-admin 是否有我没看到的隐性资产（已上线的定时任务/对接的告警渠道），影响 Phase 1 范围
- [ ] Phase 1 人力指定（谁重建 ykt-admin-go——建议 aisaas 中写 platform 层最多的人）
- [ ] 路径 B 的架构否决权：是否接受"设备入口归 aisaas"这个终局形态（建议接受，这是本计划最省钱的设计）
- [ ] G4 触发器评审的负责人与节奏（建议：技术负责人 + 运维，每季度首月）
