package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"ykt.dev/aisaas/internal/platform/crypto/pqc"
)

func newPQCJWTHarness(t *testing.T) (*PQCJWT, []byte, *pqc.HybridSigner) {
	t.Helper()

	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	mldsa, err := pqc.GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatalf("mldsa: %v", err)
	}
	signer := pqc.NewHybridSigner(rsaPriv, &rsaPriv.PublicKey, mldsa)
	verifier := pqc.NewHybridSigner(rsaPriv, &rsaPriv.PublicKey, mldsa)

	hmac := make([]byte, 32)
	for i := range hmac {
		hmac[i] = byte(i)
	}
	j, err := NewPQCJWT(signer, verifier, hmac)
	if err != nil {
		t.Fatalf("new pqc jwt: %v", err)
	}
	return j, hmac, signer
}

func TestPQCJWTIssueAndVerify(t *testing.T) {
	j, _, _ := newPQCJWTHarness(t)

	claims := jwt.MapClaims{
		"sub": "user-1",
		"aud": "ykt-aisaas",
		"exp": 99999999999,
	}
	tok, err := j.IssueToken(claims)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}

	parsed, err := j.VerifyToken(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if parsed["sub"] != "user-1" {
		t.Fatalf("sub mismatch: %v", parsed["sub"])
	}
}

func TestPQCJWTRejectTamperedSignature(t *testing.T) {
	j, _, _ := newPQCJWTHarness(t)

	claims := jwt.MapClaims{
		"sub": "user-2",
		"exp": 99999999999,
	}
	tok, err := j.IssueToken(claims)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// 篡改 pqc_sig（替换最后一个字符）
	tampered := tok[:len(tok)-1]
	if tampered[len(tampered)-1] == 'A' {
		tampered = tampered[:len(tampered)-1] + "B"
	} else {
		tampered = tampered[:len(tampered)-1] + "A"
	}
	if _, err := j.VerifyToken(tampered); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestPQCJWTConstructValidation(t *testing.T) {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	mldsa, err := pqc.GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatalf("mldsa: %v", err)
	}
	signer := pqc.NewHybridSigner(rsaPriv, &rsaPriv.PublicKey, mldsa)

	// nil signer
	if _, err := NewPQCJWT(nil, signer, make([]byte, 32)); err == nil {
		t.Fatal("nil signer accepted")
	}
	// short hmac
	if _, err := NewPQCJWT(signer, signer, make([]byte, 16)); err == nil {
		t.Fatal("short hmac accepted")
	}
}