// Package federated 联邦学习实现 - 多区域联合训练。
//
// 核心特性：
//   - FedAvg 聚合算法
//   - 差分隐私（ Laplace 噪声）
//   - 安全聚合（ MPC ）
//   - gRPC 通信
//   - 3 个 Region 客户端（ us-east-1, eu-west-1, ap-southeast-1 ）
//
// 隐私约束：
//   - 训练数据永远在本地，不出 Region
//   - 仅传输模型权重差分（ weight diff ）
//   - 不做 non-IID 优化（ V11 ）
package federated

import (
	"encoding/json"
	"time"
)

// GlobalModel 全局模型 - 在 Coordinator 端维护。
type GlobalModel struct {
	Weights    []float64 `json:"weights"`    // 模型权重向量
	Version    int       `json:"version"`    // 模型版本
	Round      int       `json:"round"`      // 联邦轮次
	Clients    int       `json:"clients"`    // 参与客户端数
	UpdatedAt  time.Time `json:"updated_at"` // 更新时间
}

// ClientUpdate 客户端更新 - 仅传输权重差分，不含原始数据。
type ClientUpdate struct {
	ClientID   string    `json:"client_id"`   // 客户端 ID
	Region     string    `json:"region"`      // Region （用于审计）
	WeightDiff []float64 `json:"weight_diff"` // 权重差分（不是完整权重）
	NumSamples int       `json:"num_samples"` // 本地样本数（用于 FedAvg 加权）
	Timestamp  time.Time `json:"timestamp"`  // 时间戳
}

// TrainingSample 本地训练样本 - 仅存在于客户端本地，不传输。
type TrainingSample struct {
	TenantID  string    `json:"tenant_id"`
	Features  []float64 `json:"features"` // 12 维特征向量
	Label     int       `json:"label"`    // 0=normal, 1=anomaly
	Timestamp time.Time `json:"timestamp"`
}

// LocalModel 本地模型 - 在客户端训练使用。
type LocalModel struct {
	Weights     []float64 // 当前权重
	PrevWeights []float64 // 上一次同步时的权重
	NumFeatures int       // 特征维度
	Epochs      int       // 本地训练轮数
}

// NewLocalModel 创建本地模型。
func NewLocalModel(dim int) *LocalModel {
	return &LocalModel{
		Weights:     make([]float64, dim),
		PrevWeights: make([]float64, dim),
		NumFeatures: dim,
		Epochs:      10,
	}
}

// LoadWeights 加载全局模型权重。
func (m *LocalModel) LoadWeights(weights []float64) {
	m.PrevWeights = m.Weights
	m.Weights = weights
}

// ComputeWeightDiff 计算与全局模型的权重差分。
// 返回：new_weights - old_weights（即权重更新量）
func (m *LocalModel) ComputeWeightDiff(globalWeights []float64) []float64 {
	if len(m.Weights) != len(globalWeights) {
		return nil
	}
	diff := make([]float64, len(m.Weights))
	for i := range m.Weights {
		diff[i] = m.Weights[i] - globalWeights[i]
	}
	return diff
}

// Train 本地训练（简单 SGD ，不暴露数据）。
func (m *LocalModel) Train(samples []TrainingSample, epochs int) {
	// 简化实现：随机梯度下降
	// 实际应使用 IsolationForest 或 XGBoost 的本地版本
	lr := 0.01

	for epoch := 0; epoch < epochs; epoch++ {
		for _, s := range samples {
			// 简化的在线学习：梯度 = (prediction - label) * features
			prediction := m.predict(s.Features)
			error := float64(prediction) - float64(s.Label)

			// 更新权重
			for j := range m.Weights {
				grad := error * s.Features[j] / float64(len(s.Features))
				m.Weights[j] -= lr * grad
			}
		}
	}
}

// predict 简单线性预测。
func (m *LocalModel) predict(features []float64) int {
	sum := 0.0
	for i := 0; i < len(features) && i < len(m.Weights); i++ {
		sum += m.Weights[i] * features[i]
	}
	if sum > 0.5 {
		return 1
	}
	return 0
}

// Marshal 序列化全局模型（ gRPC 传输用）。
func (m *GlobalModel) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal 反序列化全局模型。
func (m *GlobalModel) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// MarshalUpdate 序列化客户端更新。
func (u *ClientUpdate) Marshal() ([]byte, error) {
	return json.Marshal(u)
}

// UnmarshalUpdate 反序列化客户端更新。
func (u *ClientUpdate) Unmarshal(data []byte) error {
	return json.Unmarshal(data, u)
}

// WeightDimension 返回权重维度（用于验证）。
func (m *GlobalModel) WeightDimension() int {
	return len(m.Weights)
}

// IsValid 检查模型是否有效。
func (m *GlobalModel) IsValid() bool {
	return m.Weights != nil && len(m.Weights) > 0
}

// NewGlobalModel 创建新的全局模型（初始化）。
func NewGlobalModel(dim int) *GlobalModel {
	return &GlobalModel{
		Weights:   make([]float64, dim),
		Version:   1,
		Round:     0,
		Clients:   0,
		UpdatedAt: time.Now(),
	}
}
