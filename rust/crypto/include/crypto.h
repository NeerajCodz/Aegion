#include <cstdarg>
#include <cstdint>
#include <cstdlib>
#include <ostream>
#include <new>

/// Key size for XChaCha20-Poly1305 (256 bits)
constexpr static const uintptr_t KEY_SIZE = 32;

struct CryptoResult {
  int error_code;
  char *result;
};

struct BytesResult {
  int error_code;
  uint8_t *data;
  size_t len;
};

struct ParsedTokenResult {
  int error_code;
  char *module_id;
  int64_t timestamp;
  char *signature_hex;
};

extern "C" {

CryptoResult crypto_hash_password(const char *password);

int crypto_verify_password(const char *password, const char *hash);

CryptoResult crypto_encrypt_field(const uint8_t *key,
                                  const uint8_t *plaintext,
                                  size_t plaintext_len,
                                  const uint8_t *aad,
                                  size_t aad_len);

BytesResult crypto_decrypt_field(const uint8_t *key,
                                 const char *ciphertext,
                                 const uint8_t *aad,
                                 size_t aad_len);

int crypto_generate_key(uint8_t *out);

int crypto_constant_time_compare(const uint8_t *a, const uint8_t *b, size_t len);

CryptoResult crypto_opaque_hash(const char *token);

int crypto_opaque_validate(const char *token, const char *expected_hash);

CryptoResult crypto_opaque_prefix(const char *token, size_t length);

CryptoResult crypto_hmac_sha256_hex(const uint8_t *secret,
                                    size_t secret_len,
                                    const uint8_t *message,
                                    size_t message_len);

CryptoResult crypto_sign_envelope(const char *kind,
                                  const uint8_t *secret,
                                  size_t secret_len,
                                  int64_t timestamp,
                                  const uint8_t *payload,
                                  size_t payload_len);

int crypto_verify_envelope(const char *kind,
                           const uint8_t *secret,
                           size_t secret_len,
                           const uint8_t *payload,
                           size_t payload_len,
                           const char *envelope,
                           uint64_t max_age_seconds,
                           int64_t now_unix);

CryptoResult crypto_sign_session_cookie(const uint8_t *secret,
                                        size_t secret_len,
                                        const char *token,
                                        int64_t timestamp);

CryptoResult crypto_verify_session_cookie(const uint8_t *secret,
                                          size_t secret_len,
                                          const char *signed_,
                                          uint64_t max_age_seconds,
                                          int64_t now_unix);

CryptoResult crypto_generate_internal_token(const uint8_t *secret,
                                            size_t secret_len,
                                            const char *module_id,
                                            int64_t timestamp);

ParsedTokenResult crypto_verify_internal_token(const uint8_t *secret,
                                               size_t secret_len,
                                               const char *token,
                                               uint64_t ttl_seconds,
                                               int64_t now_unix);

CryptoResult crypto_pkce_challenge(const char *verifier, const char *method);

int crypto_pkce_verify(const char *verifier, const char *challenge, const char *method);

void crypto_free_string(char *s);

void crypto_free_bytes(uint8_t *data, size_t len);

void crypto_free_parsed_token(ParsedTokenResult result);

}  // extern "C"
