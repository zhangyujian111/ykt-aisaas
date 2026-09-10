# Face Detection 部署手册

> **目标读者**：上服务器时需要从开发模式（MockDetector）切换到生产模式（真实模型）的工程师
> **v1 范围**：人脸检测 + 5 点关键点。**不做**人脸识别 / 主人识别。
> **当前状态**：detect 流程已跑通；`MockDetector` 返回固定 bbox（140,140,160,160）+ 5 个固定 keypoint，用于开发与单元测试。
> **合规约束**（来自 P0-3 v1.0 CLOSED）：
> - 部署区域：**仅中国大陆**（PIPL + 数据安全法 + 网安法）
> - 实时帧缓存：**禁止**（aisaas HTTP client `RetryMax = 0`，失败即丢弃）
> - 审计日志保留：**30 天**（Prometheus TSDB + loki / ELK）
> - 详情见 `docs/compliance/p0-3-privacy.md`

---

## 1. 架构概览

```
xiaozhi-server-go                           ykt-aisaas
  │                                          │
  │ POST /internal/xiaozhi/v1/face/detect    │
  │ [4B crc][JPEG bytes]                     │
  ├─────────────────────────────────────────►│
  │                                          │ face.Detector
  │                                          │ ├─ MockDetector  (dev/test)
  │                                          │ └─ SCRFDDetector (prod)
  │                                          │
  │ {hit, detections[], latencyMs, model}    │
  │◄─────────────────────────────────────────┤
  │                                          │
  │ vision.FaceFollower (跨帧跟踪 + 几何)    │
  │   → 下行 servo 指令到设备                │
```

**职责边界**：
- 设备端 ESP32-S3：**只**采集 JPEG + 上传（不入库任何 PII）
- `ykt-aisaas`：实时检测（5 点 keypoints）+ 配额统计
- `xiaozhi-server-go`：跨帧跟踪（IoU + EMA + 死区 + 节流）+ servo 下发

---

## 2. 切换 MockDetector → SCRFDDetector

### 2.1 接口

```go
// ykt-aisaas/internal/tenantm/face/detector.go
type Detector interface {
    Detect(ctx context.Context, jpegData []byte) ([]Detection, error)
    Name() string
}
```

**Service 层无需改动**：`face.Service.Detect(ctx, DetectReq{DeviceID, ImageData, FrameCRC, TsMs})` 直接调用 `detector.Detect()`，返回 `DetectResp{Hit, Detections, LatencyMs, ModelUsed}`。改 Detector 实现即可。

### 2.2 MockDetector（开发）

固定返回 1 张人脸 + 5 个固定 keypoint：

```go
// ykt-aisaas/internal/tenantm/face/detector.go
type MockDetector struct{}

func (m *MockDetector) Detect(ctx context.Context, _ []byte) ([]Detection, error) {
    return []Detection{{
        BoundingBox: BoundingBox{X: 140, Y: 140, W: 160, H: 160},
        Confidence:  0.95,
        Keypoints: [5]Keypoint{
            {X: 180, Y: 200}, // left_eye
            {X: 260, Y: 200}, // right_eye
            {X: 220, Y: 230}, // nose
            {X: 200, Y: 260}, // left_mouth
            {X: 240, Y: 260}, // right_mouth
        },
    }}, nil
}

func (m *MockDetector) Name() string { return "mock-detector-v1" }
```

**适用场景**：
- 单元测试（无 GPU/CPU 模型依赖）
- 本地开发（不必安装 ONNX runtime）
- 联调 mock（用 Python mock_server 模拟）

### 2.3 SCRFDDetector（生产）

生产环境推荐 **SCRFD-10GF**（Apache 2.0 + InsightFace）：

| 项 | 值 |
|---|---|
| 模型 | SCRFD-10GF（det_10g.onnx） |
| 输入 | 640×640 RGB |
| 输出 | bbox + 5 个 landmark（与 v1 keypoints 对应）|
| 模型大小 | ~16MB（fp32）/ ~3.6MB（int8） |
| 延迟 | 640×640 单帧 CPU: ~50ms; GPU: ~10ms |
| 量化 | onnxruntime-quantization int8 |

**为什么选 SCRFD-10GF**：
- Apache 2.0 许可证（商业友好，**避免** brufik GPL-3.0）
- InsightFace 维护，CUDA EP / DirectML EP 稳定
- 输出格式直接就是 5 个 landmark（**无需**额外 landmark 模型）
- 相比 RetinaFace（MIT）：更小（10G vs MobileNet）、更准（Val AF=0.5 → AP 0.74）

**实现骨架**（`ykt-aisaas/internal/tenantm/face/scrfd_detector.go`）：

