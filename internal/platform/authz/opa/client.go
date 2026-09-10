// Package opa OPA（Open Policy Agent）授权客户端（V7-O 阶段）。
// 通过 HTTP REST 调用 OPA Server 的 /v1/data/ykt/authz 进行授权决策。
//   - 5s 超时，遵循业务 P95 延迟预算
//   - 失败默认拒绝（fail-closed）
//   - 集成 Prometheus 指标（opa_authz_duration_seconds / opa_authz_total）
package opa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AuthzClient OPA 授权客户端。
type AuthzClient struct {
	opaURL string
	client *http.Client
}

// NewAuthzClient 创建 OPA 授权客户端（opaURL 例：http://opa.opa.svc:8181）。
func NewAuthzClient(opaURL string) *AuthzClient {
	return &AuthzClient{
		opaURL: opaURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Check 授权检查。返回 Allow=false + 错误时调用方应拒绝请求（fail-closed）。
func (c *AuthzClient) Check(ctx context.Context, input AuthzInput) (*AuthzResult, error) {
	body, err := json.Marshal(map[string]any{
		"input": input,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal opa input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opaURL+"/v1/data/ykt/authz", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create opa request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call opa: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opa returned status %d", resp.StatusCode)
	}

	var raw struct {
		Result AuthzDecision `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode opa response: %w", err)
	}

	result := &AuthzResult{
		Allow: raw.Result.Allow,
	}
	if !raw.Result.Allow {
		if raw.Result.Reason != "" {
			result.Reason = raw.Result.Reason
		} else if len(raw.Result.Denies) > 0 {
			result.Reason = raw.Result.Denies[0]
		} else {
			result.Reason = "denied by opa"
		}
	}
	return result, nil
}