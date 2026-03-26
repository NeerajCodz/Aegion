//! JWT signing
//!
//! Creates signed JWTs using RS256, ES256, or EdDSA algorithms.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use ring::signature::{EcdsaKeyPair, Ed25519KeyPair, ECDSA_P256_SHA256_FIXED_SIGNING};
use serde::{Deserialize, Serialize};
use std::time::{SystemTime, UNIX_EPOCH};

use crate::error::JwtError;

/// JWT header
#[derive(Debug, Serialize, Deserialize)]
pub struct JwtHeader {
    /// Algorithm (RS256, ES256, EdDSA)
    pub alg: String,
    /// Type (always "JWT")
    pub typ: String,
    /// Key ID
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kid: Option<String>,
}

impl JwtHeader {
    pub fn new(alg: &str, kid: Option<&str>) -> Self {
        JwtHeader {
            alg: alg.to_string(),
            typ: "JWT".to_string(),
            kid: kid.map(|s| s.to_string()),
        }
    }
}

/// Standard JWT claims
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Claims {
    /// Issuer
    #[serde(skip_serializing_if = "Option::is_none")]
    pub iss: Option<String>,
    /// Subject
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sub: Option<String>,
    /// Audience
    #[serde(skip_serializing_if = "Option::is_none")]
    pub aud: Option<String>,
    /// Expiration time (Unix timestamp)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub exp: Option<u64>,
    /// Not before time (Unix timestamp)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub nbf: Option<u64>,
    /// Issued at time (Unix timestamp)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub iat: Option<u64>,
    /// JWT ID
    #[serde(skip_serializing_if = "Option::is_none")]
    pub jti: Option<String>,
    /// Session ID (Aegion-specific)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sid: Option<String>,
    /// Additional custom claims
    #[serde(flatten)]
    pub custom: std::collections::HashMap<String, serde_json::Value>,
}

impl Default for Claims {
    fn default() -> Self {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);

        Claims {
            iss: None,
            sub: None,
            aud: None,
            exp: Some(now + 3600), // 1 hour default
            nbf: Some(now),
            iat: Some(now),
            jti: None,
            sid: None,
            custom: std::collections::HashMap::new(),
        }
    }
}

/// Sign a JWT with the given claims and private key
///
/// # Arguments
/// * `claims` - The JWT claims (payload)
/// * `algorithm` - The signing algorithm (ES256, EdDSA)
/// * `private_key_pkcs8` - The private key in PKCS#8 DER format
/// * `key_id` - Optional key ID to include in the header
///
/// # Returns
/// * `Ok(String)` - The signed JWT (header.payload.signature)
/// * `Err(JwtError)` - If signing fails
pub fn sign_jwt(
    claims: &Claims,
    algorithm: &str,
    private_key_pkcs8: &[u8],
    key_id: Option<&str>,
) -> Result<String, JwtError> {
    let header = JwtHeader::new(algorithm, key_id);
    let header_json = serde_json::to_string(&header)?;
    let payload_json = serde_json::to_string(claims)?;

    let header_b64 = URL_SAFE_NO_PAD.encode(header_json.as_bytes());
    let payload_b64 = URL_SAFE_NO_PAD.encode(payload_json.as_bytes());

    let signing_input = format!("{}.{}", header_b64, payload_b64);

    let signature = match algorithm {
        "ES256" => sign_es256(&signing_input, private_key_pkcs8)?,
        "EdDSA" => sign_eddsa(&signing_input, private_key_pkcs8)?,
        _ => return Err(JwtError::InvalidAlgorithm(algorithm.to_string())),
    };

    let signature_b64 = URL_SAFE_NO_PAD.encode(&signature);

    Ok(format!("{}.{}", signing_input, signature_b64))
}

/// Sign using ES256 (ECDSA P-256 with SHA-256)
fn sign_es256(signing_input: &str, private_key_pkcs8: &[u8]) -> Result<Vec<u8>, JwtError> {
    let rng = ring::rand::SystemRandom::new();
    let key_pair =
        EcdsaKeyPair::from_pkcs8(&ECDSA_P256_SHA256_FIXED_SIGNING, private_key_pkcs8, &rng)
            .map_err(|_| JwtError::InvalidKey)?;

    let signature = key_pair
        .sign(&rng, signing_input.as_bytes())
        .map_err(|e| JwtError::SigningFailed(format!("{:?}", e)))?;

    Ok(signature.as_ref().to_vec())
}

