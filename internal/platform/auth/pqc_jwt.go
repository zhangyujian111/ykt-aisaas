package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"

	"github.com/golang-jwt/jwt/v5"

	"ykt.dev/aisaas/internal/platform/crypto/pqc"
)

// PQCJWT PQC 签名 JWT 颁发与验证器。
//
// JWT 内部 = 标准 HS256（OpenID / OAuth2 兼容）；
// Header 中追加 `pqc_sig` 字段 = HybridSigner 双签名。
//
// 验证 = HMAC 失败立即拒绝；HMAC 通过后验 PQC 签名。
type PQCJWT struct {
	signer   *pqc.HybridSigner
	verifier *pqc.HybridSigner
	hmacKey  []byte // 32 bytes HS256
}

// NewPQCJWT 构造 PQC JWT 服务。
//
// hmacKey 必须 ≥32 字节（HS256 要求）。
func NewPQCJWT(signer, verifier *pqc.HybridSigner, hmacKey []byte) (*PQCJWT, error) {
	if signer == nil || verifier == nil {
		return nil, errors.New("pqc jwt: signer/verifier required")
	}
	if len(hmacKey) < 32 {
		return nil, errors.New("pqc jwt: hmac key must be >= 32 bytes")
	}
	return &PQCJWT{signer: signer, verifier: verifier, hmacKey: hmacKey}, nil
}

// IssueToken 签发 PQC 签名 JWT。
func (j *PQCJWT) IssueToken(claims jwt.MapClaims) (string, error) {
	if j == nil {
		return "", errors.New("pqc jwt: nil receiver")
	}

	// 1. 构造 header（占位 pqc_sig 保持稳定 JSON）
	header := map[string]any{
		"alg":     "HS256",
		"typ":     "JWT",
		"pqc_sig": "",
		"pqc_alg": "RSA-2048+ML-DSA-65",
	}

	// 2. 用 canonicalRaw（稳定 JSON 顺序）构造签名输入
	rawForSign := canonicalRaw(header, claims)

	// 3. PQC 签名 over header.payload
	pqcSig, err := j.signer.Sign([]byte(rawForSign))
	if err != nil {
		return "", err
	}

	// 4. 真实构造 JWT（golang-jwt 库）
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["pqc_sig"] = base64.StdEncoding.EncodeToString(pqcSig)
	token.Header["pqc_alg"] = "RSA-2048+ML-DSA-65"

	// 5. HMAC 签名
	return token.SignedString(j.hmacKey)
}

// VerifyToken 验证 JWT（含 PQC 签名）。
func (j *PQCJWT) VerifyToken(tokenString string) (jwt.MapClaims, error) {
	if j == nil {
		return nil, errors.New("pqc jwt: nil receiver")
	}

	// 1. 解析 header + claims（不验签）
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	unverified, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}

	// 2. 提取 pqc_sig
	pqcSigB64, ok := unverified.Header["pqc_sig"].(string)
	if !ok || pqcSigB64 == "" {
		return nil, errors.New("pqc jwt: missing pqc_sig header")
	}
	pqcSig, err := base64.StdEncoding.DecodeString(pqcSigB64)
	if err != nil {
		return nil, err
	}

	// 3. 重建被签名的 raw：把 pqc_sig 临时清空再序列化（与 IssueToken 占位时一致）
	headerForSign := make(map[string]any, len(unverified.Header))
	for k, v := range unverified.Header {
		headerForSign[k] = v
	}
	headerForSign["pqc_sig"] = ""

	claimsUC, _ := unverified.Claims.(jwt.MapClaims)
	rawForSign := canonicalRaw(headerForSign, claimsUC)
	if !j.verifier.Verify([]byte(rawForSign), pqcSig) {
		return nil, errors.New("pqc jwt: pqc signature invalid")
	}

	// 4. 验证 HMAC（HS256）
	token, err := jwt.Parse(tokenString, func(_ *jwt.Token) (interface{}, error) {
		return j.hmacKey, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("pqc jwt: token invalid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("pqc jwt: claims type assertion failed")
	}
	return claims, nil
}

// PQCSignatureHeader 提取 JWT header 中的 PQC 签名（base64 字符串）。
func (j *PQCJWT) PQCSignatureHeader(tokenString string) (string, error) {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", err
	}
	sig, ok := token.Header["pqc_sig"].(string)
	if !ok {
		return "", errors.New("pqc jwt: pqc_sig header not found")
	}
	return sig, nil
}

// canonicalRaw 构造稳定的 header.payload（保证签名方与验签方输出一致）。
//
// 由于 Go map 迭代顺序随机，需要用 sort.Slice 保证 JSON 字段顺序稳定。
func canonicalRaw(header map[string]any, claims jwt.MapClaims) string {
	headerKeys := make([]string, 0, len(header))
	for k := range header {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)

	orderedHeader := make([]byte, 0, 128)
	orderedHeader = append(orderedHeader, '{')
	for i, k := range headerKeys {
		if i > 0 {
			orderedHeader = append(orderedHeader, ',')
		}
		kb, _ := json.Marshal(k)
		orderedHeader = append(orderedHeader, kb...)
		orderedHeader = append(orderedHeader, ':')
		vb, _ := json.Marshal(header[k])
		orderedHeader = append(orderedHeader, vb...)
	}
	orderedHeader = append(orderedHeader, '}')

	claimsKeys := make([]string, 0, len(claims))
	for k := range claims {
		claimsKeys = append(claimsKeys, k)
	}
	sort.Strings(claimsKeys)

	orderedClaims := make([]byte, 0, 256)
	orderedClaims = append(orderedClaims, '{')
	for i, k := range claimsKeys {
		if i > 0 {
			orderedClaims = append(orderedClaims, ',')
		}
		kb, _ := json.Marshal(k)
		orderedClaims = append(orderedClaims, kb...)
		orderedClaims = append(orderedClaims, ':')
		vb, _ := json.Marshal(claims[k])
		orderedClaims = append(orderedClaims, vb...)
	}
	orderedClaims = append(orderedClaims, '}')

	return base64.RawURLEncoding.EncodeToString(orderedHeader) + "." +
		base64.RawURLEncoding.EncodeToString(orderedClaims)
}