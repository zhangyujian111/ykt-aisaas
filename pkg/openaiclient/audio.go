package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// SpeechRequest /v1/audio/speech 请求（OpenAI 协议 + 平台 x- 扩展）。
type SpeechRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Speed          *float64 `json:"speed,omitempty"`
	Emotion        string   `json:"emotion,omitempty"` // 平台扩展：自建 CosyVoice 服务支持
}

// SpeechResult 流式合成结果：音频体 + Content-Type（audio/mpeg 等）。
type SpeechResult struct {
	Body        io.ReadCloser
	ContentType string
}

// Speech 调用上游 TTS。返回流式 body（调用方负责 Close）。
func (c *Client) Speech(ctx context.Context, req *SpeechRequest) (*SpeechResult, error) {
	httpReq, err := c.newPost(ctx, "/audio/speech", req, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	return &SpeechResult{Body: resp.Body, ContentType: ct}, nil
}

// TranscriptionResult ASR 结果（OpenAI verbose_json 超集）。
type TranscriptionResult struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration,omitempty"`
	Language string  `json:"language,omitempty"`
	Task     string  `json:"task,omitempty"`
	// 平台扩展（部分上游返回；无则为空）
	Emotion      string  `json:"x-emotion,omitempty"`
	EmotionScore float64 `json:"x-emotion-score,omitempty"`
}

// Transcribe 转发 multipart 到上游 /audio/transcriptions（verbose_json 以取 duration）。
// fileBody 为音频原始字节，fileName 含扩展名（决定上游解码器）。
func (c *Client) Transcribe(ctx context.Context, fileBody []byte, fileName, model, language string) (*TranscriptionResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(fileBody); err != nil {
		return nil, err
	}
	_ = w.WriteField("model", model)
	if language != "" {
		_ = w.WriteField("language", language)
	}
	_ = w.WriteField("response_format", "verbose_json")
	if err := w.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", w.FormDataContentType())
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out TranscriptionResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode transcription: %w", err)
	}
	return &out, nil
}
