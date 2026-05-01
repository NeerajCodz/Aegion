//! Password hashing using Argon2id
//!
//! Implements OWASP-recommended parameters for Argon2id:
//! - Memory: 64 MiB (adjustable based on environment)
//! - Iterations: 3
//! - Parallelism: 4

use crate::error::CryptoError;
use argon2::{
    password_hash::{rand_core::OsRng, PasswordHash, PasswordHasher, PasswordVerifier, SaltString},
    Algorithm, Argon2, Params, Version,
};

/// Default Argon2id parameters (OWASP recommended for moderate security)
const DEFAULT_MEMORY_KIB: u32 = 65536; // 64 MiB
const DEFAULT_ITERATIONS: u32 = 3;
const DEFAULT_PARALLELISM: u32 = 4;
const DEFAULT_OUTPUT_LEN: usize = 32;

/// Hash a password using Argon2id with secure defaults
///
/// Returns the encoded hash string in PHC format:
/// `$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`
///
/// # Arguments
/// * `password` - The plaintext password to hash
///
/// # Returns
/// * `Ok(String)` - The PHC-encoded hash string
/// * `Err(CryptoError)` - If hashing fails
pub fn hash_password(password: &str) -> Result<String, CryptoError> {
    hash_password_with_params(
        password,
        DEFAULT_MEMORY_KIB,
        DEFAULT_ITERATIONS,
        DEFAULT_PARALLELISM,
    )
}

/// Hash a password with custom Argon2id parameters
///
/// # Arguments
/// * `password` - The plaintext password to hash
/// * `memory_kib` - Memory cost in KiB (e.g., 65536 for 64 MiB)
/// * `iterations` - Time cost (number of iterations)
/// * `parallelism` - Degree of parallelism
pub fn hash_password_with_params(
    password: &str,
    memory_kib: u32,
    iterations: u32,
    parallelism: u32,
) -> Result<String, CryptoError> {
    let salt = SaltString::generate(&mut OsRng);

    let params = Params::new(
        memory_kib,
        iterations,
        parallelism,
        Some(DEFAULT_OUTPUT_LEN),
    )
    .map_err(|e| CryptoError::HashError(e.to_string()))?;

    let argon2 = Argon2::new(Algorithm::Argon2id, Version::V0x13, params);

    argon2
        .hash_password(password.as_bytes(), &salt)
        .map(|h| h.to_string())
        .map_err(|e| CryptoError::HashError(e.to_string()))
}

/// Verify a password against an Argon2id hash
///
/// This function performs constant-time comparison internally.
///
/// # Arguments
/// * `password` - The plaintext password to verify
/// * `hash` - The PHC-encoded hash string to verify against
///
/// # Returns
/// * `Ok(true)` - Password matches
/// * `Ok(false)` - Password does not match
/// * `Err(CryptoError)` - If the hash is malformed
pub fn verify_password(password: &str, hash: &str) -> Result<bool, CryptoError> {
    let parsed_hash = PasswordHash::new(hash)
        .map_err(|e| CryptoError::HashError(format!("invalid hash format: {}", e)))?;

    // Extract params from the hash to use the same algorithm settings
    let argon2 = Argon2::default();

    match argon2.verify_password(password.as_bytes(), &parsed_hash) {
        Ok(()) => Ok(true),
        Err(argon2::password_hash::Error::Password) => Ok(false),
        Err(e) => Err(CryptoError::HashError(e.to_string())),
    }
}

