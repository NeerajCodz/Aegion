#include <cstdarg>
#include <cstdint>
#include <cstdlib>
#include <ostream>
#include <new>

/// Key pair result
struct KeyPairResult {
  int error_code;
  char *algorithm;
  char *key_id;
  uint8_t *private_key;
  size_t private_key_len;
  uint8_t *public_key;
  size_t public_key_len;
};

/// Result structure for string operations
struct JwtResult {
  int error_code;
  char *result;
};

/// Claims result (after verification)
struct ClaimsResult {
  int error_code;
  /// JSON-encoded claims
  char *claims_json;
  /// Key ID from header (nullable)
  char *key_id;
};

extern "C" {

/// Generate an EC P-256 key pair for ES256 signing
///
/// # Safety
/// - `key_id` must be a valid null-terminated C string
/// - All output pointers in KeyPairResult must be freed with appropriate free functions
KeyPairResult jwt_generate_ec_keypair(const char *key_id);

/// Sign a JWT with the given claims
///
/// # Safety
/// - `claims_json` must be valid JSON
/// - `algorithm` must be "ES256" or "EdDSA"
/// - `private_key` and `private_key_len` must be valid
/// - `key_id` can be null
JwtResult jwt_sign(const char *claims_json,
                   const char *algorithm,
                   const uint8_t *private_key,
                   size_t private_key_len,
                   const char *key_id);

/// Verify a JWT and return the claims
///
/// # Safety
/// - All string parameters must be valid null-terminated C strings
/// - `public_key` and `public_key_len` must be valid
ClaimsResult jwt_verify(const char *token,
                        const char *algorithm,
                        const uint8_t *public_key,
                        size_t public_key_len,
                        const char *issuer,
                        const char *audience,
                        uint64_t leeway);

/// Convert a public key to JWK JSON format
///
/// # Safety
/// - All parameters must be valid
JwtResult jwt_to_jwk(const char *algorithm,
                     const char *key_id,
                     const uint8_t *public_key,
                     size_t public_key_len);

/// Free a string returned by JWT functions
void jwt_free_string(char *s);

/// Free bytes returned by JWT functions
void jwt_free_bytes(uint8_t *data, size_t len);

/// Free a KeyPairResult
void jwt_free_keypair(KeyPairResult result);

/// Free a ClaimsResult
void jwt_free_claims(ClaimsResult result);

}  // extern "C"
