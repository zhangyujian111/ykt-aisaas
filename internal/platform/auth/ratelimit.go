package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// LoginRateLimit 登录暴力破解防护。
// 同一 IP/用户名 5 次登录失败 → 15min 冷却。
// Redis Key：aisaas:portal:login:fail:{ipOrUsername}
// 返回 true = 允许尝试，false = 已被冷却。
func LoginRateLimit(ctx context.Context, rdb *redisx.Client, ip, username string, maxFail int, cooldown time.Duration) bool {
	if maxFail <= 0 {
		maxFail = 5
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}

	// 检查 IP 级别
	ipKey := fmt.Sprintf("aisaas:portal:login:fail:%s", ip)
	ipCount, _ := rdb.Get(ctx, ipKey).Int64()
	if ipCount >= int64(maxFail) {
		return false
	}

	// 检查用户名级别
	userKey := fmt.Sprintf("aisaas:portal:login:fail:%s", username)
	userCount, _ := rdb.Get(ctx, userKey).Int64()
	if userCount >= int64(maxFail) {
		return false
	}

	return true
}

// LoginFailRecord 记录登录失败。
func LoginFailRecord(ctx context.Context, rdb *redisx.Client, ip, username string, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}
	ipKey := fmt.Sprintf("aisaas:portal:login:fail:%s", ip)
	userKey := fmt.Sprintf("aisaas:portal:login:fail:%s", username)

	// 用 Pipelined 保证 INCR + EXPIRE 原子
	_, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Incr(ctx, ipKey)
		pipe.Expire(ctx, ipKey, cooldown)
		pipe.Incr(ctx, userKey)
		pipe.Expire(ctx, userKey, cooldown)
		return nil
	})
	if err != nil {
		// 限流记录失败不应阻塞登录，忽略错误
		_ = err
	}
}

// LoginFailReset 登录成功后重置计数。
func LoginFailReset(ctx context.Context, rdb *redisx.Client, ip, username string) {
	ipKey := fmt.Sprintf("aisaas:portal:login:fail:%s", ip)
	userKey := fmt.Sprintf("aisaas:portal:login:fail:%s", username)
	rdb.Del(ctx, ipKey)
	rdb.Del(ctx, userKey)
}