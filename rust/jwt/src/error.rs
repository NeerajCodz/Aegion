//! Error types for the JWT module

use thiserror::Error;

/// Errors that can occur during JWT operations
#[derive(Debug, Error)]
pub enum JwtError {
    #[error("key generation failed: {0}")]
    KeyGenerationFailed(String),
    
    #[error("signing failed: {0}")]
    SigningFailed(String),
    
    #[error("verification failed: {0}")]
    VerificationFailed(String),
    
    #[error("invalid token format")]
    InvalidTokenFormat,
    
    #[error("invalid algorithm: {0}")]
    InvalidAlgorithm(String),
    
    #[error("invalid key")]
    InvalidKey,
    
    #[error("token expired")]
    TokenExpired,
    
    #[error("token not yet valid")]
    TokenNotYetValid,
    
    #[error("JSON serialization error: {0}")]
    JsonError(String),
    
    #[error("base64 decode error")]
    Base64Error,
}

impl JwtError {
    /// Convert error to FFI error code
    pub fn to_error_code(&self) -> i32 {
        match self {
            JwtError::KeyGenerationFailed(_) => -1,
            JwtError::SigningFailed(_) => -2,
            JwtError::VerificationFailed(_) => -3,
            JwtError::InvalidTokenFormat => -4,
            JwtError::InvalidAlgorithm(_) => -5,
            JwtError::InvalidKey => -6,
            JwtError::TokenExpired => -7,
            JwtError::TokenNotYetValid => -8,
            JwtError::JsonError(_) => -9,
            JwtError::Base64Error => -10,
        }
    }
}

impl From<serde_json::Error> for JwtError {
    fn from(e: serde_json::Error) -> Self {
        JwtError::JsonError(e.to_string())
    }
}
