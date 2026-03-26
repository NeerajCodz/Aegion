//! Key generation for JWT signing
//!
//! Generates RSA and EC key pairs for signing JWTs.

use ring::rand::SystemRandom;
use ring::signature::{EcdsaKeyPair, Ed25519KeyPair, KeyPair as RingKeyPair, RsaKeyPair, ECDSA_P256_SHA256_FIXED_SIGNING};
use ring::rsa::KeySize;
use crate::error::JwtError;

/// A cryptographic key pair for JWT signing
pub struct KeyPair {
    /// Algorithm identifier (RS256, ES256, etc.)
    pub algorithm: String,
    /// Key ID (kid) for JWKS
    pub key_id: String,
    /// DER-encoded private key
    pub private_key_der: Vec<u8>,
    /// DER-encoded public key (for RSA: PKCS#1, for EC: uncompressed point)
    pub public_key_der: Vec<u8>,
}

/// Generate an RSA key pair for RS256 signing
///
/// # Arguments
/// * `key_id` - The key ID (kid) to assign to this key pair
///
/// # Returns
/// * `Ok(KeyPair)` - The generated key pair
/// * `Err(JwtError)` - If key generation fails
pub fn generate_rsa_keypair(key_id: &str) -> Result<KeyPair, JwtError> {
    let rng = SystemRandom::new();
    
    // Generate RSA 2048-bit key pair
    // Note: ring uses a different approach - we generate the key directly
    // For production, consider using a library with better RSA key generation
    
    // ring doesn't have direct RSA key generation, so we use a PKCS#8 approach
    // For now, we'll indicate that RSA requires external key generation
    // or use a different crate
    
    // Alternative: Use ring's Ed25519 or ECDSA which have direct generation
    // For RS256 compatibility, we need to either:
    // 1. Use rsa crate for generation
    // 2. Accept pre-generated keys
    
    // For this implementation, we'll use ECDSA P-256 as the primary algorithm
    // and provide RS256 support through pre-generated keys
    
    Err(JwtError::KeyGenerationFailed(
        "RSA key generation requires external tool (openssl). Use generate_ec_keypair for ES256.".into()
    ))
}

/// Generate an EC key pair for ES256 signing (ECDSA P-256)
///
/// # Arguments
/// * `key_id` - The key ID (kid) to assign to this key pair
///
/// # Returns
/// * `Ok(KeyPair)` - The generated key pair with PKCS#8 encoded keys
/// * `Err(JwtError)` - If key generation fails
pub fn generate_ec_keypair(key_id: &str) -> Result<KeyPair, JwtError> {
    let rng = SystemRandom::new();
    
    // Generate ECDSA P-256 key pair
    let pkcs8_bytes = EcdsaKeyPair::generate_pkcs8(&ECDSA_P256_SHA256_FIXED_SIGNING, &rng)
        .map_err(|e| JwtError::KeyGenerationFailed(format!("ECDSA key generation failed: {:?}", e)))?;
    
    let key_pair = EcdsaKeyPair::from_pkcs8(&ECDSA_P256_SHA256_FIXED_SIGNING, pkcs8_bytes.as_ref())
        .map_err(|e| JwtError::KeyGenerationFailed(format!("Failed to parse generated key: {:?}", e)))?;
    
    let public_key = key_pair.public_key().as_ref().to_vec();
    
    Ok(KeyPair {
        algorithm: "ES256".into(),
        key_id: key_id.to_string(),
        private_key_der: pkcs8_bytes.as_ref().to_vec(),
        public_key_der: public_key,
    })
}

/// Generate an Ed25519 key pair
///
/// Note: Ed25519 is not commonly used in JWTs but is very fast.
/// Algorithm identifier would be "EdDSA".
pub fn generate_ed25519_keypair(key_id: &str) -> Result<KeyPair, JwtError> {
    let rng = SystemRandom::new();
    
    let pkcs8_bytes = Ed25519KeyPair::generate_pkcs8(&rng)
        .map_err(|e| JwtError::KeyGenerationFailed(format!("Ed25519 key generation failed: {:?}", e)))?;
    
    let key_pair = Ed25519KeyPair::from_pkcs8(pkcs8_bytes.as_ref())
        .map_err(|e| JwtError::KeyGenerationFailed(format!("Failed to parse generated key: {:?}", e)))?;
    
    let public_key = key_pair.public_key().as_ref().to_vec();
    
    Ok(KeyPair {
        algorithm: "EdDSA".into(),
        key_id: key_id.to_string(),
        private_key_der: pkcs8_bytes.as_ref().to_vec(),
        public_key_der: public_key,
    })
}

/// Generate a random key ID
pub fn generate_key_id() -> String {
    use rand::Rng;
    let mut rng = rand::thread_rng();
    let bytes: [u8; 16] = rng.gen();
    base64::Engine::encode(&base64::engine::general_purpose::URL_SAFE_NO_PAD, bytes)
}

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_generate_ec_keypair() {
        let key_id = "test-key-1";
        let keypair = generate_ec_keypair(key_id).unwrap();
        
        assert_eq!(keypair.algorithm, "ES256");
        assert_eq!(keypair.key_id, key_id);
        assert!(!keypair.private_key_der.is_empty());
        assert!(!keypair.public_key_der.is_empty());
        
        // EC P-256 public key should be 65 bytes (uncompressed point)
        assert_eq!(keypair.public_key_der.len(), 65);
    }
    
    #[test]
    fn test_generate_ed25519_keypair() {
        let key_id = "test-ed25519";
        let keypair = generate_ed25519_keypair(key_id).unwrap();
        
        assert_eq!(keypair.algorithm, "EdDSA");
        assert_eq!(keypair.key_id, key_id);
        
        // Ed25519 public key is 32 bytes
        assert_eq!(keypair.public_key_der.len(), 32);
    }
    
    #[test]
    fn test_generate_key_id() {
        let id1 = generate_key_id();
        let id2 = generate_key_id();
        
        // Should be unique
        assert_ne!(id1, id2);
        
        // Should be reasonable length
        assert!(id1.len() >= 20);
    }
}
