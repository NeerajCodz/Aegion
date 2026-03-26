//! Key generation for JWT signing
//!
//! Generates RSA and EC key pairs for signing JWTs.

use crate::error::JwtError;
use ring::rand::SystemRandom;
use ring::signature::{
    EcdsaKeyPair, Ed25519KeyPair, KeyPair as RingKeyPair, ECDSA_P256_SHA256_FIXED_SIGNING,
};

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
/// * `_key_id` - The key ID (kid) to assign to this key pair
///
/// # Returns
/// * `Ok(KeyPair)` - The generated key pair
/// * `Err(JwtError)` - If key generation fails
///
/// Note: ring doesn't support direct RSA key generation. Use ES256 or EdDSA instead,
/// or provide pre-generated RSA keys.
pub fn generate_rsa_keypair(_key_id: &str) -> Result<KeyPair, JwtError> {
    Err(JwtError::KeyGenerationFailed(
        "RSA key generation requires external tool (openssl). Use generate_ec_keypair for ES256."
            .into(),
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
        .map_err(|e| {
            JwtError::KeyGenerationFailed(format!("ECDSA key generation failed: {:?}", e))
        })?;

    let key_pair =
        EcdsaKeyPair::from_pkcs8(&ECDSA_P256_SHA256_FIXED_SIGNING, pkcs8_bytes.as_ref(), &rng)
            .map_err(|e| {
                JwtError::KeyGenerationFailed(format!("Failed to parse generated key: {:?}", e))
            })?;

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

    let pkcs8_bytes = Ed25519KeyPair::generate_pkcs8(&rng).map_err(|e| {
        JwtError::KeyGenerationFailed(format!("Ed25519 key generation failed: {:?}", e))
    })?;

    let key_pair = Ed25519KeyPair::from_pkcs8(pkcs8_bytes.as_ref()).map_err(|e| {
        JwtError::KeyGenerationFailed(format!("Failed to parse generated key: {:?}", e))
    })?;

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
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;
    use base64::Engine;
    use ring::rand::{SecureRandom, SystemRandom};

    let rng = SystemRandom::new();
    let mut bytes = [0u8; 16];
    rng.fill(&mut bytes)
        .expect("Failed to generate random bytes");
    URL_SAFE_NO_PAD.encode(bytes)
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

    #[test]
    fn test_generate_multiple_keys() {
        // Generate multiple EC keys and ensure they're all unique
        let key1 = generate_ec_keypair("key1").unwrap();
        let key2 = generate_ec_keypair("key2").unwrap();
        let key3 = generate_ec_keypair("key3").unwrap();
        
        // Keys should be different
        assert_ne!(key1.private_key_der, key2.private_key_der);
        assert_ne!(key2.private_key_der, key3.private_key_der);
        assert_ne!(key1.public_key_der, key2.public_key_der);
        
        // But algorithm should be the same
        assert_eq!(key1.algorithm, key2.algorithm);
        assert_eq!(key2.algorithm, key3.algorithm);
        assert_eq!(key1.algorithm, "ES256");
        
        // Key IDs should match what we set
        assert_eq!(key1.key_id, "key1");
        assert_eq!(key2.key_id, "key2");
        assert_eq!(key3.key_id, "key3");
    }

    #[test]
    fn test_generate_multiple_ed25519_keys() {
        let key1 = generate_ed25519_keypair("ed1").unwrap();
        let key2 = generate_ed25519_keypair("ed2").unwrap();
        
        assert_eq!(key1.algorithm, "EdDSA");
        assert_eq!(key2.algorithm, "EdDSA");
        assert_ne!(key1.private_key_der, key2.private_key_der);
        assert_ne!(key1.public_key_der, key2.public_key_der);
        assert_eq!(key1.key_id, "ed1");
        assert_eq!(key2.key_id, "ed2");
    }

    #[test]
    fn test_rsa_keypair_not_supported() {
        // RSA key generation should return an error
        let result = generate_rsa_keypair("rsa-key");
        assert!(result.is_err());
        
        match result {
            Err(JwtError::KeyGenerationFailed(msg)) => {
                assert!(msg.contains("RSA"));
                assert!(msg.contains("ES256"));
            }
            _ => panic!("Expected KeyGenerationFailed error"),
        }
    }

    #[test]
    fn test_key_generation_with_special_characters() {
        // Test key generation with special characters in key ID
        let special_chars = vec![
            "key-with-dashes",
            "key_with_underscores", 
            "key.with.dots",
            "key123",
            "CamelCaseKey",
            "key/with/slashes",
        ];
        
        for key_id in special_chars {
            let result = generate_ec_keypair(key_id);
            assert!(result.is_ok(), "Failed to generate key with ID: {}", key_id);
            
            let keypair = result.unwrap();
            assert_eq!(keypair.key_id, key_id);
        }
    }

    #[test]
    fn test_key_generation_empty_key_id() {
        // Test with empty key ID
        let result = generate_ec_keypair("");
        assert!(result.is_ok());
        
        let keypair = result.unwrap();
        assert_eq!(keypair.key_id, "");
    }
}
