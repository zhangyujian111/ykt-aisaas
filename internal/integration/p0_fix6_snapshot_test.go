//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// TestQuotaSnapshot_Deduct_Atomic 测试配额快照 Lua 脚本的原子性。
func TestQuotaSnapshot_Deduct_Atomic(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，配额 1000
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"
	sessionID := "session_atomic_" + uuid.NewString()[:8]

	// 设置配额限制
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 0, 35*24*time.Hour)

	// Act: 预扣 600
	remaining, snapshot, err := env.rdb.QuotaSnapshotDeduct(ctx, tenantID, dim, sessionID, 600)
	if err != nil {
		t.Fatalf("QuotaSnapshotDeduct failed: %v", err)
	}

	// Assert: 剩余 400，快照 600
	if remaining != 400 {
		t.Errorf("expected remaining=400, got %d", remaining)
	}
	if snapshot != 600 {
		t.Errorf("expected snapshot=600, got %d", snapshot)
	}

	// Assert: Redis used 计数器增加 600
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 600 {
		t.Errorf("expected used=600, got %d", used)
	}

	// Assert: session 快照存在
	sessionKey := redisx.KeySessionQuota(sessionID, dim)
	initial, _ := env.rdb.HGet(ctx, sessionKey, "initial").Int64()
	if initial != 600 {
		t.Errorf("expected session initial=600, got %d", initial)
	}

	t.Logf("Atomic deduct success: remaining=%d, snapshot=%d, used=%d", remaining, snapshot, used)
}

// TestQuotaSnapshot_Refund_ActualUsed 测试按实际用量退还配额。
func TestQuotaSnapshot_Refund_ActualUsed(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，预扣 800，实际使用 600
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"
	sessionID := "session_refund_" + uuid.NewString()[:8]

	// 设置配额限制
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 800, 35*24*time.Hour)

	// 预先设置 session 快照
	sessionKey := redisx.KeySessionQuota(sessionID, dim)
	env.rdb.HSet(ctx, sessionKey, map[string]any{
		"initial":         800,
		"remaining":       800,
		"device_used_after": 800,
	})
	env.rdb.Expire(ctx, sessionKey, 35*24*time.Hour)

	// Act: 退款（initial - actualUsed = 800 - 600 = 200）
	err := env.rdb.QuotaSnapshotRefund(ctx, tenantID, dim, sessionID, 600)
	if err != nil {
		t.Fatalf("QuotaSnapshotRefund failed: %v", err)
	}

	// Assert: Redis used 计数器减少 200
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 600 {
		t.Errorf("expected used=600 after refund, got %d", used)
	}

	// Assert: session 快照被删除
	exists := env.rdb.Exists(ctx, sessionKey).Val()
	if exists != 0 {
		t.Error("expected session key to be deleted after refund")
	}

	t.Logf("Snapshot refund success: used=%d", used)
}

// TestQuotaSnapshot_ConcurrentSafety 测试并发场景下 Lua 脚本的原子性（防超卖）。
func TestQuotaSnapshot_ConcurrentSafety(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，配额 1000，100 个 goroutine 各借记 1
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"

	// 设置配额限制为 1000
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 0, 35*24*time.Hour)

	// Act: 100 个 goroutine 并发借记
	var wg sync.WaitGroup
	successCount := 0
	failCount := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session_concurrent_%d_%s", idx, uuid.NewString()[:8])
			_, _, err := env.rdb.QuotaSnapshotDeduct(ctx, tenantID, dim, sessionID, 1)
			mu.Lock()
			if err == nil {
				successCount++
			} else {
				failCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Assert: 100 次借记全部成功（原子性保证）
	if successCount != 100 {
		t.Errorf("expected 100 successful deductions, got %d success, %d fail", successCount, failCount)
	}

	// Assert: 最终 used = 100（无超额扣）
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 100 {
		t.Errorf("expected used=100, got %d (possible over-deduction)", used)
	}

	// Assert: 不可能出现 used > 100（Lua 原子性保证）
	if used > 100 {
		t.Errorf("CRITICAL: used=%d exceeds limit=100, Lua atomicity violated", used)
	}

	t.Logf("Concurrent safety test: success=%d, fail=%d, used=%d", successCount, failCount, used)
}

// TestQuotaSnapshot_ExceedLimit 测试配额不足时拒绝借记。
func TestQuotaSnapshot_ExceedLimit(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，配额 100
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"
	sessionID := "session_exceed_" + uuid.NewString()[:8]

	// 设置配额限制为 100
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 100, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 0, 35*24*time.Hour)

	// Act: 尝试借记 200（超过限制）
	_, _, err := env.rdb.QuotaSnapshotDeduct(ctx, tenantID, dim, sessionID, 200)

	// Assert: 应该返回配额不足错误
	if err == nil {
		t.Error("expected quota exceeded error, got nil")
	}

	// Assert: Redis used 计数器仍然为 0
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 0 {
		t.Errorf("expected used=0 after failed deduction, got %d", used)
	}

	t.Logf("Exceed limit test passed: err=%v, used=%d", err, used)
}

// TestQuotaSnapshot_LimitNotLoaded 测试 limit 未加载时放行（不限量）。
func TestQuotaSnapshot_LimitNotLoaded(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，不设置配额限制
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"
	sessionID := "session_nolimit_" + uuid.NewString()[:8]

	// 不设置 limit key，模拟 limit 未加载场景

	// Act: 尝试借记 1000
	_, _, err := env.rdb.QuotaSnapshotDeduct(ctx, tenantID, dim, sessionID, 1000)

	// Assert: 不应该报错（limit 未配置视为不限量）
	if err != nil {
		t.Errorf("expected no error when limit not loaded, got: %v", err)
	}

	t.Log("Limit not loaded test passed: unlimited quota")
}
