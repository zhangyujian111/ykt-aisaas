// Package federated 联邦学习 - 客户端实现。
package federated

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"
)

// FederatedClient 联邦学习客户端接口。
// 每个 Region 部署一个客户端，持有本地数据并参与联邦训练。
type FederatedClient interface {
	// ID 返回客户端唯一标识。
	ID() string
	// Region 返回客户端所在区域。
	Region() string
	// Train 在本地数据上训练并返回权重更新。
	Train(ctx context.Context, globalModel *GlobalModel) (*ClientUpdate, error)
	// LoadLocalData 加载本地训练数据（数据不出域）。
	LoadLocalData() ([]TrainingSample, error)
	// NumSamples 返回本地样本数。
	NumSamples() int
}

// LocalClient 本地客户端实现 - 部署到每个 Region 。
//
// 职责：
//   1. 持有本地训练数据（永不离境）
//   2. 接收全局模型权重
//   3. 本地训练（不暴露数据）
//   4. 返回权重差分（不是数据）
//
// 3 个 Region ：
//   - us-east-1（美国东部）
//   - eu-west-1（欧洲西部）
//   - ap-southeast-1（东南亚）
type LocalClient struct {
	id       string               // 客户端 ID
	region   string               // Region
	model    *LocalModel          // 本地模型
	dataset  []TrainingSample     // 本地数据集（不传输）
	dpConfig *DPConfig            // 差分隐私配置
	logger   *slog.Logger         // 日志
}

// LocalClientConfig 本地客户端配置。
type LocalClientConfig struct {
	ID         string      // 客户端 ID
	Region     string      // Region
	DPSEnabled bool        // 是否启用差分隐私
	DPEpsilon  float64     // DP epsilon
	Epochs     int         // 本地训练轮数
	BatchSize  int         // 批次大小
}

// DefaultLocalClientConfig 返回默认配置。
func DefaultLocalClientConfig(id, region string) *LocalClientConfig {
	return &LocalClientConfig{
		ID:         id,
		Region:     region,
		DPSEnabled: true,
		DPEpsilon:  1.0,
		Epochs:     10,
		BatchSize:  32,
	}
}

// NewLocalClient 创建本地客户端。
func NewLocalClient(cfg *LocalClientConfig) *LocalClient {
	return &LocalClient{
		id:     cfg.ID,
		region: cfg.Region,
		model:  NewLocalModel(12), // 12 维特征
		dpConfig: &DPConfig{
			Epsilon:     cfg.DPEpsilon,
			Sensitivity: 1.0,
			Enabled:     cfg.DPSEnabled,
		},
		logger: slog.Default(),
	}
}

// ID 返回客户端 ID 。
func (c *LocalClient) ID() string { return c.id }

// Region 返回 Region 。
func (c *LocalClient) Region() string { return c.region }

// NumSamples 返回本地样本数。
func (c *LocalClient) NumSamples() int { return len(c.dataset) }

// LoadLocalData 加载本地训练数据。
//
// 重要：此方法仅在客户端本地调用，数据永不离开客户端。
// 实际应从本地数据库或文件系统加载。
func (c *LocalClient) LoadLocalData() ([]TrainingSample, error) {
	// 模拟：从本地数据源加载
	// 实际应实现：
	//   - 从 MySQL/PostgreSQL 加载历史数据
	//   - 从 Redis 加载实时特征
	//   - 从本地文件加载缓存数据

	// 简化：返回模拟数据
	// 实际部署时应替换为真实数据加载逻辑
	if len(c.dataset) > 0 {
		return c.dataset, nil
	}

	// 模拟加载 1000 个样本
	samples := make([]TrainingSample, 0, 1000)
	for i := 0; i < 1000; i++ {
		sample := TrainingSample{
			TenantID:  c.id,
			Features:  generateMockFeatures(),
			Label:     rand.Intn(2), // 0=normal, 1=anomaly
			Timestamp: time.Now().Add(-time.Duration(i) * time.Hour),
		}
		samples = append(samples, sample)
	}

	c.dataset = samples
	return samples, nil
}

