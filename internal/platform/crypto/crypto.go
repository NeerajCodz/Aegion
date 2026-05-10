// Package crypto provides Go-native cryptographic primitives for Aegion.
package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// KeySize is the required size for encryption keys (32 bytes / 256 bits).
const KeySize = 32

const (
	argonMemoryKiB   = 65536
	argonIterations  = 3
	argonParallelism = 4
	argonSaltSize    = 16
	argonOutputLen   = 32

	envelopeVersion     = "v1"
	sessionCookieVer    = "v1"
	internalTokenKind   = "internal_token"
	internalTokenVer    = "v1"
	encryptedNonceBytes = chacha20poly1305.NonceSizeX
	encryptedTagBytes   = 16
)

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
	if _, err := rand.Read(key); err != nil {
		return -1
	}
	return 0
}

type parsedTokenResult struct {
	ModuleID      string
	TimestampUnix int64
	SignatureHex  string
}

// HashPassword hashes a password using Argon2id with secure defaults.
// Returns the PHC-encoded hash string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrHashFailed
	}

	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, argonOutputLen)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemoryKiB,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies a password against an Argon2id hash.
// Returns true if the password matches, false otherwise.
func VerifyPassword(password, hash string) (bool, error) {
	params, salt, expected, err := parsePHC(hash)
	if err != nil {
		return false, ErrVerifyFailed
	}
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memoryKiB, params.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
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

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", ErrEncryptFailed
	}
	nonce := make([]byte, encryptedNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrEncryptFailed
	}

	out := make([]byte, 0, len(nonce)+len(plaintext)+encryptedTagBytes)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptField decrypts ciphertext encrypted with EncryptField.
// The key and AAD must match those used for encryption.
func DecryptField(key []byte, ciphertext string, aad []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeyLength
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil || len(data) < encryptedNonceBytes+encryptedTagBytes {
		return nil, ErrDecryptFailed
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	nonce, encrypted := data[:encryptedNonceBytes], data[encryptedNonceBytes:]
	plaintext, err := aead.Open(nil, nonce, encrypted, aad)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// GenerateKey generates a cryptographically secure random 32-byte key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if generateKeyFn(key) != 0 {
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
	return subtle.ConstantTimeCompare(a, b) == 1
}

type phcParams struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
}

func parsePHC(hash string) (phcParams, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return phcParams{}, nil, nil, ErrVerifyFailed
	}

	params := phcParams{}
	for _, part := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return phcParams{}, nil, nil, ErrVerifyFailed
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || parsed == 0 {
			return phcParams{}, nil, nil, ErrVerifyFailed
		}
		switch key {
		case "m":
			params.memoryKiB = uint32(parsed)
		case "t":
			params.iterations = uint32(parsed)
		case "p":
			if parsed > 255 {
				return phcParams{}, nil, nil, ErrVerifyFailed
			}
			params.parallelism = uint8(parsed)
		default:
			return phcParams{}, nil, nil, ErrVerifyFailed
		}
	}
	if params.memoryKiB == 0 || params.iterations == 0 || params.parallelism == 0 {
		return phcParams{}, nil, nil, ErrVerifyFailed
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return phcParams{}, nil, nil, ErrVerifyFailed
	}
	digest, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(digest) == 0 {
		return phcParams{}, nil, nil, ErrVerifyFailed
	}
	return params, salt, digest, nil
}

