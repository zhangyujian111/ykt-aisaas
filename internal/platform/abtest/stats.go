package abtest

import (
	"context"
	"math"
	"sort"
)

// =============================================================================
// 统计分析结果
// =============================================================================

// StatisticalResult 变体统计分析结果。
type StatisticalResult struct {
	Variant         string  `json:"variant"`
	SampleSize      int     `json:"sampleSize"`
	Mean            float64 `json:"mean"`
	StdDev          float64 `json:"stdDev"`
	ConversionRate  float64 `json:"conversionRate"`
	LiftPercent     float64 `json:"liftPercent"`    // 相对 control 的提升百分比
	PValue          float64 `json:"pValue"`
	ConfidenceLevel float64 `json:"confidenceLevel"` // 0.95 / 0.99
	IsSignificant   bool    `json:"isSignificant"`
	Winner          bool    `json:"winner"`
}

// ExperimentAnalysis 完整实验分析结果。
type ExperimentAnalysis struct {
	ExperimentID      int64               `json:"experimentId"`
	ExperimentName    string              `json:"experimentName"`
	PrimaryMetric     string              `json:"primaryMetric"`
	TotalSampleSize   int                 `json:"totalSampleSize"`
	SignificanceLevel float64             `json:"significanceLevel"`
	Results           []*StatisticalResult `json:"results"`
	Recommendation    string              `json:"recommendation"` // winner / continue / stop
}

// =============================================================================
// 统计分析
// =============================================================================

// AnalyzeExperiment 分析实验结果。
//
// 流程：
//  1. 加载实验变体
//  2. 查询每个变体的事件数据
//  3. 计算转化率、均值、标准差
//  4. 与 control 进行 Z 检验
//  5. 生成决策建议
func (s *ABTestService) AnalyzeExperiment(ctx context.Context, experimentID int64) (*ExperimentAnalysis, error) {
	// 1. 加载实验
	exp, err := s.loadExperimentWithVariants(ctx, experimentID)
	if err != nil {
		return nil, err
	}

	// 2. 找到 control
	var control *Variant
	for _, v := range exp.Variants {
		if v.IsControl {
			control = v
			break
		}
	}
	if control == nil {
		return nil, ErrNoControlVariant
	}

	// 3. 获取 control 事件
	controlEvents, err := s.fetchEvents(ctx, experimentID, control.ID)
	if err != nil {
		return nil, err
	}

	analysis := &ExperimentAnalysis{
		ExperimentID:      experimentID,
		ExperimentName:    exp.Name,
		PrimaryMetric:     exp.PrimaryMetric,
		SignificanceLevel: exp.SignificanceLevel,
		Results:           make([]*StatisticalResult, 0, len(exp.Variants)),
	}

	// 4. 计算每个变体的指标
	for _, v := range exp.Variants {
		events, err := s.fetchEvents(ctx, experimentID, v.ID)
		if err != nil {
			return nil, err
		}

		result := computeVariantResult(v, events, controlEvents, exp.SignificanceLevel)
		analysis.Results = append(analysis.Results, result)
		analysis.TotalSampleSize += result.SampleSize
	}

	// 5. 生成决策建议
	analysis.Recommendation = s.generateRecommendation(analysis.Results)

	return analysis, nil
}

// computeVariantResult 计算单个变体的统计结果。
func computeVariantResult(v *Variant, events, controlEvents []*Event, sigLevel float64) *StatisticalResult {
	n := len(events)
	result := &StatisticalResult{
		Variant:    v.Name,
		SampleSize: n,
	}

	if n == 0 {
		result.PValue = 1.0
		result.ConfidenceLevel = 0.0
		return result
	}

	// 转化率（metric_value > 0 视为转化）
	conversions := 0
	var values []float64
	for _, e := range events {
		values = append(values, e.MetricValue)
		if e.MetricValue > 0 {
			conversions++
		}
	}
	result.ConversionRate = float64(conversions) / float64(n)
	result.Mean = computeMean(values)
	result.StdDev = computeStdDev(values, result.Mean)

	// 与 control 对比
	if v.IsControl {
		result.LiftPercent = 0
		result.PValue = 1.0
		result.ConfidenceLevel = 0.0
		result.IsSignificant = false
		result.Winner = false
	} else {
		result.PValue = zTestProportions(controlEvents, events)
		result.ConfidenceLevel = 1.0 - result.PValue

		controlRate := computeConversionRate(controlEvents)
		if controlRate > 0 {
			result.LiftPercent = (result.ConversionRate - controlRate) / controlRate * 100
		}

		result.IsSignificant = result.PValue < sigLevel
		result.Winner = result.IsSignificant && result.LiftPercent > 0
	}

	return result
}

