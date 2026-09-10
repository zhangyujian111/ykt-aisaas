//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// TestAudit_Record_RedisStream_ThenConsumerWrite 测试审计日志写入 Redis Stream 后被 consumer 消费写入 DB。
func TestAudit_Record_RedisStream_ThenConsumerWrite(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 准备审计条目
	entry := audit.AuditEntry{
		TenantID:  12345,
		ActorType: "apikey",
		ActorID:   "ak_123",
		ActorIP:   "192.168.1.1",
		Action:    "test.action",
		ActionDetail: map[string]any{
			"key": "value",
		},
		ResourceType: "test",
		ResourceID:   "res_123",
		Result:       "success",
		TraceID:      uuid.NewString(),
	}

	// Act: 调用 Record
	audit.Record(context.Background(), entry)

	// Assert: Redis Stream 中应该有记录
	ctx := context.Background()
	count, err := env.rdb.XLen(ctx, "aisaas:audit:stream").Result()
	if err != nil {
		t.Fatalf("failed to check stream length: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least 1 entry in audit stream")
	}

	// Assert: 等待 consumer 批量写入 DB
	dbEntry := waitForAuditLog(t, env, "test.action", 5*time.Second)
	if dbEntry == nil {
		t.Fatal("expected audit log entry in DB after consumer flush")
	}

	// Assert: 验证 eventId 是 UUID v7
	mustParseUUIDv7(t, dbEntry.EventID)

	// Assert: traceId 写入
	if dbEntry.TraceID == "" {
		t.Error("expected traceId to be written")
	}

	t.Logf("Audit record success: eventId=%s, traceId=%s, action=%s", dbEntry.EventID, dbEntry.TraceID, dbEntry.Action)
}

// TestAudit_RedactValue_Comprehensive 测试 redactValue 函数对各种敏感字段的脱敏。
func TestAudit_RedactValue_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		expected string
	}{
		{
			name:     "password field redacted",
			input:    map[string]any{"username": "alice", "password": "secret123"},
			key:      "password",
			expected: "[REDACTED]",
		},
		{
			name:     "apiKey field redacted",
			input:    map[string]any{"apiKey": "ak_xxx", "data": "ok"},
			key:      "apiKey",
			expected: "[REDACTED]",
		},
		{
			name: "nested secret field redacted",
			input: map[string]any{
				"token": "jwt.xxx.yyy",
				"nesting": map[string]any{
					"secret": "x",
				},
			},
			key:      "secret",
			expected: "[REDACTED]",
		},
		{
			name:     "token field redacted",
			input:    map[string]any{"token": "Bearer abc123", "action": "login"},
			key:      "token",
			expected: "[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 直接测试 redactValue 函数
			redacted := audit.redactValue(tt.input)

			// 检查指定 key 是否被脱敏
			val := getMapValue(redacted, tt.key)
			if val != tt.expected {
				t.Errorf("key '%s': expected '%s', got '%v'", tt.key, tt.expected, val)
			}
		})
	}
}

// TestAudit_SamplingRate_1Percent_QuotaCheck 测试配额检查的 1% 采样率。
func TestAudit_SamplingRate_1Percent_QuotaCheck(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建测试租户并设置配额
	tenantID := createTestTenant(t, env)
	ctx := context.Background()

	// 故意触发配额不足场景（设置极低配额）
	ym := time.Now().Format("200601")
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, "llm_tokens_in", ym), 1, 35*24*time.Hour)

	// Act: 大量请求触发配额检查
	for i := 0; i < 100; i++ {
		// 模拟配额检查（触发 QuotaDeduct 返回配额不足）
		_, _ = env.rdb.QuotaDeduct(ctx, tenantID, "llm_tokens_in", 100)
	}

	// 清理：恢复配额
	env.rdb.Set(ctx, redisx.KeyQuotaLimit(tenantID, "llm_tokens_in", ym), 1000000, 35*24*time.Hour)

	// Assert: 应该有约 1 条 quota.check 审计日志（1% 采样率，100 次检查）
	var count int64
	env.db.Table("ykt_aisaas_audit_log").
		Where("action = 'quota.check' AND tenantId = ?", tenantID).
		Count(&count)

	// 允许 ±1 的容差
	if count < 0 || count > 2 {
		t.Errorf("expected ~1 quota.check audit entries (1%% sampling), got %d", count)
	}

	t.Logf("Quota check sampling: %d audit entries for 100 checks", count)
}

// TestAudit_RedactString_BearerToken 测试字符串中 Bearer token 的脱敏。
func TestAudit_RedactString_BearerToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer abc123xyz", "Bearer [REDACTED]"},
		{"sk-aisaas-xxxx1234", "sk-aisaas-[REDACTED]"},
		{"normal text", "normal text"},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			redacted := audit.redactString(tt.input)
			if redacted != tt.expected {
				t.Errorf("input '%s': expected '%s', got '%s'", tt.input, tt.expected, redacted)
			}
		})
	}
}

// getMapValue 从嵌套 map 中获取值。
func getMapValue(m map[string]any, key string) any {
	parts := strings.Split(key, ".")
	current := any(m)
	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return nil
		}
	}
	return current
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
