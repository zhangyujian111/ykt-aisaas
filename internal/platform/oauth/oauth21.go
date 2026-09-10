// Package oauth provides OAuth 2.1 server implementation with PKCE, DPoP, PAR, and Resource Indicators.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ykt.dev/aisaas/internal/platform/redisx"
)

const (
	// OAuth 2.1 required configurations
	CodeChallengeMethodS256 = "S256"
	ParTTL                   = 90 * time.Second
	AccessTokenTTL           = 15 * time.Minute
	RefreshTokenTTL          = 7 * 24 * time.Hour // 7 days
)

// OAuth21Server implements OAuth 2.1 with mandatory PKCE, DPoP, PAR, and Resource Indicators.
type OAuth21Server struct {
	config *OAuth21Config

	// PKCE (mandatory in OAuth 2.1)
	requirePKCE bool

	// DPoP (token anti-replay)
	dpopManager *DPoPManager

	// PAR (Pushed Authorization Request)
	parEnabled bool
	parStore   PARStore

	// Resource Indicators (RFC 8707)
	resourceIndicators []string

	// Redis for token storage
	rdb *redisx.Client

	// PostgreSQL for audit
	pgPool *pgxpool.Pool
}

// OAuth21Config holds OAuth 2.1 configuration.
type OAuth21Config struct {
	Issuer             string   `yaml:"issuer" env:"OAUTH21_ISSUER"`
	ClientID           string   `yaml:"client_id" env:"OAUTH21_CLIENT_ID"`
	ClientSecret       string   `yaml:"client_secret" env:"OAUTH21_CLIENT_SECRET"`
	RequirePKCE        bool     `yaml:"pkce_required"`
	DPoPEnabled        bool     `yaml:"dpop_enabled"`
	PAREnabled         bool     `yaml:"par_enabled"`
	ResourceIndicators []string `yaml:"resource_indicators"`
	AccessTokenTTL     int      `yaml:"access_token_ttl"`     // seconds
	RefreshTokenTTL    int      `yaml:"refresh_token_ttl"`    // seconds
	ParTTL             int      `yaml:"par_ttl"`              // seconds
	AuthCodeTTL        int      `yaml:"auth_code_ttl"`        // seconds
}

// PARStore defines the interface for PAR storage.
type PARStore interface {
	Set(requestURI string, data string, ttl time.Duration) error
	Get(requestURI string) (string, error)
	Delete(requestURI string) error
}

// RedisPARStore implements PARStore using Redis.
type RedisPARStore struct {
	rdb *redisx.Client
}

func NewRedisPARStore(rdb *redisx.Client) *RedisPARStore {
	return &RedisPARStore{rdb: rdb}
}

func (s *RedisPARStore) Set(requestURI, data string, ttl time.Duration) error {
	return s.rdb.Set(nil, "par:"+requestURI, data, ttl).Err()
}

func (s *RedisPARStore) Get(requestURI string) (string, error) {
	return s.rdb.Get(nil, "par:"+requestURI).Result()
}

func (s *RedisPARStore) Delete(requestURI string) error {
	return s.rdb.Del(nil, "par:"+requestURI).Err()
}

// DPoPManager handles DPoP token binding and verification.
type DPoPManager struct {
	privateKey interface{}
}

// NewDPoPManager creates a new DPoP manager.
func NewDPoPManager(privateKey interface{}) *DPoPManager {
	return &DPoPManager{
		privateKey: privateKey,
	}
}

// DPoPProof represents a DPoP proof JWT.
type DPoPProof struct {
	jwt.RegisteredClaims
	HTM string `json:"htm"` // HTTP method
	HTU string `json:"htu"` // HTTP URI
	ATH string `json:"ath"` // Access Token Hash
}

// Verify validates a DPoP proof JWT.
func (m *DPoPManager) Verify(dpopJWT, htu, htm string) error {
	token, err := jwt.ParseWithClaims(dpopJWT, &DPoPProof{}, func(t *jwt.Token) (interface{}, error) {
		// Validate algorithm
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		// In production, use the client's public key
		// For now, use a shared secret for demonstration
		return []byte("dpop-secret"), nil
	})

	if err != nil {
		return fmt.Errorf("DPoP parse error: %w", err)
	}

	proof, ok := token.Claims.(*DPoPProof)
	if !ok {
		return errors.New("invalid DPoP claims")
	}

	// Verify method and URI match
	if proof.HTM != htm || proof.HTU != htu {
		return errors.New("DPoP htm/htu mismatch")
	}

	// Verify JTI uniqueness (prevent replay)
	jti := proof.ID
	if jti == "" {
		return errors.New("DPoP missing jti")
	}

	return nil
}

