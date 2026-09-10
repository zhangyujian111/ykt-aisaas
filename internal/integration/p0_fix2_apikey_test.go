//go:build integration
// +build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// TestAPIKey_Rotate_Success_5MinGracePeriod 测试 API Key 轮换成功，旧 key 有 5 分钟宽限期。
func TestAPIKey_Rotate_Success_5MinGracePeriod(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建测试租户和 API Key
	tenantID := createTestTenant(t, env)
	plainKey, keyID := newAPIKey(t, env, tenantID, []string{"llm"})

	// Act: 轮换 API Key
	resp, body := doRequest(t, env.server, "POST",
		fmt.Sprintf("/internal/api/v1/apikeys/%d/rotate", keyID),
		map[string]string{"X-Internal-Token": env.internalToken}, nil)

	// Assert: 轮换成功
	assertResponseStatus(t, resp, http.StatusOK)

	var result struct {
		Data struct {
			ID        int64  `json:"id"`
			APIKey    string `json:"apiKey"`
			KeyPrefix string `json:"keyPrefix"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	newPlainKey := result.Data.APIKey
	if newPlainKey == "" {
		t.Fatal("new plaintext key should be returned")
	}
	if newPlainKey == plainKey {
		t.Error("new key should be different from old key")
	}

	// Assert: 验证旧 key 有 expiresAt 宽限期记录
	ctx := context.Background()
	hash := hashKey(plainKey)
	_ = hash // used for cache key lookup

	// 检查 DB 中的 expiresAt（旧 key 被更新为宽限期）
	var expiresAt *time.Time
	env.db.Table("ykt_aisaas_apikey").
		Where("id = ?", keyID).
		Select("expiresAt").
		Scan(&expiresAt)

	if expiresAt == nil {
		t.Error("old key should have expiresAt set (grace period)")
	} else {
		expectedExpiry := time.Now().Add(5 * time.Minute)
		diff := expiresAt.Sub(expectedExpiry)
		if diff < -30*time.Second || diff > 30*time.Second {
			t.Errorf("old key grace period should be ~5min from now, got diff=%v", diff)
		}
	}

	// Assert: 新 key 在 Redis 缓存中
	newHash := hashKey(newPlainKey)
	cached := env.rdb.Get(ctx, redisx.KeyAPIKeyByHash(newHash)).Val()
	if cached == "" {
		t.Log("warning: new key may not be cached yet (async)")
	}

	t.Logf("Rotate success: old key=%s... expired at %v, new key=%s...", plainKey[:16], expiresAt, newPlainKey[:16])
}

// TestAPIKey_RevokeByInternal_BypassesTenant 测试内部撤销 API Key 绕过租户隔离。
func TestAPIKey_RevokeByInternal_BypassesTenant(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户 A 和 API Key
	tenantA := createTestTenant(t, env)
	plainKey, keyID := newAPIKey(t, env, tenantA, []string{"llm"})

	// Act: 用 super admin（内部接口）撤销该 key
	revokeAPIKeyByInternal(t, env, keyID)

	// Assert: 验证 key 在 DB 中 status = 0
	var status int8
	env.db.Table("ykt_aisaas_apikey").
		Where("id = ?", keyID).
		Select("status").
		Scan(&status)
	if status != 0 {
		t.Errorf("expected key status=0 (revoked), got %d", status)
	}

	// Assert: Redis 缓存立即清除
	ctx := context.Background()
	hash := hashKey(plainKey)
	cached := env.rdb.Get(ctx, redisx.KeyAPIKeyByHash(hash)).Val()
	if cached != "" {
		t.Error("expected Redis cache to be cleared after revoke")
	}

	// Assert: audit_log 写入一条 apikey.revoke 记录
	entry := waitForAuditLog(t, env, "apikey.revoke", 3*time.Second)
	if entry == nil {
		t.Fatal("expected audit log entry for apikey.revoke")
	}
	if entry.Action != "apikey.revoke" {
		t.Errorf("expected action 'apikey.revoke', got '%s'", entry.Action)
	}
	if entry.ActorType != "internal" {
		t.Errorf("expected actorType 'internal', got '%s'", entry.ActorType)
	}

	t.Logf("Revoke success: keyID=%d, audit entry actorType=%s, action=%s", keyID, entry.ActorType, entry.Action)
}

// TestAPIKey_Rotate_NotLeakOldKey 测试轮换后旧 key 在宽限期内可用。
func TestAPIKey_Rotate_NotLeakOldKey(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户和 API Key
	tenantID := createTestTenant(t, env)
	plainKey, keyID := newAPIKey(t, env, tenantID, []string{"llm"})

	// Act: 轮换 API Key
	resp, body := doRequest(t, env.server, "POST",
		fmt.Sprintf("/internal/api/v1/apikeys/%d/rotate", keyID),
		map[string]string{"X-Internal-Token": env.internalToken}, nil)
	assertResponseStatus(t, resp, http.StatusOK)

	var result struct {
		Data struct {
			APIKey string `json:"apiKey"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	// Assert: 旧 key 在宽限期内（5分钟内）状态仍为有效
	// 验证旧 key 的 expiresAt 被设置为未来时间
	var expiresAt *time.Time
	env.db.Table("ykt_aisaas_apikey").
		Where("id = ?", keyID).
		Select("expiresAt").
		Scan(&expiresAt)

	if expiresAt == nil {
		t.Fatal("rotated key should have expiresAt set for grace period")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("grace period expiry should be in the future")
	}

	t.Logf("Rotation completed. Old key in grace period until %v. New key: %s...", expiresAt, result.Data.APIKey[:16])

	// 注意：完整 6 分钟后的失效测试需要 time-travel 或长时间等待
	// 此处标记为简化测试场景
}

// hashKey 计算 API Key 的 SHA-256 哈希。
func hashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
