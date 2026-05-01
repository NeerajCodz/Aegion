// Package crypto provides Go bindings for the Aegion Rust crypto engine.
//
// This package wraps the Rust implementations of:
// - Argon2id password hashing
// - XChaCha20-Poly1305 field encryption
// - Constant-time comparison
//
// The Rust code is compiled as a static library and linked via CGo.
package crypto

/*
#cgo linux LDFLAGS: -L${SRCDIR}/../../../rust/target/release -laegion_crypto -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../rust/target/release -laegion_crypto -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../../rust/target/x86_64-pc-windows-gnu/release -Wl,-Bstatic -l:libaegion_crypto.a -Wl,-Bdynamic -lws2_32 -lbcrypt -luserenv -lntdll
#cgo CFLAGS: -I${SRCDIR}/../../../rust/crypto/include

#include <stdlib.h>
#include <stdint.h>

typedef struct {
    int error_code;
    char* result;
} CryptoResult;

typedef struct {
    int error_code;
    uint8_t* data;
    size_t len;
} BytesResult;

typedef struct {
    int error_code;
    char* module_id;
    int64_t timestamp;
    char* signature_hex;
} ParsedTokenResult;

extern CryptoResult crypto_hash_password(const char* password);
extern int crypto_verify_password(const char* password, const char* hash);
extern CryptoResult crypto_encrypt_field(const uint8_t* key, const uint8_t* plaintext, size_t plaintext_len, const uint8_t* aad, size_t aad_len);
extern BytesResult crypto_decrypt_field(const uint8_t* key, const char* ciphertext, const uint8_t* aad, size_t aad_len);
extern int crypto_generate_key(uint8_t* out);
extern int crypto_constant_time_compare(const uint8_t* a, const uint8_t* b, size_t len);
extern CryptoResult crypto_opaque_hash(const char* token);
extern int crypto_opaque_validate(const char* token, const char* expected_hash);
extern CryptoResult crypto_opaque_prefix(const char* token, size_t length);
extern CryptoResult crypto_hmac_sha256_hex(const uint8_t* secret, size_t secret_len, const uint8_t* message, size_t message_len);
extern CryptoResult crypto_sign_envelope(const char* kind, const uint8_t* secret, size_t secret_len, int64_t timestamp, const uint8_t* payload, size_t payload_len);
extern int crypto_verify_envelope(const char* kind, const uint8_t* secret, size_t secret_len, const uint8_t* payload, size_t payload_len, const char* envelope, uint64_t max_age_seconds, int64_t now_unix);
extern CryptoResult crypto_sign_session_cookie(const uint8_t* secret, size_t secret_len, const char* token, int64_t timestamp);
extern CryptoResult crypto_verify_session_cookie(const uint8_t* secret, size_t secret_len, const char* signed_value, uint64_t max_age_seconds, int64_t now_unix);
extern CryptoResult crypto_generate_internal_token(const uint8_t* secret, size_t secret_len, const char* module_id, int64_t timestamp);
extern ParsedTokenResult crypto_verify_internal_token(const uint8_t* secret, size_t secret_len, const char* token, uint64_t ttl_seconds, int64_t now_unix);
extern CryptoResult crypto_pkce_challenge(const char* verifier, const char* method);
extern int crypto_pkce_verify(const char* verifier, const char* challenge, const char* method);
extern void crypto_free_string(char* s);
extern void crypto_free_bytes(uint8_t* data, size_t len);
extern void crypto_free_parsed_token(ParsedTokenResult result);
*/
import "C"
import (
	"errors"
	"math"
	"unsafe"
)

// KeySize is the required size for encryption keys (32 bytes / 256 bits).
const KeySize = 32

var (
	ErrHashFailed       = errors.New("password hashing failed")
	ErrVerifyFailed     = errors.New("password verification failed")
	ErrEncryptFailed    = errors.New("encryption failed")
	ErrDecryptFailed    = errors.New("decryption failed")
	ErrInvalidKeyLength = errors.New("invalid key length: expected 32 bytes")
	ErrRngFailed        = errors.New("random number generation failed")
	ErrOpaqueFailed     = errors.New("opaque token operation failed")
	ErrSignatureFailed  = errors.New("signature operation failed")
	ErrEnvelopeFailed   = errors.New("envelope operation failed")
	ErrSessionCookie    = errors.New("session cookie operation failed")
	ErrInternalToken    = errors.New("internal token operation failed")
	ErrPKCEFailed       = errors.New("pkce operation failed")
	ErrExpired          = errors.New("crypto token expired")
	ErrInvalidSignature = errors.New("crypto signature invalid")
)

