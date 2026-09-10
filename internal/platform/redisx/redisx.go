// Package redisx 封装 go-redis：连接、Key 规范、配额预扣 Lua。
package redisx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// QuotaRefund 通用配额退款（支持任意 amount）。
// 与 QuotaRollback 语义相同：DecrBy used 计数器。
func (c *Client) QuotaRefund(ctx context.Context, tenantID int64, dim string, amount int64) {
	c.QuotaRollback(ctx, tenantID, dim, amount)
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

// ---- quota_snapshot Lua 原子借记（Fix 6）----

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

var quotaSnapshotDeduct = redis.NewScript(luaQuotaSnapshotDeduct)

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
	if remaining == -2 {
		return 0, 0, nil
	}
	if remaining == -1 {
		return 0, 0, ErrQuotaExceeded
	}
	return remaining, snapshot, nil
}

// QuotaSnapshotRefund session 结束时退差额。
// 1. 读取 session 快照 initial 值
// 2. DecrBy device_quota_used(initial - actualUsed)
// 3. Del session_quota_key
func (c *Client) QuotaSnapshotRefund(ctx context.Context, tenantID int64, dim, sessionID string, actualUsed int64) error {
	sessionKey := KeySessionQuota(sessionID, dim)
	initial, err := c.HGet(ctx, sessionKey, "initial").Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			slog.Debug("session quota key not found, skipping refund",
				"sessionKey", sessionKey, "tenantID", tenantID, "dim", dim)
			return nil // session 不存在（可能已过期）
		}
		return fmt.Errorf("hget session quota: %w", err)
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

// KeySessionQuota session 配额快照 Key。
func KeySessionQuota(sessionID, dim string) string {
	return fmt.Sprintf("aisaas:session:%s:quota:%s", sessionID, dim)
}
