# 文档变更记录（CHANGELOG）

> 本文件记录所有架构文档的版本变更。每个版本对应一次完整的评审周期。

---

## [v2.2] — 2026-08-24（**当前版本**）

### 触发原因

用户指令：ykt-aisaas 统一目录（删除多余的 ykt-aisaas-go 后缀目录）+ **移除全部 Java 实现内容**。

### 变更

1. **目录合并**：`ykt-aisaas-go/*` 并入 `ykt-aisaas/`（docs/ 与 Go 代码同库；`ykt-aisaas-go` 已删除）
2. **代码层**：确认无 Java 代码（历史上只生成过文档，从未落 Java 骨架）
3. **文档去 Java 化**：
   - MODULES.md **全文重写**：Maven 模块树/Java 包结构 → 实际 Go 代码结构（对齐 cmd/internal/pkg/tools，标注 ✅ 已实现项 + 请求链路 + 接口契约 + backlog）
   - ARCHITECTURE.md：§3 技术栈表换 Go；§4.2 换 Go 目录；§5.2 租户上下文（TTL/Reactor 双路径与陷阱清单 → Go context 单机制）；§6/7/8/11 的 Java 代码示例 → Go 或文字说明
   - API.md：去除 Spring AI 字样，Java 示例改为"调用方示例"（xiaozhi 接入参考保留）
   - OVERVIEW：部署图 Java → Go
4. **合理保留的 Java 字样**：GOLANG-IMPLEMENTATION（迁移映射表）、GO-MIGRATION-PLAN（Java 系统迁移计划）、CHANGELOG（历史）、通路 B/xiaozhi 调用方示例（Java 系统接入参考）

---

## [v2.1] — 2026-08-24

### 触发原因

用户决策：**ykt-admin 一起转 Go；xiaozhi-server 视必要性转 Go**。

### 关键发现（实测代码规模）

- ykt-admin 仅 **109 文件 / 3,670 行**（骨架期）→ 定性为"重建"而非"迁移"，1 人 × 3 周
- xiaozhi-server 43,082 行，其中 xiaozhi-ai 12,612 行本就要删（v2.0 W13）；dialogue 7,841 行是唯一并发核心，可被 aisaas 实时网关吸收

### 新增文档

`GO-MIGRATION-PLAN.md` — 四阶段迁移计划：

| 阶段 | 时间 | 内容 |
|---|---|---|
| Phase 0 | W1-10 | aisaas MVP，其余系统冻结（bugfix only） |
| Phase 1 | W11-14 | ykt-admin 用 Go 重建（platform 库抽取 + 双数据源 + 5 维指标 + 并行对账切换） |
| Phase 2 | W15-20 | 融合期：admin-go 接管监控（含 aisaas）、xiaozhi 瘦身收尾、运维收口 |
| Phase 3 | W21+ | xiaozhi 触发器制（T1 并发/T2 双语言成本/T3 团队余量），首选路径 B"吸收"（设备入口切 aisaas 实时网关），备选 A 独立重写 dialogue-go |

终态预期：**2 Go 服务（aisaas + admin-go）+ 1 Java 孤岛（xiaozhi 瘦身版）**，该形态为合法长期终态。

---

## [v2.0-GO] — 2026-08-12

### 触发原因

用户决策：**实现语言从 Java 切换为 Golang**。

### 核心判断

- v1.2 选 Java 的核心理由（拷贝 xiaozhi-ai provider + Spring AI 生态）经重估收益有限：MVP 仅 4 个 provider 且已统一 OpenAI 协议
- Go 的 `context.Context` **结构性消除** v1.2 头号风险（多租户上下文在流式/异步链路丢失）
- goroutine 原生适配流式管道；同物理机部署内存占用小 5-10 倍
- asynq 替代 RocketMQ+PowerJob，**少运维 2 个中间件**
- 代价：工期 8-9 周 → **10-12 周**（含团队 Go ramp）

### 变更范围

| 文档 | 效力 |
|---|---|
| 新增 `GOLANG-IMPLEMENTATION.md` | Go 技术栈映射 / 项目结构 / 核心代码骨架（租户插件/客户端/流水线/计量）/ 工期重估 |
| DATABASE.md | ✅ 100% 有效 |
| API.md | ✅ 100% 有效 |
| ARCHITECTURE.md | §3 技术栈、§7.3+ 代码示例被取代；业务架构/隔离/部署/xiaozhi 改造有效 |
| MODULES.md | 模块边界有效；Maven 树被 Go 项目结构取代 |
| OVERVIEW-NON-TECHNICAL.md | ✅ 有效（业务方无感知） |

