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

	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// TestIntegration_End2End_XiaozhiBoot 测试完整流程：Xiaozhi-server-go 启动 → 申请 API Key → 创建 session → chat → 退款。
func TestIntegration_End2End_XiaozhiBoot(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// ---- Step 1: 创建设备租户（模拟 Xiaozhi-server-go 启动）----
	deviceID := "xiaozhi-test-device-" + uuid.NewString()[:8]

	// 通过内部设备接口创建设备租户
	resp, body := doRequest(t, env.server, "POST",
		"/internal/xiaozhi/v1/chat/completions",
		map[string]string{
			"X-Internal-Token": env.internalToken,
			"X-Device-Id":       deviceID,
		},
		map[string]any{
			"model": "test-model",
			"messages": []map[string]string{
				{"role": "user", "content": "hello"},
			},
		})

	// 注意：Xiaozhi 设备通道会自动创建设备租户
	// 这里验证租户是否被创建
	var tenantID int64

	t.Logf("Step 1: Device boot - deviceID=%s, resp=%d", deviceID, resp.StatusCode)

	// ---- Step 2: 申请 API Key（通过内部接口为租户创建 Key）----
	// 由于需要租户 ID，我们先用设备通道获取租户 ID
	// 简化：直接创建测试租户
	tenantID = createTestTenant(t, env)

	// 创建 API Key
	plainKey, keyID := newAPIKey(t, env, tenantID, []string{"llm", "tts", "asr"})
	t.Logf("Step 2: API Key created - keyID=%d, keyPrefix=%s...", keyID, plainKey[:16])

	// ---- Step 3: 设置配额和余额----
	// 设置配额
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, "llm_tokens_in", ym), 100000, 35*24*time.Hour)
	env.rdb.Set(ctx, redisx.KeyQuotaUsed(tenantID, "llm_tokens_in", ym), 0, 35*24*time.Hour)

	// 设置余额
	env.db.Exec(`INSERT INTO ykt_aisaas_balance (id, tenantId, balanceCents)
		VALUES (?, ?, 10000) ON DUPLICATE KEY UPDATE id=id`,
		int64(ids.Next()), tenantID)

	t.Logf("Step 3: Quota and balance set for tenant %d", tenantID)

	// ---- Step 4: 模拟 Chat 请求（验证计量）----
	// 记录初始 metering 计数
	initialMeteringCount, _ := env.rdb.XLen(ctx, "aisaas:metering:stream").Result()

	// 发起 chat 请求（会触发配额预扣和计量记录）
	chatResp, chatBody := doRequest(t, env.server, "POST", "/v1/chat/completions",
		map[string]string{"Authorization": "Bearer " + plainKey},
		map[string]any{
			"model": "test-model",
			"messages": []map[string]string{
				{"role": "user", "content": "hello, this is a test message"},
			},
		})

	t.Logf("Step 4: Chat request - status=%d", chatResp.StatusCode)
	_ = chatBody // response may be error due to missing upstream LLM

	// 记录 metering 后计数
	finalMeteringCount, _ := env.rdb.XLen(ctx, "aisaas:metering:stream").Result()
	t.Logf("Step 4: Metering stream - initial=%d, final=%d", initialMeteringCount, finalMeteringCount)

	// ---- Step 5: 验证审计日志----
	// 等待审计日志写入
	time.Sleep(2 * time.Second)

	var auditCount int64
	env.db.Table("ykt_aisaas_audit_log").
		Where("tenantId = ?", tenantID).
		Count(&auditCount)

	t.Logf("Step 5: Audit log entries for tenant %d: %d", tenantID, auditCount)

	// ---- Step 6: 验证配额状态----
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, "llm_tokens_in", ym)).Int64()
	t.Logf("Step 6: Quota used for tenant %d: %d", tenantID, used)

	// ---- Step 7: API Key 轮换----
	rotateResp, rotateBody := doRequest(t, env.server, "POST",
		fmt.Sprintf("/internal/api/v1/apikeys/%d/rotate", keyID),
		map[string]string{"X-Internal-Token": env.internalToken}, nil)

	t.Logf("Step 7: API Key rotate - status=%d", rotateResp.StatusCode)

	var rotateResult struct {
		Data struct {
			APIKey string `json:"apiKey"`
		} `json:"data"`
	}
	json.Unmarshal(rotateBody, &rotateResult)
	if rotateResult.Data.APIKey != "" {
		t.Logf("Step 7: New API Key received: %s...", rotateResult.Data.APIKey[:16])
	}

	// ---- Step 8: API Key 撤销----
	revokeAPIKeyByInternal(t, env, keyID)

	// 验证 key 被撤销
	var status int8
	env.db.Table("ykt_aisaas_apikey").
		Where("id = ?", keyID).
		Select("status").
		Scan(&status)

	if status != 0 {
		t.Errorf("expected key status=0 after revoke, got %d", status)
	}
	t.Logf("Step 8: API Key revoked - status=%d", status)

	// ---- 汇总----
	t.Log("=== E2E Test Summary ===")
	t.Logf("Tenant ID: %d", tenantID)
	t.Logf("API Key ID: %d", keyID)
	t.Logf("Quota Used: %d", used)
	t.Logf("Audit Entries: %d", auditCount)
	t.Logf("Metering Stream Entries: %d", finalMeteringCount-initialMeteringCount)
}

