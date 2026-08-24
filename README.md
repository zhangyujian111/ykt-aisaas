# ykt-aisaas-go

YKT AI SaaS 平台 Go 实现（v0.1 可运行版本）。

设计文档：`../ykt-aisaas/docs/`（ARCHITECTURE / DATABASE / API / GOLANG-IMPLEMENTATION）。

## 当前版本包含

| 能力 | 状态 |
|---|---|
| 多租户（GORM 插件自动 WHERE tenantId / Create 填充 / fail-fast） | ✅ |
| API Key 鉴权（sha256 哈希存储 / Redis 缓存 / scope / IP 白名单 / 过期） | ✅ |
| 内部超级租户（X-Internal-Token + loopback，租户签发 Key） | ✅ |
| OpenAI 兼容 `/v1/chat/completions`（非流式 + SSE 流式 + x-metering 事件） | ✅ |
| OpenAI 兼容 `/v1/models` | ✅ |
| OpenAI 兼容 `/v1/audio/speech`（TTS 流式音频 + x-emotion + 字符计量） | ✅ |
| OpenAI 兼容 `/v1/audio/transcriptions`（ASR multipart + duration/emotion + 秒计量） | ✅ |
| **RAG**：知识库 CRUD / 文档上传→切片→向量化（异步管道）/ PgVector 检索 | ✅ |
| **RAG×Chat**：`x-knowledge-base-ids` 自动检索注入 + `x-rag-citations` 引用事件 | ✅ |
| **MCP 工具**：工具注册（http/builtin）/ 租户绑定 / 直调测试 | ✅ |
| **MCP×Chat**：`x-tools-mcp` 装载工具 + tool_calls 循环（5 轮上限）+ `x-tool-call` 事件 | ✅ |
| **计费闭环**：配额自动加载（启动+每小时）→ 按模型价格扣余额 → 流水 → 用量/余额查询 | ✅ |
| 充值（管理端记账版）`POST /internal/api/v1/tenants/:id/recharge` | ✅ |
| 模型注册表（全局/租户私有路由，AES-GCM 密钥加密） | ✅ |
| 配额预扣（Redis Lua 原子预扣 + 402 拒绝） | ✅ |
| 计量（Redis 实时计数 + 批量异步落库 usage_detail） | ✅ |
| API Key 管理接口 `/api/v1/apikeys`（租户隔离 CRUD） | ✅ |
| 统一错误码 / RequestID / slog JSON 日志 | ✅ |
| 测试：租户隔离 5 项 + openaiclient SSE 解析 | ✅ |

## 快速开始

```bash
# 0) Go 1.23+（本机无 go 时）
#    下载 https://goproxy.cn/dl/module/github.com/golang/go ... 或使用镜像 tarball 到 ~/go-toolchain

# 1) 起依赖（MySQL:13306 / Redis:16379，端口错开宿主已有服务）
make docker-up

# 2) 建表 + 种子数据（首次会自动安装 migrate CLI）
make migrate

# 3) 起 mock 上游（OpenAI 兼容，开发用）
make mock

# 4) 把演示模型指向上游并加密密钥（AES key 与 config.yaml 一致）
make seed-e2e

# 5) 启动平台
make run   # :8190
```

## 验证（e2e）

