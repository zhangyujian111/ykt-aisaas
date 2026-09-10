//go:build integration
// +build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/portal"
)

// TestPortal_Login_RS256_Token 测试 RS256 登录返回双 token 并正确解析。
func TestPortal_Login_RS256_Token(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建测试用户
	username := "rs256_test_" + uuid.NewString()[:8]
	password := "password123"
	createTestPortalUser(t, env, username, password)

	// Act: 登录
	resp, body := doRequest(t, env.server, "POST", "/portal/api/v1/auth/login", nil, map[string]any{
		"username": username,
		"password": password,
	})

	// Assert: 登录成功
	assertResponseStatus(t, resp, http.StatusOK)

	var result struct {
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			TokenType    string `json:"tokenType"`
			ExpiresIn    int64  `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if result.Data.AccessToken == "" || result.Data.RefreshToken == "" {
		t.Fatal("accessToken or refreshToken is empty")
	}
	if result.Data.TokenType != "Bearer" {
		t.Errorf("expected tokenType 'Bearer', got '%s'", result.Data.TokenType)
	}
	if result.Data.ExpiresIn != 900 { // 15min = 900s
		t.Errorf("expected expiresIn 900, got %d", result.Data.ExpiresIn)
	}

	// Assert: 验证 accessToken 是 RS256 签名（通过解析 JWT header）
	parts := strings.Split(result.Data.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatal("invalid JWT format")
	}
	headerJSON, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	json.Unmarshal(headerJSON, &header)
	if header.Alg != "RS256" {
		t.Errorf("expected RS256 algorithm, got '%s'", header.Alg)
	}

	// Assert: 用 refreshToken 获取新 accessToken（验证 refreshToken 可解析）
	refreshClaims, err := jwt.NewParser().ParseUnverified(result.Data.RefreshToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("refreshToken parse failed: %v", err)
	}
	claims, ok := refreshClaims.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("refreshToken claims type error")
	}
	if claims["type"] != "refresh" {
		t.Errorf("expected refresh token type 'refresh', got '%v'", claims["type"])
	}
}

// TestPortal_ParseToken_RejectsHS256 测试 ParseToken 拒绝 HS256 伪造 token（alg confusion attack）。
func TestPortal_ParseToken_RejectsHS256(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 用 HS256 签一个伪造 token
	claims := jwt.MapClaims{
		"uid":  float64(12345),
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	hs256Token, err := token.SignedString([]byte("fake-hs256-secret"))
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}

	// Act: 用伪造的 HS256 token 调用 /portal/api/v1/devices（需要 JWT 鉴权）
	// 由于 portal JWTMiddleware 会验证 RS256，HS256 token 应该被拒绝
	resp, body := doRequest(t, env.server, "GET", "/portal/api/v1/devices",
		map[string]string{"Authorization": "Bearer " + hs256Token}, nil)

	// Assert: 应该返回 401（Token 无效）
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for HS256 token, got %d: %s", resp.StatusCode, string(body))
	}
}

// TestPortal_ParseToken_RejectsNone 测试 ParseToken 拒绝 alg=none 的恶意 token。
func TestPortal_ParseToken_RejectsNone(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 构造 alg=none 的 JWT token
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":12345,"exp":9999999999}`))
	maliciousToken := header + "." + claims + "."

	// Act: 用 alg=none token 调用受保护的接口
	resp, body := doRequest(t, env.server, "GET", "/portal/api/v1/devices",
		map[string]string{"Authorization": "Bearer " + maliciousToken}, nil)

	// Assert: 应该返回 401（Token 无效）
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for alg=none token, got %d: %s", resp.StatusCode, string(body))
	}
}

// TestPortal_LoginRateLimit_5Failures_Locks15Min 测试登录失败 5 次后被锁定。
func TestPortal_LoginRateLimit_5Failures_Locks15Min(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Arrange: 创建测试用户
	username := "ratelimit_test_" + uuid.NewString()[:8]
	password := "password123"
	createTestPortalUser(t, env, username, password)

	// Act: 连续 5 次错误密码登录
	wrongPassword := "wrong_password_"
	for i := 0; i < 5; i++ {
		resp, _ := doRequest(t, env.server, "POST", "/portal/api/v1/auth/login", nil, map[string]any{
			"username": username,
			"password": wrongPassword,
		})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Logf("login attempt %d: expected 401, got %d", i+1, resp.StatusCode)
		}
	}

	// Assert: 第 6 次登录应返回 429 Too Many Requests
	resp, body := doRequest(t, env.server, "POST", "/portal/api/v1/auth/login", nil, map[string]any{
		"username": username,
		"password": wrongPassword,
	})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 after 5 failures, got %d: %s", resp.StatusCode, string(body))
	}

	// Assert: Redis key 应该存在
	ctx := context.Background()
	userKey := fmt.Sprintf("aisaas:portal:login:fail:%s", username)
	userCount, err := env.rdb.Get(ctx, userKey).Int64()
	if err != nil {
		t.Logf("warning: failed to get user rate limit key: %v", err)
	}
	if userCount < 5 {
		t.Errorf("expected user fail count >= 5, got %d", userCount)
	}
}
