// Package pqc 提供后量子密码（PQC）混合方案。
//
// 本包实现 NIST FIPS 203/204/205 标准的混合封装：
//   - 密钥封装：ML-KEM-768（FIPS 203，前称 Kyber）
//   - 数字签名：ML-DSA-65（FIPS 204，前称 Dilithium）
//   - 哈希签名：SLH-DSA（FIPS 205，前称 SPHINCS+）
//
// 为避免 CGO / liboqs 编译依赖，并便于在测试环境快速落地，
// 后量子原语以纯 Go 实现核心尺寸约束 + X25519 真实 ECDH 组合。
// 生产环境可通过替换 Backend 接口（circl / liboqs binding）无缝升级。
package pqc

import (
	"errors"
)

// 业务级 sentinel 错误。
var (
	// ErrAlgorithmUnknown 不支持的 PQC 算法。
	ErrAlgorithmUnknown = errors.New("pqc: unknown algorithm")

	// ErrInvalidCiphertext 密文长度异常或格式错误。
	ErrInvalidCiphertext = errors.New("pqc: invalid ciphertext")

	// ErrInvalidSignature 签名长度异常或校验失败。
	ErrInvalidSignature = errors.New("pqc: invalid signature")

	// ErrKeyNotInitialized 密钥未初始化。
	ErrKeyNotInitialized = errors.New("pqc: key not initialized")

	// ErrFeatureDisabled PQC 特性开关未开启。
	ErrFeatureDisabled = errors.New("pqc: feature disabled (set PQC_ENABLED=true)")
)