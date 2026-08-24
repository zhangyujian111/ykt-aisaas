// mock-upstream：OpenAI 兼容 mock 服务（开发/测试用）。
// 支持：POST /v1/chat/completions（流式 + 非流式）、GET /healthz。
package main

import (
	"encoding/json"
	"strings"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	port := flag.Int("port", 18080, "listen port")
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// OpenAI 兼容 TTS：返回合成的 WAV（流式分块写出）
	r.POST("/v1/audio/speech", func(c *gin.Context) {
		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
			Voice string `json:"voice"`
		}
		_ = c.ShouldBindJSON(&body)
		wav := synthWav(body.Input, body.Voice)
		c.Header("Content-Type", "audio/wav")
		fl, _ := c.Writer.(http.Flusher)
		for i := 0; i < len(wav); i += 4096 {
			end := i + 4096
			if end > len(wav) {
				end = len(wav)
			}
			c.Writer.Write(wav[i:end])
			if fl != nil {
				fl.Flush()
				time.Sleep(10 * time.Millisecond)
			}
		}
	})

	// OpenAI 兼容 ASR：接收 multipart，返回 verbose_json
	r.POST("/v1/audio/transcriptions", func(c *gin.Context) {
		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(400, gin.H{"error": gin.H{"message": "no file"}})
			return
		}
		f, _ := fileHeader.Open()
		defer f.Close()
		buf := make([]byte, fileHeader.Size)
		_, _ = f.Read(buf)
		c.JSON(200, gin.H{
			"task":     "transcribe",
			"language": "zh",
			"duration": float64(len(buf)) / 32000.0,
			"text":     "这是 mock 上游的识别结果，音频大小 " + itoa(int(fileHeader.Size)) + " 字节",
			"x-emotion":       "neutral",
			"x-emotion-score": 0.9,
		})
	})

	// OpenAI 兼容 embeddings：确定性向量（sha256 种子），dim 从模型名 -<N> 后缀解析（默认 16）
	r.POST("/v1/embeddings", func(c *gin.Context) {
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = c.ShouldBindJSON(&body)
		dim := parseDim(body.Model)
		data := make([]gin.H, len(body.Input))
		total := 0
		for i, txt := range body.Input {
			data[i] = gin.H{"object": "embedding", "index": i, "embedding": embedVec(txt, dim)}
			total += len([]rune(txt))
		}
		c.JSON(200, gin.H{"object": "list", "data": data, "model": body.Model,
			"usage": gin.H{"prompt_tokens": total, "total_tokens": total}})
	})

	// 工具 echo 端点（HTTP 工具 e2e 用）
	r.POST("/tools/echo", func(c *gin.Context) {
		var args map[string]any
		_ = c.ShouldBindJSON(&args)
		c.JSON(200, gin.H{"echo": args, "ok": true})
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		var body struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []msg `json:"messages"`
		}
		_ = c.ShouldBindJSON(&body)

		// 带工具且尚无 tool 结果 → 请求调用第一个工具
		if len(body.Tools) > 0 && !hasToolResult(body.Messages) {
			fn := body.Tools[0].Function.Name
			c.JSON(200, gin.H{
				"id": "chatcmpl-mock-tools", "object": "chat.completion", "model": body.Model,
				"choices": []gin.H{{
					"index": 0, "finish_reason": "tool_calls",
					"message": gin.H{
						"role": "assistant",
						"tool_calls": []gin.H{{
							"id": "call_mock_1", "type": "function",
							"function": gin.H{"name": fn, "arguments": fmt.Sprintf(`{"input":"%s"}`, truncRunes(lastUser(body.Messages), 20))},
						}},
					},
				}},
				"usage": gin.H{"prompt_tokens": 15, "completion_tokens": 10, "total_tokens": 25},
			})
			return
		}
		// 有 tool 结果 → 终答（引用工具输出）
		if hasToolResult(body.Messages) {
			toolOut := lastToolResult(body.Messages)
			c.JSON(200, gin.H{
				"id": "chatcmpl-mock-tools", "object": "chat.completion", "model": body.Model,
				"choices": []gin.H{{
					"index": 0, "finish_reason": "stop",
					"message": gin.H{"role": "assistant", "content": "根据工具结果回答：" + truncRunes(toolOut, 60)},
				}},
				"usage": gin.H{"prompt_tokens": 30, "completion_tokens": 20, "total_tokens": 50},
			})
			return
		}

		reply := fmt.Sprintf("你好，这是 mock 上游的回复。你使用模型 %s 说了：%s", body.Model, lastUser(body.Messages))

		if !body.Stream {
			c.JSON(200, gin.H{
				"id": "chatcmpl-mock", "object": "chat.completion", "model": body.Model,
				"choices": []gin.H{{
					"index": 0, "finish_reason": "stop",
					"message": gin.H{"role": "assistant", "content": reply},
				}},
				"usage": gin.H{"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36},
			})
			return
		}

		// 流式：按 2 字符切片下发，末尾带 usage
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		fl, _ := c.Writer.(http.Flusher)
		runes := []rune(reply)
		for i := 0; i < len(runes); i += 2 {
			end := i + 2
			if end > len(runes) {
				end = len(runes)
			}
			chunk := gin.H{
				"id": "chatcmpl-mock", "object": "chat.completion.chunk", "model": body.Model,
				"choices": []gin.H{{"index": 0, "delta": gin.H{"content": string(runes[i:end])}, "finish_reason": nil}},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", b)
			fl.Flush()
			time.Sleep(time.Duration(20+rand.Intn(30)) * time.Millisecond)
		}
		stop := gin.H{
			"id": "chatcmpl-mock", "object": "chat.completion.chunk", "model": body.Model,
			"choices": []gin.H{{"index": 0, "delta": gin.H{}, "finish_reason": "stop"}},
		}
		b, _ := json.Marshal(stop)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		usage := gin.H{
			"id": "chatcmpl-mock", "object": "chat.completion.chunk", "model": body.Model,
			"choices": []gin.H{},
			"usage":   gin.H{"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36},
		}
		b2, _ := json.Marshal(usage)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b2)
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		fl.Flush()
	})

	_ = r.Run(fmt.Sprintf(":%d", *port))
}

