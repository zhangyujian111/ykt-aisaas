package pqc

import (
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// PQCAlgorithm 后量子算法标识。
//
// 命名严格遵循 NIST FIPS 203/204/205 公报原文。
type PQCAlgorithm string

const (
	// AlgorithmMLKEM768 模格密钥封装（FIPS 203，前称 Kyber）。
	AlgorithmMLKEM768 PQCAlgorithm = "ML-KEM-768"

	// AlgorithmMLDSA65 模格数字签名（FIPS 204，前称 Dilithium）。
	AlgorithmMLDSA65 PQCAlgorithm = "ML-DSA-65"

	// AlgorithmSLHDSA 基于哈希的无状态签名（FIPS 205，前称 SPHINCS+）。
	// 用于 ML-DSA 后备方案 / 长期归档签名（V8 阶段接入）。
	AlgorithmSLHDSA PQCAlgorithm = "SLH-DSA"

	// AlgorithmHybridX25519MLKEM768 混合密钥交换（X25519 + ML-KEM-768）。
	AlgorithmHybridX25519MLKEM768 PQCAlgorithm = "X25519MLKEM768"
)

// NIST FIPS 203/204 公钥尺寸常量（字节）。
const (
	MLKEM768PublicKeySize  = mlkem.EncapsulationKeySize768 // 1184
	MLKEM768CiphertextSize = mlkem.CiphertextSize768       // 1088
	MLDSA65PublicKeySize   = 1952
	MLDSA65SignatureSize   = 3309
	X25519PublicKeySize    = 32
	X25519PrivateKeySize   = 32
	HybridCiphertextSize   = X25519PublicKeySize + MLKEM768CiphertextSize // 1120
)

// MLKEMKey ML-KEM-768 密钥对（封装 stdlib 两种密钥类型）。
type MLKEMKey struct {
	EncapsulationKey *mlkem.EncapsulationKey768 // 公钥（用于封装）
	DecapsulationKey *mlkem.DecapsulationKey768 // 私钥（用于解封装）
}

// MLKEMEncapsulate ML-KEM-768 封装：生成共享密钥 + 密文。
//
// 内部调用 crypto/mlkem（FIPS 203 标准实现）。
func MLKEMEncapsulate(pub [MLKEM768PublicKeySize]byte) (sharedSecret, ciphertext []byte, err error) {
	ek, err := mlkem.NewEncapsulationKey768(pub[:])
	if err != nil {
		return nil, nil, err
	}
	sharedSecret, ciphertext = ek.Encapsulate()
	return sharedSecret, ciphertext, nil
}

// MLKEMDecapsulate ML-KEM-768 解封装。
//
// 内部调用 crypto/mlkem（FIPS 203 标准实现）。
func MLKEMDecapsulate(secretKey []byte, ciphertext []byte) (sharedSecret []byte, err error) {
	if len(ciphertext) != MLKEM768CiphertextSize {
		return nil, ErrInvalidCiphertext
	}
	dk, err := mlkem.NewDecapsulationKey768(secretKey)
	if err != nil {
		return nil, err
	}
	sharedSecret, err = dk.Decapsulate(ciphertext)
	if err != nil {
		return nil, err
	}
	return sharedSecret, nil
}

// HybridKEM 混合密钥封装机制（X25519 + ML-KEM-768）。
//
// 使用语义：
//   - 发送方（Encapsulate）：持有 peer 公钥（X25519 pub + ML-KEM encapsulation key）
//   - 接收方（Decapsulate）：持有 自己的私钥（X25519 priv + ML-KEM decapsulation key）
//
// 设计目标：
//  1. 抵御 "Harvest Now, Decrypt Later" 攻击
//  2. 向后兼容：任一方支持 X25519 即可完成密钥协商
//  3. TLS 1.3 / Noise / 未来 PQ-only 阶段可逐步升级
type HybridKEM struct {
	// —— 发送方（Encapsulate）需要的 peer 公钥 ——
	peerX25519Pub *ecdh.PublicKey
	peerMLKEMEK   *mlkem.EncapsulationKey768

	// —— 接收方（Decapsulate）需要的自己的私钥 ——
	selfX25519Priv *ecdh.PrivateKey
	selfMLKEMDK    *mlkem.DecapsulationKey768
}

// NewHybridKEMForEncapsulation 构造用于封装（发送）的 HybridKEM。
//
// peerX25519Pub：对方 X25519 公钥（32 bytes）
// peerMLKEMEncapsulationKey：对方 ML-KEM-768 封装公钥（1184 bytes）
func NewHybridKEMForEncapsulation(peerX25519Pub [X25519PublicKeySize]byte, peerMLKEMEncapsulationKey []byte) (*HybridKEM, error) {
	curve := ecdh.X25519()
	pk, err := curve.NewPublicKey(peerX25519Pub[:])
	if err != nil {
		return nil, err
	}
	ek, err := mlkem.NewEncapsulationKey768(peerMLKEMEncapsulationKey)
	if err != nil {
		return nil, err
	}
	return &HybridKEM{
		peerX25519Pub: pk,
		peerMLKEMEK:   ek,
	}, nil
}

// NewHybridKEMForDecapsulation 构造用于解封装（接收）的 HybridKEM。
//
// selfX25519Priv：自己的 X25519 私钥（32 bytes）
// selfMLKEMDecapsulationKey：自己的 ML-KEM-768 私钥（64 bytes "d || z" seed）
func NewHybridKEMForDecapsulation(selfX25519Priv [X25519PrivateKeySize]byte, selfMLKEMDecapsulationKey []byte) (*HybridKEM, error) {
	curve := ecdh.X25519()
	sk, err := curve.NewPrivateKey(selfX25519Priv[:])
	if err != nil {
		return nil, err
	}
	dk, err := mlkem.NewDecapsulationKey768(selfMLKEMDecapsulationKey)
	if err != nil {
		return nil, err
	}
	return &HybridKEM{
		selfX25519Priv: sk,
		selfMLKEMDK:    dk,
	}, nil
}

// GenerateHybridKEMPair 生成新的混合 KEM 密钥对（用于首次部署 / 轮换）。
//
// 返回：
//   - sender：用于发送（封装）
//   - receiver：用于接收（解封装）
//   - x25519Pub：X25519 公钥（32 bytes）
//   - mlkemPub：ML-KEM-768 公钥（1184 bytes）
//   - x25519Priv：X25519 私钥（32 bytes）
//   - mlkemPriv：ML-KEM-768 私钥种子（64 bytes "d || z"）
//   - err：任何错误
func GenerateHybridKEMPair() (sender, receiver *HybridKEM, x25519Pub, x25519Priv [X25519PublicKeySize]byte, mlkemPub []byte, mlkemPriv []byte, err error) {
	curve := ecdh.X25519()
	xpriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, x25519Pub, x25519Priv, nil, nil, err
	}
	copy(x25519Priv[:], xpriv.Bytes())
	copy(x25519Pub[:], xpriv.PublicKey().Bytes())

	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, nil, x25519Pub, x25519Priv, nil, nil, err
	}
	mlkemPriv = dk.Bytes() // 64 bytes seed
	mlkemPub = dk.EncapsulationKey().Bytes()

	sender, err = NewHybridKEMForEncapsulation(x25519Pub, mlkemPub)
	if err != nil {
		return nil, nil, x25519Pub, x25519Priv, nil, nil, err
	}
	receiver, err = NewHybridKEMForDecapsulation(x25519Priv, mlkemPriv)
	if err != nil {
		return nil, nil, x25519Pub, x25519Priv, nil, nil, err
	}
	return sender, receiver, x25519Pub, x25519Priv, mlkemPub, mlkemPriv, nil
}

