package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// stdioConn 通过子进程 stdin/stdout 实现 MCP 传输。
//
// 协议：JSON-RPC 2.0，每行一条 JSON 消息（newline-delimited）。
// 线程安全：stdin/stdout 非并发安全，通过 mutex 串行化所有 Call。
type stdioConn struct {
	cfg    ServerConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	reader *bufio.Scanner
	mu     sync.Mutex
}

func newStdioConn(cfg ServerConfig) *stdioConn {
	return &stdioConn{cfg: cfg}
}

// connect 启动子进程并建立 stdio 管道。
//
// 命令参数来自 ServerConfig（数据库/配置文件），应假定可信。
// 但为防御潜在配置错误，仍验证命令可执行性。
func (s *stdioConn) connect(ctx context.Context) error {
	if len(s.cfg.Command) == 0 {
		return fmt.Errorf("stdio transport requires non-empty command")
	}

	cmdName := s.cfg.Command[0]
	// C-1: 验证命令在 PATH 中存在
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("command not found: %s: %w", cmdName, err)
	}

	var args []string
	if len(s.cfg.Command) > 1 {
		args = s.cfg.Command[1:]
	}

	s.cmd = exec.CommandContext(ctx, cmdName, args...)

	var err error
	s.stdin, err = s.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	s.stdout, err = s.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	s.stderr, err = s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start command %s: %w", cmdName, err)
	}

	// 初始化逐行读取器（1MB 缓冲区上限）
	s.reader = bufio.NewScanner(s.stdout)
	s.reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// 后台 goroutine 读取 stderr 并记录日志
	go s.readStderr(ctx)

	slog.Debug("stdio process started",
		"server", s.cfg.Name,
		"command", cmdName,
		"pid", s.cmd.Process.Pid,
	)
	return nil
}

// readStderr 后台读取 stderr 并记录日志。
func (s *stdioConn) readStderr(ctx context.Context) {
	scanner := bufio.NewScanner(s.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Warn("stdio stderr", "server", s.cfg.Name, "stderr", line)
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("stdio stderr closed", "server", s.cfg.Name, "err", err)
	}
}

// call 发送 JSON-RPC 请求到 stdin 并从 stdout 读取响应。
func (s *stdioConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查进程状态
	if s.cmd == nil || s.cmd.Process == nil {
		return nil, fmt.Errorf("stdio connection not established")
	}
	if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
		return nil, fmt.Errorf("stdio process exited with code %d", s.cmd.ProcessState.ExitCode())
	}

	// 序列化 params
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	// 构造 JSON-RPC 请求
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      newRequestID(),
		Method:  method,
		Params:  paramsBytes,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	// 写入 stdin
	if _, err := s.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	// 读取 stdout 响应（带超时）
	return s.readResponse(ctx, method)
}

// readResponse 从 stdout 读取一行 JSON-RPC 响应，带超时控制。
//
// C-3: 使用 done channel 通知 goroutine 退出，避免 goroutine 泄漏。
// W-1: 使用 time.NewTimer 替代 time.After，避免 timer 泄漏。
func (s *stdioConn) readResponse(ctx context.Context, method string) (json.RawMessage, error) {
	type result struct {
		data json.RawMessage
		err  error
	}
	ch := make(chan result, 1)
	done := make(chan struct{}) // C-3: 通知 goroutine 调用方已超时/取消

	go func() {
		defer close(done) // C-3: goroutine 退出信号

		var r result
		if s.reader.Scan() {
			line := s.reader.Bytes()
			var resp JSONRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				r.err = fmt.Errorf("parse response: %w (raw=%s)", err, trunc(line, 200))
			} else if resp.Error != nil {
				r.err = fmt.Errorf("json-rpc error [%d]: %s", resp.Error.Code, resp.Error.Message)
			} else {
				r.data = resp.Result
			}
		} else {
			if err := s.reader.Err(); err != nil {
				r.err = fmt.Errorf("read stdout: %w", err)
			} else {
				r.err = fmt.Errorf("unexpected EOF from stdout")
			}
		}

		select {
		case ch <- r:
		case <-done:
			// C-3: 调用方已超时，不发送结果
		}
	}()

	timeout := s.cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	timer := time.NewTimer(timeout) // W-1: 使用 timer 避免泄漏
	defer timer.Stop()

	select {
	case <-ctx.Done():
		close(done) // C-3: 通知 goroutine 退出
		return nil, fmt.Errorf("stdio call %s: %w", method, ctx.Err())
	case <-timer.C:
		close(done) // C-3: 通知 goroutine 退出
		return nil, fmt.Errorf("stdio call %s: timeout after %v", method, timeout)
	case r := <-ch:
		return r.data, r.err
	}
}

// close 关闭子进程并释放资源。
//
// C-2: 两阶段关闭避免持锁 Close 导致的潜在死锁。
func (s *stdioConn) close() error {
	// 阶段 1: 关闭 stdin（锁外执行，避免阻塞）
	s.mu.Lock()
	if s.stdin != nil {
		stdin := s.stdin
		s.stdin = nil
		s.mu.Unlock()
		_ = stdin.Close()
	} else {
		s.mu.Unlock()
	}

	// 阶段 2: 终止进程并等待退出
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		if s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited() {
			if err := s.cmd.Process.Kill(); err != nil {
				slog.Warn("stdio kill process", "server", s.cfg.Name, "err", err)
			}
		}
		// 等待进程退出（避免僵尸进程）
		_ = s.cmd.Wait()
		slog.Debug("stdio process closed", "server", s.cfg.Name)
	}
	s.mu.Unlock()
	return nil
}

// trunc 截断字节切片到指定长度。
func trunc(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}