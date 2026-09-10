package pqc

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// TestHybridKEM 验证 GenerateHybridKEMPair → Encapsulate → Decapsulate 完整链路。
func TestHybridKEM(t *testing.T) {
	sender, receiver, x25519Pub, _, mlkemPub, _, err := GenerateHybridKEMPair()
	if err != nil {
		t.Fatalf("generate pair: %v", err)
	}
	if len(x25519Pub) != X25519PublicKeySize {
		t.Fatalf("x25519 pub size = %d, want %d", len(x25519Pub), X25519PublicKeySize)
	}
	if len(mlkemPub) != MLKEM768PublicKeySize {
		t.Fatalf("mlkem pub size = %d, want %d", len(mlkemPub), MLKEM768PublicKeySize)
	}

	shared, ciphertext, err := sender.Encapsulate()
	if err != nil {
		t.Fatalf("encapsulate: %v", err)
	}
	if len(ciphertext) != HybridCiphertextSize {
		t.Fatalf("hybrid ct size = %d, want %d", len(ciphertext), HybridCiphertextSize)
	}
	if len(shared) != 32 {
		t.Fatalf("shared key size = %d, want 32", len(shared))
	}

	shared2, err := receiver.Decapsulate(ciphertext)
	if err != nil {
		t.Fatalf("decapsulate: %v", err)
	}
	if !bytes.Equal(shared, shared2) {
		t.Fatalf("shared keys differ: a=%x b=%x", shared, shared2)
	}
}

// TestHybridKEMFromSeed 验证 NewHybridKEMFromSeed 自封闭实例互通。
func TestHybridKEMFromSeed(t *testing.T) {
	_, err := NewHybridKEMFromSeed([]byte("alice-self-seed-32bytes-test"))
	if err != nil {
		t.Fatalf("new alice: %v", err)
	}
	b, err := NewHybridKEMFromSeed([]byte("bob-self-seed-32bytes-testtt"))
	if err != nil {
		t.Fatalf("new bob: %v", err)
	}

	// a 封装到 b（用 b 的 pub 构造 sender 视角的 HybridKEM）
	bPub := b.X25519PublicKeyBytes()
	bMLKEMPub := b.MLKEMEncapsulationKeyBytes()
	var x25519PubArr [X25519PublicKeySize]byte
	copy(x25519PubArr[:], bPub)
	sender, err := NewHybridKEMForEncapsulation(x25519PubArr, bMLKEMPub)
	if err != nil {
		t.Fatalf("new sender view: %v", err)
	}
	shared, ciphertext, err := sender.Encapsulate()
	if err != nil {
		t.Fatalf("encapsulate: %v", err)
	}

	shared2, err := b.Decapsulate(ciphertext)
	if err != nil {
		t.Fatalf("decapsulate: %v", err)
	}
	if !bytes.Equal(shared, shared2) {
		t.Fatalf("shared keys differ: a=%x b=%x", shared, shared2)
	}
}

// TestMLDSASigner 验证 ML-DSA 签名生命周期。
func TestMLDSASigner(t *testing.T) {
	signer, err := GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatalf("gen mldsa: %v", err)
	}
	msg := []byte("ykt-pqc-mldsa-test-message-v7")

	sig, err := signer.Sign(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(sig) != MLDSA65SignatureSize {
		t.Fatalf("sig size = %d, want %d", len(sig), MLDSA65SignatureSize)
	}
	if !signer.Verify(msg, sig) {
		t.Fatal("verify failed")
	}
	// 篡改消息应验签失败
	if signer.Verify([]byte("tampered-msg"), sig) {
		t.Fatal("tampered message accepted")
	}
}

// TestHybridSigner 验证 RSA + ML-DSA 混合签名。
func TestHybridSigner(t *testing.T) {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	mldsa, err := GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatalf("gen mldsa: %v", err)
	}
	hs := NewHybridSigner(rsaPriv, &rsaPriv.PublicKey, mldsa)

	msg := []byte("ykt-pqc-hybrid-test-msg")
	sig, err := hs.Sign(msg)
	if err != nil {
		t.Fatalf("hybrid sign: %v", err)
	}
	if len(sig) != HybridSignatureSize() {
		t.Fatalf("hybrid sig size = %d, want %d", len(sig), HybridSignatureSize())
	}

	if !hs.Verify(msg, sig) {
		t.Fatal("hybrid verify (loose) failed")
	}
	if !hs.VerifyStrict(msg, sig) {
		t.Fatal("hybrid verify strict failed")
	}
	if hs.Verify([]byte("ykt-pqc-hybrid-test-msg-tampered"), sig) {
		t.Fatal("tampered message accepted by loose verify")
	}
}

// TestBackendVersion sanity check。
func TestBackendVersion(t *testing.T) {
	if v := BackendVersion(); v == "" {
		t.Fatal("backend version empty")
	}
}

// TestRSAPEMExport PEM 编解码 sanity。
func TestRSAPEMExport(t *testing.T) {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(rsaPriv)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	pemBytes := pem.EncodeToMemory(block)
	parsed, _ := pem.Decode(pemBytes)
	if parsed == nil {
		t.Fatal("pem decode failed")
	}
	restored, err := x509.ParsePKCS1PrivateKey(parsed.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if restored.D.Cmp(rsaPriv.D) != 0 {
		t.Fatal("restored rsa mismatch")
	}
}