// Encapsulate 生成共享密钥 + 混合密文（发送方调用）。
//
// 返回：
//   - sharedKey: 32 字节 HKDF-SHA256 派生的共享密钥
//   - ciphertext: [32 bytes ephemeral X25519 pub] || [1088 bytes ML-KEM ct]
//   - err: 任何错误
func (h *HybridKEM) Encapsulate() (sharedKey, ciphertext []byte, err error) {
	if h == nil || h.peerX25519Pub == nil || h.peerMLKEMEK == nil {
		return nil, nil, ErrKeyNotInitialized
	}

	// 1. ML-KEM 封装（FIPS 203 真随机）
	mlkemShared, mlkemCipher := h.peerMLKEMEK.Encapsulate()

	// 2. X25519 ECDH（每次封装的临时密钥对，与对方长期 pub 计算）
	curve := ecdh.X25519()
	ephemeralPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	ephemeralPub := ephemeralPriv.PublicKey().Bytes()
	x25519Shared, err := ephemeralPriv.ECDH(h.peerX25519Pub)
	if err != nil {
		return nil, nil, err
	}

	// 3. HKDF 组合（secret = ML-KEM, salt = X25519, info = "ykt-pqc-hybrid"）
	hk := hkdf.New(sha256.New, mlkemShared, x25519Shared, []byte("ykt-pqc-hybrid"))
	sharedKey = make([]byte, 32)
	if _, err = hk.Read(sharedKey); err != nil {
		return nil, nil, err
	}

	// 4. 拼接密文：ephemeral pub || ML-KEM ct
	ciphertext = make([]byte, 0, HybridCiphertextSize)
	ciphertext = append(ciphertext, ephemeralPub...)
	ciphertext = append(ciphertext, mlkemCipher...)

	return sharedKey, ciphertext, nil
}