// generateRecommendation 生成实验决策建议。
func (s *ABTestService) generateRecommendation(results []*StatisticalResult) string {
	hasWinner := false
	for _, r := range results {
		if r.Winner {
			hasWinner = true
			break
		}
	}

	if hasWinner {
		return "winner"
	}

	// 检查是否有显著性但提升为负的（treatment 劣于 control）
	hasSignificantNegative := false
	for _, r := range results {
		if r.IsSignificant && r.LiftPercent < 0 {
			hasSignificantNegative = true
			break
		}
	}
	if hasSignificantNegative {
		return "stop"
	}

	return "continue"
}

// =============================================================================
// 统计计算
// =============================================================================

// computeConversionRate 计算事件转化率。
func computeConversionRate(events []*Event) float64 {
	if len(events) == 0 {
		return 0
	}
	conversions := 0
	for _, e := range events {
		if e.MetricValue > 0 {
			conversions++
		}
	}
	return float64(conversions) / float64(len(events))
}

// computeRate 计算事件转化率（向后兼容别名）。
func computeRate(events []*Event) float64 {
	return computeConversionRate(events)
}

// computeMean 计算均值。
func computeMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// computeStdDev 计算标准差。
func computeStdDev(values []float64, mean float64) float64 {
	n := len(values)
	if n <= 1 {
		return 0
	}
	var sumSqDiff float64
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	return math.Sqrt(sumSqDiff / float64(n-1))
}

// =============================================================================
// Z 检验（双样本比例检验）
// =============================================================================

// zTestProportions 双样本比例 Z 检验。
//
// H0: p1 = p2（两组无差异）
// H1: p1 ≠ p2（双尾）
//
// 返回双尾 p-value。
func zTestProportions(controlEvents, treatmentEvents []*Event) float64 {
	n1 := float64(len(controlEvents))
	n2 := float64(len(treatmentEvents))
	if n1 == 0 || n2 == 0 {
		return 1.0
	}

	p1 := computeConversionRate(controlEvents)
	p2 := computeConversionRate(treatmentEvents)

	p := (p1*n1 + p2*n2) / (n1 + n2)
	if p <= 0 || p >= 1 {
		return 1.0
	}

	se := math.Sqrt(p * (1 - p) * (1/n1 + 1/n2))
	if se == 0 {
		return 1.0
	}

	z := (p2 - p1) / se
	// 双尾 p-value
	pValue := 2 * (1 - normalCDF(math.Abs(z)))
	return pValue
}

// zTest 两样本比例检验（向后兼容别名）。
func (s *ABTestService) zTest(controlEvents, treatmentEvents []*Event) float64 {
	return zTestProportions(controlEvents, treatmentEvents)
}

// normalCDF 标准正态分布累积分布函数。
// 使用误差函数 erf 近似：Φ(x) = 0.5 * (1 + erf(x / √2))
func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

// =============================================================================
// 样本量计算
// =============================================================================

// RequiredSampleSize 计算所需最小样本量。
//
// 公式：n = 16 * p * (1-p) / MDE²
// 其中 p 为基线转化率，MDE 为最小可检测效应。
//
// 示例：
//
//	baselineRate = 0.10, mde = 0.02 → n = 16 * 0.1 * 0.9 / 0.0004 = 36000
func RequiredSampleSize(baselineRate, minDetectableEffect float64) int {
	if minDetectableEffect <= 0 {
		return 0
	}
	n := 16 * baselineRate * (1 - baselineRate) / (minDetectableEffect * minDetectableEffect)
	return int(math.Ceil(n))
}

// =============================================================================
// 置信区间
// =============================================================================

// ConfidenceInterval 计算比例置信区间（Wald 方法）。
//
// 返回 [lower, upper]。
func ConfidenceInterval(rate float64, sampleSize int, zScore float64) (float64, float64) {
	if sampleSize <= 0 {
		return 0, 0
	}
	se := math.Sqrt(rate * (1 - rate) / float64(sampleSize))
	margin := zScore * se
	return rate - margin, rate + margin
}

// ZScore 返回对应置信水平的 z-score。
// 95% → 1.96, 99% → 2.576
func ZScore(confidenceLevel float64) float64 {
	if confidenceLevel <= 0 || confidenceLevel >= 1 {
		return 1.96 // 默认 95%
	}

	// 二分查找 z-score
	alpha := 1 - confidenceLevel
	target := 1 - alpha/2

	lo, hi := 0.0, 10.0
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		cdf := normalCDF(mid)
		if cdf < target {
			lo = mid
		} else {
			hi = mid
		}
	}

	return (lo + hi) / 2
}

// =============================================================================
// 结果排序
// =============================================================================

// SortResultsByLift 按提升百分比降序排列分析结果。
func SortResultsByLift(results []*StatisticalResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].LiftPercent > results[j].LiftPercent
	})
}