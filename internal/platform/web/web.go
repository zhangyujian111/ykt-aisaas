// Package web gin 引擎装配、统一响应/错误输出、SSE Writer。
package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ykt.dev/aisaas/internal/platform/errs"
)

// Result 统一响应信封（SaaS 自有接口用；OpenAI 兼容接口直接输出 OpenAI 格式）。
type Result struct {
	Code      errs.ErrorCode `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId,omitempty"`
	Data      any            `json:"data"`
}

// OK 成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Result{
		Code:      errs.OK,
		Message:   "success",
		RequestID: RequestID(c),
		Data:      data,
	})
}

// Abort 以 Result 信封输出错误并中断。
func Abort(c *gin.Context, err error) {
	e := errs.From(err)
	c.AbortWithStatusJSON(e.Code.HTTPStatus(), Result{
		Code:      e.Code,
		Message:   e.Msg,
		RequestID: RequestID(c),
		Data:      e.Detail,
	})
}

// openAIError OpenAI 兼容错误结构（API.md §1.3）。
type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type openAIErrorBody struct {
	Error     openAIError `json:"error"`
	RequestID string      `json:"requestId,omitempty"`
}

// AbortOpenAI 以 OpenAI 协议格式输出错误（/v1/* 用）。
func AbortOpenAI(c *gin.Context, err error) {
	e := errs.From(err)
	c.AbortWithStatusJSON(e.Code.HTTPStatus(), openAIErrorBody{
		Error: openAIError{
			Message: e.Msg,
			Type:    "aisaas_error",
			Code:    json.Number(itoa(int(e.Code))).String(),
		},
		RequestID: RequestID(c),
	})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// ---- RequestID ----

type reqIDKey struct{}

// RequestIDMiddleware 注入 X-Request-Id（无则生成）。
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set("requestId", rid)
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// RequestID 从 gin.Context 取请求 ID。
func RequestID(c *gin.Context) string {
	if v, ok := c.Get("requestId"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// NewEngine 构建 gin 引擎（生产模式 + Recovery + CORS）。
func NewEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.RecoveryWithWriter(gin.DefaultErrorWriter, func(c *gin.Context, err any) {
		slog.Error("panic recovered", "path", c.Request.URL.Path, "err", err, "requestId", RequestID(c))
	}))
	r.Use(requestLogger(), cors())
	return r
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"costMs", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"requestId", RequestID(c),
		)
	}
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id, X-Internal-Token")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// IsGinCanceled 客户端断开/上下文取消（不记错误日志）。
func IsGinCanceled(err error) bool {
	return errors.Is(err, gin.Error{}.Err) || errors.Is(err, http.ErrHandlerTimeout)
}