### 技术栈关键替换

| Java | Go |
|---|---|
| SpringBoot 3.5.8 | Gin + gorilla/websocket |
| Spring AI | 自研 openaiclient（~500 行）+ mark3labs/mcp-go |
| MyBatis-Plus 租户拦截器 | GORM 自研租户插件 |
| Sa-Token | golang-jwt/v5 + 自研中间件 |
| RocketMQ + PowerJob | **asynq（Redis）** |
| Reactor Flux + TTL | goroutine + channel + context |
| Flyway | golang-migrate |
| Concentus/ONNX | hraban/opus + sherpa-onnx-go |

---

## [v1.2] — 2026-08-12

### 触发原因

用户在 v1.1 基础上拍板 5 个细节决策：
1. WebSocket 双通路（A+B）→ **MVP 只做 B**，A 延后 V1.1
2. xiaozhi-dialogue 复用方式 → **拷贝**（不抽 SPI）
3. 内部超级租户鉴权 → **`X-Internal-Token` Header**（不用 mTLS）
4. 平台与 xiaozhi-server 部署 → **同一台物理机**（localhost < 1ms）
5. （重申）MVP 阶段只做通路 B

### 影响范围

| 维度 | v1.1 | v1.2 |
|---|---|---|
| WebSocket 通路 | A + B 都做 | **仅 B**（A 延后 V1.1） |
| 内部超级租户鉴权 | 待评审 | **`X-Internal-Token` Header** |
| 平台与 xiaozhi-server 部署 | 同机房（< 5ms） | **同物理机**（localhost < 1ms） |
| xiaozhi-dialogue 复用 | 待评审 | **拷贝**（不抽 SPI） |
| MVP 工时 | 10 周 | **8-9 周**（节省 1-2 周） |
| MVP 持续时间 | 3 个月 | **2.5 个月** |

### 各文档变更详情

#### ARCHITECTURE.md

- **§1.1 红线**：从"同机房 < 5ms"改为"同物理机 < 1ms"
- **§7.1 协议选型**：通路 A 标为"延后 V1.1"，新增"为什么 MVP 只做通路 B"小节
- **§7.5 WebSocket 实时流**：通路 A 标为"延后 V1.1"（保留规划，不在 MVP 实现）；通路 B 鉴权方式明确为 `X-Internal-Token`
- **§10.1 部署架构**：标题改为"同物理机"，加入"为什么必须同物理机"和"未来演进"说明
- **§13 决策表**：新增 4 条 v1.2 决策（通路 A 延后、拷贝 dialogue、X-Internal-Token、同物理机）
- 评审清单更新为 v1.2

#### MODULES.md

- **§3.13 ai-realtime**：删除 `protocol/openai/` 子树（约 15 个类），删除 `OpenAiRealtimeSession`、`RealtimeWebSocketHandler` 等
- **§9 开发顺序**：8 周 → **8-9 周**（原 10 周）
  - Week 5-6 ai-realtime 只做通路 B（节省 1 周）
  - xiaozhi 改造从 Week 6 提前到 Week 5（因平台进度快）
  - P1 阶段增加"通路 A"作为待办
- **§10 待评审**：移除已决策项，新增 v1.2 待评审项

#### API.md

- **§0 总览**：增加"MVP"列，通路 A 明确标"❌ 延后 V1.1"；新增"外部租户实时对话方案"说明
- **§5 WebSocket**：
  - §5.1 通路 A：整段重写为"延后 V1.1"（保留规划信息但不展开）
  - §5.2 通路 B：鉴权方式明确为 `X-Internal-Token` Header，新增调用示例
  - §5.3 新增：外部租户的"实时对话"替代方案（SSE + HTTP Chunked）
- **§15 待评审**：移除已决策项，新增 v1.2 待评审项

#### OVERVIEW-NON-TECHNICAL.md

- **§2 整体架构图**：v1.2 关键变化清单更新（含 5 条新决策）
- **§10 路线图**：MVP 从 3 个月压缩到 2.5 个月；V1.1 增加通路 A；关键里程碑时间前移

### v1.2 已决策清单（不再评审）

