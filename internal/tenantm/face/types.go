// Package face provides real-time face detection + 5-point keypoint extraction.
package face

// Keypoint 5 点人脸关键点（按 YuNet 标准顺序）
//
// 序号 | 含义         | 典型用途
// -----|--------------|--------------------------
// 0    | 左眼         | 视线估计
// 1    | 右眼         | 视线估计
// 2    | 鼻尖         | 头部中心参考
// 3    | 左嘴角       | 表情识别（v1.1+）
// 4    | 右嘴角       | 表情识别（v1.1+）
type Keypoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Detection 一次检测结果（v1 仅检测 + 关键点，不做识别）
type Detection struct {
	BoundingBox [4]int     `json:"boundingBox"` // [x, y, w, h]
	Confidence  float64    `json:"confidence"`
	Keypoints   [5]Keypoint `json:"keypoints"`  // 5 点 landmarks（与 Keypoint 表顺序一致）
}
