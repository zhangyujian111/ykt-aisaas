// Package internalapi 内部超级租户接口（/internal/xiaozhi/v1/*，X-Internal-Token + X-Device-Id）。
package internalapi

import (
	"io"
	"log/slog"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/face"
)

// FaceHandler /internal/xiaozhi/v1/face/* 人脸检测（v1：detect + 5 点 keypoints）
type FaceHandler struct {
	Svc   face.Service
	Quota *quota.Guard
	Meter *metering.Recorder
}

// Detect POST /internal/xiaozhi/v1/face/detect
// body: raw JPEG bytes (image/jpeg)
// header: X-Device-Id (由 InternalDeviceMiddleware 注入到 c.Set)
//
// v1 范围：仅人脸检测 + 5 点关键点提取；不返回任何 PII（无 profile_id / display_name）。
// body 格式：[4B frame_crc BE][N-4 bytes JPEG payload]
//   - 前 4 字节为协议 §5.2 约定的 frame_crc（CRC32-IEEE BE）
//   - 后续为纯 JPEG（不允许 EXIF GPS）
func (h *FaceHandler) Detect(c *gin.Context) {
	deviceID, _ := c.Get("deviceId")
	deviceIDStr, ok := deviceID.(string)
	if !ok || deviceIDStr == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "缺少 X-Device-Id"))
		return
	}

	// 读取 JPEG body（最大 10MB）
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 10<<20))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, "读取图片失败"))
		return
	}
	if len(body) == 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "图片数据为空"))
		return
	}

	// 前 4 字节是 frame_crc（来自协议 §5.2）
	var frameCRC uint32
	if len(body) >= 4 {
		frameCRC = uint32(body[0])<<24 | uint32(body[1])<<16 | uint32(body[2])<<8 | uint32(body[3])
	}

	// 调用服务（无识别、无 embedding、无 profile）
	resp, err := h.Svc.Detect(c.Request.Context(), face.DetectReq{
		DeviceID:  deviceIDStr,
		ImageData: body,
		FrameCRC:  frameCRC,
		TsMs:      0,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "face detect failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 计量（按帧计 quota 1）
	if h.Meter != nil {
		h.Meter.Record(c.Request.Context(), metering.Record{
			TenantID:  0,
			BizType:   "face",
			Dimension: "detect_frames",
			Amount:    1,
		})
	}

	web.OK(c, resp)
}
