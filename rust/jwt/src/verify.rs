//! JWT verification
//!
//! Verifies JWT signatures using RS256, ES256, or EdDSA algorithms.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use ring::signature::{
    UnparsedPublicKey, ECDSA_P256_SHA256_FIXED, ED25519,
};
use std::time::{SystemTime, UNIX_EPOCH};

use crate::error::JwtError;
use crate::sign::{Claims, JwtHeader};

/// Options for JWT verification
#[derive(Debug, Clone, Default)]
pub struct VerifyOptions {
    /// Expected issuer
    pub issuer: Option<String>,
    /// Expected audience
    pub audience: Option<String>,
    /// Allow clock skew (seconds)
    pub leeway: u64,
    /// Skip expiration check
    pub ignore_exp: bool,
    /// Skip not-before check
    pub ignore_nbf: bool,
}

/// Result of JWT verification
#[derive(Debug)]
pub struct VerifyResult {
    /// Decoded header
    pub header: JwtHeader,
    /// Decoded and validated claims
    pub claims: Claims,
}

/// Verify a JWT and return the decoded claims
///
/// # Arguments
/// * `token` - The JWT string (header.payload.signature)
/// * `algorithm` - Expected algorithm (ES256, EdDSA)
/// * `public_key` - The public key bytes
/// * `options` - Verification options
///
/// # Returns
/// * `Ok(VerifyResult)` - The decoded header and claims
/// * `Err(JwtError)` - If verification fails
pub fn verify_jwt(
    token: &str,
    algorithm: &str,
    public_key: &[u8],
    options: &VerifyOptions,
) -> Result<VerifyResult, JwtError> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 3 {
        return Err(JwtError::InvalidTokenFormat);
    }
    
    let header_b64 = parts[0];
    let payload_b64 = parts[1];
    let signature_b64 = parts[2];
    
    // Decode header
    let header_json = URL_SAFE_NO_PAD.decode(header_b64)
        .map_err(|_| JwtError::Base64Error)?;
    let header: JwtHeader = serde_json::from_slice(&header_json)?;
    
    // Verify algorithm matches
    if header.alg != algorithm {
        return Err(JwtError::InvalidAlgorithm(format!(
            "expected {}, got {}",
            algorithm, header.alg
        )));
    }
    
    // Decode signature
    let signature = URL_SAFE_NO_PAD.decode(signature_b64)
        .map_err(|_| JwtError::Base64Error)?;
    
    // Verify signature
    let signing_input = format!("{}.{}", header_b64, payload_b64);
    verify_signature(algorithm, public_key, signing_input.as_bytes(), &signature)?;
    
    // Decode payload
    let payload_json = URL_SAFE_NO_PAD.decode(payload_b64)
        .map_err(|_| JwtError::Base64Error)?;
    let claims: Claims = serde_json::from_slice(&payload_json)?;
    
    // Validate claims
    validate_claims(&claims, options)?;
    
    Ok(VerifyResult { header, claims })
}

/// Verify signature based on algorithm
fn verify_signature(
    algorithm: &str,
    public_key: &[u8],
    message: &[u8],
    signature: &[u8],
) -> Result<(), JwtError> {
    match algorithm {
        "ES256" => {
            let key = UnparsedPublicKey::new(&ECDSA_P256_SHA256_FIXED, public_key);
            key.verify(message, signature)
                .map_err(|_| JwtError::VerificationFailed("invalid signature".into()))
        }
        "EdDSA" => {
            let key = UnparsedPublicKey::new(&ED25519, public_key);
            key.verify(message, signature)
                .map_err(|_| JwtError::VerificationFailed("invalid signature".into()))
        }
        _ => Err(JwtError::InvalidAlgorithm(algorithm.to_string())),
    }
}

/// Validate JWT claims (exp, nbf, iss, aud)
fn validate_claims(claims: &Claims, options: &VerifyOptions) -> Result<(), JwtError> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    
    // Check expiration
    if !options.ignore_exp {
        if let Some(exp) = claims.exp {
            if now > exp + options.leeway {
                return Err(JwtError::TokenExpired);
            }
        }
    }
    
    // Check not-before
    if !options.ignore_nbf {
        if let Some(nbf) = claims.nbf {
            if now + options.leeway < nbf {
                return Err(JwtError::TokenNotYetValid);
            }
        }
    }
    
    // Check issuer
    if let Some(expected_iss) = &options.issuer {
        match &claims.iss {
            Some(iss) if iss == expected_iss => {}
            _ => return Err(JwtError::VerificationFailed("issuer mismatch".into())),
        }
    }
    
    // Check audience
    if let Some(expected_aud) = &options.audience {
        match &claims.aud {
            Some(aud) if aud == expected_aud => {}
            _ => return Err(JwtError::VerificationFailed("audience mismatch".into())),
        }
    }
    
    Ok(())
}

