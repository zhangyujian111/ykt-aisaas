// Package crypto 提供 API Key 生成/哈希与 AES 加解密（model_registry.apiKeyEnc）。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// KeyPrefix API Key 前缀。
const KeyPrefix = "sk-aisaas-"

// NewAPIKey 生成明文 API Key：sk-aisaas-{32 hex}。
func NewAPIKey() (plain string, err error) {
	b := make([]byte, 16)
	if _, err = io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return KeyPrefix + hex.EncodeToString(b), nil
}

// HashKey SHA-256 哈希（入库索引列）。
func HashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// KeyDisplay 返回 UI 展示用前缀（前 16 字符）。
func KeyDisplay(plain string) string {
	if len(plain) > 16 {
		return plain[:16]
	}
	return plain
}

// AES 加解密（key 必须 32 字节，AES-256-GCM）。

func Encrypt(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func Decrypt(cipherHex string, key []byte) ([]byte, error) {
	raw, err := hex.DecodeString(cipherHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