- ✅ ~~WebSocket 通路 A vs B MVP 是否都做~~ → 只做 B
- ✅ ~~xiaozhi-dialogue 拷贝 vs SPI~~ → 拷贝
- ✅ ~~内部超级租户鉴权 X-Internal-Token vs mTLS~~ → X-Internal-Token
- ✅ ~~平台与 xiaozhi-server 部署形态~~ → 同物理机
- ✅ ~~8-9 周工时是否紧张~~ → 不再讨论，按现计划执行
- ✅ ~~通路 B 代码同步策略~~ → 每月对账 + 关键 bug 即时 cherry-pick
- ✅ ~~外部租户长音频 ASR（>60 秒）~~ → MVP 不支持，超过提示分片
- ✅ ~~通路 B 协议演进策略~~ → 保持 1 个月滞后窗口

---

## [v1.1] — 2026-08-12

### 触发原因

用户指出关键修正：**"xiaozhi-server 后期作为本平台的'内部租户'接入，但 MVP 阶段双轨运行" — 错误**。

正确表述：**MVP 阶段即改造 xiaozhi-server 接入 ykt-aisaas**，不再双轨运行。

### 影响范围（连锁修改 5 份文档 + 新增 CHANGELOG）

| 维度 | v1.0 | v1.1 |
|---|---|---|
| 平台定位 | 对外 SaaS | **内外统一 AI 能力平台** |
| MVP 范围 | 通用 AI API | **必须满足 xiaozhi-server 全部 AI 场景** |
| WebSocket 协议 | P1（设备阶段才做） | **P0（设备实时双向流）** |
| 性能要求 | 通用 SaaS | **设备对话全链路 P95 < 3 秒** |
| 部署架构 | 独立部署 | **与 xiaozhi-server 同机房**（v1.2 改为同物理机） |
| 认证模型 | API Key + Sa-Token | 增加**内部超级租户**（特殊权限 + 设备上下文） |
| 数据迁移 | 无 | xiaozhi `sys_config`/`sys_role`/`sys_template`/`sys_knowledge_*` 迁到平台 |
| xiaozhi-ai 模块 | 双轨保留 | **MVP 期内接入平台**（保留 1 个月稳定期后删代码） |
| MVP 工时 | 8 周 | **10-12 周**（v1.2 调整为 8-9 周） |
| xiaozhi 融合路径 | 三阶段（MVP 双轨→V1.1 接入→V2.0 统一） | **两阶段**（MVP 即接入→V1.1 删 xiaozhi-ai） |

### 各文档变更详情

#### ARCHITECTURE.md

- **§1.1 平台定位**：从"对外 SaaS"改为"内外统一 AI 能力平台"
- **§1.2 能力清单**：增加"xiaozhi-server 是否依赖"列；WebSocket/Persona 从 P1 升 P0
- **§2.1 部署架构图**：完全重绘，加入 xiaozhi-server、设备 WebSocket 通路、ai-realtime 模块
- **§7 流式架构**：重大调整，WebSocket 从 P1 升 P0；增加 §7.5 WebSocket 实时流双通路（A=OpenAI Realtime 兼容，B=xiaozhi 内部协议）
- **§10 部署架构**：单机部署增加 xiaozhi-server 同主机，加入性能 SLA（P95 < 3s）
- **§11 演进路径**：三阶段改为两阶段（MVP 即融合 + 1 个月稳定期删代码）
- **§12 新增**：xiaozhi-server 改造方案（改造范围/数据迁移/工作量/风险/应急回滚/内部超级租户配置/接口调用映射）
- 原 §12/§13/§14 顺延为 §13/§14/§15

#### DATABASE.md

- **§3.1 租户域**：新增 `ykt_aisaas_internal_tenant_config`（内部超级租户配置）+ `ykt_aisaas_device_session`（设备-会话绑定）
- **§10 新增**：xiaozhi-server 数据迁移方案
  - §10.1 迁移总览（7 张表的迁移策略）
  - §10.2 字段映射（sys_config → model_registry，sys_role → persona，sys_knowledge_* → 平台 RAG）
  - §10.3 Flyway 脚本规划（V2/V3/V8 新增）
  - §10.4 迁移流程（Week 6/8/8-12/12+）
  - §10.5 应急回滚

#### MODULES.md

