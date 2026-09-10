package anomaly

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// MetricsCollector 指标收集器接口。
// 从 Redis Stream 聚合指定窗口的指标值。
type MetricsCollector interface {
	Collect(ctx context.Context, metric string, window time.Duration) (float64, error)
}

// RedisStreamCollector 基于 Redis Stream 的指标收集器。
// 复用现有 metering stream（aisaas:metering:stream）。
type RedisStreamCollector struct {
	rdb        *redisx.Client
	streamName string
}

// NewRedisStreamCollector 创建 Redis Stream 指标收集器。
func NewRedisStreamCollector(rdb *redisx.Client) *RedisStreamCollector {
	return &RedisStreamCollector{
		rdb:        rdb,
		streamName: "aisaas:metering:stream",
	}
}

// StreamName 返回当前使用的 stream 名称。
func (c *RedisStreamCollector) StreamName() string {
	return c.streamName
}

// Collect 从 Redis Stream 聚合指定窗口的指标值。
// 使用 XRange 查询最近 window 时间内的记录，按 metric 字段聚合求和。
func (c *RedisStreamCollector) Collect(ctx context.Context, metric string, window time.Duration) (float64, error) {
	now := time.Now()
	start := now.Add(-window)

	startID := strconv.FormatInt(start.UnixMilli(), 10)
	endID := strconv.FormatInt(now.UnixMilli(), 10)

	records, err := c.rdb.XRange(ctx, c.streamName, startID, endID).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}

	var total float64
	for _, record := range records {
		if v, ok := record.Values[metric]; ok {
			switch val := v.(type) {
			case string:
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					total += f
				}
			case float64:
				total += val
			case int64:
				total += float64(val)
			}
		}
	}
	return total, nil
}