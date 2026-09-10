package face

import "context"

// Detector 人脸检测器接口（v1：检测 + 5 点关键点）
//
// v1 不做人脸识别 / embedding / profile 匹配。
// 该接口是 aisaas 与具体推理实现（YuNet/SCRFD/...）的契约点。
type Detector interface {
	// Detect 从 JPEG 提取所有人脸位置 + 5 点关键点
	Detect(ctx context.Context, jpegData []byte) ([]Detection, error)
	// Name 返回 detector 名称（用于 metrics / 日志）
	Name() string
}

// MockDetector 开发/测试用 mock 实现
//
// 返回 1 张固定位置 + 5 个固定关键点的人脸。
//
// 生产环境替换：实现 SCRFD-10GF（Apache 2.0）+ ONNX Runtime Go，
// 参考 ykt-aisaas/docs/face/deployment.md §6。
type MockDetector struct{}

// NewMockDetector 构造 MockDetector
func NewMockDetector() *MockDetector {
	return &MockDetector{}
}

// Name 返回 detector 标识
func (m *MockDetector) Name() string {
	return "mock-detector-v1"
}

// Detect 返回 1 张固定人脸 + 5 点关键点
//
// 人脸 bbox (140,140,160,160)，中心 (220,220)，置信度 0.95
// 5 点关键点：左眼(180,200) 右眼(260,200) 鼻(220,230) 左嘴(200,260) 右嘴(240,260)
//
// 实现成本：不做实际 CV 推理；返回固定坐标便于测试 + 跟踪算法验证。
func (m *MockDetector) Detect(ctx context.Context, jpegData []byte) ([]Detection, error) {
	_ = jpegData
	_ = ctx
	return []Detection{
		{
			BoundingBox: [4]int{140, 140, 160, 160},
			Confidence:  0.95,
			Keypoints: [5]Keypoint{
				{X: 180, Y: 200}, // 0: left eye
				{X: 260, Y: 200}, // 1: right eye
				{X: 220, Y: 230}, // 2: nose
				{X: 200, Y: 260}, // 3: left mouth
				{X: 240, Y: 260}, // 4: right mouth
			},
		},
	}, nil
}