var generateKeyFn = func(key []byte) int {
	return int(C.crypto_generate_key((*C.uint8_t)(unsafe.Pointer(&key[0]))))
}

func byteSlicePtr(data []byte) *C.uint8_t {
	if len(data) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&data[0]))
}

func goStringResult(result C.CryptoResult, fallback error) (string, error) {
	if result.error_code != 0 {
		return "", errorFromCode(int(result.error_code), fallback)
	}
	defer C.crypto_free_string(result.result)
	return C.GoString(result.result), nil
}

type parsedTokenResult struct {
	ModuleID      string
	TimestampUnix int64
	SignatureHex  string
}

func goParsedTokenResult(result C.ParsedTokenResult, fallback error) (*parsedTokenResult, error) {
	if result.error_code != 0 {
		return nil, errorFromCode(int(result.error_code), fallback)
	}
	defer C.crypto_free_parsed_token(result)
	return &parsedTokenResult{
		ModuleID:      C.GoString(result.module_id),
		TimestampUnix: int64(result.timestamp),
		SignatureHex:  C.GoString(result.signature_hex),
	}, nil
}

func errorFromCode(code int, fallback error) error {
	switch code {
	case -9:
		return ErrInvalidSignature
	case -10:
		return ErrExpired
	default:
		return fallback
	}
}

// HashPassword hashes a password using Argon2id with secure defaults.
// Returns the PHC-encoded hash string.
func HashPassword(password string) (string, error) {
	cPassword := C.CString(password)
	defer C.free(unsafe.Pointer(cPassword))

	result := C.crypto_hash_password(cPassword)
	if result.error_code != 0 {
		return "", ErrHashFailed
	}
	defer C.crypto_free_string(result.result)

	return C.GoString(result.result), nil
}

// VerifyPassword verifies a password against an Argon2id hash.
// Returns true if the password matches, false otherwise.
func VerifyPassword(password, hash string) (bool, error) {
	cPassword := C.CString(password)
	cHash := C.CString(hash)
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cHash))

	result := C.crypto_verify_password(cPassword, cHash)
	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	default:
		return false, ErrVerifyFailed
	}
}

// EncryptField encrypts plaintext using XChaCha20-Poly1305.
// The key must be exactly 32 bytes.
// AAD (additional authenticated data) is optional but recommended for binding
// the ciphertext to a context (e.g., identity ID).
// Returns base64-encoded ciphertext.
func EncryptField(key, plaintext, aad []byte) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKeyLength
	}

	result := C.crypto_encrypt_field(
		(*C.uint8_t)(unsafe.Pointer(&key[0])),
		byteSlicePtr(plaintext),
		C.size_t(len(plaintext)),
		byteSlicePtr(aad),
		C.size_t(len(aad)),
	)
	if result.error_code != 0 {
		return "", ErrEncryptFailed
	}
	defer C.crypto_free_string(result.result)

	return C.GoString(result.result), nil
}

// DecryptField decrypts ciphertext encrypted with EncryptField.
// The key and AAD must match those used for encryption.
func DecryptField(key []byte, ciphertext string, aad []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeyLength
	}

	cCiphertext := C.CString(ciphertext)
	defer C.free(unsafe.Pointer(cCiphertext))

	result := C.crypto_decrypt_field(
		(*C.uint8_t)(unsafe.Pointer(&key[0])),
		cCiphertext,
		byteSlicePtr(aad),
		C.size_t(len(aad)),
	)
	if result.error_code != 0 {
		return nil, ErrDecryptFailed
	}
	defer C.crypto_free_bytes(result.data, result.len)

	if result.data == nil && result.len > 0 {
		return nil, ErrDecryptFailed
	}
	if result.len > C.size_t(math.MaxInt32) {
		return nil, ErrDecryptFailed
	}

	return C.GoBytes(unsafe.Pointer(result.data), C.int(result.len)), nil
}

// GenerateKey generates a cryptographically secure random 32-byte key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	result := generateKeyFn(key)
	if result != 0 {
		return nil, ErrRngFailed
	}
	return key, nil
}

// ConstantTimeCompare compares two byte slices in constant time.
// Returns true if they are equal.
func ConstantTimeCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	result := C.crypto_constant_time_compare(
		(*C.uint8_t)(unsafe.Pointer(&a[0])),
		(*C.uint8_t)(unsafe.Pointer(&b[0])),
		C.size_t(len(a)),
	)
	return result == 1
}

