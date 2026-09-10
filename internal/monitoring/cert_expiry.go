// Package monitoring 提供 mTLS 证书过期检测与告警。
//
// 由 aisaas 启动期调用一次 + Prometheus scrape 周期调用 CheckCertExpiry。
// 当任意服务证书剩余有效期 < 7 天时，触发告警事件。
package monitoring

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	expiryAlertThreshold = 7 * 24 * time.Hour
	defaultCertDir       = "certs"
	expiryCheckTimeout   = 5 * time.Second
)

var defaultServices = []string{
	"ykt-aisaas",
	"xiaozhi-server-go",
	"ykt-admin",
}

// CertAlert 描述单个服务的证书过期告警。
type CertAlert struct {
	Service       string
	ExpiresAt     time.Time
	RemainingDays int
	Severity      string
}

// CheckCertExpiry 扫描 certDir 下所有服务的 .crt 文件，剩余有效期低于阈值即返回告警。
//
// 参数：
//   - certDir：证书目录（每个服务一个 <svc>.crt 文件）
//   - services：要扫描的服务列表（nil 时使用默认三服务）
//
// 返回：告警列表（按剩余天数升序）+ 严重错误（目录不存在等）。
func CheckCertExpiry(certDir string, services []string) ([]CertAlert, error) {
	if certDir == "" {
		certDir = defaultCertDir
	}
	if len(services) == 0 {
		services = defaultServices
	}

	alerts := make([]CertAlert, 0)
	now := time.Now()

	for _, svc := range services {
		certPath := filepath.Join(certDir, svc+".crt")
		notAfter, err := readCertNotAfter(certPath)
		if err != nil {
			return nil, fmt.Errorf("cert_expiry: %w", err)
		}
		remaining := notAfter.Sub(now)
		if remaining >= expiryAlertThreshold {
			continue
		}
		severity := "warning"
		if remaining < 24*time.Hour {
			severity = "critical"
		} else if remaining < 3*24*time.Hour {
			severity = "high"
		}
		remainingDays := int(remaining.Hours() / 24)
		if remaining < 0 && remaining > -24*time.Hour {
			remainingDays = 0
		} else if remaining <= -24*time.Hour {
			remainingDays = int(remaining.Hours() / 24)
		}
		alerts = append(alerts, CertAlert{
			Service:       svc,
			ExpiresAt:     notAfter,
			RemainingDays: remainingDays,
			Severity:      severity,
		})
	}

	return alerts, nil
}

// DaysUntilExpiry 返回单个服务证书的剩余天数（负数表示已过期）。
// 用于 Prometheus gauge `mtls_cert_expires_in_days{service="..."}`。
func DaysUntilExpiry(certPath string) (int, error) {
	notAfter, err := readCertNotAfter(certPath)
	if err != nil {
		return 0, err
	}
	remaining := time.Until(notAfter)
	return int(remaining.Hours() / 24), nil
}

func readCertNotAfter(certPath string) (time.Time, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("read %s: %w", certPath, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block in %s", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cert %s: %w", certPath, err)
	}
	return cert.NotAfter, nil
}
