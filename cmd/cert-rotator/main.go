// Package main 启动 cert-rotator 守护进程：
// 定期扫描本地 mTLS 服务证书的过期时间，
// 当剩余有效期低于阈值时调用 gen-ca.sh 重新签发，
// 并通过 webhook 通知运维。
//
// 适用：传统部署路径（VM / 裸机）作为 K8s cert-manager 的 fallback。
// K8s 环境下请直接使用 k8s/cert-manager/* 资源对象。
package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"
)

var rotationsTotal atomic.Int64

func main() {
	certFile := flag.String("cert", "certs/ykt-aisaas.crt", "证书路径")
	keyFile := flag.String("key", "certs/ykt-aisaas.key", "私钥路径")
	caFile := flag.String("ca", "certs/ca.crt", "CA 证书")
	daysBefore := flag.Int("days-before", 7, "提前多少天续期")
	checkInterval := flag.Duration("check-interval", 24*time.Hour, "检查周期")
	webhookURL := flag.String("webhook", os.Getenv("CERT_ROTATOR_WEBHOOK"), "Webhook 通知 URL（可选）")
	genScript := flag.String("gen-script", "scripts/certs/gen-ca.sh", "证书重新签发脚本")
	certDir := flag.String("cert-dir", "certs", "证书目录（rotateCert 使用）")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("cert-rotator started: cert=%s key=%s ca=%s days-before=%d interval=%s",
		*certFile, *keyFile, *caFile, *daysBefore, *checkInterval)

	ticker := time.NewTicker(*checkInterval)
	defer ticker.Stop()

	checkAndRotate(*certFile, *keyFile, *caFile, *genScript, *certDir, *daysBefore, *webhookURL)

	for range ticker.C {
		checkAndRotate(*certFile, *keyFile, *caFile, *genScript, *certDir, *daysBefore, *webhookURL)
	}
}

func checkAndRotate(certFile, keyFile, caFile, genScript, certDir string, daysBefore int, webhookURL string) {
	notBefore, notAfter, err := readCertValidity(certFile)
	if err != nil {
		log.Printf("read cert validity failed: %v", err)
		return
	}
	remaining := time.Until(notAfter)
	log.Printf("cert %s valid: %s ~ %s (remaining %s)",
		certFile, notBefore.Format(time.RFC3339), notAfter.Format(time.RFC3339), remaining.Round(time.Second))

	if !needsRotation(notAfter, daysBefore) {
		return
	}
	log.Println("证书即将过期，自动续期...")
	if err := rotateCert(genScript, certDir, certFile, keyFile, caFile); err != nil {
		log.Printf("续期失败: %v", err)
		notifyWebhook(webhookURL, "error", fmt.Sprintf("cert rotation failed: %v", err))
		return
	}
	n := rotationsTotal.Add(1)
	log.Printf("续期成功（累计 %d 次）", n)
	notifyWebhook(webhookURL, "success",
		fmt.Sprintf("cert %s rotated, valid until %s", certFile, time.Now().Add(2160*time.Hour).Format(time.RFC3339)))
}

func needsRotation(notAfter time.Time, daysBefore int) bool {
	remaining := time.Until(notAfter)
	return remaining < time.Duration(daysBefore)*24*time.Hour
}

func readCertValidity(certFile string) (notBefore, notAfter time.Time, err error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("read cert %s: %w", certFile, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("no PEM block in %s", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse cert %s: %w", certFile, err)
	}
	return cert.NotBefore, cert.NotAfter, nil
}

func rotateCert(genScript, certDir, certFile, keyFile, caFile string) error {
	if _, err := os.Stat(genScript); err != nil {
		return fmt.Errorf("gen-script not found %s: %w", genScript, err)
	}
	absDir, err := filepath.Abs(certDir)
	if err != nil {
		return fmt.Errorf("resolve cert dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("mkdir cert dir: %w", err)
	}
	cmd := exec.Command("bash", genScript, absDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w (stderr=%s)", genScript, err, stderr.String())
	}
	if _, err := os.Stat(certFile); err != nil {
		return fmt.Errorf("post-rotation cert missing %s: %w", certFile, err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return fmt.Errorf("post-rotation key missing %s: %w", keyFile, err)
	}
	if _, err := os.Stat(caFile); err != nil {
		return fmt.Errorf("post-rotation ca missing %s: %w", caFile, err)
	}
	return nil
}

func notifyWebhook(webhookURL, status, message string) {
	if webhookURL == "" {
		log.Printf("webhook not configured, skip (status=%s)", status)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"service":    "cert-rotator",
		"status":     status,
		"message":    message,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"rotations":  rotationsTotal.Load(),
	})
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("webhook post failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("webhook returned %d", resp.StatusCode)
		return
	}
	log.Printf("webhook notified: status=%s", status)
}
