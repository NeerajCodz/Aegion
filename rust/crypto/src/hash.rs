//! Password hashing using Argon2id
//!
//! Implements OWASP-recommended parameters for Argon2id:
//! - Memory: 64 MiB (adjustable based on environment)
//! - Iterations: 3
//! - Parallelism: 4

use argon2::{
    Argon2, Algorithm, Version, Params,
    password_hash::{
        rand_core::OsRng,
        PasswordHash, PasswordHasher, PasswordVerifier, SaltString,
    },
};
use crate::error::CryptoError;

/// Default Argon2id parameters (OWASP recommended for moderate security)
const DEFAULT_MEMORY_KIB: u32 = 65536;  // 64 MiB
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
    ).map_err(|e| CryptoError::HashError(e.to_string()))?;
    
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
    
    // Check params
    if let Some(params) = parsed.params().ok() {
        let m = params.get_decimal("m").unwrap_or(0);
        let t = params.get_decimal("t").unwrap_or(0);
        let p = params.get_decimal("p").unwrap_or(0);
        
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
}