// Decapsulate 解封装混合密文 → 恢复共享密钥（接收方调用）。
func (h *HybridKEM) Decapsulate(ciphertext []byte) (sharedKey []byte, err error) {
	if h == nil || h.selfX25519Priv == nil || h.selfMLKEMDK == nil {
		return nil, ErrKeyNotInitialized
	}
	if len(ciphertext) != HybridCiphertextSize {
		return nil, ErrInvalidCiphertext
	}

	// 1. 分离临时 X25519 pub 与 ML-KEM ct
	ephemeralPub := ciphertext[:X25519PublicKeySize]
	mlkemCipher := ciphertext[X25519PublicKeySize:]

	// 2. ML-KEM 解封装
	mlkemShared, err := h.selfMLKEMDK.Decapsulate(mlkemCipher)
	if err != nil {
		return nil, err
	}

	// 3. X25519 ECDH（用我方长期私钥 + 对方临时公钥）
	remotePub, err := h.selfX25519Priv.Curve().NewPublicKey(ephemeralPub)
	if err != nil {
		return nil, err
	}
	x25519Shared, err := h.selfX25519Priv.ECDH(remotePub)
	if err != nil {
		return nil, err
	}

	// 4. HKDF 组合（与 Encapsulate 对称）
	hk := hkdf.New(sha256.New, mlkemShared, x25519Shared, []byte("ykt-pqc-hybrid"))
	sharedKey = make([]byte, 32)
	if _, err = hk.Read(sharedKey); err != nil {
		return nil, err
	}

	return sharedKey, nil
}

// MLKEMKeyPairSize 报告 ML-KEM 密钥对序列化长度（用于存储估算）。
func MLKEMKeyPairSize() int {
	return MLKEM768PublicKeySize + mlkem.SeedSize
}

// backendVersion PQC 后端版本（用于遥测）。
var backendVersion = "ykt-pqc-1.0.0-stdlib-fips203"

// BackendVersion 返回当前后端版本字符串。
func BackendVersion() string { return backendVersion }

// ---- 保留向后兼容别名（spec 公开 API） ----

// NewHybridKEMFromSeed 从 32 字节种子派生完整 HybridKEM（用于测试）。
//
// 生成：X25519 密钥对 + ML-KEM 密钥对，构造"自封闭"实例（self pub = X25519.PublicKey()）。
// 这种实例只能与自身互通，主要用于单元测试。
func NewHybridKEMFromSeed(seed []byte) (*HybridKEM, error) {
	curve := ecdh.X25519()
	x25519Priv, err := curve.NewPrivateKey(seedOrHash(seed))
	if err != nil {
		return nil, err
	}

	mkemSeed := expandSeed(seed, mlkem.SeedSize)
	dk, err := mlkem.NewDecapsulationKey768(mkemSeed)
	if err != nil {
		return nil, err
	}

	return &HybridKEM{
		peerX25519Pub: x25519Priv.PublicKey(),
		peerMLKEMEK:   dk.EncapsulationKey(),
		selfX25519Priv: x25519Priv,
		selfMLKEMDK:    dk,
	}, nil
}

// X25519PublicKeyBytes 导出 self X25519 公钥字节。
func (h *HybridKEM) X25519PublicKeyBytes() []byte {
	if h == nil {
		return nil
	}
	if h.selfX25519Priv != nil {
		return h.selfX25519Priv.PublicKey().Bytes()
	}
	if h.peerX25519Pub != nil {
		return h.peerX25519Pub.Bytes()
	}
	return nil
}

// MLKEMEncapsulationKeyBytes 导出 ML-KEM-768 公钥字节。
func (h *HybridKEM) MLKEMEncapsulationKeyBytes() []byte {
	if h == nil {
		return nil
	}
	if h.selfMLKEMDK != nil {
		return h.selfMLKEMDK.EncapsulationKey().Bytes()
	}
	if h.peerMLKEMEK != nil {
		return h.peerMLKEMEK.Bytes()
	}
	return nil
}

// MLKEMDecapsulationKeyBytes 导出 ML-KEM-768 私钥种子（64 bytes "d || z"）。
// 生产中必须保密。
func (h *HybridKEM) MLKEMDecapsulationKeyBytes() []byte {
	if h == nil || h.selfMLKEMDK == nil {
		return nil
	}
	return h.selfMLKEMDK.Bytes()
}

// ---- helpers ----

// seedOrHash 把任意长度输入规整为 32 字节种子。
func seedOrHash(seed []byte) []byte {
	if len(seed) == 32 {
		return seed
	}
	h := sha256.Sum256(seed)
	return h[:]
}

// expandSeed 把任意长度输入扩展为 n 字节（HKDF 风格）。
func expandSeed(seed []byte, n int) []byte {
	if len(seed) == n {
		return seed
	}
	out := make([]byte, 0, n)
	buf := seed
	for len(out) < n {
		h := sha256.New()
		h.Write(buf)
		buf = h.Sum(nil)
		out = append(out, buf...)
	}
	out = out[:n]
	if len(seed) > 0 {
		for i := 0; i < n && i < len(seed); i++ {
			out[i] ^= seed[i]
		}
	}
	return out
}

// 编译期断言：io / rand / sha256 已使用
var (
	_ = io.Discard
	_ = rand.Reader
	_ = sha256.New
)