// Package federated 联邦学习 - 协调器实现。
package federated

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// FederatedCoordinator 联邦学习协调器 - 管理全局模型和训练轮次。
//
// 职责：
//   1. 管理全局模型（初始化、版本控制）
//   2. 选择参与的客户端（ minClients 阈值）
//   3. 并行触发客户端训练
//   4. 聚合客户端更新（ FedAvg ）
//   5. 分发新的全局模型
//
// 客户端选择策略（简化版，实际应考虑）：
//   - 客户端数据量
//   - 网络质量
//   - 算力
//   - 隐私约束
//   - 历史参与率
type FederatedCoordinator struct {
	// 配置
	clients      []FederatedClient // 注册的客户端列表
	globalModel  *GlobalModel       // 当前全局模型
	roundTimeout time.Duration      // 单轮超时时间
	minClients   int                // 最少参与客户端数
	aggregator   Aggregator         // 聚合器（ FedAvg ）

	// 状态
	round        int32              // 当前轮次
	clientWeight map[string]float64 // 客户端权重（基于数据量）

	// 并发控制
	mu           sync.RWMutex       // 保护全局状态
	activeRounds sync.Map           // 活跃轮次

	// 日志
	logger *slog.Logger

	// gRPC 服务（可选）
	grpcServer interface {
		Start() error
		Stop()
	}
}

// CoordinatorConfig 协调器配置。
type CoordinatorConfig struct {
	// 模型配置
	ModelDimension int // 模型权重维度

	// 训练配置
	RoundTimeout time.Duration // 单轮超时（默认 5 分钟）
	MinClients   int           // 最少参与客户端数（默认 3 ）

	// 聚合配置
	Aggregator Aggregator // 聚合器（默认 FedAvgAggregator）

	// 差分隐私
	DPEnabled  bool    // 是否启用差分隐私
	DPEpsilon float64  // DP epsilon
}

// DefaultCoordinatorConfig 返回默认配置。
func DefaultCoordinatorConfig() *CoordinatorConfig {
	return &CoordinatorConfig{
		ModelDimension: 12,
		RoundTimeout:     5 * time.Minute,
		MinClients:       3,
		Aggregator:       NewFedAvgAggregator(),
		DPEnabled:        true,
		DPEpsilon:        1.0,
	}
}

// NewFederatedCoordinator 创建联邦学习协调器。
func NewFederatedCoordinator(cfg *CoordinatorConfig) *FederatedCoordinator {
	if cfg == nil {
		cfg = DefaultCoordinatorConfig()
	}
	if cfg.Aggregator == nil {
		cfg.Aggregator = NewFedAvgAggregator()
	}
	if cfg.RoundTimeout == 0 {
		cfg.RoundTimeout = 5 * time.Minute
	}
	if cfg.MinClients == 0 {
		cfg.MinClients = 3
	}

	coord := &FederatedCoordinator{
		clients:      make([]FederatedClient, 0),
		roundTimeout: cfg.RoundTimeout,
		minClients:   cfg.MinClients,
		aggregator:   cfg.Aggregator,
		clientWeight: make(map[string]float64),
		logger:       slog.Default(),
	}

	// 初始化全局模型
	coord.globalModel = NewGlobalModel(cfg.ModelDimension)

	return coord
}

// RegisterClient 注册客户端。
func (c *FederatedCoordinator) RegisterClient(client FederatedClient) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.clients = append(c.clients, client)
	c.logger.Info("client registered",
		"client", client.ID(),
		"region", client.Region(),
		"total_clients", len(c.clients))
}

// GetGlobalModel 获取当前全局模型。
func (c *FederatedCoordinator) GetGlobalModel() *GlobalModel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.globalModel
}

// GetRound 获取当前轮次。
func (c *FederatedCoordinator) GetRound() int {
	return int(atomic.LoadInt32(&c.round))
}

