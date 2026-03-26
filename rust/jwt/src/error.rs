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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_error_display() {
        // Test that error messages are properly formatted
        let errors = vec![
            JwtError::KeyGenerationFailed("test reason".into()),
            JwtError::SigningFailed("signing issue".into()),
            JwtError::VerificationFailed("verification issue".into()),
            JwtError::InvalidTokenFormat,
            JwtError::InvalidAlgorithm("RS512".into()),
            JwtError::InvalidKey,
            JwtError::TokenExpired,
            JwtError::TokenNotYetValid,
            JwtError::JsonError("parse error".into()),
            JwtError::Base64Error,
        ];
        
        for error in errors {
            let display_str = format!("{}", error);
            assert!(!display_str.is_empty());
            assert!(!display_str.contains("Error")); // Should be clean message
        }
        
        // Test specific error messages
        let key_gen_error = JwtError::KeyGenerationFailed("RSA failed".into());
        assert!(format!("{}", key_gen_error).contains("key generation failed: RSA failed"));
        
        let alg_error = JwtError::InvalidAlgorithm("HS256".into());
        assert!(format!("{}", alg_error).contains("invalid algorithm: HS256"));
    }

    #[test]
    fn test_error_codes() {
        // Test that error codes are unique and negative
        let test_cases = vec![
            (JwtError::KeyGenerationFailed("".into()), -1),
            (JwtError::SigningFailed("".into()), -2),
            (JwtError::VerificationFailed("".into()), -3),
            (JwtError::InvalidTokenFormat, -4),
            (JwtError::InvalidAlgorithm("".into()), -5),
            (JwtError::InvalidKey, -6),
            (JwtError::TokenExpired, -7),
            (JwtError::TokenNotYetValid, -8),
            (JwtError::JsonError("".into()), -9),
            (JwtError::Base64Error, -10),
        ];
        
        for (error, expected_code) in test_cases {
            assert_eq!(error.to_error_code(), expected_code);
        }
        
        // Ensure all error codes are unique
        let mut codes = std::collections::HashSet::new();
        let all_errors = vec![
            JwtError::KeyGenerationFailed("".into()),
            JwtError::SigningFailed("".into()),
            JwtError::VerificationFailed("".into()),
            JwtError::InvalidTokenFormat,
            JwtError::InvalidAlgorithm("".into()),
            JwtError::InvalidKey,
            JwtError::TokenExpired,
            JwtError::TokenNotYetValid,
            JwtError::JsonError("".into()),
            JwtError::Base64Error,
        ];
        
        for error in all_errors {
            let code = error.to_error_code();
            assert!(codes.insert(code), "Duplicate error code: {}", code);
            assert!(code < 0, "Error code should be negative: {}", code);
        }
    }

    #[test]
    fn test_json_error_conversion() {
        // Test that serde_json::Error converts properly
        let json_str = "{ invalid json }";
        let json_error: Result<serde_json::Value, serde_json::Error> = serde_json::from_str(json_str);
        
        match json_error {
            Err(e) => {
                let jwt_error: JwtError = e.into();
                match jwt_error {
                    JwtError::JsonError(msg) => {
                        assert!(!msg.is_empty());
                        assert!(msg.contains("expected"));
                    }
                    _ => panic!("Expected JsonError variant"),
                }
            }
            Ok(_) => panic!("Expected JSON parsing to fail"),
        }
    }

    #[test]
    fn test_error_debug() {
        // Test that Debug implementation works
        let error = JwtError::InvalidKey;
        let debug_str = format!("{:?}", error);
        assert!(debug_str.contains("InvalidKey"));
    }
}
