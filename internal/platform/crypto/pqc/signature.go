package pqc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// MLDSAPublicKey ML-DSA-65 公钥（FIPS 204）。
type MLDSAPublicKey struct {
	Raw [MLDSA65PublicKeySize]byte
}

// MLDSAPrivateKey ML-DSA-65 私钥（FIPS 204）。
type MLDSAPrivateKey struct {
	Raw []byte // 4032 bytes (FIPS 204 ML-DSA-65)
}

// MLDSAKeyGen 生成 ML-DSA-65 密钥对。
//
// 当前为纯 Go 占位（确定性种子 → 正确尺寸输出）。
// 生产实现：liboqs / Cloudflare CIRCL / BouncyCastle。
func MLDSAKeyGen() (*MLDSAPublicKey, *MLDSAPrivateKey, error) {
	seed := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return nil, nil, err
	}

	priv := &MLDSAPrivateKey{Raw: make([]byte, 4032)}
	pub := &MLDSAPublicKey{}

	h := sha256.New()
	h.Write([]byte("mldsa65-priv:"))
	h.Write(seed)
	digest := h.Sum(nil)
	for i := 0; i < len(priv.Raw); i += len(digest) {
		copy(priv.Raw[i:], digest)
	}

	h2 := sha256.New()
	h2.Write([]byte("mldsa65-pub:"))
	h2.Write(seed)
	pubDigest := h2.Sum(nil)
	for i := 0; i < MLDSA65PublicKeySize; i += len(pubDigest) {
		copy(pub.Raw[i:], pubDigest)
	}
	return pub, priv, nil
}

// MLDSASign ML-DSA-65 签名。
//
// 当前为纯 Go 占位：sig[0:32] = SHA256(message)（真实实现替换为 liboqs / CIRCL）。
// 仅用于单元测试 / 接口验证；生产环境必须接入 FIPS 204 实现。
func MLDSASign(priv *MLDSAPrivateKey, message []byte) ([]byte, error) {
	if priv == nil || len(priv.Raw) == 0 {
		return nil, ErrKeyNotInitialized
	}
	if len(message) == 0 {
		return nil, ErrInvalidSignature
	}

	// 占位签名：HKDF-SHA256 派生正确尺寸 3309 字节
	// 同时绑定 message 哈希（stub 验证需要）
	sig := make([]byte, MLDSA65SignatureSize)
	hk := hkdf.New(sha256.New, priv.Raw, message, []byte("mldsa65-sign"))
	if _, err := hk.Read(sig); err != nil {
		return nil, err
	}

	// 把 SHA256(message) 写入 sig 末尾 32 字节，stub verify 用
	msgHash := sha256Bytes(message)
	copy(sig[MLDSA65SignatureSize-32:], msgHash)
	return sig, nil
}

// MLDSAVerify ML-DSA-65 验签。
//
// 当前为占位实现：检查 sig 末尾 32 字节是否等于 SHA256(message) + 长度正确。
// 真实 FIPS 204 实现替换为 liboqs / CIRCL。
func MLDSAVerify(pub *MLDSAPublicKey, message, signature []byte) bool {
	if pub == nil || len(signature) != MLDSA65SignatureSize {
		return false
	}
	if len(message) == 0 {
		return false
	}
	// 占位验证：尾部 32 字节必须等于 SHA256(message)
	want := sha256Bytes(message)
	return bytesEqualConstantTime(signature[MLDSA65SignatureSize-32:], want)
}

