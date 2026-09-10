package outbox

import "errors"

// 信箱模块错误。
var (
	// ErrMaxRetryExceeded 超过最大重试次数，消息进入 failed 状态。
	ErrMaxRetryExceeded = errors.New("outbox: max retry exceeded")

	// ErrHandlerNotFound 未找到对应消息类型的处理器。
	ErrHandlerNotFound = errors.New("outbox: handler not found for message type")

	// ErrPayloadUnmarshal 载荷反序列化失败。
	ErrPayloadUnmarshal = errors.New("outbox: failed to unmarshal payload")
)