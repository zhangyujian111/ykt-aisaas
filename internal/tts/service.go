// Package tts 语音合成服务。
package tts

import (
	"context"
	"unicode/utf8"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// Service TTS 编排：模型路由 + 透传上游（OpenAI 兼容音频端点）。
type Service struct{ Registry *llm.Registry }

// New 构造。
func New(r *llm.Registry) *Service { return &Service{Registry: r} }

// Synthesize 返回流式音频。
func (s *Service) Synthesize(ctx context.Context, req *openaiclient.SpeechRequest) (*openaiclient.SpeechResult, *llm.Resolved, error) {
	resolved, err := s.Registry.ResolveFor(ctx, req.Model, "tts")
	if err != nil {
		return nil, nil, err
	}
	if utf8.RuneCountInString(req.Input) == 0 {
		return nil, nil, errs.New(errs.InvalidParam, "input 不能为空")
	}
	up := &openaiclient.SpeechRequest{
		Model:          resolved.UpstreamModel,
		Input:          req.Input,
		Voice:          req.Voice,
		ResponseFormat: req.ResponseFormat,
		Speed:          req.Speed,
		Emotion:        req.Emotion,
	}
	result, err := resolved.Client.Speech(ctx, up)
	if err != nil {
		return nil, resolved, errs.Wrap(errs.ProviderError, err)
	}
	return result, resolved, nil
}
