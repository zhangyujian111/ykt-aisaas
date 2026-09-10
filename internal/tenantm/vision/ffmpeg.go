package vision

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FrameExtractor 视频帧提取接口
type FrameExtractor interface {
	// ExtractKeyframes 提取视频关键帧（基于场景变化检测）
	ExtractKeyframes(ctx context.Context, videoPath string, outputDir string) ([]string, error)
	// ExtractByInterval 按固定间隔提取视频帧
	ExtractByInterval(ctx context.Context, videoPath string, intervalSec int) ([]string, error)
}

// FFmpegExtractor 基于 FFmpeg 的帧提取器
type FFmpegExtractor struct {
	ffmpegPath string
	outputDir  string
}

// NewFFmpegExtractor 创建 FFmpeg 帧提取器
func NewFFmpegExtractor(ffmpegPath string, outputDir string) *FFmpegExtractor {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if outputDir == "" {
		outputDir = os.TempDir()
	}
	return &FFmpegExtractor{
		ffmpegPath: ffmpegPath,
		outputDir:  outputDir,
	}
}

// ExtractKeyframes 提取视频关键帧（基于场景变化检测）
//
// 使用 FFmpeg scene detection 算法，检测场景变化大于 0.3 的帧作为关键帧
// ffmpeg -i input.mp4 -vf "select=gt(scene\,0.3)" -vsync vfr frame_%04d.jpg
func (e *FFmpegExtractor) ExtractKeyframes(ctx context.Context, videoPath string, outputDir string) ([]string, error) {
	if outputDir == "" {
		outputDir = e.outputDir
	}

	// 确保输出目录存在
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir failed: %w", err)
	}

	// 生成输出文件名前缀
	outputPattern := filepath.Join(outputDir, "kf_%04d.jpg")

	args := []string{
		"-i", videoPath,
		"-vf", "select=gt(scene\\,0.3)",
		"-vsync", "vfr",
		"-q:v", "2",
		outputPattern,
	}

	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg keyframes extraction failed: %w, stderr: %s", err, stderr.String())
	}

	// 收集提取的帧文件
	return e.collectFrameFiles(outputDir, "kf_")
}

// ExtractByInterval 按固定间隔提取视频帧
//
// 使用 FFmpeg 按指定间隔提取帧
// ffmpeg -i input.mp4 -vf "fps=1/interval" frame_%04d.jpg
func (e *FFmpegExtractor) ExtractByInterval(ctx context.Context, videoPath string, intervalSec int) ([]string, error) {
	outputDir := filepath.Join(e.outputDir, "frames_"+strconv.Itoa(intervalSec)+"s")
	
	// 确保输出目录存在
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir failed: %w", err)
	}

	// 计算 fps 值
	fps := fmt.Sprintf("1/%d", intervalSec)
	outputPattern := filepath.Join(outputDir, "frame_%04d.jpg")

	args := []string{
		"-i", videoPath,
		"-vf", fmt.Sprintf("fps=%s", fps),
		"-q:v", "2",
		"-frames:v", "60", // 最大 60 帧
		outputPattern,
	}

	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg interval extraction failed: %w, stderr: %s", err, stderr.String())
	}

	// 收集提取的帧文件
	return e.collectFrameFiles(outputDir, "frame_")
}

// collectFrameFiles 收集指定目录下符合前缀的帧文件
func (e *FFmpegExtractor) collectFrameFiles(dir, prefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir failed: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".jpg") {
			files = append(files, filepath.Join(dir, name))
		}
	}

	return files, nil
}

// CleanupFrameFiles 清理提取的帧文件
func (e *FFmpegExtractor) CleanupFrameFiles(dir string) error {
	return os.RemoveAll(dir)
}