/// Decode a JWT without verifying the signature
///
/// WARNING: Only use this for inspection or when signature verification
/// is handled externally. Never trust the claims without verification.
pub fn decode_jwt_unverified(token: &str) -> Result<VerifyResult, JwtError> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 3 {
        return Err(JwtError::InvalidTokenFormat);
    }
    
    let header_json = URL_SAFE_NO_PAD.decode(parts[0])
        .map_err(|_| JwtError::Base64Error)?;
    let header: JwtHeader = serde_json::from_slice(&header_json)?;
    
    let payload_json = URL_SAFE_NO_PAD.decode(parts[1])
        .map_err(|_| JwtError::Base64Error)?;
    let claims: Claims = serde_json::from_slice(&payload_json)?;
    
    Ok(VerifyResult { header, claims })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keygen::generate_ec_keypair;
    use crate::sign::sign_jwt;
    
    #[test]
    fn test_verify_jwt_es256() {
        let keypair = generate_ec_keypair("test-key").unwrap();
        
        let mut claims = Claims::default();
        claims.iss = Some("aegion".into());
        claims.sub = Some("user-123".into());
        
        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, Some(&keypair.key_id)).unwrap();
        
        let options = VerifyOptions::default();
        let result = verify_jwt(&token, "ES256", &keypair.public_key_der, &options).unwrap();
        
        assert_eq!(result.claims.sub, Some("user-123".into()));
        assert_eq!(result.header.kid, Some("test-key".into()));
    }
    
    #[test]
    fn test_verify_with_issuer_check() {
        let keypair = generate_ec_keypair("test").unwrap();
        
        let mut claims = Claims::default();
        claims.iss = Some("aegion".into());
        
        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, None).unwrap();
        
        // Correct issuer
        let options = VerifyOptions {
            issuer: Some("aegion".into()),
            ..Default::default()
        };
        assert!(verify_jwt(&token, "ES256", &keypair.public_key_der, &options).is_ok());
        
        // Wrong issuer
        let options = VerifyOptions {
            issuer: Some("other".into()),
            ..Default::default()
        };
        assert!(verify_jwt(&token, "ES256", &keypair.public_key_der, &options).is_err());
    }
    
    #[test]
    fn test_expired_token() {
        let keypair = generate_ec_keypair("test").unwrap();
        
        let mut claims = Claims::default();
        claims.exp = Some(0); // Expired long ago
        
        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, None).unwrap();
        
        let options = VerifyOptions::default();
        let result = verify_jwt(&token, "ES256", &keypair.public_key_der, &options);
        assert!(matches!(result, Err(JwtError::TokenExpired)));
        
        // With ignore_exp
        let options = VerifyOptions {
            ignore_exp: true,
            ..Default::default()
        };
        assert!(verify_jwt(&token, "ES256", &keypair.public_key_der, &options).is_ok());
    }
    
    #[test]
    fn test_wrong_signature() {
        let keypair1 = generate_ec_keypair("key1").unwrap();
        let keypair2 = generate_ec_keypair("key2").unwrap();
        
        let claims = Claims::default();
        let token = sign_jwt(&claims, "ES256", &keypair1.private_key_der, None).unwrap();
        
        // Verify with wrong public key
        let options = VerifyOptions::default();
        let result = verify_jwt(&token, "ES256", &keypair2.public_key_der, &options);
        assert!(result.is_err());
    }
    
    #[test]
    fn test_decode_unverified() {
        let keypair = generate_ec_keypair("test").unwrap();
        
        let mut claims = Claims::default();
        claims.sub = Some("user-999".into());
        
        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, None).unwrap();
        
        let result = decode_jwt_unverified(&token).unwrap();
        assert_eq!(result.claims.sub, Some("user-999".into()));
    }
}