func cOpaqueHash(token string) (string, error) {
	sum := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

func cOpaqueValidate(token, expectedHash string) bool {
	actual, err := cOpaqueHash(token)
	if err != nil {
		return false
	}
	return ConstantTimeCompare([]byte(actual), []byte(expectedHash))
}

func cOpaquePrefix(token string, length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	for i := range token {
		if length == 0 {
			return token[:i], nil
		}
		length--
	}
	return token, nil
}

func cHMACSHA256Hex(secret, message []byte) (string, error) {
	if len(secret) == 0 {
		return "", ErrSignatureFailed
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func cSignEnvelope(kind string, secret, payload []byte, timestampUnix int64) (string, error) {
	signature, err := cHMACSHA256Hex(secret, envelopeMessage(kind, timestampUnix, payload))
	if err != nil {
		return "", ErrEnvelopeFailed
	}
	return fmt.Sprintf("%s;t=%d;s=%s", envelopeVersion, timestampUnix, signature), nil
}

func cVerifyEnvelope(kind string, secret, payload []byte, envelope string, maxAgeSeconds uint64, nowUnix int64) bool {
	timestamp, signature, err := parseEnvelope(envelope)
	if err != nil {
		return false
	}
	if maxAgeSeconds > 0 {
		if timestamp > nowUnix || nowUnix-timestamp > int64(maxAgeSeconds) {
			return false
		}
	}
	return verifyHMACHex(secret, envelopeMessage(kind, timestamp, payload), signature)
}

func envelopeMessage(kind string, timestamp int64, payload []byte) []byte {
	out := make([]byte, 0, len(kind)+len(payload)+32)
	out = append(out, envelopeVersion...)
	out = append(out, '\n')
	out = append(out, kind...)
	out = append(out, '\n')
	out = strconv.AppendInt(out, timestamp, 10)
	out = append(out, '\n')
	out = append(out, payload...)
	return out
}

func parseEnvelope(envelope string) (int64, string, error) {
	parts := strings.Split(envelope, ";")
	if len(parts) != 3 || parts[0] != envelopeVersion {
		return 0, "", ErrInvalidSignature
	}
	timestampText, ok := strings.CutPrefix(parts[1], "t=")
	if !ok {
		return 0, "", ErrInvalidSignature
	}
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidSignature
	}
	signature, ok := strings.CutPrefix(parts[2], "s=")
	if !ok || signature == "" {
		return 0, "", ErrInvalidSignature
	}
	return timestamp, signature, nil
}

func cSignSessionCookie(secret []byte, token string, timestampUnix int64) (string, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(token))
	signature, err := cHMACSHA256Hex(secret, sessionCookieMessage(encoded, timestampUnix))
	if err != nil {
		return "", ErrSessionCookie
	}
	return fmt.Sprintf("%s.%s.%d.%s", sessionCookieVer, encoded, timestampUnix, signature), nil
}

func cVerifySessionCookie(secret []byte, signed string, maxAgeSeconds uint64, nowUnix int64) (string, error) {
	parts := strings.Split(signed, ".")
	if len(parts) != 4 || parts[0] != sessionCookieVer {
		return "", ErrInvalidSignature
	}
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", ErrInvalidSignature
	}
	if maxAgeSeconds > 0 {
		if timestamp > nowUnix {
			return "", ErrInvalidSignature
		}
		if nowUnix-timestamp > int64(maxAgeSeconds) {
			return "", ErrExpired
		}
	}
	if !verifyHMACHex(secret, sessionCookieMessage(parts[1], timestamp), parts[3]) {
		return "", ErrInvalidSignature
	}
	token, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrSessionCookie
	}
	return string(token), nil
}

func sessionCookieMessage(encodedToken string, timestamp int64) []byte {
	out := make([]byte, 0, len(encodedToken)+48)
	out = append(out, sessionCookieKind...)
	out = append(out, '\n')
	out = append(out, sessionCookieVer...)
	out = append(out, '\n')
	out = strconv.AppendInt(out, timestamp, 10)
	out = append(out, '\n')
	out = append(out, encodedToken...)
	return out
}

func cGenerateInternalToken(secret []byte, moduleID string, timestampUnix int64) (string, error) {
	if strings.TrimSpace(moduleID) == "" {
		return "", ErrInternalToken
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(moduleID))
	signature, err := cHMACSHA256Hex(secret, internalTokenMessage(encoded, timestampUnix))
	if err != nil {
		return "", ErrInternalToken
	}
	return fmt.Sprintf("%s.%s.%d.%s", internalTokenVer, encoded, timestampUnix, signature), nil
}

func cVerifyInternalToken(secret []byte, token string, ttlMillis uint64, nowUnix int64) (*parsedTokenResult, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != internalTokenVer {
		return nil, ErrInvalidSignature
	}
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrInvalidSignature
	}
	if timestamp > nowUnix {
		return nil, ErrInvalidSignature
	}
	if ttlMillis > 0 && nowUnix-timestamp > int64(ttlMillis) {
		return nil, ErrExpired
	}
	if !verifyHMACHex(secret, internalTokenMessage(parts[1], timestamp), parts[3]) {
		return nil, ErrInvalidSignature
	}
	moduleID, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInternalToken
	}
	return &parsedTokenResult{
		ModuleID:      string(moduleID),
		TimestampUnix: timestamp,
		SignatureHex:  parts[3],
	}, nil
}

func internalTokenMessage(encodedModuleID string, timestamp int64) []byte {
	out := make([]byte, 0, len(encodedModuleID)+48)
	out = append(out, internalTokenKind...)
	out = append(out, '\n')
	out = append(out, internalTokenVer...)
	out = append(out, '\n')
	out = strconv.AppendInt(out, timestamp, 10)
	out = append(out, '\n')
	out = append(out, encodedModuleID...)
	return out
}

func cPKCEChallenge(verifier, method string) (string, error) {
	normalized := normalizePKCEMethod(method)
	switch normalized {
	case "plain":
		return verifier, nil
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]), nil
	default:
		return "", ErrPKCEFailed
	}
}

func cPKCEVerify(verifier, challenge, method string) (bool, error) {
	computed, err := cPKCEChallenge(verifier, method)
	if err != nil {
		return false, err
	}
	return ConstantTimeCompare([]byte(computed), []byte(challenge)), nil
}

func normalizePKCEMethod(method string) string {
	if strings.TrimSpace(method) == "" || strings.EqualFold(method, "plain") {
		return "plain"
	}
	if strings.EqualFold(method, "s256") {
		return "S256"
	}
	return method
}

func verifyHMACHex(secret, message []byte, signatureHex string) bool {
	provided, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	expectedHex, err := cHMACSHA256Hex(secret, message)
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, provided)
}
