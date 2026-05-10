// Package jwt provides Go-native JWT signing, verification, and JWK helpers.
package jwt

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
)

var (
	ErrKeyGenFailed     = errors.New("key generation failed")
	ErrSigningFailed    = errors.New("JWT signing failed")
	ErrVerifyFailed     = errors.New("JWT verification failed")
	ErrInvalidToken     = errors.New("invalid token format")
	ErrInvalidAlg       = errors.New("invalid algorithm")
	ErrInvalidKey       = errors.New("invalid key: must be non-empty")
	ErrInvalidLeeway    = errors.New("invalid leeway: must be non-negative")
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenNotYetValid = errors.New("token not yet valid")
)

var reservedClaimNames = map[string]struct{}{
	"iss": {},
	"sub": {},
	"aud": {},
	"exp": {},
	"nbf": {},
	"iat": {},
	"jti": {},
	"sid": {},
}

// KeyPair represents an asymmetric key pair for JWT signing.
type KeyPair struct {
	Algorithm  string
	KeyID      string
	PrivateKey []byte
	PublicKey  []byte
}

// Claims represents standard JWT claims.
type Claims struct {
	Issuer    string                 `json:"iss,omitempty"`
	Subject   string                 `json:"sub,omitempty"`
	Audience  string                 `json:"aud,omitempty"`
	ExpiresAt int64                  `json:"exp,omitempty"`
	NotBefore int64                  `json:"nbf,omitempty"`
	IssuedAt  int64                  `json:"iat,omitempty"`
	JWTID     string                 `json:"jti,omitempty"`
	SessionID string                 `json:"sid,omitempty"`
	Custom    map[string]interface{} `json:"-"`
}

// MarshalJSON implements custom JSON marshaling to flatten custom claims.
func (c Claims) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})

	if c.Issuer != "" {
		m["iss"] = c.Issuer
	}
	if c.Subject != "" {
		m["sub"] = c.Subject
	}
	if c.Audience != "" {
		m["aud"] = c.Audience
	}
	if c.ExpiresAt != 0 {
		m["exp"] = c.ExpiresAt
	}
	if c.NotBefore != 0 {
		m["nbf"] = c.NotBefore
	}
	if c.IssuedAt != 0 {
		m["iat"] = c.IssuedAt
	}
	if c.JWTID != "" {
		m["jti"] = c.JWTID
	}
	if c.SessionID != "" {
		m["sid"] = c.SessionID
	}

	for k, v := range c.Custom {
		if _, isReserved := reservedClaimNames[k]; isReserved {
			continue
		}
		m[k] = v
	}

	return json.Marshal(m)
}

// UnmarshalJSON implements custom JSON unmarshaling for flattened custom claims.
func (c *Claims) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var m map[string]interface{}
	if err := decoder.Decode(&m); err != nil {
		return err
	}

	*c = Claims{}
	for k, v := range m {
		switch k {
		case "iss":
			c.Issuer, _ = v.(string)
		case "sub":
			c.Subject, _ = v.(string)
		case "aud":
			c.Audience, _ = v.(string)
		case "exp":
			c.ExpiresAt = numberToInt64(v)
		case "nbf":
			c.NotBefore = numberToInt64(v)
		case "iat":
			c.IssuedAt = numberToInt64(v)
		case "jti":
			c.JWTID, _ = v.(string)
		case "sid":
			c.SessionID, _ = v.(string)
		default:
			if c.Custom == nil {
				c.Custom = make(map[string]interface{})
			}
			c.Custom[k] = v
		}
	}
	return nil
}

func numberToInt64(value interface{}) int64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

// VerifyOptions configures JWT verification.
type VerifyOptions struct {
	Issuer   string
	Audience string
	Leeway   time.Duration
}

// VerifyResult contains the verified JWT data.
type VerifyResult struct {
	Claims Claims
	KeyID  string
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	KID string `json:"kid,omitempty"`
}

