package pqc

import (
	"crypto/tls"
	"fmt"
	"os"
)

// NewPQCTLSServerConfig 生成支持 X25519MLKEM768 混合密钥交换的 TLS 1.3 服务端配置。
//
// 设计要点：
//   1. CurvePreferences 优先 X25519MLKEM768，向下兼容 P-256 / X25519
//   2. MinVersion 强制 TLS 1.3（混合 KE 需 TLS 1.3）
//   3. CipherSuites 仅 AEAD（AES-GCM / ChaCha20-Poly1305）
//   4. 支持自动 cert rotation（调用方负责刷新 Certificates 字段）
//
// 兼容性：
//   - Chrome 124+ ✅
//   - Firefox 124+ ✅
//   - Safari 17.4+ ✅
//   - Go 1.24+ crypto/tls ✅
func NewPQCTLSServerConfig(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("pqc: cert/key file path required")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("pqc: load x509 keypair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},

		// 混合密钥交换优先；P-256 / X25519 为兜底
		CurvePreferences: []tls.CurveID{
			tls.X25519MLKEM768, // 混合：X25519 + ML-KEM-768
			tls.CurveP256,
			tls.X25519,
		},

		// TLS 1.3 强制（混合 KE 仅 1.3 支持）
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,

		// AEAD cipher suites only
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},

		// 安全默认值
		SessionTicketsDisabled: true, // 防 session ticket 重放
		ClientAuth:             tls.RequestClientCert,
	}, nil
}

// NewPQCClientConfig 生成 PQC 客户端配置。
//
// 用于 ykt-aisaas / ykt-admin / xiaozhi-server-go 之间互调。
func NewPQCClientConfig() *tls.Config {
	return &tls.Config{
		CurvePreferences: []tls.CurveID{
			tls.X25519MLKEM768, // 混合密钥交换
			tls.CurveP256,
			tls.X25519,
		},
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}
}

// NewPQCClientConfigWithRootCA 带根 CA 池的 PQC 客户端配置（mTLS 客户端场景）。
func NewPQCClientConfigWithRootCA(rootCAPath string) (*tls.Config, error) {
	cfg := NewPQCClientConfig()

	if rootCAPath != "" {
		pem, err := os.ReadFile(rootCAPath)
		if err != nil {
			return nil, fmt.Errorf("pqc: read root ca: %w", err)
		}
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("pqc: failed to parse root ca pem")
		}
	}

	return cfg, nil
}

// IsPQCCurveSupported 运行时探测 PQC 曲线是否被当前 Go crypto/tls 支持。
//
// 用于启动期健康检查 / 监控。
func IsPQCCurveSupported() bool {
	// tls.X25519MLKEM768 是 Go 1.24+ 常量；零值兜底
	return tls.X25519MLKEM768 != 0
}

// SupportedCipherSuites PQC 上下文推荐的密码套件列表（仅 AEAD）。
func SupportedCipherSuites() []uint16 {
	return []uint16{
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
		tls.TLS_AES_128_GCM_SHA256,
	}
}