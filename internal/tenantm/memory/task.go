package memory

import "context"

// TaskExecutor 异步任务执行接口。
// 供 Worker 统一调度不同的任务类型（extract / summarize）。
type TaskExecutor interface {
	// Type 返回任务类型标识。
	Type() string
	// Execute 执行任务。payload 为 JSON 序列化的任务体。
	Execute(ctx context.Context, payload []byte) error
}

// TaskResult 任务执行结果。
type TaskResult struct {
	TaskID string    `json:"taskId"`
	Status TaskState `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// extractTaskExecutor 适配 ExtractWorker 到 TaskExecutor。
type extractTaskExecutor struct {
	w *ExtractWorker
}

func (e *extractTaskExecutor) Type() string { return "extract" }

func (e *extractTaskExecutor) Execute(ctx context.Context, payload []byte) error {
	return e.w.ProcessExtractTask(ctx, nil) // payload parsed by worker caller
}

// summarizeTaskExecutor 适配 SummarizeWorker 到 TaskExecutor。
type summarizeTaskExecutor struct {
	w *SummarizeWorker
}

func (e *summarizeTaskExecutor) Type() string { return "summarize" }

func (e *summarizeTaskExecutor) Execute(ctx context.Context, payload []byte) error {
	return e.w.ProcessSummarizeTask(ctx, nil) // payload parsed by worker caller
}

// 编译期接口合规检查
var (
	_ TaskExecutor = (*extractTaskExecutor)(nil)
	_ TaskExecutor = (*summarizeTaskExecutor)(nil)
)