```bash
# 为租户 1001 签发首个 API Key（内部接口，运营后台同款路径）
KEY=$(curl -s -X POST http://127.0.0.1:8190/internal/api/v1/tenants/1001/apikeys \
  -H "X-Internal-Token: dev-internal-token" -H "Content-Type: application/json" \
  -d '{"name":"e2e","scope":["llm"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['apiKey'])")

# OpenAI 兼容：非流式
curl -s http://127.0.0.1:8190/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model":"demo-chat","messages":[{"role":"user","content":"你好"}]}'

# OpenAI 兼容：流式（SSE，含 x-metering 事件 + [DONE]）
curl -N http://127.0.0.1:8190/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model":"demo-chat","stream":true,"messages":[{"role":"user","content":"你好"}]}'

# 模型列表
curl -s http://127.0.0.1:8190/v1/models -H "Authorization: Bearer $KEY"

# TTS（流式音频，含情绪扩展）
curl -X POST http://127.0.0.1:8190/v1/audio/speech \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model":"tts-demo","input":"你好小智","voice":"longxiaoxia","x-emotion":"happy"}' -o out.wav

# ASR（multipart）
curl -X POST http://127.0.0.1:8190/v1/audio/transcriptions \
  -H "Authorization: Bearer $KEY" \
  -F "file=@out.wav" -F "model=asr-demo" -F "language=zh"

# RAG：建库 → 传文档 → 检索
KB=$(curl -s -X POST http://127.0.0.1:8190/api/v1/knowledge-bases \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"产品手册","embeddingModel":"mock-embed-16","embeddingDim":16}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])")
curl -X POST http://127.0.0.1:8190/api/v1/knowledge-bases/$KB/documents \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"fileName":"manual.md","content":"WiFi 配置：设置→网络→输入密码"}'
curl -X POST http://127.0.0.1:8190/api/v1/knowledge-bases/$KB/search \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"query":"怎么连WiFi","topK":3}'

# 计费：充值 → 调用扣费 → 查用量/余额
curl -X POST http://127.0.0.1:8190/internal/api/v1/tenants/1001/recharge \
  -H "X-Internal-Token: dev-internal-token" -H "Content-Type: application/json" \
  -d '{"amountCents":100000,"remark":"首充"}'
curl -s http://127.0.0.1:8190/api/v1/usage/overview -H "Authorization: Bearer $KEY"       # 用量+配额余量
curl -s http://127.0.0.1:8190/api/v1/billing/balance -H "Authorization: Bearer $KEY"      # 余额
curl -s http://127.0.0.1:8190/api/v1/billing/transactions -H "Authorization: Bearer $KEY" # 流水

# MCP：注册 HTTP 工具 → chat 自动调用
curl -X POST http://127.0.0.1:8190/api/v1/mcp/tools \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"get_weather","description":"查天气","toolType":"http","endpoint":"http://weather-svc/api","inputSchema":{"type":"object","properties":{"city":{"type":"string"}}}}'
curl -N http://127.0.0.1:8190/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model":"demo-chat","stream":true,"x-tools-mcp":true,"messages":[{"role":"user","content":"北京天气"}]}'
# ↑ SSE 首事件 event: x-tool-call（工具名/参数/结果），随后是终答 chunk 流

# RAG 对话（自动检索注入 + 引用事件）
curl -N http://127.0.0.1:8190/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model":"demo-chat","stream":true,"x-knowledge-base-ids":['"$KB"'],"messages":[{"role":"user","content":"怎么连WiFi"}]}' 

# 换 openai SDK 也行（base_url 指向平台）：
#   client = OpenAI(base_url="http://127.0.0.1:8190/v1", api_key=KEY)
```

## 测试

```bash
make test   # 租户隔离 + SSE 客户端解析
make vet
```

## 接真实上游（替换 mock）

```bash
go run ./tools/seedmodel \
  -model deepseek-chat \
  -base-url https://api.deepseek.com/v1 \
  -upstream deepseek-chat \
  -api-key <你的真实密钥> \
  -provider deepseek
```

## xiaozhi-server 接入映射（AI 能力全部依赖本平台）

| xiaozhi-server 原调用 | 替换为平台接口 | 状态 |
|---|---|---|
| `chatModelFactory.getModel(cfg).call/stream(...)` | `POST /v1/chat/completions`（OpenAI SDK 直接指 baseUrl） | ✅ 可用 |
| `ttsServiceFactory.get(cfg).synthesize(text)` | `POST /v1/audio/speech`（chunked 流式音频） | ✅ 可用 |
| `sttServiceFactory.get(cfg).recognize(audio)` | `POST /v1/audio/transcriptions`（VAD 切段后逐段上传） | ✅ 可用 |
| `knowledgeRagService.search(...)` | `POST /api/v1/knowledge-bases/{id}/search` + chat `x-knowledge-base-ids` 自动注入 | ✅ 可用 |
| `toolCallingManager` / MCP | chat `x-tools-mcp:true` + `/api/v1/mcp/tools`（http/builtin 工具 + 绑定） | ✅ 可用 |
| 设备实时双向流 | WebSocket 通路 B（Phase 3 / GO-MIGRATION-PLAN） | 📅 规划中 |