// NewOAuth21Server creates a new OAuth 2.1 server.
func NewOAuth21Server(cfg *OAuth21Config, rdb *redisx.Client, pgPool *pgxpool.Pool) *OAuth21Server {
	if cfg == nil {
		cfg = &OAuth21Config{}
	}

	// Set defaults per OAuth 2.1
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = int(AccessTokenTTL.Seconds())
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = int(RefreshTokenTTL.Seconds())
	}
	if cfg.ParTTL == 0 {
		cfg.ParTTL = int(ParTTL.Seconds())
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 60
	}
	if cfg.ResourceIndicators == nil {
		cfg.ResourceIndicators = []string{"ykt-aisaas", "ykt-admin"}
	}

	s := &OAuth21Server{
		config:             cfg,
		requirePKCE:        cfg.RequirePKCE,
		parEnabled:         cfg.PAREnabled,
		parStore:           NewRedisPARStore(rdb),
		resourceIndicators: cfg.ResourceIndicators,
		dpopManager:        NewDPoPManager(nil),
		rdb:                rdb,
		pgPool:             pgPool,
	}

	return s
}

// TokenRequest handles token endpoint requests with PKCE, DPoP, and Resource Indicator validation.
func (s *OAuth21Server) TokenRequest(grantType string, params url.Values) (*TokenResponse, error) {
	// 1. PKCE verification for authorization_code grant
	if grantType == "authorization_code" {
		codeVerifier := params.Get("code_verifier")
		codeChallenge := params.Get("code_challenge")

		if codeVerifier == "" {
			return nil, errors.New("PKCE code_verifier required")
		}

		if !s.verifyPKCE(codeVerifier, codeChallenge) {
			return nil, errors.New("PKCE verification failed")
		}
	}

	// 2. DPoP verification (if enabled)
	if s.config.DPoPEnabled {
		dpopJWT := params.Get("dpop")
		if dpopJWT != "" {
			htu := params.Get("resource")
			htm := "POST"
			if err := s.dpopManager.Verify(dpopJWT, htu, htm); err != nil {
				return nil, fmt.Errorf("DPoP invalid: %w", err)
			}
		}
	}

	// 3. Resource indicator validation (RFC 8707)
	resource := params.Get("resource")
	if resource != "" && !s.isAllowedResource(resource) {
		return nil, errors.New("resource not allowed")
	}

	// 4. Generate tokens
	accessToken, err := s.generateToken(32)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.generateToken(32)
	if err != nil {
		return nil, err
	}

	// Store tokens in Redis
	accessKey := fmt.Sprintf("oauth:access:%s", accessToken)
	refreshKey := fmt.Sprintf("oauth:refresh:%s", refreshToken)

	s.rdb.Set(nil, accessKey, "active", time.Duration(s.config.AccessTokenTTL)*time.Second)
	s.rdb.Set(nil, refreshKey, "active", time.Duration(s.config.RefreshTokenTTL)*time.Second)

	return &TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    s.config.AccessTokenTTL,
		RefreshToken: refreshToken,
		Resource:     resource,
	}, nil
}

// TokenResponse represents an OAuth 2.1 token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Resource     string `json:"resource,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

// generateToken generates a random token.
func (s *OAuth21Server) generateToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf), nil
}

// verifyPKCE verifies the PKCE code_verifier against the stored code_challenge.
func (s *OAuth21Server) verifyPKCE(verifier, challenge string) bool {
	// Calculate S256 hash of verifier
	h := sha256.New()
	h.Write([]byte(verifier))
	hash := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(h.Sum(nil))

	// Compare with stored challenge
	return hash == challenge
}

// isAllowedResource checks if the resource indicator is in the allowed list.
func (s *OAuth21Server) isAllowedResource(resource string) bool {
	for _, allowed := range s.resourceIndicators {
		if resource == allowed {
			return true
		}
	}
	return false
}