// GenerateECKeyPair generates an ES256 (ECDSA P-256) key pair.
func GenerateECKeyPair(keyID string) (*KeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, ErrKeyGenFailed
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, ErrKeyGenFailed
	}
	publicKey := elliptic.Marshal(elliptic.P256(), key.X, key.Y)

	return &KeyPair{
		Algorithm:  "ES256",
		KeyID:      keyID,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

// Sign creates a signed JWT from the given claims.
func Sign(claims Claims, privateKey []byte, algorithm, keyID string) (string, error) {
	if len(privateKey) == 0 {
		return "", ErrInvalidKey
	}

	headerJSON, err := json.Marshal(jwtHeader{Alg: algorithm, Typ: "JWT", KID: keyID})
	if err != nil {
		return "", ErrSigningFailed
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload

	signature, err := signBytes([]byte(signingInput), privateKey, algorithm)
	if err != nil {
		if errors.Is(err, ErrInvalidAlg) {
			return "", ErrInvalidAlg
		}
		if errors.Is(err, ErrInvalidKey) {
			return "", ErrInvalidKey
		}
		return "", ErrSigningFailed
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// Verify verifies a JWT and returns the claims.
func Verify(token string, publicKey []byte, algorithm string, opts VerifyOptions) (*VerifyResult, error) {
	if len(publicKey) == 0 {
		return nil, ErrInvalidKey
	}
	if opts.Leeway < 0 {
		return nil, ErrInvalidLeeway
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrVerifyFailed
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrVerifyFailed
	}
	if header.Alg != algorithm {
		return nil, ErrInvalidAlg
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrVerifyFailed
	}
	if err := verifyBytes([]byte(parts[0]+"."+parts[1]), signature, publicKey, algorithm); err != nil {
		if errors.Is(err, ErrInvalidAlg) {
			return nil, ErrInvalidAlg
		}
		return nil, ErrVerifyFailed
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrVerifyFailed
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrVerifyFailed
	}
	if err := validateClaims(claims, opts); err != nil {
		return nil, err
	}

	return &VerifyResult{Claims: claims, KeyID: header.KID}, nil
}

// ToJWK converts a public key to JWK JSON format.
func ToJWK(algorithm, keyID string, publicKey []byte) (string, error) {
	if len(publicKey) == 0 {
		return "", ErrInvalidKey
	}

	var jwk interface{}
	switch algorithm {
	case "ES256":
		x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
		if x == nil || y == nil {
			return "", errors.New("failed to convert to JWK")
		}
		jwk = struct {
			KTY string `json:"kty"`
			KID string `json:"kid,omitempty"`
			Use string `json:"use,omitempty"`
			Alg string `json:"alg,omitempty"`
			Crv string `json:"crv,omitempty"`
			X   string `json:"x,omitempty"`
			Y   string `json:"y,omitempty"`
		}{
			KTY: "EC",
			KID: keyID,
			Use: "sig",
			Alg: "ES256",
			Crv: "P-256",
			X:   base64.RawURLEncoding.EncodeToString(padBytes(x.Bytes(), 32)),
			Y:   base64.RawURLEncoding.EncodeToString(padBytes(y.Bytes(), 32)),
		}
	case "EdDSA":
		if len(publicKey) != ed25519.PublicKeySize {
			return "", errors.New("failed to convert to JWK")
		}
		jwk = struct {
			KTY string `json:"kty"`
			KID string `json:"kid,omitempty"`
			Use string `json:"use,omitempty"`
			Alg string `json:"alg,omitempty"`
			Crv string `json:"crv,omitempty"`
			X   string `json:"x,omitempty"`
		}{
			KTY: "OKP",
			KID: keyID,
			Use: "sig",
			Alg: "EdDSA",
			Crv: "Ed25519",
			X:   base64.RawURLEncoding.EncodeToString(publicKey),
		}
	default:
		return "", errors.New("failed to convert to JWK")
	}

	out, err := json.Marshal(jwk)
	if err != nil {
		return "", errors.New("failed to convert to JWK")
	}
	return string(out), nil
}

func signBytes(message, privateKey []byte, algorithm string) ([]byte, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, ErrInvalidKey
	}

	switch algorithm {
	case "ES256":
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok || key.Curve != elliptic.P256() {
			return nil, ErrInvalidKey
		}
		digest := sha256.Sum256(message)
		r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
		if err != nil {
			return nil, err
		}
		return append(padBytes(r.Bytes(), 32), padBytes(s.Bytes(), 32)...), nil
	case "EdDSA":
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, ErrInvalidKey
		}
		return ed25519.Sign(key, message), nil
	default:
		return nil, ErrInvalidAlg
	}
}

func verifyBytes(message, signature, publicKey []byte, algorithm string) error {
	switch algorithm {
	case "ES256":
		if len(signature) != 64 {
			return ErrVerifyFailed
		}
		x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
		if x == nil || y == nil {
			return ErrVerifyFailed
		}
		digest := sha256.Sum256(message)
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:], r, s) {
			return ErrVerifyFailed
		}
		return nil
	case "EdDSA":
		if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, message, signature) {
			return ErrVerifyFailed
		}
		return nil
	case "RS256":
		key, err := parseRSAPublicKey(publicKey)
		if err != nil {
			return ErrVerifyFailed
		}
		digest := sha256.Sum256(message)
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
			return ErrVerifyFailed
		}
		return nil
	default:
		return ErrInvalidAlg
	}
}

func parseRSAPublicKey(publicKey []byte) (*rsa.PublicKey, error) {
	if key, err := x509.ParsePKCS1PublicKey(publicKey); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, ErrInvalidKey
	}
	return key, nil
}

func validateClaims(claims Claims, opts VerifyOptions) error {
	now := time.Now().UTC().Unix()
	leeway := int64(opts.Leeway / time.Second)
	if leeway < 0 {
		return ErrInvalidLeeway
	}

	if claims.ExpiresAt == 0 {
		return ErrVerifyFailed
	}
	if now > claims.ExpiresAt+leeway {
		return ErrTokenExpired
	}
	if claims.NotBefore != 0 && now+leeway < claims.NotBefore {
		return ErrTokenNotYetValid
	}
	if opts.Issuer != "" && claims.Issuer != opts.Issuer {
		return ErrVerifyFailed
	}
	if opts.Audience != "" && claims.Audience != opts.Audience {
		return ErrVerifyFailed
	}
	return nil
}

func padBytes(in []byte, size int) []byte {
	if len(in) >= size {
		return in
	}
	out := make([]byte, size)
	copy(out[size-len(in):], in)
	return out
}