```go
//go:build onnxruntime

package face

import (
    "context"
    ort "github.com/yalue/onnxruntime_go"
)

type SCRFDDetector struct {
    session    *ort.AdvancedSession
    inputShape []int64  // [1, 3, 640, 640]
    name       string
}

func NewSCRFDDetector(modelPath string, logger *slog.Logger) (*SCRFDDetector, error) {
    // 1. 初始化 onnxruntime（动态库路径）
    if err := ort.InitializeEnvironment(); err != nil {
        return nil, fmt.Errorf("init onnxruntime: %w", err)
    }

    // 2. 创建 session
    session, err := ort.NewAdvancedSession(
        modelPath,
        []string{"input.1"},     // input name
        []string{"448", "471", "494"}, // 3 个 scale 输出
        nil,
    )
    if err != nil {
        return nil, fmt.Errorf("load SCRFD: %w", err)
    }

    return &SCRFDDetector{
        session:    session,
        inputShape: []int64{1, 3, 640, 640},
        name:       "scrfd-10gf-v1",
    }, nil
}

func (s *SCRFDDetector) Detect(ctx context.Context, jpegData []byte) ([]Detection, error) {
    // 1. JPEG decode + resize 640×640 + normalize (mean/std = 127.5/128.0)
    img, err := decodeAndResize(jpegData, 640, 640)
    if err != nil {
        return nil, fmt.Errorf("decode: %w", err)
    }
    input := imageToTensor(img)

    // 2. 推理
    outputs := s.session.Run([]ort.Value{ort.NewTensor(input)})

    // 3. 后处理：3 个 scale 的 bbox 解码 + NMS
    boxes := decodeBoxes(outputs, confThreshold=0.5, nmsThreshold=0.4)

    // 4. 转换到 Detection + 5 个 keypoint（scale 回原图）
    detections := make([]Detection, 0, len(boxes))
    for _, b := range boxes {
        detections = append(detections, Detection{
            BoundingBox: BoundingBox{
                X: int(b.X1 * scale),
                Y: int(b.Y1 * scale),
                W: int((b.X2 - b.X1) * scale),
                H: int((b.Y2 - b.Y1) * scale),
            },
            Confidence: b.Score,
            Keypoints: [5]Keypoint{
                {X: b.Landmarks[0].X * scale, Y: b.Landmarks[0].Y * scale}, // left_eye
                {X: b.Landmarks[1].X * scale, Y: b.Landmarks[1].Y * scale}, // right_eye
                {X: b.Landmarks[2].X * scale, Y: b.Landmarks[2].Y * scale}, // nose
                {X: b.Landmarks[3].X * scale, Y: b.Landmarks[3].Y * scale}, // left_mouth
                {X: b.Landmarks[4].X * scale, Y: b.Landmarks[4].Y * scale}, // right_mouth
            },
        })
    }
    return detections, nil
}

func (s *SCRFDDetector) Name() string { return s.name }
```

**注意**：
- 后处理解码逻辑（`decodeBoxes`）见 SCRFD 论文 §3 + InsightFace `scrfd.py`
- 输入分辨率固定 640×640（不能用 320 否则精度掉 20%+）
- CPU 推理用 `CPUExecutionProvider`；GPU 加 `CUDAExecutionProvider`

### 2.4 替代方案

| 方案 | 许可证 | 模型大小 | 精度 | 延迟 | 备注 |
|------|------|---------|------|------|------|
| SCRFD-10GF（推荐）| Apache-2.0 | 16MB | 高 | ~50ms CPU | InsightFace 维护 |
| YuNet | Apache-2.0 | ~340KB | 中 | ~20ms CPU | OpenCV 带，适合边缘 |
| RetinaFace-ResNet50 | MIT | 100MB+ | 高 | ~150ms CPU | 太大 |
| dlib-HOG | Boost | 0 | 低 | ~30ms CPU | 无关键点 |

**v1 推荐 SCRFD-10GF**（精度 + 许可证 + 维护活跃度最优）。

---

## 3. 配置（`config/face.yaml`）

```yaml
face:
  # v1 默认：MockDetector（dev/test）
  # 生产：改为 "scrfd"
  detector: "mock"

  scrfd:
    model_path: "/opt/aisaas/models/scrfd_10g_bnkps.onnx"
    int8_model_path: "/opt/aisaas/models/scrfd_10g_bnkps_int8.onnx"
    use_int8: true
    num_threads: 4

  quota:
    # 每设备每秒最多 detect 帧数（防滥用 + 限流）
    per_device_fps: 5

  # v1 不存任何 PII，无 retention 配置
```

---

## 4. 装配（`cmd/aisaas/main.go`）

```go
import (
    "ykt-aisaas/internal/tenantm/face"
)

func buildFaceService(ctx context.Context, cfg *config.FaceConfig, logger *slog.Logger) (face.Service, error) {
    var detector face.Detector
    var err error

    switch cfg.Detector {
    case "mock":
        detector = face.NewMockDetector()
    case "scrfd":
        // +build onnxruntime
        detector, err = face.NewSCRFDDetector(
            pickModelPath(cfg.SCRFD),
            logger,
        )
    default:
        return nil, fmt.Errorf("unknown face detector: %s", cfg.Detector)
    }
    if err != nil {
        return nil, fmt.Errorf("init detector: %w", err)
    }

    return face.NewService(detector, logger), nil
}
```