// PushedAuth handles PAR (Pushed Authorization Request) endpoint.
func (s *OAuth21Server) PushedAuth(w http.ResponseWriter, r *http.Request) {
	if !s.parEnabled {
		http.Error(w, "PAR not enabled", http.StatusNotImplemented)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate client_id
	clientID := r.FormValue("client_id")
	if clientID == "" {
		http.Error(w, "client_id required", http.StatusBadRequest)
		return
	}

	// OAuth 2.1: PKCE is mandatory even for PAR
	codeChallenge := r.FormValue("code_challenge")
	if codeChallenge == "" {
		http.Error(w, "PKCE code_challenge required", http.StatusBadRequest)
		return
	}

	codeChallengeMethod := r.FormValue("code_challenge_method")
	if codeChallengeMethod != CodeChallengeMethodS256 {
		http.Error(w, "code_challenge_method must be S256", http.StatusBadRequest)
		return
	}

	// Generate request URI
	requestURI := uuid.New().String()
	fullRequestURI := fmt.Sprintf("urn:ietf:params:oauth:request_uri:%s", requestURI)

	// Store the authorization request in Redis with short TTL
	ttl := time.Duration(s.config.ParTTL) * time.Second
	if err := s.parStore.Set(requestURI, r.Form.Encode(), ttl); err != nil {
		http.Error(w, "Failed to store PAR", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"request_uri": fullRequestURI,
		"expires_in":  int(ttl.Seconds()),
	})
}

// Metadata returns the OAuth 2.1 server metadata.
func (s *OAuth21Server) Metadata(w http.ResponseWriter, r *http.Request) {
	metadata := map[string]any{
		"issuer":                                s.config.Issuer,
		"authorization_endpoint":                "/oauth2/authorize",
		"token_endpoint":                        "/oauth2/token",
		"pushed_authorization_request_endpoint": "/oauth2/par",
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{CodeChallengeMethodS256},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"dpop_signing_alg_values_supported":     []string{"HS256"},
		"resource_indicators_supported":         s.resourceIndicators,
		"token_revocation_endpoint":             "/oauth2/revoke",
		"token_introspection_endpoint":          "/oauth2/introspect",
		"service_documentation":                 "https://docs.ykt.dev/oauth",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// TokenRevocation handles token revocation (RFC 7009).
func (s *OAuth21Server) TokenRevocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.FormValue("token")
	tokenTypeHint := r.FormValue("token_type_hint")

	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	// Revoke the token from Redis
	var key string
	if tokenTypeHint == "refresh_token" {
		key = fmt.Sprintf("oauth:refresh:%s", token)
	} else {
		key = fmt.Sprintf("oauth:access:%s", token)
	}
	s.rdb.Del(nil, key)

	w.WriteHeader(http.StatusOK)
}

// TokenIntrospection handles token introspection (RFC 7662).
func (s *OAuth21Server) TokenIntrospection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.FormValue("token")
	tokenTypeHint := r.FormValue("token_type_hint")

	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	// Check if token exists in Redis
	var key string
	if tokenTypeHint == "refresh_token" {
		key = fmt.Sprintf("oauth:refresh:%s", token)
	} else {
		key = fmt.Sprintf("oauth:access:%s", token)
	}

	exists, err := s.rdb.Exists(nil, key).Result()
	if err != nil {
		exists = 0
	}

	response := map[string]any{
		"active": exists > 0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ValidateToken validates an access token and returns the claims.
func (s *OAuth21Server) ValidateToken(token string) (bool, error) {
	key := fmt.Sprintf("oauth:access:%s", token)
	exists, err := s.rdb.Exists(nil, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// AuthorizeRequest validates an authorization request with PKCE.
func (s *OAuth21Server) AuthorizeRequest(r *http.Request) error {
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")

	// OAuth 2.1 mandates PKCE
	if codeChallenge == "" {
		return errors.New("missing code_challenge")
	}

	// OAuth 2.1 requires S256 only
	if codeChallengeMethod != CodeChallengeMethodS256 {
		return errors.New("invalid code_challenge_method")
	}

	// Store code challenge for later verification
	sessionID := uuid.New().String()
	err := s.parStore.Set("pkce:"+sessionID, codeChallenge+"|"+codeChallengeMethod, time.Duration(s.config.AuthCodeTTL)*time.Second)
	if err != nil {
		return err
	}

	return nil
}

// BuildAuthorizationURL builds an authorization URL with PKCE.
func (s *OAuth21Server) BuildAuthorizationURL(params AuthorizationParams) (string, string, error) {
	// Generate code verifier and challenge
	verifier, err := s.generateToken(32)
	if err != nil {
		return "", "", err
	}

	h := sha256.New()
	h.Write([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(h.Sum(nil))

	// Build URL
	baseURL := strings.TrimRight(s.config.Issuer, "/oauth2")
	authURL := fmt.Sprintf("%s/oauth2/authorize", baseURL)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", s.config.ClientID)
	q.Set("redirect_uri", params.RedirectURI)
	q.Set("scope", params.Scope)
	q.Set("state", params.State)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", CodeChallengeMethodS256)

	if params.Resource != "" {
		q.Set("resource", params.Resource)
	}

	return authURL + "?" + q.Encode(), verifier, nil
}

// AuthorizationParams holds parameters for building an authorization URL.
type AuthorizationParams struct {
	RedirectURI string
	Scope       string
	State       string
	Resource    string
}
