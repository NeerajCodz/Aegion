// Package jwt provides Go bindings for the Aegion Rust JWT engine.
//
// This package wraps the Rust implementations of:
// - EC key pair generation (ES256)
// - JWT signing and verification
// - JWKS serialization
package jwt

/*
#cgo linux LDFLAGS: -L${SRCDIR}/../../../rust/target/release -laegion_jwt -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../rust/target/release -laegion_jwt -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../../rust/target/x86_64-pc-windows-gnu/release -Wl,-Bstatic -l:libaegion_jwt.a -Wl,-Bdynamic -lws2_32 -lbcrypt -luserenv -lntdll
#cgo CFLAGS: -I${SRCDIR}/../../../rust/jwt/include

#include <stdlib.h>
#include <stdint.h>

typedef struct {
    int error_code;
    char* result;
} JwtResult;

typedef struct {
    int error_code;
    char* algorithm;
    char* key_id;
    uint8_t* private_key;
    size_t private_key_len;
    uint8_t* public_key;
    size_t public_key_len;
} KeyPairResult;

typedef struct {
    int error_code;
    char* claims_json;
    char* key_id;
} ClaimsResult;

extern KeyPairResult jwt_generate_ec_keypair(const char* key_id);
extern JwtResult jwt_sign(const char* claims_json, const char* algorithm, const uint8_t* private_key, size_t private_key_len, const char* key_id);
extern ClaimsResult jwt_verify(const char* token, const char* algorithm, const uint8_t* public_key, size_t public_key_len, const char* issuer, const char* audience, uint64_t leeway);
extern JwtResult jwt_to_jwk(const char* algorithm, const char* key_id, const uint8_t* public_key, size_t public_key_len);
extern void jwt_free_string(char* s);
extern void jwt_free_bytes(uint8_t* data, size_t len);
extern void jwt_free_keypair(KeyPairResult result);
extern void jwt_free_claims(ClaimsResult result);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"time"
	"unsafe"
)

var (
	ErrKeyGenFailed     = errors.New("key generation failed")
	ErrSigningFailed    = errors.New("JWT signing failed")
	ErrVerifyFailed     = errors.New("JWT verification failed")
	ErrInvalidToken     = errors.New("invalid token format")
	ErrInvalidAlg       = errors.New("invalid algorithm")
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenNotYetValid = errors.New("token not yet valid")
)

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

	// Add standard claims
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

	// Add custom claims
	for k, v := range c.Custom {
		m[k] = v
	}

	return json.Marshal(m)
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

// GenerateECKeyPair generates an ES256 (ECDSA P-256) key pair.
func GenerateECKeyPair(keyID string) (*KeyPair, error) {
	var cKeyID *C.char
	if keyID != "" {
		cKeyID = C.CString(keyID)
		defer C.free(unsafe.Pointer(cKeyID))
	}

	result := C.jwt_generate_ec_keypair(cKeyID)
	if result.error_code != 0 {
		return nil, ErrKeyGenFailed
	}
	defer C.jwt_free_keypair(result)

	kp := &KeyPair{
		Algorithm:  C.GoString(result.algorithm),
		KeyID:      C.GoString(result.key_id),
		PrivateKey: C.GoBytes(unsafe.Pointer(result.private_key), C.int(result.private_key_len)),
		PublicKey:  C.GoBytes(unsafe.Pointer(result.public_key), C.int(result.public_key_len)),
	}

	return kp, nil
}

// Sign creates a signed JWT from the given claims.
func Sign(claims Claims, privateKey []byte, algorithm, keyID string) (string, error) {
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	cClaims := C.CString(string(claimsJSON))
	cAlgorithm := C.CString(algorithm)
	defer C.free(unsafe.Pointer(cClaims))
	defer C.free(unsafe.Pointer(cAlgorithm))

	var cKeyID *C.char
	if keyID != "" {
		cKeyID = C.CString(keyID)
		defer C.free(unsafe.Pointer(cKeyID))
	}

	result := C.jwt_sign(
		cClaims,
		cAlgorithm,
		(*C.uint8_t)(unsafe.Pointer(&privateKey[0])),
		C.size_t(len(privateKey)),
		cKeyID,
	)
	if result.error_code != 0 {
		return "", ErrSigningFailed
	}
	defer C.jwt_free_string(result.result)

	return C.GoString(result.result), nil
}

// Verify verifies a JWT and returns the claims.
func Verify(token string, publicKey []byte, algorithm string, opts VerifyOptions) (*VerifyResult, error) {
	cToken := C.CString(token)
	cAlgorithm := C.CString(algorithm)
	defer C.free(unsafe.Pointer(cToken))
	defer C.free(unsafe.Pointer(cAlgorithm))

	var cIssuer, cAudience *C.char
	if opts.Issuer != "" {
		cIssuer = C.CString(opts.Issuer)
		defer C.free(unsafe.Pointer(cIssuer))
	}
	if opts.Audience != "" {
		cAudience = C.CString(opts.Audience)
		defer C.free(unsafe.Pointer(cAudience))
	}

	leeway := C.uint64_t(opts.Leeway.Seconds())

	result := C.jwt_verify(
		cToken,
		cAlgorithm,
		(*C.uint8_t)(unsafe.Pointer(&publicKey[0])),
		C.size_t(len(publicKey)),
		cIssuer,
		cAudience,
		leeway,
	)

	if result.error_code != 0 {
		switch result.error_code {
		case -7:
			return nil, ErrTokenExpired
		case -8:
			return nil, ErrTokenNotYetValid
		case -4:
			return nil, ErrInvalidToken
		case -5:
			return nil, ErrInvalidAlg
		default:
			return nil, ErrVerifyFailed
		}
	}
	defer C.jwt_free_claims(result)

	var claims Claims
	if err := json.Unmarshal([]byte(C.GoString(result.claims_json)), &claims); err != nil {
		return nil, err
	}

	vr := &VerifyResult{
		Claims: claims,
	}
	if result.key_id != nil {
		vr.KeyID = C.GoString(result.key_id)
	}

	return vr, nil
}

// ToJWK converts a public key to JWK JSON format.
func ToJWK(algorithm, keyID string, publicKey []byte) (string, error) {
	cAlgorithm := C.CString(algorithm)
	cKeyID := C.CString(keyID)
	defer C.free(unsafe.Pointer(cAlgorithm))
	defer C.free(unsafe.Pointer(cKeyID))

	result := C.jwt_to_jwk(
		cAlgorithm,
		cKeyID,
		(*C.uint8_t)(unsafe.Pointer(&publicKey[0])),
		C.size_t(len(publicKey)),
	)
	if result.error_code != 0 {
		return "", errors.New("failed to convert to JWK")
	}
	defer C.jwt_free_string(result.result)

	return C.GoString(result.result), nil
}
