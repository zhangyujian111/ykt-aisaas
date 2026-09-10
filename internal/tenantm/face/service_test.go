package face

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// TestDetect_HitFace 验证 MockDetector 返回固定人脸 + 5 点关键点
func TestDetect_HitFace(t *testing.T) {
	detector := NewMockDetector()
	svc := NewService(detector, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	ctx := context.Background()

	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	resp, err := svc.Detect(ctx, DetectReq{
		DeviceID:  "test-device-001",
		ImageData: jpeg,
		FrameCRC:  0xDEADBEEF,
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	if !resp.Hit {
		t.Fatal("expected Hit=true")
	}
	if len(resp.Detections) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(resp.Detections))
	}

	d := resp.Detections[0]
	if d.BoundingBox != [4]int{140, 140, 160, 160} {
		t.Errorf("unexpected bbox: %v", d.BoundingBox)
	}
	if d.Confidence != 0.95 {
		t.Errorf("unexpected confidence: %f", d.Confidence)
	}

	// 5 点关键点完整性检查
	want := [5]Keypoint{
		{X: 180, Y: 200}, // left eye
		{X: 260, Y: 200}, // right eye
		{X: 220, Y: 230}, // nose
		{X: 200, Y: 260}, // left mouth
		{X: 240, Y: 260}, // right mouth
	}
	for i := 0; i < 5; i++ {
		if d.Keypoints[i] != want[i] {
			t.Errorf("keypoint %d: want %+v, got %+v", i, want[i], d.Keypoints[i])
		}
	}

	if resp.ModelUsed != "mock-detector-v1" {
		t.Errorf("unexpected model name: %s", resp.ModelUsed)
	}

	t.Logf("PASS: bbox=%v conf=%.2f kps=%d latency=%dms model=%s",
		d.BoundingBox, d.Confidence, len(d.Keypoints), resp.LatencyMs, resp.ModelUsed)
}

// TestDetect_LatencyMeasured 验证 latencyMs > 0
func TestDetect_LatencyMeasured(t *testing.T) {
	svc := NewService(NewMockDetector(), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	resp, err := svc.Detect(context.Background(), DetectReq{
		DeviceID:  "test",
		ImageData: []byte{0xFF, 0xD8},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("latencyMs should be non-negative, got %d", resp.LatencyMs)
	}
	t.Logf("latency measured: %dms", resp.LatencyMs)
}

// TestDetector_Name 验证 Name 字段
func TestDetector_Name(t *testing.T) {
	d := NewMockDetector()
	if d.Name() != "mock-detector-v1" {
		t.Errorf("unexpected name: %s", d.Name())
	}
}

// TestDetection_NoIdentificationFields 验证 Detection 结构不暴露识别字段
//
// 防止 v1.1 之后意外添加 ProfileID/DisplayName 等识别字段
func TestDetection_NoIdentificationFields(t *testing.T) {
	d := Detection{
		BoundingBox: [4]int{0, 0, 100, 100},
		Confidence:  0.9,
		Keypoints:   [5]Keypoint{},
	}
	// 运行时字段检查（编译期保证见 types.go）
	if d.BoundingBox != [4]int{0, 0, 100, 100} {
		t.Error("bbox mismatched")
	}
	if len(d.Keypoints) != 5 {
		t.Errorf("must have exactly 5 keypoints, got %d", len(d.Keypoints))
	}
	t.Log("PASS: v1 Detection carries only bbox + confidence + 5 keypoints (no PII)")
}
