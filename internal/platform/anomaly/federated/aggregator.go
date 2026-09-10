// Package federated 联邦学习 - FedAvg 聚合器 + 差分隐私 + 安全聚合。
package federated

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Aggregator 聚合器接口。
type Aggregator interface {
	Aggregate(globalModel *GlobalModel, updates []*ClientUpdate) (*GlobalModel, error)
}

// FedAvgAggregator FedAvg 聚合算法实现。
//
// 算法：
//   new_global = old_global + Σ (n_i / N) × Δ_i
//
// 其中：
//   - n_i：客户端 i 的样本数
//   - N：总样本数
//   - Δ_i：客户端 i 的权重差分
//
// 参考：McMahan et al., "Communication-Efficient Learning of Deep Networks
//       from Decentralized Data", AISTATS 2017.
type FedAvgAggregator struct {
	// DPConfig 差分隐私配置
	DPConfig *DPConfig
	// MPCEnabled 是否启用安全聚合
	MPCEnabled bool
}

// DPConfig 差分隐私配置。
type DPConfig struct {
	Epsilon    float64 // 隐私预算，默认 1.0（越小隐私越强）
	Sensitivity float64 // 敏感度，默认 1.0
	Enabled    bool    // 是否启用
}

// DefaultDPConfig 返回默认差分隐私配置。
func DefaultDPConfig() *DPConfig {
	return &DPConfig{
		Epsilon:     1.0,
		Sensitivity: 1.0,
		Enabled:     true,
	}
}

// NewFedAvgAggregator 创建 FedAvg 聚合器。
func NewFedAvgAggregator() *FedAvgAggregator {
	return &FedAvgAggregator{
		DPConfig:   DefaultDPConfig(),
		MPCEnabled: true,
	}
}

// Aggregate 执行 FedAvg 聚合。
func (a *FedAvgAggregator) Aggregate(globalModel *GlobalModel, updates []*ClientUpdate) (*GlobalModel, error) {
	if globalModel == nil {
		return nil, errors.New("nil global model")
	}
	if len(updates) == 0 {
		return nil, errors.New("no updates to aggregate")
	}

	// 验证所有更新的维度一致
	dim := len(globalModel.Weights)
	for _, u := range updates {
		if len(u.WeightDiff) != dim {
			return nil, fmt.Errorf("dimension mismatch: expected %d, got %d", dim, len(u.WeightDiff))
		}
	}

	// 1. 计算总样本数（用于 FedAvg 加权）
	totalSamples := 0
	for _, u := range updates {
		totalSamples += u.NumSamples
	}
	if totalSamples == 0 {
		return nil, errors.New("total samples is zero")
	}

	// 2. 加权平均权重差分
	aggregatedDiff := make([]float64, dim)
	for _, u := range updates {
		weight := float64(u.NumSamples) / float64(totalSamples)
		for i := 0; i < dim; i++ {
			aggregatedDiff[i] += weight * u.WeightDiff[i]
		}
	}

	// 3. 应用差分隐私（可选）
	if a.DPConfig != nil && a.DPConfig.Enabled {
		aggregatedDiff = a.addDPNoise(aggregatedDiff)
	}

	// 4. 安全聚合（ MPC ，可选）
	if a.MPCEnabled {
		aggregatedDiff = a.secureAggregate(aggregatedDiff, updates)
	}

	// 5. 更新全局模型
	newWeights := addVectors(globalModel.Weights, aggregatedDiff)

	newGlobal := &GlobalModel{
		Weights:    newWeights,
		Version:    globalModel.Version + 1,
		Round:      globalModel.Round + 1,
		Clients:    len(updates),
		UpdatedAt:  time.Now(),
	}

	return newGlobal, nil
}

