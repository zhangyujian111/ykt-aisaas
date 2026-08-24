package openaiclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ykt.dev/aisaas/pkg/openaiclient"
)

// SSE 流式解析：跨 chunk 半行、[DONE] 终止、usage 尾包。
func TestStreamParsesChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		// 故意把一个 data 行拆成两次 Write（半行测试）
		w.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n\n"))
		w.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{"))
		fl.Flush()
		w.Write([]byte("\"content\":\"好\"}}]}\n\n"))
		w.Write([]byte("data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	c := openaiclient.New(srv.URL, "test-key")
	ch := c.Stream(context.Background(), &openaiclient.ChatRequest{Model: "m", Messages: []openaiclient.Message{{Role: "user", Content: "hi"}}})

	var content strings.Builder
	var usage *openaiclient.Usage
	n := 0
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("stream err: %v", ev.Err)
		}
		n++
		for _, c := range ev.Chunk.Choices {
			content.WriteString(c.Delta.Content)
		}
		if ev.Chunk.Usage != nil {
			usage = ev.Chunk.Usage
		}
	}
	if n < 3 {
		t.Fatalf("expected >=3 chunks, got %d", n)
	}
	if content.String() != "你好" {
		t.Fatalf("content = %q, want 你好", content.String())
	}
	if usage == nil || usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v, want total 15", usage)
	}
}

// 非流式调用 + 上游错误透传。
func TestCompleteAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	c := openaiclient.New(srv.URL, "k")
	resp, err := c.Complete(context.Background(), &openaiclient.ChatRequest{Model: "m", Messages: []openaiclient.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" || resp.Usage.TotalTokens != 3 {
		t.Fatalf("bad resp: %+v", resp)
	}

	var apiErr *openaiclient.APIError
	c2 := openaiclient.New(srv.URL+"/badpath", "k")
	_, err = c2.Complete(context.Background(), &openaiclient.ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !asAPIErr(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("expected 401 APIError, got %v", err)
	}
}

func asAPIErr(err error, target **openaiclient.APIError) bool {
	if e, ok := err.(*openaiclient.APIError); ok {
		*target = e
		return true
	}
	return false
}
