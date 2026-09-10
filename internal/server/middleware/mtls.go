// Package middleware 提供 mTLS 鉴权中间件与 TLS 配置构造器。
//
// 设计目标：
//   - /internal/* API 强制 mTLS（防御纵深：TLS 握手 + CN 校验 + 既有 X-Internal-Token）
//   - 配置驱动：CA / 证书路径全部来自命令行/配置文件
//   - 失败闭合（fail-closed）：CA 加载失败 → 拒绝所有请求
//   - TLS 1.2+：MinVersion: tls.VersionTLS12
package middleware

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// ClientCNKey gin.Context 中存储客户端证书 CN 的键。
const ClientCNKey = "client_cn"

// LoadCAPool 从 PEM 文件加载 CA 证书池。
func LoadCAPool(caCertPath string) (*x509.CertPool, error) {
	if caCertPath == "" {
		return nil, errors.New("mtls: CA cert path is empty")
	}
	data, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("mtls: read CA cert %q: %w", caCertPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("mtls: failed to parse CA cert PEM in %q", caCertPath)
	}
	return pool, nil
}

// MTLSRequired 强制客户端必须提供由指定 CA 签发的证书，且证书 SAN 必须在允许列表中。
//
// 调用前置条件：
//   - HTTP Server 必须配置 TLSConfig.ClientAuth=RequireAndVerifyClientCert（TLS 层验证）
//   - 监听端口必须为 HTTPS（r.TLS != nil）
//
// 行为：
//   - 缺证书 / 证书无效 → 拒绝（401）
//   - 非 HTTPS 请求 → 拒绝（401，告知必须使用 HTTPS）
//   - SAN 不在允许列表 → 拒绝（403）
//   - 验证通过 → 将客户端 CN 写入 gin.Context，便于下游 handler 审计/限流
//
// 注意：本中间件为失败闭合（fail-closed）。CA 加载失败时启动期直接 panic，
// 避免静默放行。
func MTLSRequired(caCertPath string, allowedSANs []string) gin.HandlerFunc {
	pool, err := LoadCAPool(caCertPath)
	if err != nil {
		// 启动期错误比运行期错误代价更低。配置错误不应让服务继续运行。
		panic(fmt.Sprintf("mtls: failed to load CA at startup: %v", err))
	}

	verifyOpts := x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	return func(c *gin.Context) {
		tlsConn := c.Request.TLS
		if tlsConn == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "mTLS required (HTTPS only)",
				"code":  "MTLS_HTTPS_REQUIRED",
			})
			return
		}

		peers := tlsConn.PeerCertificates
		if len(peers) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "client certificate required",
				"code":  "MTLS_NO_CLIENT_CERT",
			})
			return
		}

		// 验证客户端证书链是否由我们的 CA 签发
		if _, err := peers[0].Verify(verifyOpts); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "invalid client certificate",
				"code":    "MTLS_INVALID_CLIENT_CERT",
				"details": err.Error(),
			})
			return
		}

		// 验证 SAN 是否在允许列表中
		if len(allowedSANs) > 0 {
			allowedSet := stringSet(allowedSANs)
			if !validateSAN(peers[0], allowedSet) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "SAN not allowed",
					"code":  "MTLS_SAN_NOT_ALLOWED",
				})
				return
			}
		}

		// 将客户端 CN 写入 gin.Context（供下游 handler 审计）
		c.Set(ClientCNKey, peers[0].Subject.CommonName)
		c.Next()
	}
}

// GetServerTLSConfig 构造要求客户端证书的 *tls.Config。
//
// 用于 ykt-aisaas :8443 HTTPS+mTLS 监听器。客户端证书必须由 caCertFile 签发。
// MinVersion=TLS 1.2（避免 SSLv3/TLS 1.0/1.1 已知漏洞）。
// 显式选择 TLS 1.2 cipher suites（TLS 1.3 由 Go 运行时固定）。
func GetServerTLSConfig(certFile, keyFile, caCertFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("mtls: server cert and key files are required")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: load server keypair: %w", err)
	}
	caPool, err := LoadCAPool(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: load CA pool: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS12,
		// TLS 1.2 cipher suites（ECDHE + AEAD，禁止 CBC/RC4/3DES）
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	}, nil
}

// GetClientTLSConfig 构造客户端 mTLS *tls.Config。
//
// 用于 xiaozhi-server-go / ykt-admin 调用 ykt-aisaas :8443 时加载。
//   - Certificates: 客户端证书（xiaozhi-server-go.crt / ykt-admin.crt）
//   - RootCAs: 服务端证书链校验 CA（ca.crt）
//   - ServerName: SNI/hostname 校验的目标名
func GetClientTLSConfig(certFile, keyFile, caCertFile, serverName string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("mtls: client cert and key files are required")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: load client keypair: %w", err)
	}
	caPool, err := LoadCAPool(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: load CA pool: %w", err)
	}

	if serverName == "" {
		serverName = "ykt-aisaas"
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
		ServerName:   serverName,
	}, nil
}

// validateSAN 验证证书的 SAN（DNS 或 IP）是否在允许集合中。
func validateSAN(cert *x509.Certificate, allowed map[string]bool) bool {
	for _, dns := range cert.DNSNames {
		if allowed[dns] {
			return true
		}
	}
	for _, ip := range cert.IPAddresses {
		if allowed[ip.String()] {
			return true
		}
	}
	return false
}

// stringSet 将字符串切片转换为 map，用于快速查找。
func stringSet(strs []string) map[string]bool {
	set := make(map[string]bool, len(strs))
	for _, s := range strs {
		set[s] = true
	}
	return set
}