**接入步骤**（xiaozhi-server 侧）：
1. 运营用内部接口为 xiaozhi 租户（tenantId=1，不限配额）签发 scope=llm,tts,asr 的 Key：
   `POST /internal/api/v1/tenants/1/apikeys -H "X-Internal-Token: ..."`
2. LLM：`OpenAiChatModel.builder().baseUrl("http://127.0.0.1:8190/v1").apiKey(KEY)`（同物理机 <1ms）
3. TTS/ASR：HTTP 客户端按上面两个接口调用（音频体 chunked 透传）
4. 供应商切换（如 DeepSeek → 自建 vllm / cosyvoice-server）：平台 `seedmodel` 改 baseUrl，xiaozhi 零改动

**真实上游接入**（替换 mock，OpenAI 兼容端点直接透传）：
```bash
# LLM：DeepSeek
go run ./tools/seedmodel -model deepseek-chat -type chat -id 1 \
  -base-url https://api.deepseek.com/v1 -upstream deepseek-chat -api-key <KEY> -provider deepseek
# TTS：自建 CosyVoice OpenAI 兼容服务
go run ./tools/seedmodel -model cosyvoice -type tts -id 2 \
  -base-url http://cosyvoice:9880/v1 -upstream cosyvoice-v2 -api-key <KEY> -provider self-hosted
# ASR：whisper.cpp server / FunASR OpenAI 兼容端点
go run ./tools/seedmodel -model funasr -type asr -id 3 \
  -base-url http://funasr:8000/v1 -upstream paraformer -api-key <KEY> -provider self-hosted
# Embedding：bge-m3（TEI）/ DashScope text-embedding-v3
go run ./tools/seedmodel -model bge-m3 -type embedding -id 4 \
  -base-url http://tei:8080/v1 -upstream bge-m3 -api-key <KEY> -provider self-hosted
```

> 注：ivfflat 索引在万级以下数据会漏召回，Store 默认顺序扫描；数据上量后按 `lists ≈ rows/1000` 手工创建。

## 目录结构

```
cmd/aisaas/            入口（装配 + 优雅停机）
internal/platform/     框架层：errs/config/tenant/database(租户插件)/redisx/auth/quota/metering/web(SSE)
internal/llm/          模型注册表 + Chat 服务
internal/tenantm/      API Key 管理
internal/server/       路由装配：v1(OpenAI 兼容)/apiv1(自有)/internalapi(内部)
pkg/openaiclient/      OpenAI 兼容客户端（SSE 流式解析 + 半行保护）
tools/mockupstream/    OpenAI 兼容 mock 上游
tools/seedmodel/       模型注册 seed 工具（AES 加密）
migrations/            golang-migrate SQL
deploy/                docker-compose 依赖栈
```

## 配置

`config.yaml`（env 覆盖：`AISAA_MYSQL_DSN`、`AISAA_SERVER_INTERNALTOKEN` 等）。

## 下一步（按 GOLANG-IMPLEMENTATION §8 路线）

- [x] W4：TTS / ASR（OpenAI 兼容透传）
- [x] W7：RAG（PgVector 动态表 + ingest 管道 + 检索 + chat 注入）
- [x] MCP 工具调用（注册/绑定/循环/事件）
- [ ] 外部 MCP Server 接入（stdio/sse 协议客户端，mcp_server 表）
- [ ] realtime 通路 B（xiaozhi 协议 + VAD/Opus + pipeline + Barge-in）
- [ ] 支付宝/微信在线充值（当前为管理端手动记账）
- [ ] 月账单生成 cron（bill 表已建）
- [ ] 重排序（rerank）