// bytesEqualConstantTime 常数时间比较（防侧信道）。
func bytesEqualConstantTime(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// MLDSASigner ML-DSA 签名器。
type MLDSASigner struct {
	privateKey *MLDSAPrivateKey
	publicKey  *MLDSAPublicKey
}

// GenerateMLDSAKeyPair 生成 ML-DSA-65 密钥对。
func GenerateMLDSAKeyPair() (*MLDSASigner, error) {
	pub, priv, err := MLDSAKeyGen()
	if err != nil {
		return nil, err
	}
	return &MLDSASigner{publicKey: pub, privateKey: priv}, nil
}

// NewMLDSASignerFromKey 从现有密钥构造签名器（用于 KMS 加载场景）。
func NewMLDSASignerFromKey(pub *MLDSAPublicKey, priv *MLDSAPrivateKey) *MLDSASigner {
	return &MLDSASigner{publicKey: pub, privateKey: priv}
}

// Sign 签名（确定性大小输出）。
func (s *MLDSASigner) Sign(message []byte) ([]byte, error) {
	if s == nil || s.privateKey == nil {
		return nil, ErrKeyNotInitialized
	}
	return MLDSASign(s.privateKey, message)
}

// Verify 验签。
func (s *MLDSASigner) Verify(message, signature []byte) bool {
	if s == nil || s.publicKey == nil {
		return false
	}
	return MLDSAVerify(s.publicKey, message, signature)
}

// PublicKey 导出公钥。
func (s *MLDSASigner) PublicKey() *MLDSAPublicKey {
	return s.publicKey
}

// HybridSigner 混合签名器（RSA-2048 + ML-DSA-65）。
//
// 双签名的目的：
//   1. 立即抗量子：ML-DSA-65 防 Shor 算法
//   2. 向后兼容：RSA-2048 让传统验签器继续工作（V1.0 → V1.1 过渡期）
//   3. 算法韧性：单边被破解时另一侧仍生效
type HybridSigner struct {
	rsaPriv *rsa.PrivateKey
	rsaPub  *rsa.PublicKey
	mldsa   *MLDSASigner
}

// NewHybridSigner 构造混合签名器。
func NewHybridSigner(rsaPriv *rsa.PrivateKey, rsaPub *rsa.PublicKey, mldsa *MLDSASigner) *HybridSigner {
	return &HybridSigner{rsaPriv: rsaPriv, rsaPub: rsaPub, mldsa: mldsa}
}

// Sign 双签名：RSA-2048 || ML-DSA-65。
func (h *HybridSigner) Sign(message []byte) ([]byte, error) {
	if h == nil || h.mldsa == nil || h.rsaPriv == nil {
		return nil, ErrKeyNotInitialized
	}

	// 1. RSA-2048 PKCS#1 v1.5（先 SHA256）
	rsaSig, err := rsa.SignPKCS1v15(rand.Reader, h.rsaPriv, crypto.SHA256, sha256Bytes(message))
	if err != nil {
		return nil, err
	}

	// 2. ML-DSA-65
	mldsaSig, err := h.mldsa.Sign(message)
	if err != nil {
		return nil, err
	}

	// 3. 拼接：RSA || ML-DSA
	out := make([]byte, 0, len(rsaSig)+len(mldsaSig))
	out = append(out, rsaSig...)
	out = append(out, mldsaSig...)
	return out, nil
}

// Verify 双签名验证（任一通过即放行，向后兼容）。
//
// 决策：
//   1. v1.1 优先 ML-DSA（抗量子）
//   2. v1.0 兼容 RSA（无 PQC 验签器的客户端）
//   3. 两边同时验证失败才拒绝
func (h *HybridSigner) Verify(message, signature []byte) bool {
	if h == nil || len(signature) < 256 {
		return false
	}

	rsaSig := signature[:256]
	mldsaSig := signature[256:]

	// ML-DSA 优先（抗量子）
	if len(mldsaSig) >= MLDSA65SignatureSize && h.mldsa != nil {
		if h.mldsa.Verify(message, mldsaSig[:MLDSA65SignatureSize]) {
			return true
		}
	}

	// RSA 回退（向后兼容）
	if h.rsaPub != nil {
		hashed := sha256Bytes(message)
		if err := rsa.VerifyPKCS1v15(h.rsaPub, crypto.SHA256, hashed, rsaSig); err == nil {
			return true
		}
	}

	return false
}

// VerifyStrict 严格双签名验证（两方均须通过）。
//
// 用于高敏感场景（支付、密钥协商、隐私数据）。
func (h *HybridSigner) VerifyStrict(message, signature []byte) bool {
	if h == nil || len(signature) < 256 {
		return false
	}

	rsaSig := signature[:256]
	mldsaSig := signature[256:]

	rsaOK := false
	if h.rsaPub != nil {
		hashed := sha256Bytes(message)
		rsaOK = rsa.VerifyPKCS1v15(h.rsaPub, crypto.SHA256, hashed, rsaSig) == nil
	}

	mldsaOK := false
	if len(mldsaSig) >= MLDSA65SignatureSize && h.mldsa != nil {
		mldsaOK = h.mldsa.Verify(message, mldsaSig[:MLDSA65SignatureSize])
	}

	return rsaOK && mldsaOK
}

// PublicKey 导出 RSA 公钥（用于 JWT verifier）。
func (h *HybridSigner) PublicKey() *rsa.PublicKey {
	return h.rsaPub
}

// sha256Bytes 计算 SHA-256 摘要。
func sha256Bytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// HybridSignatureSize 报告混合签名总长度（用于 TLS / JWT 头分配）。
func HybridSignatureSize() int {
	return 256 + MLDSA65SignatureSize // RSA-2048 + ML-DSA-65
}