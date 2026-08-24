-- 000002_seed.up.sql — 种子数据（内部租户 + 演示租户）
-- 注意：apiKeyEnc 需要 AES-GCM(key=config.crypto.aesKey) 加密后写入，
-- 真实上游密钥用 tools/seedmodel 工具生成；此处 seed 一个演示端点（mock-upstream）。
SET NAMES utf8mb4;

-- 内部超级租户（xiaozhi-server）
INSERT IGNORE INTO ykt_aisaas_tenant (id, code, name, status, remark)
VALUES (1, 'xiaozhi-internal', '小智 AI 内部租户', 1, '内部超级租户，不限配额');

INSERT IGNORE INTO ykt_aisaas_internal_tenant_config (id, tenantId, tenantType, isUnlimited, internalApiKey, status)
VALUES (1, 1, 'xiaozhi', 1, 'seed-internal-key-replace-me', 1);

-- 演示租户
INSERT IGNORE INTO ykt_aisaas_tenant (id, code, name, status, remark)
VALUES (1001, 'demo', '演示租户', 1, 'e2e 测试用');

-- 演示租户配额（月 10 万 in / 10 万 out）
INSERT IGNORE INTO ykt_aisaas_quota (id, tenantId, periodStart, periodEnd, dimension, limitValue)
VALUES
  (1, 1001, DATE_FORMAT(NOW(), '%Y-%m-01'), LAST_DAY(NOW()) + INTERVAL 1 DAY, 'llm_tokens_in', 100000),
  (2, 1001, DATE_FORMAT(NOW(), '%Y-%m-01'), LAST_DAY(NOW()) + INTERVAL 1 DAY, 'llm_tokens_out', 100000);

INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId, balanceCents)
VALUES (1, 1001, 10000);

-- 演示模型：指向本机 mock-upstream（:18080/v1），密钥为明文 'demo-key' 的 AES-GCM 密文
-- 由 make seed 重新生成写入；此占位 hex 解密失败会在 Resolve 时报错（预期行为）
INSERT IGNORE INTO ykt_aisaas_model_registry
  (id, tenantId, modelId, provider, baseUrl, apiKeyEnc, upstreamModel, modality, type, contextLength, isDefault, status)
VALUES
  (1, NULL, 'demo-chat', 'mock', 'http://127.0.0.1:18080/v1', 'REPLACE_VIA_SEED', 'demo-chat', '["text"]', 'chat', 8192, 1, 1);

-- 演示模型定价（分/千token，mock 无真实成本，仅验证扣费链路）
UPDATE ykt_aisaas_model_registry SET priceInputCents = 10, priceOutputCents = 20 WHERE modelId = 'demo-chat' AND priceInputCents = 0;