---

## 5. 性能与配额

### 5.1 单帧延迟预算

| 阶段 | 目标 | 备注 |
|------|------|------|
| JPEG decode + resize | < 5ms | Go `image/jpeg` + `imaging` |
| 模型推理 | < 50ms | SCRFD-10GF int8 @ CPU |
| 后处理（NMS + decode）| < 5ms | |
| **合计** | **< 60ms** | 单帧 p99 |

### 5.2 配额（quota）

每次 `detect` 扣 1 个 `detect_frames` quota：
- 默认每设备 5 fps（5 fps × 3600s/h × 1/h = 18000 帧/小时/设备）
- 超出返回 `429 RATE_LIMIT`
- 配额数据库表：`face.detect_quota_daily(device_id, day, count)`（可后续扩展）

### 5.3 并发

- SCRFD session 内部是单线程推理（GPU 异步）
- 多个 xiaozhi 设备并发请求 → 服务端用 worker pool 串行化
- 推荐 `runtime.NumCPU() / 2` 个并发 worker
- 监控指标：`aisaas_face_detect_inflight_gauge`

---

## 6. 监控与日志

### 6.1 Prometheus 指标

```
# 实时检测请求总数（按结果分）
aisaas_face_detect_total{status="hit|miss|error"}

# 单帧处理延迟（直方图）
aisaas_face_detect_latency_ms_bucket{le="..."}

# 并发 inflight 请求
aisaas_face_detect_inflight_gauge

# 配额扣减
aisaas_face_detect_quota_consumed_total{device_id="..."}
```

### 6.2 结构化日志（不打印 PII）

```json
{
  "ts": "2026-09-09T12:34:56Z",
  "level": "info",
  "msg": "face detect",
  "device_id": "esp32s3-A4CF12",
  "hit": true,
  "detection_count": 1,
  "latency_ms": 76,
  "model": "scrfd-10g-v1",
  "frame_crc": "0xDEADBEEF"
}
```

**禁止日志字段**：
- `image_bytes` / `image_base64`（绝对不能打）
- `embedding`（v1 没有）

---

## 7. 测试

### 7.1 单元测试（已实现）

```bash
cd ykt-aisaas
go test ./internal/tenantm/face/... -v
```

4 个测试（`service_test.go`）：
- `TestDetect_HitFace`：MockDetector 固定返回，断言 hit=true
- `TestDetect_LatencyMeasured`：latency > 0
- `TestDetector_Name`：name 字段非空
- `TestDetection_NoIdentificationFields`：检测结果无 PII 字段（回归保护）

### 7.2 集成测试（todo）

- [ ] 用真实 JPEG 测试 SCRFDDetector（需要模型文件，CI 跑不下）
- [ ] 并发压测：1000 QPS 持续 60s
- [ ] 异常处理：JPEG 损坏、巨大图（>5MB）、EXIF GPS 注入

### 7.3 E2E 测试

完整链路：ESP32 mock server → xiaozhi-server-go → aisaas → face_track_ack
详见 `xiaozhi-server-go/docs/firmware/esp32-vision-tracking.md` §6。

---

## 8. 切换检查清单（生产上线前）

- [ ] `config/face.yaml` `detector: "scrfd"`
- [ ] `scrfd.model_path` / `int8_model_path` 文件存在（chmod 644）
- [ ] `/opt/aisaas/models/` 目录权限正确（aisaas user 只读）
- [ ] onnxruntime 动态库 `LD_LIBRARY_PATH` 已配置（Linux）
- [ ] `num_threads` 与容器 CPU 配额一致
- [ ] 监控告警：`aisaas_face_detect_latency_ms_p99 > 100ms` 触发
- [ ] 日志脱敏已验证：`image_bytes` 不出现在日志
- [ ] quota 表 schema 已 migration（`face.detect_quota_daily`）
- [ ] 单元测试 + 集成测试 全过
- [ ] 灰度：先开 1 台设备，观察 30min

---

## 9. 故障排查

| 现象 | 可能原因 | 排查 |
|------|---------|------|
| 500 "model not initialized" | onnxruntime 动态库未找到 | `ldd /opt/aisaas/bin/aisaas | grep onnx` |
| 500 "decode failed" | JPEG 损坏 / EXIF GPS 注入被拒 | 抓原始 `camera_frame` 二进制分析 |
| 延迟 p99 > 200ms | CPU 配额不够 | `docker stats aisaas`，给到 4 vCPU+ |
| 429 RATE_LIMIT | 设备发送 fps 超限 | 看 `device_id` 日志，确认设备是否异常 |
| 误检（无人脸有结果）| confidence 阈值太低 | 改 `cfg.SCRFD.conf_threshold = 0.6` |

---

**版本**：1.0
**下次评审**：SCRFDDetector 实现 + 上线后反馈延迟/精度数据
**配套文档**：`docs/face/openapi.yaml`、`xiaozhi-server-go/docs/protocol/vision-servo-v1.md`、`xiaozhi-server-go/docs/firmware/esp32-vision-tracking.md`