// StartRound 启动一轮联邦训练。
//
// 流程：
//   1. 增加轮次计数
//   2. 选择参与的客户端（最少 minClients ）
//   3. 并行发送全局模型给客户端训练
//   4. 等待所有客户端返回（带超时）
//   5. 验证客户端更新
//   6. 聚合更新（ FedAvg ）
//   7. 更新全局模型
//   8. 返回新的全局模型
func (c *FederatedCoordinator) StartRound(ctx context.Context) (*GlobalModel, error) {
	// 1. 原子增加轮次
	newRound := atomic.AddInt32(&c.round, 1)

	c.mu.Lock()
	c.globalModel.Round = int(newRound)
	globalModel := c.globalModel
	c.mu.Unlock()

	c.logger.Info("starting federated round",
		"round", newRound,
		"clients", len(c.clients),
		"min_required", c.minClients)

	// 2. 选择参与的客户端
	selected := c.selectClients(c.minClients)
	if len(selected) < c.minClients {
		c.logger.Error("insufficient clients",
			"selected", len(selected),
			"min_required", c.minClients)
		return nil, errors.New("insufficient clients for training round")
	}

	c.logger.Info("clients selected",
		"round", newRound,
		"selected_count", len(selected),
		"clients", getClientIDs(selected))

	// 3. 并行训练
	updateCtx, cancel := context.WithTimeout(ctx, c.roundTimeout)
	defer cancel()

	type result struct {
		client FederatedClient
		update *ClientUpdate
		err    error
	}

	resultCh := make(chan result, len(selected))
	var wg sync.WaitGroup

	for _, client := range selected {
		wg.Add(1)
		go func(cl FederatedClient) {
			defer wg.Done()

			update, err := cl.Train(updateCtx, globalModel)
			if err != nil {
				c.logger.Warn("client training failed",
					"client", cl.ID(),
					"region", cl.Region(),
					"err", err)
				resultCh <- result{client: cl, update: nil, err: err}
				return
			}

			// 验证更新有效性
			if err := ValidateClientUpdate(update, globalModel); err != nil {
				c.logger.Warn("invalid client update",
					"client", cl.ID(),
					"err", err)
				resultCh <- result{client: cl, update: nil, err: err}
				return
			}

			resultCh <- result{client: cl, update: update, err: nil}
		}(client)
	}

	// 等待所有客户端完成
	wg.Wait()
	close(resultCh)

	// 收集有效更新
	validUpdates := make([]*ClientUpdate, 0, len(selected))
	var failedClients []string
	for r := range resultCh {
		if r.err != nil {
			failedClients = append(failedClients, r.client.ID())
			continue
		}
		validUpdates = append(validUpdates, r.update)
	}

	if len(validUpdates) < c.minClients {
		c.logger.Error("insufficient valid updates",
			"valid", len(validUpdates),
			"min_required", c.minClients,
			"failed", failedClients)
		return nil, errors.New("insufficient valid updates")
	}

	c.logger.Info("received updates from clients",
		"round", newRound,
		"valid_updates", len(validUpdates),
		"failed_clients", failedClients)

	// 5. 聚合更新
	newGlobalModel, err := c.aggregator.Aggregate(globalModel, validUpdates)
	if err != nil {
		c.logger.Error("aggregation failed",
			"round", newRound,
			"err", err)
		return nil, err
	}

	// 6. 更新全局模型
	c.mu.Lock()
	c.globalModel = newGlobalModel
	c.mu.Unlock()

	c.logger.Info("round completed",
		"round", newRound,
		"new_version", newGlobalModel.Version,
		"participating_clients", len(validUpdates),
		"clients", getClientIDs(selected))

	return newGlobalModel, nil
}

// selectClients 选择参与的客户端。
//
// 当前实现：随机选择
// 实际应考虑：
//   - 客户端数据量（权重）
//   - 网络质量（延迟、带宽）
//   - 算力（ CPU/GPU ）
//   - 隐私约束（合规要求）
//   - 历史参与率
//   - 联邦公平性
func (c *FederatedCoordinator) selectClients(n int) []FederatedClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.clients) <= n {
		return c.clients
	}

	// 简化：随机选择
	// 实际应使用加权随机选择
	perm := rand.Perm(len(c.clients))
	selected := make([]FederatedClient, n)
	for i := 0; i < n; i++ {
		selected[i] = c.clients[perm[i]]
	}
	return selected
}

// StartGRPCServer 启动 gRPC 服务器（可选）。
// 用于生产环境中客户端通过 gRPC 连接协调器。
func (c *FederatedCoordinator) StartGRPCServer(address string) error {
	// 简化：gRPC 服务器应在单独进程中运行
	// 此处仅提供接口定义
	c.logger.Info("gRPC server not implemented, use external server")
	return nil
}

// GetClientStats 获取客户端统计信息。
func (c *FederatedCoordinator) GetClientStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"total_clients": len(c.clients),
		"min_clients":   c.minClients,
		"current_round": c.round,
		"model_version": c.globalModel.Version,
		"model_round":   c.globalModel.Round,
	}

	clientList := make([]map[string]interface{}, len(c.clients))
	for i, cl := range c.clients {
		clientList[i] = map[string]interface{}{
			"id":     cl.ID(),
			"region": cl.Region(),
			"samples": cl.NumSamples(),
		}
	}
	stats["clients"] = clientList

	return stats
}

// Stop 停止协调器。
func (c *FederatedCoordinator) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("stopping federated coordinator",
		"round", c.round,
		"model_version", c.globalModel.Version)

	// 清理资源
	if c.grpcServer != nil {
		c.grpcServer.Stop()
	}
}

// Helper functions

func getClientIDs(clients []FederatedClient) []string {
	ids := make([]string, len(clients))
	for i, c := range clients {
		ids[i] = c.ID()
	}
	return ids
}

// addVectors 向量加法。
func addVectors(a, b []float64) []float64 {
	if len(a) != len(b) {
		return nil
	}
	result := make([]float64, len(a))
	for i := range a {
		result[i] = a[i] + b[i]
	}
	return result
}

// FederatedServer gRPC 服务接口（用于生成 proto 文件）。
// 实际应使用 protobuf 定义：
/*
syntax = "proto3";

package federated;

service FederatedLearning {
  rpc RegisterClient(ClientInfo) returns (RegisterResponse);
  rpc TrainRound(TrainRequest) returns (TrainResponse);
  rpc GetModel(GetModelRequest) returns (Model);
  rpc ReportUpdate(ClientUpdate) returns (UpdateAck);
}

message ClientInfo {
  string client_id = 1;
  string region = 2;
  int32 num_samples = 3;
}

message RegisterResponse {
  bool success = 1;
  string message = 2;
}

message TrainRequest {
  Model global_model = 1;
  int32 round = 2;
}

message TrainResponse {
  ClientUpdate update = 1;
}

message GetModelRequest {}

message UpdateAck {
  bool success = 1;
  int32 new_version = 2;
}
*/
