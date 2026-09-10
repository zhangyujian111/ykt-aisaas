// Package middleware 提供 CRL（Certificate Revocation List）检查中间件。
//
// CRL 缓存策略：
//   - 启动期从 --crl-url 加载一次（同步，失败 → 中间件降级为 fail-open 但记录 error）
//   - 后台 goroutine 每 --crl-refresh-interval 重新拉取
//   - 内存 sync.Map 持有 serial → revocation_time 映射（O(1) 查询）
//
// 安全模型：
//   - CRL 加载失败 → CheckRevoked 返回 false（fail-open，由启动期 panic 或告警负责兜底）
//   - 该中间件假定 MTLSRequired 已先一步验证证书链
//   - 调用方必须将 CheckCRL(caCertPath, crlURL) 放在 MTLSRequired 之后
package middleware

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	revokedSerials sync.Map
	crlLoadCount   atomic.Int64
	crlHitCount    atomic.Int64
	crlLoadedAt    atomic.Pointer[time.Time]
)

// LoadCRL 从 crlURL 拉取 CRL（DER 或 PEM），解析后写入 revokedSerials 缓存。
//
// 支持格式：
//   - PEM（"-----BEGIN X509 CRL-----"）
//   - DER（裸二进制）
func LoadCRL(crlURL string) error {
	if crlURL == "" {
		return fmt.Errorf("crl: empty URL")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(crlURL)
	if err != nil {
		return fmt.Errorf("crl: fetch %s: %w", crlURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("crl: %s returned %d", crlURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("crl: read body: %w", err)
	}

	crl, err := parseCRL(body)
	if err != nil {
		return fmt.Errorf("crl: parse %s: %w", crlURL, err)
	}

	newMap := &sync.Map{}
	for _, revoked := range crl.RevokedCertificates {
		newMap.Store(revoked.SerialNumber.String(), revoked.RevocationTime)
	}

	revokedSerials.Range(func(k, _ any) bool {
		revokedSerials.Delete(k)
		return true
	})
	newMap.Range(func(k, v any) bool {
		revokedSerials.Store(k, v)
		return true
	})

	crlLoadCount.Add(1)
	now := time.Now()
	crlLoadedAt.Store(&now)
	log.Printf("crl: loaded %d revoked certs from %s", len(crl.RevokedCertificates), crlURL)
	return nil
}

// LoadCRLFromFile 从本地文件加载 CRL（PEM 或 DER），用于离线 / K8s ConfigMap 场景。
func LoadCRLFromFile(crlPath string) error {
	data, err := os.ReadFile(crlPath)
	if err != nil {
		return fmt.Errorf("crl: read %s: %w", crlPath, err)
	}
	crl, err := parseCRL(data)
	if err != nil {
		return fmt.Errorf("crl: parse %s: %w", crlPath, err)
	}
	for _, revoked := range crl.RevokedCertificates {
		revokedSerials.Store(revoked.SerialNumber.String(), revoked.RevocationTime)
	}
	crlLoadCount.Add(1)
	now := time.Now()
	crlLoadedAt.Store(&now)
	log.Printf("crl: loaded %d revoked certs from %s", len(crl.RevokedCertificates), crlPath)
	return nil
}

// CheckRevoked 查询证书序列号是否在 CRL 中。
func CheckRevoked(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if t, ok := revokedSerials.Load(cert.SerialNumber.String()); ok {
		crlHitCount.Add(1)
		if revokedAt, ok := t.(time.Time); ok {
			return time.Since(revokedAt) >= 0
		}
		return true
	}
	return false
}

// CRLStats 返回 CRL 状态（监控用）。
func CRLStats() (loadedAt time.Time, loadCount, hitCount int64, revokedCount int) {
	if p := crlLoadedAt.Load(); p != nil {
		loadedAt = *p
	}
	count := 0
	revokedSerials.Range(func(_, _ any) bool {
		count++
		return true
	})
	return loadedAt, crlLoadCount.Load(), crlHitCount.Load(), count
}

// StartCRLRefresher 启动后台 goroutine 按 interval 周期刷新 CRL。
// 调用方负责在程序退出时通过返回的 stop chan 关闭。
func StartCRLRefresher(crlURL string, interval time.Duration) chan<- struct{} {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := LoadCRL(crlURL); err != nil {
					log.Printf("crl: refresh failed: %v", err)
				}
			}
		}
	}()
	return stop
}

// CRLCheck 返回 gin 中间件，对通过 MTLSRequired 校验的客户端证书再查 CRL。
//
// 使用方式（在路由链中放在 MTLSRequired 之后）：
//
//	r.Use(middleware.MTLSRequired(caPath))
//	r.Use(middleware.CRLCheck())
//
// 行为：
//   - 无 TLS 连接 → 不处理（交给 MTLSRequired 拒绝）
//   - 无客户端证书 → 不处理（交给 MTLSRequired 拒绝）
//   - 证书被撤销 → 401 certificate_revoked
//   - 证书正常 → c.Next()
func CRLCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		tlsConn := c.Request.TLS
		if tlsConn == nil || len(tlsConn.PeerCertificates) == 0 {
			c.Next()
			return
		}
		if CheckRevoked(tlsConn.PeerCertificates[0]) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "certificate revoked",
				"code":  "MTLS_CERT_REVOKED",
			})
			return
		}
		c.Next()
	}
}

func parseCRL(data []byte) (*x509.RevocationList, error) {
	if block, _ := pem.Decode(data); block != nil && block.Type == "X509 CRL" {
		return x509.ParseRevocationList(block.Bytes)
	}
	return x509.ParseRevocationList(data)
}
