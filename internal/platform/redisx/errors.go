package redisx

import "errors"

// ErrQuotaExceeded 配额不足 sentinel。
var ErrQuotaExceeded = errors.New("quota exceeded")
