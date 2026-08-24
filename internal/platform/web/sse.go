package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SSEWriter OpenAI 兼容 SSE 输出器（API.md §8：心跳、x- 扩展事件、[DONE]）。
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	closed  bool
	beat    *time.Ticker
}

// NewSSE 初始化 SSE 响应头并返回 Writer。
func NewSSE(c *gin.Context) *SSEWriter {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	f, ok := c.Writer.(http.Flusher)
	if !ok {
		f = noopFlusher{}
	}
	f.Flush()
	return &SSEWriter{w: c.Writer, flusher: f, beat: time.NewTicker(15 * time.Second)}
}

type noopFlusher struct{}

func (noopFlusher) Flush() {}

// Heartbeat 心跳 channel（15s 注释行）。
func (s *SSEWriter) Heartbeat() <-chan time.Time { return s.beat.C }

// Raw 写原始行（已含换行语义由调用方控制）。
func (s *SSEWriter) Raw(line string) {
	if s.closed {
		return
	}
	_, _ = io.WriteString(s.w, line)
	s.flusher.Flush()
}

// WriteData 输出 data: {json}\n\n。
func (s *SSEWriter) WriteData(v any) {
	b, _ := json.Marshal(v)
	s.Raw(fmt.Sprintf("data: %s\n\n", b))
}

// WriteEvent 输出 event: {name}\ndata: {json}\n\n。
func (s *SSEWriter) WriteEvent(name string, v any) {
	b, _ := json.Marshal(v)
	s.Raw(fmt.Sprintf("event: %s\ndata: %s\n\n", name, b))
}

// WriteDone 输出终止符。
func (s *SSEWriter) WriteDone() {
	s.Raw("data: [DONE]\n\n")
}

// Close 停止心跳。
func (s *SSEWriter) Close() {
	if !s.closed {
		s.closed = true
		s.beat.Stop()
	}
}
