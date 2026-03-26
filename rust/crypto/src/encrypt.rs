//! Field-level encryption using XChaCha20-Poly1305
//!
//! Provides AEAD encryption for sensitive database fields.
//! Uses 24-byte nonces for safe random generation.

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use rand::RngCore;
use base64::{Engine, engine::general_purpose::STANDARD as BASE64};
use crate::error::CryptoError;

/// Key size for XChaCha20-Poly1305 (256 bits)
pub const KEY_SIZE: usize = 32;

/// Nonce size for XChaCha20-Poly1305 (192 bits)
const NONCE_SIZE: usize = 24;

/// AEAD tag size (128 bits)
const TAG_SIZE: usize = 16;

/// Encrypt a plaintext field with optional associated data
///
/// # Arguments
/// * `key` - 32-byte encryption key
/// * `plaintext` - The data to encrypt
/// * `aad` - Optional associated data (authenticated but not encrypted)
///
/// # Returns
/// * Base64-encoded string: `nonce || ciphertext || tag`
pub fn encrypt_field(key: &[u8], plaintext: &[u8], aad: Option<&[u8]>) -> Result<String, CryptoError> {
    if key.len() != KEY_SIZE {
        return Err(CryptoError::InvalidKeyLength {
            expected: KEY_SIZE,
            actual: key.len(),
        });
    }
    
    let cipher = XChaCha20Poly1305::new_from_slice(key)
        .map_err(|e| CryptoError::EncryptionFailed(e.to_string()))?;
    
    // Generate random nonce
    let mut nonce_bytes = [0u8; NONCE_SIZE];
    rand::thread_rng()
        .try_fill_bytes(&mut nonce_bytes)
        .map_err(|_| CryptoError::RngError)?;
    let nonce = XNonce::from_slice(&nonce_bytes);
    
    // Encrypt with optional AAD
    let ciphertext = match aad {
        Some(ad) => {
            let payload = Payload { msg: plaintext, aad: ad };
            cipher.encrypt(nonce, payload)
        }
        None => {
            cipher.encrypt(nonce, plaintext)
        }
    }.map_err(|e| CryptoError::EncryptionFailed(e.to_string()))?;
    
    // Combine: nonce || ciphertext (includes tag)
    let mut output = Vec::with_capacity(NONCE_SIZE + ciphertext.len());
    output.extend_from_slice(&nonce_bytes);
    output.extend_from_slice(&ciphertext);
    
    Ok(BASE64.encode(&output))
}

/// Decrypt a ciphertext field with optional associated data
///
/// # Arguments
/// * `key` - 32-byte encryption key (must match encryption key)
/// * `ciphertext_b64` - Base64-encoded ciphertext from `encrypt_field`
/// * `aad` - Optional associated data (must match encryption AAD)
///
/// # Returns
/// * Decrypted plaintext bytes
pub fn decrypt_field(key: &[u8], ciphertext_b64: &str, aad: Option<&[u8]>) -> Result<Vec<u8>, CryptoError> {
    if key.len() != KEY_SIZE {
        return Err(CryptoError::InvalidKeyLength {
            expected: KEY_SIZE,
            actual: key.len(),
        });
    }
    
    let data = BASE64.decode(ciphertext_b64)
        .map_err(|_| CryptoError::InvalidCiphertext)?;
    
    if data.len() < NONCE_SIZE + TAG_SIZE {
        return Err(CryptoError::InvalidCiphertext);
    }
    
    let (nonce_bytes, ciphertext) = data.split_at(NONCE_SIZE);
    let nonce = XNonce::from_slice(nonce_bytes);
    
    let cipher = XChaCha20Poly1305::new_from_slice(key)
        .map_err(|e| CryptoError::DecryptionFailed(e.to_string()))?;
    
    let plaintext = match aad {
        Some(ad) => {
            let payload = Payload { msg: ciphertext, aad: ad };
            cipher.decrypt(nonce, payload)
        }
        None => {
            cipher.decrypt(nonce, ciphertext)
        }
    }.map_err(|_| CryptoError::DecryptionFailed("authentication failed".into()))?;
    
    Ok(plaintext)
}

/// Generate a random 32-byte encryption key
pub fn generate_key() -> Result<[u8; KEY_SIZE], CryptoError> {
    let mut key = [0u8; KEY_SIZE];
    rand::thread_rng()
        .try_fill_bytes(&mut key)
        .map_err(|_| CryptoError::RngError)?;
    Ok(key)
}

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let key = generate_key().unwrap();
        let plaintext = b"Hello, World! This is sensitive data.";
        
        let ciphertext = encrypt_field(&key, plaintext, None).unwrap();
        let decrypted = decrypt_field(&key, &ciphertext, None).unwrap();
        
        assert_eq!(plaintext.as_slice(), decrypted.as_slice());
    }
    
    #[test]
    fn test_encrypt_with_aad() {
        let key = generate_key().unwrap();
        let plaintext = b"secret data";
        let aad = b"identity_id:12345";
        
        let ciphertext = encrypt_field(&key, plaintext, Some(aad)).unwrap();
        
        // Decrypt with correct AAD
        let decrypted = decrypt_field(&key, &ciphertext, Some(aad)).unwrap();
        assert_eq!(plaintext.as_slice(), decrypted.as_slice());
        
        // Decrypt with wrong AAD should fail
        let result = decrypt_field(&key, &ciphertext, Some(b"wrong_aad"));
        assert!(result.is_err());
    }
    
    #[test]
    fn test_different_ciphertexts() {
        let key = generate_key().unwrap();
        let plaintext = b"same data";
        
        let ct1 = encrypt_field(&key, plaintext, None).unwrap();
        let ct2 = encrypt_field(&key, plaintext, None).unwrap();
        
        // Different nonces produce different ciphertexts
        assert_ne!(ct1, ct2);
        
        // Both decrypt to same plaintext
        assert_eq!(
            decrypt_field(&key, &ct1, None).unwrap(),
            decrypt_field(&key, &ct2, None).unwrap()
        );
    }
    
    #[test]
    fn test_wrong_key_fails() {
        let key1 = generate_key().unwrap();
        let key2 = generate_key().unwrap();
        let plaintext = b"secret";
        
        let ciphertext = encrypt_field(&key1, plaintext, None).unwrap();
        let result = decrypt_field(&key2, &ciphertext, None);
        
        assert!(result.is_err());
    }
    
    #[test]
    fn test_invalid_key_length() {
        let short_key = [0u8; 16];
        let result = encrypt_field(&short_key, b"test", None);
        
        match result {
            Err(CryptoError::InvalidKeyLength { expected: 32, actual: 16 }) => {}
            _ => panic!("Expected InvalidKeyLength error"),
        }
    }
    
    #[test]
    fn test_invalid_ciphertext() {
        let key = generate_key().unwrap();
        
        // Too short
        let result = decrypt_field(&key, "dG9vIHNob3J0", None);
        assert!(result.is_err());
        
        // Invalid base64
        let result = decrypt_field(&key, "not!valid!base64!!!", None);
        assert!(result.is_err());
    }
    
    #[test]
    fn test_tampered_ciphertext() {
        let key = generate_key().unwrap();
        let ciphertext = encrypt_field(&key, b"secret", None).unwrap();
        
        // Decode, tamper, re-encode
        let mut data = BASE64.decode(&ciphertext).unwrap();
        data[data.len() - 1] ^= 0xFF;  // Flip bits in last byte
        let tampered = BASE64.encode(&data);
        
        let result = decrypt_field(&key, &tampered, None);
        assert!(result.is_err());
    }
}