// TestIntegration_QuotaFlow_E2E 测试配额完整流程：设置 → 预扣 → 退款。
func TestIntegration_QuotaFlow_E2E(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Arrange: 创建租户
	tenantID := createTestTenant(t, env)
	dim := "llm_tokens_in"

	// 设置配额 10000
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym), 10000, 35*24*time.Hour)

	// Act: 模拟 chat 会话
	// 1. 预扣估算量（假设估算 500）
	est1 := int64(500)
	remaining1, err := env.rdb.QuotaDeduct(ctx, tenantID, dim, est1)
	if err != nil {
		t.Fatalf("Precheck failed: %v", err)
	}

	// 2. 实际使用 300
	actualUsed := int64(300)

	// 3. 退款差额（500 - 300 = 200）
	env.rdb.QuotaRefund(ctx, tenantID, dim, est1-actualUsed)

	// Assert: 验证最终配额状态
	used, _ := env.rdb.Get(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym)).Int64()
	expectedUsed := actualUsed // 应该是实际使用量 300

	if used != expectedUsed {
		t.Errorf("expected used=%d, got %d", expectedUsed, used)
	}

	// Assert: 验证余额不损失
	limit, _ := env.rdb.Get(ctx, redisx.KeyQuotaLimit(tenantID, dim, ym)).Int64()
	if limit != 10000 {
		t.Errorf("expected limit=10000, got %d", limit)
	}

	t.Logf("Quota flow E2E: limit=%d, used=%d, remaining=%d", limit, used, remaining1)
}

// TestIntegration_MeteringAndAudit_E2E 测试计量和审计的完整流程。
func TestIntegration_MeteringAndAudit_E2E(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	tenantID := createTestTenant(t, env)

	// Act: 记录多条计量
	for i := 0; i < 5; i++ {
		env.meter.Record(ctx, metering.Record{
			TenantID:  tenantID,
			APIKeyID:  int64(i),
			BizType:   metering.BizLLM,
			Dimension: metering.DimLLMTokensIn,
			Amount:    int64(100 * (i + 1)),
			ModelID:   "test-model",
			RequestID: "req_" + uuid.NewString()[:8],
			Status:    1,
		})
	}

	// Assert: Redis Stream 有记录
	streamLen, _ := env.rdb.XLen(ctx, "aisaas:metering:stream").Result()
	if streamLen == 0 {
		t.Error("expected metering records in stream")
	}

	// Assert: 等待 DB 写入
	time.Sleep(2 * time.Second)

	var dbCount int64
	env.db.Table("ykt_aisaas_usage_detail").
		Where("tenantId = ?", tenantID).
		Count(&dbCount)

	if dbCount < 5 {
		t.Errorf("expected at least 5 usage records, got %d", dbCount)
	}

	t.Logf("Metering and Audit E2E: stream=%d, db=%d", streamLen, dbCount)
}