// Train 本地训练并返回权重更新。
//
// 流程：
//   1. 加载本地数据（本地，不传输）
//   2. 加载全局模型权重
//   3. 本地训练（不暴露数据）
//   4. 计算权重差分
//   5. 添加差分隐私噪声（可选）
//   6. 返回权重差分（不是数据）
func (c *LocalClient) Train(ctx context.Context, globalModel *GlobalModel) (*ClientUpdate, error) {
	if globalModel == nil {
		return nil, errors.New("nil global model")
	}
	if !globalModel.IsValid() {
		return nil, errors.New("invalid global model")
	}

	// 1. 加载本地数据（本地，不传输）
	if len(c.dataset) == 0 {
		data, err := c.LoadLocalData()
		if err != nil {
			c.logger.Error("load local data failed", "client", c.id, "err", err)
			return nil, err
		}
		c.dataset = data
	}

	c.logger.Info("starting local training",
		"client", c.id,
		"region", c.region,
		"samples", len(c.dataset),
		"round", globalModel.Round)

	// 2. 加载全局模型权重
	c.model.LoadWeights(globalModel.Weights)

	// 3. 本地训练（不暴露数据）
	c.model.Train(c.dataset, c.model.Epochs)

	// 4. 计算权重差分
	weightDiff := c.model.ComputeWeightDiff(globalModel.Weights)
	if weightDiff == nil {
		return nil, errors.New("compute weight diff failed")
	}

	// 5. 创建更新
	update := &ClientUpdate{
		ClientID:   c.id,
		Region:     c.region,
		WeightDiff: weightDiff,
		NumSamples: len(c.dataset),
		Timestamp:  time.Now(),
	}

	// 6. 添加差分隐私噪声（可选）
	if c.dpConfig != nil && c.dpConfig.Enabled {
		c.AddDPNoise(update, c.dpConfig.Sensitivity, c.dpConfig.Epsilon)
	}

	c.logger.Info("local training completed",
		"client", c.id,
		"region", c.region,
		"num_samples", update.NumSamples,
		"weight_diff_norm", computeNorm(weightDiff))

	return update, nil
}

// AddDPNoise 添加差分隐私噪声。
//
// 使用 Laplace 机制：
//   noise ~ Laplace(0, sensitivity / epsilon)
//
// 参数：
//   - sensitivity：敏感度（权重变化范围）
//   - epsilon：隐私预算（越小隐私越强）
func (c *LocalClient) AddDPNoise(update *ClientUpdate, sensitivity, epsilon float64) {
	scale := sensitivity / epsilon
	for i := range update.WeightDiff {
		update.WeightDiff[i] += generateLaplaceNoise(scale)
	}
	c.logger.Info("added DP noise",
		"client", c.id,
		"sensitivity", sensitivity,
		"epsilon", epsilon,
		"scale", scale)
}

// GenerateMockFeatures 生成模拟特征向量。
func generateMockFeatures() []float64 {
	return []float64{
		rand.Float64() * 10000,         // quota_per_minute
		rand.Float64() * 10000,         // tokens_per_call
		rand.Float64() * 1000,          // calls_per_minute
		rand.Float64(),                  // error_rate
		rand.Float64() * 50000,         // latency_p50
		rand.Float64() * 50000,          // latency_p95
		rand.Float64() * 100,            // unique_devices
		rand.Float64() * 1000,          // cost_per_minute
		rand.Float64(),                  // streaming_ratio
		rand.Float64() * 10,            // concurrent_sessions
		rand.Float64(),                  // token_efficiency
		rand.Float64(),                  // cache_hit_rate
	}
}

// computeNorm 计算向量 L2 范数。
func computeNorm(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	return sum
}

// RegionClients 预定义的 3 个 Region 客户端工厂函数。
var RegionClients = []struct {
	ID     string
	Region string
}{
	{ID: "client-us-east-1", Region: "us-east-1"},
	{ID: "client-eu-west-1", Region: "eu-west-1"},
	{ID: "client-ap-southeast-1", Region: "ap-southeast-1"},
}

// NewRegionClients 创建所有 Region 的客户端。
func NewRegionClients() []FederatedClient {
	clients := make([]FederatedClient, len(RegionClients))
	for i, rc := range RegionClients {
		clients[i] = NewLocalClient(&LocalClientConfig{
			ID:         rc.ID,
			Region:     rc.Region,
			DPSEnabled: true,
			DPEpsilon:  1.0,
			Epochs:     10,
		})
	}
	return clients
}

// ValidateClientUpdate 验证客户端更新的有效性。
func ValidateClientUpdate(update *ClientUpdate, globalModel *GlobalModel) error {
	if update == nil {
		return errors.New("nil update")
	}
	if update.ClientID == "" {
		return errors.New("empty client ID")
	}
	if len(update.WeightDiff) == 0 {
		return errors.New("empty weight diff")
	}
	if len(update.WeightDiff) != len(globalModel.Weights) {
		return errors.New("weight dimension mismatch")
	}
	if update.NumSamples <= 0 {
		return errors.New("invalid num samples")
	}
	return nil
}