func cOpaqueHash(token string) (string, error) {
	cToken := C.CString(token)
	defer C.free(unsafe.Pointer(cToken))
	return goStringResult(C.crypto_opaque_hash(cToken), ErrOpaqueFailed)
}

func cOpaqueValidate(token, expectedHash string) bool {
	cToken := C.CString(token)
	cHash := C.CString(expectedHash)
	defer C.free(unsafe.Pointer(cToken))
	defer C.free(unsafe.Pointer(cHash))
	return C.crypto_opaque_validate(cToken, cHash) == 1
}

func cOpaquePrefix(token string, length int) (string, error) {
	cToken := C.CString(token)
	defer C.free(unsafe.Pointer(cToken))
	return goStringResult(C.crypto_opaque_prefix(cToken, C.size_t(length)), ErrOpaqueFailed)
}

func cHMACSHA256Hex(secret, message []byte) (string, error) {
	result := C.crypto_hmac_sha256_hex(
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		byteSlicePtr(message),
		C.size_t(len(message)),
	)
	return goStringResult(result, ErrSignatureFailed)
}

func cSignEnvelope(kind string, secret, payload []byte, timestampUnix int64) (string, error) {
	cKind := C.CString(kind)
	defer C.free(unsafe.Pointer(cKind))
	result := C.crypto_sign_envelope(
		cKind,
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		C.int64_t(timestampUnix),
		byteSlicePtr(payload),
		C.size_t(len(payload)),
	)
	return goStringResult(result, ErrEnvelopeFailed)
}

func cVerifyEnvelope(kind string, secret, payload []byte, envelope string, maxAgeSeconds uint64, nowUnix int64) bool {
	cKind := C.CString(kind)
	cEnvelope := C.CString(envelope)
	defer C.free(unsafe.Pointer(cKind))
	defer C.free(unsafe.Pointer(cEnvelope))
	return C.crypto_verify_envelope(
		cKind,
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		byteSlicePtr(payload),
		C.size_t(len(payload)),
		cEnvelope,
		C.uint64_t(maxAgeSeconds),
		C.int64_t(nowUnix),
	) == 1
}

func cSignSessionCookie(secret []byte, token string, timestampUnix int64) (string, error) {
	cToken := C.CString(token)
	defer C.free(unsafe.Pointer(cToken))
	result := C.crypto_sign_session_cookie(
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		cToken,
		C.int64_t(timestampUnix),
	)
	return goStringResult(result, ErrSessionCookie)
}

func cVerifySessionCookie(secret []byte, signed string, maxAgeSeconds uint64, nowUnix int64) (string, error) {
	cSigned := C.CString(signed)
	defer C.free(unsafe.Pointer(cSigned))
	result := C.crypto_verify_session_cookie(
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		cSigned,
		C.uint64_t(maxAgeSeconds),
		C.int64_t(nowUnix),
	)
	return goStringResult(result, ErrSessionCookie)
}

func cGenerateInternalToken(secret []byte, moduleID string, timestampUnix int64) (string, error) {
	cModuleID := C.CString(moduleID)
	defer C.free(unsafe.Pointer(cModuleID))
	result := C.crypto_generate_internal_token(
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		cModuleID,
		C.int64_t(timestampUnix),
	)
	return goStringResult(result, ErrInternalToken)
}

func cVerifyInternalToken(secret []byte, token string, ttlSeconds uint64, nowUnix int64) (*parsedTokenResult, error) {
	cToken := C.CString(token)
	defer C.free(unsafe.Pointer(cToken))
	result := C.crypto_verify_internal_token(
		byteSlicePtr(secret),
		C.size_t(len(secret)),
		cToken,
		C.uint64_t(ttlSeconds),
		C.int64_t(nowUnix),
	)
	return goParsedTokenResult(result, ErrInternalToken)
}

func cPKCEChallenge(verifier, method string) (string, error) {
	cVerifier := C.CString(verifier)
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cVerifier))
	defer C.free(unsafe.Pointer(cMethod))
	return goStringResult(C.crypto_pkce_challenge(cVerifier, cMethod), ErrPKCEFailed)
}

func cPKCEVerify(verifier, challenge, method string) (bool, error) {
	cVerifier := C.CString(verifier)
	cChallenge := C.CString(challenge)
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cVerifier))
	defer C.free(unsafe.Pointer(cChallenge))
	defer C.free(unsafe.Pointer(cMethod))
	result := C.crypto_pkce_verify(cVerifier, cChallenge, cMethod)
	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	default:
		return false, ErrPKCEFailed
	}
}
