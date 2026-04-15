//! Error types for the crypto module

use thiserror::Error;

/// Errors that can occur during cryptographic operations
#[derive(Debug, Error)]
pub enum CryptoError {
    #[error("password hashing failed: {0}")]
    HashError(String),

    #[error("password verification failed")]
    VerificationFailed,

    #[error("encryption failed: {0}")]
    EncryptionFailed(String),

    #[error("decryption failed: {0}")]
    DecryptionFailed(String),

    #[error("invalid key length: expected {expected}, got {actual}")]
    InvalidKeyLength { expected: usize, actual: usize },

    #[error("invalid ciphertext")]
    InvalidCiphertext,

    #[error("random number generation failed")]
    RngError,

    #[error("invalid input: {0}")]
    InvalidInput(String),

    #[error("signature format invalid")]
    InvalidSignature,

    #[error("token expired")]
    Expired,

    #[error("unsupported operation: {0}")]
    Unsupported(String),
}

impl CryptoError {
    /// Convert error to FFI error code
    pub fn to_error_code(&self) -> i32 {
        match self {
            CryptoError::HashError(_) => -1,
            CryptoError::VerificationFailed => -2,
            CryptoError::EncryptionFailed(_) => -3,
            CryptoError::DecryptionFailed(_) => -4,
            CryptoError::InvalidKeyLength { .. } => -5,
            CryptoError::InvalidCiphertext => -6,
            CryptoError::RngError => -7,
            CryptoError::InvalidInput(_) => -8,
            CryptoError::InvalidSignature => -9,
            CryptoError::Expired => -10,
            CryptoError::Unsupported(_) => -11,
        }
    }
}
