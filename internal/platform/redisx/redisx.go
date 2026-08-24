// Package redisx 封装 go-redis：连接、Key 规范、配额预扣 Lua。
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"ykt.dev/aisaas/internal/platform/config"
)

// Client 平台 Redis 客户端。
type Client struct {
	*redis.Client
}

// New 建立 Redis 连接（含 ping）。
func New(cfg *config.Redis) (*Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{c}, nil
}

// 计量维度常量（与 metering 对齐）
const (
	DimLLMTokensIn  = "llm_tokens_in"
	DimLLMTokensOut = "llm_tokens_out"
	DimTTSChars     = "tts_chars"
	DimASRSeconds   = "asr_seconds"
)

// ---- Key 规范（API.md/DATABASE.md §4）----

// KeyAPIKey API Key 缓存：aisaas:tenant:{tid}:apikey:{hash}
func KeyAPIKey(tenantID int64, keyHash string) string {
	return fmt.Sprintf("aisaas:tenant:%d:apikey:%s", tenantID, keyHash)
}

// KeyAPIKeyByHash 仅按 hash 索引（用于反查租户）。
func KeyAPIKeyByHash(keyHash string) string {
	return "aisaas:apikey:idx:" + keyHash
}

// KeyQuotaUsed 月度配额已用计数：aisaas:tenant:{tid}:quota:{dim}:{yyyymm}
func KeyQuotaUsed(tenantID int64, dim string, ym string) string {
	return fmt.Sprintf("aisaas:tenant:%d:quota:%s:%s", tenantID, dim, ym)
}

// KeyQuotaLimit 月度配额上限（从 DB 加载后的快照）。
func KeyQuotaLimit(tenantID int64, dim string, ym string) string {
	return KeyQuotaUsed(tenantID, dim, ym) + ":limit"
}

// ---- 配额预扣 Lua（原子，防超卖）----

const luaQuotaDeduct = `
local usedKey    = KEYS[1]
local limitKey   = KEYS[2]
local amount     = tonumber(ARGV[1])
local ttl        = tonumber(ARGV[2])

local limit = tonumber(redis.call('GET', limitKey) or '-1')
if limit < 0 then
  return -2  -- limit 未加载：允许放行（配额未配置=不限）
end
local used = tonumber(redis.call('GET', usedKey) or '0')
if used + amount > limit then
  return -1  -- 配额不足
end
redis.call('INCRBY', usedKey, amount)
redis.call('EXPIRE', usedKey, ttl)
return limit - used - amount
`

var quotaDeduct = redis.NewScript(luaQuotaDeduct)

// QuotaDeduct 预扣配额。返回（剩余额度，error）。
// limit 未配置时视为不限量（返回 0, nil）；不足返回 QuotaExceeded。
func (c *Client) QuotaDeduct(ctx context.Context, tenantID int64, dim string, amount int64) (int64, error) {
	ym := time.Now().Format("200601")
	usedKey := KeyQuotaUsed(tenantID, dim, ym)
	limitKey := KeyQuotaLimit(tenantID, dim, ym)
	res, err := quotaDeduct.Run(ctx, c.Client, []string{usedKey, limitKey},
		amount, int64((35*24*time.Hour)/time.Second)).Int64()
	if err != nil {
		return 0, fmt.Errorf("quota lua: %w", err)
	}
	switch res {
	case -2:
		return 0, nil // 未配置限额
	case -1:
		return 0, ErrQuotaExceeded
	default:
		return res, nil
	}
}

// QuotaRollback 预扣回滚（调用失败时）。
func (c *Client) QuotaRollback(ctx context.Context, tenantID int64, dim string, amount int64) {
	ym := time.Now().Format("200601")
	c.DecrBy(ctx, KeyQuotaUsed(tenantID, dim, ym), amount)
}

// QuotaSetLimit 设置/刷新租户维度限额快照（TTL 35 天）。
func (c *Client) QuotaSetLimit(ctx context.Context, tenantID int64, dim string, limit int64) error {
	ym := time.Now().Format("200601")
	return c.Set(ctx, KeyQuotaLimit(tenantID, dim, ym), limit, 35*24*time.Hour).Err()
}

// QuotaRemaining 查询剩余。
func (c *Client) QuotaRemaining(ctx context.Context, tenantID int64, dim string) (limit, used int64) {
	ym := time.Now().Format("200601")
	limit, _ = c.Get(ctx, KeyQuotaLimit(tenantID, dim, ym)).Int64()
	used, _ = c.Get(ctx, KeyQuotaUsed(tenantID, dim, ym)).Int64()
	return
}