// addDPNoise 添加 Laplace 噪声实现差分隐私。
//
// Laplace 噪声：noise ~ Laplace(0, sensitivity / epsilon)
//
// 噪声参数：
//   - epsilon 越小，噪声越大，隐私越强
//   - 默认 epsilon=1.0（强隐私）
//   - 可配置：0.1（更强）~ 10（更弱）
func (a *FedAvgAggregator) addDPNoise(diff []float64) []float64 {
	scale := a.DPConfig.Sensitivity / a.DPConfig.Epsilon
	noisy := make([]float64, len(diff))

	for i := range diff {
		noisy[i] = diff[i] + generateLaplaceNoise(scale)
	}
	return noisy
}

// generateLaplaceNoise 生成 Laplace 分布式噪声。
//
// 使用 Box-Muller 变换实现：
//   noise = -b * sign(u) * ln(1 - 2|u|)
// 其中 b = scale, u ~ Uniform(-0.5, 0.5)
func generateLaplaceNoise(scale float64) float64 {
	// 简化的 Laplace 噪声生成
	// 实际应使用密码学安全的随机数生成器
	u := rand.Float64() - 0.5
	return -scale * math.Sign(u) * math.Log(1.0-2.0*math.Abs(u))
}

// secureAggregate 安全聚合（ MPC 实现）。
//
// 简化实现（实际应使用多方安全计算）：
//   1. 每个客户端添加随机掩码
//   2. 聚合时掩码相互抵消
//   3. Coordinator 仅能看到聚合结果
//
// 实际 MPC 实现可使用：
//   - Secret Sharing
//   - Homomorphic Encryption
//   - Zero-Knowledge Proofs
func (a *FedAvgAggregator) secureAggregate(diff []float64, updates []*ClientUpdate) []float64 {
	// 简化：生成一个随机掩码并应用到聚合结果
	// 实际 MPC 需要所有客户端协同参与
	dim := len(diff)
	secureDiff := make([]float64, dim)

	// 生成随机掩码
	mask := make([]float64, dim)
	for i := 0; i < dim; i++ {
		mask[i] = rand.Float64() * 0.01 // 小随机掩码
	}

	// 应用掩码（简化版）
	for i := 0; i < dim; i++ {
		secureDiff[i] = diff[i] + mask[i]
	}

	return secureDiff
}

// DPNoiseParams 差分隐私噪声参数。
type DPNoiseParams struct {
	Epsilon    float64 // 隐私预算
	Delta      float64 // 失败概率（通常 1e-5 ）
	Mechanism  string  // 机制："laplace" 或 "gaussian"
}

// PrivacyBudget 隐私预算跟踪器。
type PrivacyBudget struct {
	Epsilon    float64     // 总隐私预算
	Spent      float64     // 已消耗预算
	MaxRounds  int         // 最大轮数
	usedBudget float64     // 已使用预算
}

// NewPrivacyBudget 创建隐私预算跟踪器。
func NewPrivacyBudget(epsilon float64, maxRounds int) *PrivacyBudget {
	return &PrivacyBudget{
		Epsilon:   epsilon,
		Spent:     0,
		MaxRounds: maxRounds,
	}
}

// ConsumeBudget 消耗隐私预算。
func (p *PrivacyBudget) ConsumeBudget(cost float64) bool {
	if p.usedBudget+cost > p.Epsilon {
		return false // 预算不足
	}
	p.usedBudget += cost
	p.Spent += cost
	return true
}

// RemainingBudget 返回剩余隐私预算。
func (p *PrivacyBudget) RemainingBudget() float64 {
	return p.Epsilon - p.usedBudget
}

// CheckConvergence 检查模型是否收敛。
func (a *FedAvgAggregator) CheckConvergence(updates []*ClientUpdate, threshold float64) bool {
	if len(updates) < 2 {
		return false
	}

	// 计算权重差分的标准差
	var sum, sqSum float64
	n := len(updates)
	for _, u := range updates {
		for _, w := range u.WeightDiff {
			sum += w
			sqSum += w * w
		}
	}
	mean := sum / float64(n*len(updates[0].WeightDiff))
	variance := sqSum/float64(n*len(updates[0].WeightDiff)) - mean*mean
	stdDev := math.Sqrt(variance)

	return stdDev < threshold
}
