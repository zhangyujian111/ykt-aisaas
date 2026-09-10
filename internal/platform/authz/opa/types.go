// Package opa OPA（Open Policy Agent）授权客户端（V7-O 阶段）。
// 用于 ykt-aisaas 业务 API 的统一授权决策：
//   - tenant 隔离：用户只能访问自己租户的资源
//   - role × resource × action 矩阵（deployer / operator / developer / viewer）
//   - 高敏操作阻断（tenant/user 删除）
package opa

// User 授权上下文用户信息（与 RBAC 层用户模型对齐）。
type User struct {
	ID        string   `json:"id"`
	Username  string   `json:"username,omitempty"`
	Type      string   `json:"type,omitempty"` // user / service_account
	Roles     []string `json:"roles"`
	TenantIDs []int64  `json:"tenantIds"`
}

// AuthzInput OPA 授权输入。
type AuthzInput struct {
	User     *User  `json:"user"`
	Resource string `json:"resource"` // rollout / pod / session / ai.inference ...
	Action   string `json:"action"`   // read / create / update / delete / promote / abort ...
	TenantID int64  `json:"tenant_id"`
}

// AuthzResult OPA 授权结果。
type AuthzResult struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

// AuthzDecision OPA REST /v1/data/ykt/authz 响应包装。
type AuthzDecision struct {
	Allow  bool     `json:"allow"`
	Reason string   `json:"reason,omitempty"`
	Denies []string `json:"denies,omitempty"`
}