package vision

import "fmt"

// Vision 错误定义

var (
	// ErrImageTooLarge 图片超过最大限制
	ErrImageTooLarge = fmt.Errorf("image size exceeds maximum limit of 20MB")

	// ErrInvalidImageFormat 无效的图片格式
	ErrInvalidImageFormat = fmt.Errorf("invalid image format, supported: jpeg, png, gif, webp")

	// ErrProviderNotFound Provider 未找到
	ErrProviderNotFound = fmt.Errorf("vision provider not found")

	// ErrInvalidImageURL 无效的图片 URL
	ErrInvalidImageURL = fmt.Errorf("invalid image URL")

	// ErrAnalysisFailed 分析失败
	ErrAnalysisFailed = fmt.Errorf("vision analysis failed")

	// ErrQuotaExceeded 配额不足
	ErrQuotaExceeded = fmt.Errorf("vision quota exceeded")

	// ErrModelDisabled 模型已禁用
	ErrModelDisabled = fmt.Errorf("vision model is disabled")

	// ErrMissingPrompt 缺少提示词
	ErrMissingPrompt = fmt.Errorf("prompt is required for vision analysis")

	// ErrCompareRequiresMultipleImages 多图对比需要至少两张图片
	ErrCompareRequiresMultipleImages = fmt.Errorf("compare requires at least 2 images")

	// ErrVideoFrameExtractionFailed 视频帧提取失败
	ErrVideoFrameExtractionFailed = fmt.Errorf("video frame extraction failed")
)

// IsRetryable 判断错误是否可重试
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	// 网络错误、超时等可重试
	errStr := err.Error()
	retryableErrors := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"network unreachable",
		"i/o timeout",
		"server misbehaving",
	}
	for _, s := range retryableErrors {
		if contains(errStr, s) {
			return true
		}
	}
	return false
}

// contains 判断 s 是否包含 substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
