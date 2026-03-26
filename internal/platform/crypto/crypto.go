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
#cgo LDFLAGS: -L${SRCDIR}/../../../rust/target/release -laegion_crypto -ldl -lm
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

extern CryptoResult crypto_hash_password(const char* password);
extern int crypto_verify_password(const char* password, const char* hash);
extern CryptoResult crypto_encrypt_field(const uint8_t* key, const uint8_t* plaintext, size_t plaintext_len, const uint8_t* aad, size_t aad_len);
extern BytesResult crypto_decrypt_field(const uint8_t* key, const char* ciphertext, const uint8_t* aad, size_t aad_len);
extern int crypto_generate_key(uint8_t* out);
extern int crypto_constant_time_compare(const uint8_t* a, const uint8_t* b, size_t len);
extern void crypto_free_string(char* s);
extern void crypto_free_bytes(uint8_t* data, size_t len);
*/
import "C"
import (
	"errors"
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
)

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

	var aadPtr *C.uint8_t
	var aadLen C.size_t
	if len(aad) > 0 {
		aadPtr = (*C.uint8_t)(unsafe.Pointer(&aad[0]))
		aadLen = C.size_t(len(aad))
	}

	result := C.crypto_encrypt_field(
		(*C.uint8_t)(unsafe.Pointer(&key[0])),
		(*C.uint8_t)(unsafe.Pointer(&plaintext[0])),
		C.size_t(len(plaintext)),
		aadPtr,
		aadLen,
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

	var aadPtr *C.uint8_t
	var aadLen C.size_t
	if len(aad) > 0 {
		aadPtr = (*C.uint8_t)(unsafe.Pointer(&aad[0]))
		aadLen = C.size_t(len(aad))
	}

	result := C.crypto_decrypt_field(
		(*C.uint8_t)(unsafe.Pointer(&key[0])),
		cCiphertext,
		aadPtr,
		aadLen,
	)
	if result.error_code != 0 {
		return nil, ErrDecryptFailed
	}
	defer C.crypto_free_bytes(result.data, result.len)

	out := make([]byte, result.len)
	copy(out, (*[1 << 30]byte)(unsafe.Pointer(result.data))[:result.len:result.len])
	return out, nil
}

// GenerateKey generates a cryptographically secure random 32-byte key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	result := C.crypto_generate_key((*C.uint8_t)(unsafe.Pointer(&key[0])))
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
