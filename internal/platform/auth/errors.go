package auth

import "errors"

// ErrTenantNotFound 租户不存在（sentinel，由注入方 TenantStatus 实现返回）。
var ErrTenantNotFound = errors.New("tenant not found")