func parseDim(model string) int {
	i := strings.LastIndex(model, "-")
	if i < 0 {
		return 16
	}
	n := 0
	for _, ch := range model[i+1:] {
		if ch < '0' || ch > '9' {
			return 16
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 || n > 4096 {
		return 16
	}
	return n
}

// embedVec 确定性伪向量：词元哈希叠加，让相似文本在 mock 下也有可复现的相似性。
func embedVec(text string, dim int) []float64 {
	v := make([]float64, dim)
	words := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\n' || r == ',' || r == '。' })
	for _, w := range words {
		for _, k := range []rune(w) {
			v[int(k)%dim] += float64(int(k)%7-3) * 0.01
		}
		v[len(w)%dim] += 0.05
	}
	// 归一化
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	if norm == 0 {
		v[0] = 1
		norm = 1
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}

// synthWav 生成含 440Hz 正弦波的 16kHz 16bit 单声道 WAV（长度随输入文本）。
func synthWav(text, voice string) []byte {
	samples := 16000 * max(1, len([]rune(text))/4) // 每字符 0.25s
	dataLen := samples * 2
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	le(hdr[4:8], 36+dataLen)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	le(hdr[16:20], 16)
	hdr[20] = 1
	hdr[22] = 1
	le(hdr[24:28], 16000)
	le(hdr[28:32], 32000)
	hdr[32] = 2
	hdr[34] = 16
	copy(hdr[36:40], "data")
	le(hdr[40:44], dataLen)
	out := make([]byte, 44+dataLen)
	copy(out, hdr)
	for i := 0; i < samples; i++ {
		v := int16(8000 * sin(float64(i) * 2 * 3.14159 / 16000 * 440))
		out[44+i*2] = byte(v)
		out[44+i*2+1] = byte(v >> 8)
	}
	_ = voice
	return out
}

func le(b []byte, v int) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func sin(x float64) float64 {
	// 三项泰勒展开足够 mock 用
	return x - x*x*x/6 + x*x*x*x*x/120
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type msg struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

func hasToolResult(msgs []msg) bool {
	for _, m := range msgs {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

func lastToolResult(msgs []msg) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			return msgs[i].Content
		}
	}
	return ""
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func lastUser(msgs []msg) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return "(无用户消息)"
}
