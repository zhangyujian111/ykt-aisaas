// Package asr 语音识别服务。
package asr

import (
	"context"
	"math"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// Service ASR 编排：模型路由 + multipart 透传。
type Service struct{ Registry *llm.Registry }

// New 构造。
func New(r *llm.Registry) *Service { return &Service{Registry: r} }

// Recognize 整段音频识别（xiaozhi-server 的 VAD 切段后逐段调用）。
func (s *Service) Recognize(ctx context.Context, body []byte, fileName, modelID, language string) (*openaiclient.TranscriptionResult, *llm.Resolved, error) {
	resolved, err := s.Registry.ResolveFor(ctx, modelID, "asr")
	if err != nil {
		return nil, nil, err
	}
	if len(body) == 0 {
		return nil, nil, errs.New(errs.InvalidParam, "file 不能为空")
	}
	result, err := resolved.Client.Transcribe(ctx, body, fileName, resolved.UpstreamModel, language)
	if err != nil {
		return nil, resolved, errs.Wrap(errs.ProviderError, err)
	}
	return result, resolved, nil
}

// EstimateSeconds 秒数估算（上游未返回 duration 时）：
// 按 16kHz 16bit 单声道 ≈ 32KB/s 折算，至少 1 秒。
func EstimateSeconds(bodyLen int, duration float64) int64 {
	if duration > 0 {
		return int64(math.Ceil(duration))
	}
	sec := int64(math.Ceil(float64(bodyLen) / 32000.0))
	if sec < 1 {
		sec = 1
	}
	return sec
}
