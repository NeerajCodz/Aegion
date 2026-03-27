#include <cstdarg>
#include <cstdint>
#include <cstdlib>
#include <ostream>
#include <new>

/// Key size for XChaCha20-Poly1305 (256 bits)
constexpr static const uintptr_t KEY_SIZE = 32;

/// Result structure for functions that return strings
struct CryptoResult {
  /// 0 on success, negative error code on failure
  int error_code;
  /// Null-terminated result string (caller must free with crypto_free_string)
  char *result;
};

/// Result structure for functions that return bytes
struct BytesResult {
  /// 0 on success, negative error code on failure
  int error_code;
  /// Result bytes (caller must free with crypto_free_bytes)
  uint8_t *data;
  /// Length of the data
  size_t len;
};

extern "C" {

/// Hash a password using Argon2id
///
/// # Safety
/// - `password` must be a valid null-terminated C string
/// - The returned `CryptoResult.result` must be freed with `crypto_free_string`
CryptoResult crypto_hash_password(const char *password);

/// Verify a password against an Argon2id hash
///
/// # Safety
/// - `password` must be a valid null-terminated C string
/// - `hash` must be a valid null-terminated C string
///
/// # Returns
/// - 1 if password matches
/// - 0 if password does not match
/// - Negative error code on failure
int crypto_verify_password(const char *password, const char *hash);

/// Encrypt a field with XChaCha20-Poly1305
///
/// # Safety
/// - `key` must point to exactly 32 bytes
/// - `plaintext` and `plaintext_len` must be valid
/// - `aad` can be null if `aad_len` is 0
/// - The returned `CryptoResult.result` must be freed with `crypto_free_string`
CryptoResult crypto_encrypt_field(const uint8_t *key,
                                  const uint8_t *plaintext,
                                  size_t plaintext_len,
                                  const uint8_t *aad,
                                  size_t aad_len);

/// Decrypt a field with XChaCha20-Poly1305
///
/// # Safety
/// - `key` must point to exactly 32 bytes
/// - `ciphertext` must be a valid null-terminated C string (base64)
/// - `aad` can be null if `aad_len` is 0
/// - The returned `BytesResult.data` must be freed with `crypto_free_bytes`
BytesResult crypto_decrypt_field(const uint8_t *key,
                                 const char *ciphertext,
                                 const uint8_t *aad,
                                 size_t aad_len);

/// Generate a random 32-byte encryption key
///
/// # Safety
/// - `out` must point to a buffer of at least 32 bytes
///
/// # Returns
/// - 0 on success
/// - Negative error code on failure
int crypto_generate_key(uint8_t *out);

/// Constant-time byte comparison
///
/// # Safety
/// - `a` and `b` must point to valid memory of at least `len` bytes
///
/// # Returns
/// - 1 if equal
/// - 0 if not equal
int crypto_constant_time_compare(const uint8_t *a, const uint8_t *b, size_t len);

/// Free a string returned by crypto functions
///
/// # Safety
/// - `s` must be a string returned by a crypto function, or null
void crypto_free_string(char *s);

/// Free bytes returned by crypto functions
///
/// # Safety
/// - `data` must be bytes returned by a crypto function, or null
/// - `len` must match the original length
void crypto_free_bytes(uint8_t *data, size_t len);

}  // extern "C"
