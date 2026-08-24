// Package errs 定义平台统一错误码与业务异常。
// 编码规则与 API.md §8 一致：{HTTP 状态}{业务域}{序号}。
package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode 平台错误码。
type ErrorCode int

const (
	OK                ErrorCode = 0
	InvalidParam      ErrorCode = 40001 // 参数错误
	InvalidJSON       ErrorCode = 40002 // 请求体解析失败
	InvalidAPIKey     ErrorCode = 40101 // API Key 无效
	APIKeyExpired     ErrorCode = 40102 // API Key 已过期
	IPNotAllowed      ErrorCode = 40103 // IP 不在白名单
	TokenInvalid      ErrorCode = 40104 // Token 无效
	QuotaExceeded     ErrorCode = 40201 // 配额不足
	BalanceInsuff     ErrorCode = 40202 // 余额不足
	RateLimited       ErrorCode = 40203 // 请求过于频繁
	TenantFrozen      ErrorCode = 40301 // 租户已冻结
	Forbidden         ErrorCode = 40302 // 权限不足
	ModelNotFound     ErrorCode = 40401 // 模型不存在或未启用
	ResourceNotFound  ErrorCode = 40404 // 资源不存在
	Conflict          ErrorCode = 40901 // 资源已存在
	TenantContextLost ErrorCode = 50001 // 租户上下文丢失（严重，触发告警）
	Internal          ErrorCode = 50002 // 系统内部错误
	ProviderError     ErrorCode = 50201 // 上游服务异常
	ProviderTimeout   ErrorCode = 50202 // 上游服务超时
)

// httpStatus 错误码 → HTTP 状态码映射。
var httpStatus = map[ErrorCode]int{
	OK:                http.StatusOK,
	InvalidParam:      http.StatusBadRequest,
	InvalidJSON:       http.StatusBadRequest,
	InvalidAPIKey:     http.StatusUnauthorized,
	APIKeyExpired:     http.StatusUnauthorized,
	IPNotAllowed:      http.StatusUnauthorized,
	TokenInvalid:      http.StatusUnauthorized,
	QuotaExceeded:     http.StatusPaymentRequired,
	BalanceInsuff:     http.StatusPaymentRequired,
	RateLimited:       http.StatusTooManyRequests,
	TenantFrozen:      http.StatusForbidden,
	Forbidden:         http.StatusForbidden,
	ModelNotFound:     http.StatusNotFound,
	ResourceNotFound:  http.StatusNotFound,
	Conflict:          http.StatusConflict,
	TenantContextLost: http.StatusInternalServerError,
	Internal:          http.StatusInternalServerError,
	ProviderError:     http.StatusBadGateway,
	ProviderTimeout:   http.StatusGatewayTimeout,
}

// HTTPStatus 返回错误码对应的 HTTP 状态码。
func (c ErrorCode) HTTPStatus() int {
	if s, ok := httpStatus[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// message 错误码默认文案。
var message = map[ErrorCode]string{
	OK:                "success",
	InvalidParam:      "参数错误",
	InvalidJSON:       "请求体解析失败",
	InvalidAPIKey:     "API Key 无效",
	APIKeyExpired:     "API Key 已过期",
	IPNotAllowed:      "IP 不在白名单",
	TokenInvalid:      "Token 无效",
	QuotaExceeded:     "配额不足",
	BalanceInsuff:     "余额不足",
	RateLimited:       "请求过于频繁",
	TenantFrozen:      "租户已冻结",
	Forbidden:         "权限不足",
	ModelNotFound:     "模型不存在或未启用",
	ResourceNotFound:  "资源不存在",
	Conflict:          "资源已存在",
	TenantContextLost: "租户上下文丢失",
	Internal:          "系统内部错误",
	ProviderError:     "上游服务异常",
	ProviderTimeout:   "上游服务超时",
}

// Message 返回错误码默认文案。
func (c ErrorCode) Message() string {
	if m, ok := message[c]; ok {
		return m
	}
	return "未知错误"
}

// Error 业务异常。code 为平台错误码，detail 可携带结构化补充信息。
type Error struct {
	Code   ErrorCode   `json:"code"`
	Msg    string      `json:"message"`
	Detail interface{} `json:"detail,omitempty"`
	cause  error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("code=%d msg=%s cause=%s", e.Code, e.Msg, e.cause)
	}
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Msg)
}

func (e *Error) Unwrap() error { return e.cause }

// New 创建业务异常。
func New(code ErrorCode, msg ...string) *Error {
	e := &Error{Code: code, Msg: code.Message()}
	if len(msg) > 0 {
		e.Msg = msg[0]
	}
	return e
}

// Wrap 包装底层错误。
func Wrap(code ErrorCode, cause error, msg ...string) *Error {
	e := New(code, msg...)
	e.cause = cause
	return e
}

// From 提取 error 中的 *Error；否则归为 Internal。
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Wrap(Internal, err)
}

// IsFatal 是否为致命错误（流式中需终止）。
func IsFatal(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return true
	}
	switch e.Code {
	case ProviderError, ProviderTimeout, Internal, TenantContextLost:
		return false // 上游/系统错误：可降级提示，不强制断流
	default:
		return true
	}
}
