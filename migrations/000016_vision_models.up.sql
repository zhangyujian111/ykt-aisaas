-- Vision Models Table
-- Stores configuration for multimodal vision models (OpenAI GPT-4V, Qwen-VL, GLM-4V)
CREATE TABLE IF NOT EXISTS ykt_aisaas_vision_models (
    id VARCHAR(64) PRIMARY KEY,
    provider VARCHAR(32) NOT NULL,  -- 'openai' / 'qwen-vl' / 'glm-4v'
    model_name VARCHAR(64) NOT NULL,  -- 'gpt-4-vision-preview' / 'qwen-vl-max' / 'glm-4v-plus'
    api_base VARCHAR(255),
    api_key_encrypted TEXT,
    max_image_size INT DEFAULT 20971520,  -- 20MB
    max_tokens INT DEFAULT 4096,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_vision_provider (provider, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Seed default vision models
INSERT INTO ykt_aisaas_vision_models (id, provider, model_name, api_base, max_image_size, max_tokens) VALUES
    ('openai-gpt4v', 'openai', 'gpt-4-vision-preview', 'https://api.openai.com/v1', 20971520, 4096),
    ('qwen-vl-max', 'qwen-vl', 'qwen-vl-max', 'https://dashscope.aliyuncs.com/api/v1', 20971520, 2048),
    ('glm-4v', 'glm-4v', 'glm-4v-plus', 'https://open.bigmodel.cn/api/paas/v4', 20971520, 2048);