- **§4.2 模块树**：新增 `ykt-aisaas-ai-realtime`（WebSocket 实时流，P0）
- **§3.12 ai-stream**：剥离 WebSocket 实现到 ai-realtime
- **§3.13 新增**：`ykt-aisaas-ai-realtime` 模块详细设计（WebSocketConfig、RealtimeWebSocketHandler、XiaozhiBridgeHandler、SessionRegistry、protocol/{openai,xiaozhi}、audio/{vad,codec,frame}、pipeline/{RealtimePipeline,BargeInHandler}）
- 原 §3.13/3.14/3.15 顺延为 §3.14/3.15/3.16
- **§9 开发顺序**：8 周改为 10 周，新增 Week 5-6 ai-realtime；新增 xiaozhi-server 并行改造（Week 6-10，4 周）
- **§11 新增**：xiaozhi-server 改造任务清单（代码改造 13 人天 + 数据迁移 4 人天 + 联调 9 人天 = 26 人天）

#### API.md

- **§0 总览**：四类 API（增加 WebSocket 实时流 + xiaozhi 内部协议两类）
- **§3.1 OpenAI 兼容**：扩展 `x-` 字段，增加 `x-device-id`/`x-device-session-id`/`x-internal-call`/`x-barge-in-supported`（xiaozhi-server 内部调用专用）
- **§5 WebSocket**：完全重写，从 P1 升 P0
  - §5.1 通路 A：OpenAI Realtime API 兼容（完整事件列表）
  - §5.2 通路 B：xiaozhi 内部协议（保留现有消息类型）
  - §5.3 选择指南
- **§6 新增**：内部超级租户接口（`/internal/api/v1/*`）
  - 设备会话绑定、批量导入、实时统计 SSE
- 原 §6/§7/§8...顺延为 §7/§8/§9...

#### OVERVIEW-NON-TECHNICAL.md

- **§0 一句话讲清**：平台双重身份（对内+对外）
- **§2 整体架构图**：重绘，加入 xiaozhi-server、设备链路
- **§3.0 新增**：设备对话流数据流图（最关键场景，端到端 11 步 + barge-in 处理）
- **§10 路线图**：MVP 从 2 个月延长到 3 个月，加入 xiaozhi-server 改造任务
- **§11 与 xiaozhi 关系**：完全重写
  - 改造前 vs 改造后对比
  - 改造好处（代码量减少 50%、依赖瘦身、议价能力）
  - 两阶段融合路径
  - 风险与应急（feature flag 回滚）

### 新增的待评审问题

| 文档 | 待评审 |
|---|---|
| ARCHITECTURE | WebSocket 双通路（A+B）MVP 是否都做？建议先做 B |
| DATABASE | 内部租户的计量策略（不计费但统计 vs isInternal 标记）？ |
| MODULES | ai-realtime 是否过大？可拆 ai-realtime-core + ai-audio-codec |
| API | 通路 A vs B MVP 优先级？内部超级租户鉴权方式（X-Internal-Token vs mTLS）？ |
| OVERVIEW | 无新增 |

---

## [v1.0] — 2026-08-12（已废弃部分内容）

### 初版交付

5 份文档（架构 / 数据库 / 模块 / API / 非研发版），共 4724 行 / 约 21 万字符。

### v1.0 已被 v1.1 修正的关键错误

- ❌ "xiaozhi-server MVP 阶段双轨运行" → ✅ MVP 即接入
- ❌ "WebSocket 实时流 P1" → ✅ P0
- ❌ "三阶段融合路径" → ✅ 两阶段
- ❌ "MVP 8 周" → ✅ 10-12 周

其他 v1.0 内容（多租户字段隔离、OpenAI 兼容协议、PgVector、计量切面、模块化单体等）在 v1.1 中保持不变。

---

## 评审节奏建议

| 节点 | 动作 |
|---|---|
| **现在** | 请你过一遍 v1.1 各文档的"待评审"清单 |
| **2-3 天内** | 出 v1.2（针对评审反馈修订） |
| **Week 1 末** | 锁定 v1.X，开始写代码骨架（父 pom + common + framework） |
| **Week 2** | 启动主线开发 |

**未做的事项（避免误以为已完成）**：
- 没写代码骨架（pom.xml / 启动类 / 配置文件）
- 没画正式 PlantUML/drawio 图（文档里是 ASCII 框图）
- 没写测试用例
- 没写部署 docker-compose.yml
- 没设计 logo / UI 原型