/// Sign using EdDSA (Ed25519)
fn sign_eddsa(signing_input: &str, private_key_pkcs8: &[u8]) -> Result<Vec<u8>, JwtError> {
    let key_pair =
        Ed25519KeyPair::from_pkcs8(private_key_pkcs8).map_err(|_| JwtError::InvalidKey)?;

    let signature = key_pair.sign(signing_input.as_bytes());

    Ok(signature.as_ref().to_vec())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keygen::generate_ec_keypair;

    #[test]
    fn test_sign_jwt_es256() {
        let keypair = generate_ec_keypair("test-key").unwrap();

        let mut claims = Claims::default();
        claims.iss = Some("aegion".into());
        claims.sub = Some("user-123".into());

        let token = sign_jwt(
            &claims,
            "ES256",
            &keypair.private_key_der,
            Some(&keypair.key_id),
        )
        .unwrap();

        // Token should have 3 parts
        let parts: Vec<&str> = token.split('.').collect();
        assert_eq!(parts.len(), 3);

        // Decode and verify header
        let header_json = URL_SAFE_NO_PAD.decode(parts[0]).unwrap();
        let header: JwtHeader = serde_json::from_slice(&header_json).unwrap();
        assert_eq!(header.alg, "ES256");
        assert_eq!(header.typ, "JWT");
        assert_eq!(header.kid, Some("test-key".into()));
    }

    #[test]
    fn test_sign_jwt_with_custom_claims() {
        let keypair = generate_ec_keypair("test-key").unwrap();

        let mut claims = Claims::default();
        claims.sub = Some("user-456".into());
        claims
            .custom
            .insert("role".into(), serde_json::json!("admin"));
        claims
            .custom
            .insert("permissions".into(), serde_json::json!(["read", "write"]));

        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, None).unwrap();

        let parts: Vec<&str> = token.split('.').collect();
        let payload_json = URL_SAFE_NO_PAD.decode(parts[1]).unwrap();
        let decoded: serde_json::Value = serde_json::from_slice(&payload_json).unwrap();

        assert_eq!(decoded["sub"], "user-456");
        assert_eq!(decoded["role"], "admin");
    }

    #[test]
    fn test_invalid_algorithm() {
        let keypair = generate_ec_keypair("test").unwrap();
        let claims = Claims::default();

        let result = sign_jwt(&claims, "RS256", &keypair.private_key_der, None);
        assert!(matches!(result, Err(JwtError::InvalidAlgorithm(_))));
    }

    #[test]
    fn test_sign_with_invalid_key() {
        let claims = Claims::default();
        
        // Test with empty key
        let result = sign_jwt(&claims, "ES256", &[], None);
        assert!(result.is_err());
        
        // Test with wrong key format
        let invalid_key = vec![0x00; 32]; // Just random bytes, not a valid PKCS#8 key
        let result = sign_jwt(&claims, "ES256", &invalid_key, None);
        assert!(result.is_err());
        
        // Test with key for wrong algorithm (EdDSA key used for ES256)
        let ed_keypair = crate::keygen::generate_ed25519_keypair("test").unwrap();
        let result = sign_jwt(&claims, "ES256", &ed_keypair.private_key_der, None);
        assert!(result.is_err());
    }

    #[test]
    fn test_sign_jwt_comprehensive() {
        let keypair = generate_ec_keypair("comprehensive-test").unwrap();
        
        // Test with all standard claims populated
        let mut claims = Claims::default();
        claims.iss = Some("test-issuer".into());
        claims.sub = Some("user-789".into());
        claims.aud = Some("test-audience".into());
        claims.jti = Some("unique-jwt-id".into());
        claims.sid = Some("session-123".into());
        
        // Add custom claims
        claims.custom.insert("role".into(), serde_json::json!("admin"));
        claims.custom.insert("permissions".into(), serde_json::json!(["read", "write", "delete"]));
        claims.custom.insert("metadata".into(), serde_json::json!({
            "version": "1.0",
            "features": ["jwt", "auth"]
        }));
        
        let token = sign_jwt(&claims, "ES256", &keypair.private_key_der, Some(&keypair.key_id)).unwrap();
        
        // Verify token structure
        let parts: Vec<&str> = token.split('.').collect();
        assert_eq!(parts.len(), 3);
        
        // Decode and verify all parts
        let header_json = URL_SAFE_NO_PAD.decode(parts[0]).unwrap();
        let header: JwtHeader = serde_json::from_slice(&header_json).unwrap();
        assert_eq!(header.alg, "ES256");
        assert_eq!(header.typ, "JWT");
        assert_eq!(header.kid, Some(keypair.key_id));
        
        let payload_json = URL_SAFE_NO_PAD.decode(parts[1]).unwrap();
        let decoded_claims: serde_json::Value = serde_json::from_slice(&payload_json).unwrap();
        assert_eq!(decoded_claims["iss"], "test-issuer");
        assert_eq!(decoded_claims["sub"], "user-789");
        assert_eq!(decoded_claims["aud"], "test-audience");
        assert_eq!(decoded_claims["role"], "admin");
    }

    #[test]
    fn test_sign_jwt_eddsa() {
        let keypair = crate::keygen::generate_ed25519_keypair("eddsa-test").unwrap();
        
        let mut claims = Claims::default();
        claims.sub = Some("eddsa-user".into());
        
        let token = sign_jwt(&claims, "EdDSA", &keypair.private_key_der, Some(&keypair.key_id)).unwrap();
        
        let parts: Vec<&str> = token.split('.').collect();
        assert_eq!(parts.len(), 3);
        
        let header_json = URL_SAFE_NO_PAD.decode(parts[0]).unwrap();
        let header: JwtHeader = serde_json::from_slice(&header_json).unwrap();
        assert_eq!(header.alg, "EdDSA");
    }

    #[test]
    fn test_claims_default_values() {
        let claims = Claims::default();
        
        // Should have reasonable default timestamps
        assert!(claims.iat.is_some());
        assert!(claims.nbf.is_some());
        assert!(claims.exp.is_some());
        
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs();
            
        // Times should be reasonable (within a few seconds of now)
        let iat = claims.iat.unwrap();
        let nbf = claims.nbf.unwrap();
        let exp = claims.exp.unwrap();
        
        assert!(iat <= now + 5); // Should not be in the future by much
        assert!(iat >= now - 5); // Should not be in the past by much
        assert_eq!(iat, nbf); // Should be the same
        assert!(exp > now); // Should be in the future
        assert!(exp <= now + 3700); // Should be about an hour from now
        
        // Optional claims should be None
        assert!(claims.iss.is_none());
        assert!(claims.sub.is_none());
        assert!(claims.aud.is_none());
        assert!(claims.jti.is_none());
        assert!(claims.sid.is_none());
        
        // Custom claims should be empty
        assert!(claims.custom.is_empty());
    }
}