/// Check if a hash needs rehashing (parameters differ from current defaults)
///
/// Returns true if the hash was created with weaker parameters and should
/// be updated after successful verification.
pub fn needs_rehash(hash: &str) -> Result<bool, CryptoError> {
    let parsed = PasswordHash::new(hash)
        .map_err(|e| CryptoError::HashError(format!("invalid hash format: {}", e)))?;

    // Check algorithm
    if parsed.algorithm != argon2::ARGON2ID_IDENT {
        return Ok(true);
    }

    // Check version
    if parsed.version != Some(Version::V0x13.into()) {
        return Ok(true);
    }

    // Check params by parsing the hash string directly
    // PHC format: $argon2id$v=19$m=65536,t=3,p=4$salt$hash
    let hash_str = hash;

    // Extract memory, time, parallelism from params section
    let parts: Vec<&str> = hash_str.split('$').collect();
    if parts.len() >= 4 {
        let params_str = parts[3]; // e.g., "m=65536,t=3,p=4"
        let mut m: u32 = 0;
        let mut t: u32 = 0;
        let mut p: u32 = 0;

        for param in params_str.split(',') {
            if let Some(val) = param.strip_prefix("m=") {
                m = val.parse().unwrap_or(0);
            } else if let Some(val) = param.strip_prefix("t=") {
                t = val.parse().unwrap_or(0);
            } else if let Some(val) = param.strip_prefix("p=") {
                p = val.parse().unwrap_or(0);
            }
        }

        if m < DEFAULT_MEMORY_KIB || t < DEFAULT_ITERATIONS || p < DEFAULT_PARALLELISM {
            return Ok(true);
        }
    }

    Ok(false)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_hash_and_verify() {
        let password = "correct horse battery staple";
        let hash = hash_password(password).unwrap();

        // Should start with argon2id identifier
        assert!(hash.starts_with("$argon2id$"));

        // Correct password should verify
        assert!(verify_password(password, &hash).unwrap());

        // Wrong password should not verify
        assert!(!verify_password("wrong password", &hash).unwrap());
    }

    #[test]
    fn test_custom_params() {
        let password = "test123";
        // Use lower params for faster test
        let hash = hash_password_with_params(password, 4096, 1, 1).unwrap();

        assert!(verify_password(password, &hash).unwrap());
    }

    #[test]
    fn test_invalid_hash_format() {
        let result = verify_password("test", "not-a-valid-hash");
        assert!(result.is_err());
    }

    #[test]
    fn test_different_hashes_for_same_password() {
        let password = "same-password";
        let hash1 = hash_password(password).unwrap();
        let hash2 = hash_password(password).unwrap();

        // Different salts should produce different hashes
        assert_ne!(hash1, hash2);

        // But both should verify
        assert!(verify_password(password, &hash1).unwrap());
        assert!(verify_password(password, &hash2).unwrap());
    }

    #[test]
    fn test_needs_rehash_weak_params() {
        // Hash with weak params
        let hash = hash_password_with_params("test", 4096, 1, 1).unwrap();
        assert!(needs_rehash(&hash).unwrap());

        // Hash with current defaults should not need rehash
        let strong_hash = hash_password("test").unwrap();
        assert!(!needs_rehash(&strong_hash).unwrap());
    }

    #[test]
    fn test_empty_password() {
        let empty_password = "";
        let hash = hash_password(empty_password).unwrap();

        assert!(hash.starts_with("$argon2id$"));
        assert!(verify_password("", &hash).unwrap());
        assert!(!verify_password("not empty", &hash).unwrap());
    }

    #[test]
    fn test_unicode_password() {
        let unicode_password = "café🔐汉字パスワード";
        let hash = hash_password(unicode_password).unwrap();

        assert!(verify_password(unicode_password, &hash).unwrap());
        assert!(!verify_password("cafe", &hash).unwrap());
    }

    #[test]
    fn test_very_long_password() {
        // Create a 1KB password
        let long_password = "a".repeat(1024);
        let hash = hash_password(&long_password).unwrap();

        assert!(verify_password(&long_password, &hash).unwrap());
        assert!(!verify_password("short", &hash).unwrap());
    }

    #[test]
    fn test_needs_rehash_different_algorithm() {
        // Create a hash string that looks like it's using a different algorithm
        // This is a simplified test - in reality we'd need a real bcrypt hash
        let fake_bcrypt_hash = "$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW";

        // This should fail to parse rather than indicate rehash needed
        let result = needs_rehash(fake_bcrypt_hash);
        assert!(result.is_err()); // Should error on invalid format

        // Test with malformed argon2id hash
        let malformed = "$argon2id$v=19$invalid$salt$hash";
        let result = needs_rehash(malformed);
        // Should either error or indicate rehash needed
        assert!(result.is_err() || result.unwrap());
    }

    #[test]
    fn test_needs_rehash_edge_cases() {
        // Test with empty string
        let result = needs_rehash("");
        assert!(result.is_err());

        // Test with invalid format
        let result = needs_rehash("not-a-hash");
        assert!(result.is_err());

        // Test with minimal but weak params (below defaults)
        let weak_hash = hash_password_with_params("test", 1024, 1, 1).unwrap();
        assert!(needs_rehash(&weak_hash).unwrap());
    }
}
