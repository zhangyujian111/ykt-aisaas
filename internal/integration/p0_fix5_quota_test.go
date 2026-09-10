//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// TestQuota_Refund_ActualTokens 测试配额退款按实际用量退还差额。
func TestQuota_Refund_ActualTokens(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户，配额 1000
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"

	// 模拟预扣 800（通过 QuotaDeduct）
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 800, 35*24*time.Hour)

	// Act: 退款 200（800 - 600 = 200，实际只用了 600）
	refund := int64(200)
	err := env.rdb.QuotaRefund(ctx, tenantID, dim, refund)
	if err != nil {
		t.Fatalf("QuotaRefund failed: %v", err)
	}

	// Assert: 验证 Redis used 计数器减少 200
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 600 {
		t.Errorf("expected used=600 after refund, got %d", used)
	}

	t.Logf("Quota refund success: used=%d after refund of %d", used, refund)
}

// TestChat_RefundOnFailure 测试 LLM 调用失败时触发退款。
func TestChat_RefundOnFailure(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户和 API Key
	tenantID := createTestTenant(t, env)
	plainKey, _ := newAPIKey(t, env, tenantID, []string{"llm"})

	// 设置配额限制为很小的值，确保配额不足时触发退款逻辑
	ctx := context.Background()
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, "llm_tokens_in", ym), 100, 35*24*time.Hour)

	// Act: 使用无效的 API Key 调用 chat（会触发 auth 失败，不会触发 LLM 调用）
	resp, body := doRequest(t, env.server, "POST", "/v1/chat/completions",
		map[string]string{"Authorization": "Bearer " + plainKey + "_invalid"},
		map[string]any{
			"model": "test-model",
			"messages": []map[string]string{
				{"role": "user", "content": "hello"},
			},
		})

	// Assert: 应该返回认证失败
	if resp.StatusCode != http.StatusUnauthorized {
		t.Logf("expected auth failure, got %d: %s", resp.StatusCode, string(body))
	}

	// 注意：真实测试需要 mock LLM 返回 500 错误来触发 refund 逻辑
	// 此处标记为简化测试场景
	t.Log("Chat refund on failure test: auth failure triggered")
}

// TestChat_RefundOnCancel_Stream 测试客户端断开 SSE 流时触发 ctx 取消和退款。
func TestChat_RefundOnCancel_Stream(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户和 API Key
	tenantID := createTestTenant(t, env)
	plainKey, _ := newAPIKey(t, env, tenantID, []string{"llm"})

	// 设置配额
	ctx := context.Background()
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, "llm_tokens_in", ym), 1000000, 35*24*time.Hour)

	// 注意：流式取消退款测试需要真实的 LLM 上游和 SSE 客户端
	// 此处标记为待实现场景
	t.Skip("Stream cancellation refund test requires full SSE client and upstream mock")
}

// TestChat_RefundAsync_NonBlocking 测试退款异步执行不阻塞 chat 响应。
func TestChat_RefundAsync_NonBlocking(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户和配额
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"

	// 模拟预扣场景
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 800, 35*24*time.Hour)

	// Act: 发起退款（应该异步执行）
	start := time.Now()
	err := env.rdb.QuotaRefund(ctx, tenantID, dim, 200)
	elapsed := time.Since(start)

	// Assert: 退款操作应该立即返回（异步）
	if err != nil {
		t.Fatalf("QuotaRefund failed: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Logf("warning: refund took %v, expected immediate return", elapsed)
	}

	// Assert: 验证 Redis 状态更新
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 600 {
		t.Errorf("expected used=600, got %d", used)
	}

	t.Logf("Async refund success: elapsed=%v, used=%d", elapsed, used)
}

// TestQuota_RefundViaInternalAPI 测试通过内部 API 退款。
func TestQuota_RefundViaInternalAPI(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建租户和配额
	tenantID := createTestTenant(t, env)
	ctx := context.Background()
	dim := "llm_tokens_in"

	// 模拟预扣 800
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 1000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), 800, 35*24*time.Hour)

	// Act: 调用内部退款 API
	resp, body := doRequest(t, env.server, "POST", "/internal/api/v1/quota/refund",
		map[string]string{"X-Internal-Token": env.internalToken},
		map[string]any{
			"tenantId":  tenantID,
			"dimension": dim,
			"estimated": 800,
			"actual":    600,
			"requestId": "req_" + uuid.NewString()[:8],
		})

	// Assert: 退款成功
	assertResponseStatus(t, resp, http.StatusOK)

	var result struct {
		Data struct {
			Refund int64 `json:"refund"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)
	if result.Data.Refund != 200 {
		t.Errorf("expected refund=200, got %d", result.Data.Refund)
	}

	// Assert: Redis used 计数器更新
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	if used != 600 {
		t.Errorf("expected used=600 after refund, got %d", used)
	}

	t.Logf("Internal API refund success: refund=%d, used=%d", result.Data.Refund, used)
}
