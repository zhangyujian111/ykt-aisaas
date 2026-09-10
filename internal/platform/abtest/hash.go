package abtest

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// HashBucket 计算实验的用户哈希桶（0~10000 范围）。
//
// 使用 SHA-256 哈希，确保：
//   - 确定性：同一 (experimentName, userID) 始终产生相同桶
//   - 均匀分布：哈希值均匀分布在 0~10000 范围
//   - 无中心化依赖：不依赖 Redis 等外部服务
func HashBucket(experimentName, userID string) int {
	key := fmt.Sprintf("%s:%s", experimentName, userID)
	hash := sha256.Sum256([]byte(key))

	// 取前 8 字节转为 uint64，再对 10000 取模
	hashUint := binary.BigEndian.Uint64(hash[:8])
	return int(hashUint % 10000)
}

// BucketToPercent 将哈希桶（0~10000）转换为百分比（0.00~100.00）。
func BucketToPercent(bucket int) float64 {
	return float64(bucket) / 100.0
}

// SelectVariant 根据哈希桶和变体分配比例选择变体。
//
// variants 按 allocation_percent 升序排列，累计百分比超过 bucket 时选中。
// 若所有变体都不匹配（因浮点精度），返回最后一个变体作为兜底。
func SelectVariant(bucket int, variants []*Variant) *Variant {
	if len(variants) == 0 {
		return nil
	}

	cumulative := 0.0
	bucketPercent := BucketToPercent(bucket)

	for _, v := range variants {
		cumulative += v.AllocationPercent
		if bucketPercent < cumulative {
			return v
		}
	}

	// 兜底：返回最后一个变体（浮点精度边界情况）
	return variants[len(variants)-1]
}