// Package crypto KMS 密钥加载接口（RSA 私钥/公钥）。
// 提供开发环境 PEM 文件加载 + 环境变量 PEM 加载。
// 生产环境实现由 cloud-security-architect 提供（如 AWS KMS / 阿里云 KMS）。
package crypto

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// 环境变量名常量（保持 main.go 与 kms.go 一致）
const (
	EnvJWTPublicKeyPEM  = "AISAAS_JWT_PUBLIC_KEY_PEM"
	EnvJWTPrivateKeyPEM = "AISAAS_JWT_PRIVATE_KEY_PEM"
)

// KMSKeyLoader KMS 密钥加载接口。
// 生产实现：从 KMS 服务加载 RSA 密钥（如 AWS KMS、阿里云 KMS）。
type KMSKeyLoader interface {
	LoadRSAPrivateKey(ctx context.Context, keyID string) (*rsa.PrivateKey, error)
	LoadRSAPublicKey(ctx context.Context, keyID string) (*rsa.PublicKey, error)
}

// FileKeyLoader 开发环境：从 PEM 文件加载 RSA 密钥。
// 生产环境禁止使用，应替换为 KMS 实现。
type FileKeyLoader struct {
	PrivateKeyPath string
	PublicKeyPath  string
}

// LoadRSAPrivateKey 从 PEM 文件加载 RSA 私钥。
func (l *FileKeyLoader) LoadRSAPrivateKey(_ context.Context, _ string) (*rsa.PrivateKey, error) {
	pemData, err := os.ReadFile(l.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key pem: %w", err)
	}
	return parseRSAPrivateKey(pemData)
}

// LoadRSAPublicKey 从 PEM 文件加载 RSA 公钥。
func (l *FileKeyLoader) LoadRSAPublicKey(_ context.Context, _ string) (*rsa.PublicKey, error) {
	pemData, err := os.ReadFile(l.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read public key pem: %w", err)
	}
	return parseRSAPublicKey(pemData)
}

// EnvKeyLoader 开发/测试环境：从环境变量读取 PEM 格式的 RSA 密钥。
type EnvKeyLoader struct {
	PrivateKeyEnv string // 环境变量名，默认 "AISAA_JWT_PRIVATE_KEY"
	PublicKeyEnv  string // 环境变量名，默认 "AISAA_JWT_PUBLIC_KEY"
}

// LoadRSAPrivateKey 从环境变量加载 RSA 私钥（PEM 格式）。
func (l *EnvKeyLoader) LoadRSAPrivateKey(_ context.Context, _ string) (*rsa.PrivateKey, error) {
	env := l.PrivateKeyEnv
	if env == "" {
		env = "AISAA_JWT_PRIVATE_KEY"
	}
	pemStr := os.Getenv(env)
	if pemStr == "" {
		return nil, fmt.Errorf("env %s not set", env)
	}
	return parseRSAPrivateKey([]byte(pemStr))
}

// LoadRSAPublicKey 从环境变量加载 RSA 公钥（PEM 格式）。
func (l *EnvKeyLoader) LoadRSAPublicKey(_ context.Context, _ string) (*rsa.PublicKey, error) {
	env := l.PublicKeyEnv
	if env == "" {
		env = "AISAA_JWT_PUBLIC_KEY"
	}
	pemStr := os.Getenv(env)
	if pemStr == "" {
		return nil, fmt.Errorf("env %s not set", env)
	}
	return parseRSAPublicKey([]byte(pemStr))
}

// LoadFromEnv 从环境变量加载 JWT 密钥（生产推荐）。
// pubEnv/privEnv 为环境变量名，值为 PEM 格式的密钥内容。
func LoadFromEnv(pubEnv, privEnv string) (*rsa.PublicKey, *rsa.PrivateKey, error) {
	loader := &EnvKeyLoader{PublicKeyEnv: pubEnv, PrivateKeyEnv: privEnv}
	ctx := context.Background()
	pubKey, err := loader.LoadRSAPublicKey(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("load public key: %w", err)
	}
	privKey, err := loader.LoadRSAPrivateKey(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("load private key: %w", err)
	}
	return pubKey, privKey, nil
}

// LoadFromFile 从 PEM 文件加载 JWT 密钥（开发环境）。
func LoadFromFile(pubPath, privPath string) (*rsa.PublicKey, *rsa.PrivateKey, error) {
	loader := &FileKeyLoader{PublicKeyPath: pubPath, PrivateKeyPath: privPath}
	ctx := context.Background()
	pubKey, err := loader.LoadRSAPublicKey(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("load public key: %w", err)
	}
	privKey, err := loader.LoadRSAPrivateKey(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("load private key: %w", err)
	}
	return pubKey, privKey, nil
}

// parseRSAPrivateKey 解析 PEM 格式的 RSA 私钥。
func parseRSAPrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	// 尝试 PKCS#1
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	// 尝试 PKCS#8
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA private key")
	}
	return rsaKey, nil
}

// parseRSAPublicKey 解析 PEM 格式的 RSA 公钥。
func parseRSAPublicKey(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	// 尝试 PKCS#1
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	// 尝试 PKIX
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX public key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaKey, nil
}