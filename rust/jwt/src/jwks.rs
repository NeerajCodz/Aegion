//! JWKS (JSON Web Key Set) serialization
//!
//! Converts key pairs to JWK/JWKS format for public key distribution.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde::{Deserialize, Serialize};

use crate::error::JwtError;
use crate::keygen::KeyPair;

/// JSON Web Key (JWK)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Jwk {
    /// Key type (EC, RSA, OKP)
    pub kty: String,
    /// Key ID
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kid: Option<String>,
    /// Key use (sig, enc)
    #[serde(rename = "use", skip_serializing_if = "Option::is_none")]
    pub use_: Option<String>,
    /// Algorithm
    #[serde(skip_serializing_if = "Option::is_none")]
    pub alg: Option<String>,

    // EC keys (P-256, P-384, P-521)
    /// EC curve name
    #[serde(skip_serializing_if = "Option::is_none")]
    pub crv: Option<String>,
    /// EC x coordinate (base64url)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub x: Option<String>,
    /// EC y coordinate (base64url)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub y: Option<String>,

    // OKP keys (Ed25519, X25519)
    // Uses crv and x fields

    // RSA keys
    /// RSA modulus (base64url)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub n: Option<String>,
    /// RSA exponent (base64url)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub e: Option<String>,
}

/// JSON Web Key Set (JWKS)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Jwks {
    /// Array of JWKs
    pub keys: Vec<Jwk>,
}

/// Convert a key pair to JWK format (public key only)
///
/// # Arguments
/// * `keypair` - The key pair to convert
///
/// # Returns
/// * `Ok(Jwk)` - The public key in JWK format
/// * `Err(JwtError)` - If conversion fails
pub fn to_jwk(keypair: &KeyPair) -> Result<Jwk, JwtError> {
    match keypair.algorithm.as_str() {
        "ES256" => ec_to_jwk(keypair, "P-256"),
        "ES384" => ec_to_jwk(keypair, "P-384"),
        "ES512" => ec_to_jwk(keypair, "P-521"),
        "EdDSA" => ed25519_to_jwk(keypair),
        _ => Err(JwtError::InvalidAlgorithm(keypair.algorithm.clone())),
    }
}

/// Convert EC key pair to JWK
fn ec_to_jwk(keypair: &KeyPair, curve: &str) -> Result<Jwk, JwtError> {
    // EC public key is in uncompressed form: 0x04 || x || y
    let public_key = &keypair.public_key_der;

    if public_key.is_empty() || public_key[0] != 0x04 {
        return Err(JwtError::InvalidKey);
    }

    let coord_len = (public_key.len() - 1) / 2;
    let x = &public_key[1..1 + coord_len];
    let y = &public_key[1 + coord_len..];

    Ok(Jwk {
        kty: "EC".into(),
        kid: Some(keypair.key_id.clone()),
        use_: Some("sig".into()),
        alg: Some(keypair.algorithm.clone()),
        crv: Some(curve.into()),
        x: Some(URL_SAFE_NO_PAD.encode(x)),
        y: Some(URL_SAFE_NO_PAD.encode(y)),
        n: None,
        e: None,
    })
}

/// Convert Ed25519 key pair to JWK
fn ed25519_to_jwk(keypair: &KeyPair) -> Result<Jwk, JwtError> {
    Ok(Jwk {
        kty: "OKP".into(),
        kid: Some(keypair.key_id.clone()),
        use_: Some("sig".into()),
        alg: Some("EdDSA".into()),
        crv: Some("Ed25519".into()),
        x: Some(URL_SAFE_NO_PAD.encode(&keypair.public_key_der)),
        y: None,
        n: None,
        e: None,
    })
}

/// Convert multiple key pairs to JWKS format
pub fn to_jwks(keypairs: &[KeyPair]) -> Result<Jwks, JwtError> {
    let keys = keypairs.iter().map(to_jwk).collect::<Result<Vec<_>, _>>()?;

    Ok(Jwks { keys })
}

/// Serialize JWKS to JSON string
pub fn jwks_to_json(jwks: &Jwks) -> Result<String, JwtError> {
    serde_json::to_string(jwks).map_err(Into::into)
}

/// Serialize JWKS to pretty JSON string
pub fn jwks_to_json_pretty(jwks: &Jwks) -> Result<String, JwtError> {
    serde_json::to_string_pretty(jwks).map_err(Into::into)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keygen::{generate_ec_keypair, generate_ed25519_keypair};

    #[test]
    fn test_ec_to_jwk() {
        let keypair = generate_ec_keypair("ec-key-1").unwrap();
        let jwk = to_jwk(&keypair).unwrap();

        assert_eq!(jwk.kty, "EC");
        assert_eq!(jwk.kid, Some("ec-key-1".into()));
        assert_eq!(jwk.alg, Some("ES256".into()));
        assert_eq!(jwk.crv, Some("P-256".into()));
        assert!(jwk.x.is_some());
        assert!(jwk.y.is_some());

        // x and y should be 32 bytes each (base64url encoded)
        let x_bytes = URL_SAFE_NO_PAD.decode(jwk.x.as_ref().unwrap()).unwrap();
        let y_bytes = URL_SAFE_NO_PAD.decode(jwk.y.as_ref().unwrap()).unwrap();
        assert_eq!(x_bytes.len(), 32);
        assert_eq!(y_bytes.len(), 32);
    }

    #[test]
    fn test_ed25519_to_jwk() {
        let keypair = generate_ed25519_keypair("ed-key-1").unwrap();
        let jwk = to_jwk(&keypair).unwrap();

        assert_eq!(jwk.kty, "OKP");
        assert_eq!(jwk.kid, Some("ed-key-1".into()));
        assert_eq!(jwk.alg, Some("EdDSA".into()));
        assert_eq!(jwk.crv, Some("Ed25519".into()));
        assert!(jwk.x.is_some());
        assert!(jwk.y.is_none()); // Ed25519 doesn't have y
    }

    #[test]
    fn test_to_jwks() {
        let keypair1 = generate_ec_keypair("key1").unwrap();
        let keypair2 = generate_ec_keypair("key2").unwrap();

        let jwks = to_jwks(&[keypair1, keypair2]).unwrap();

        assert_eq!(jwks.keys.len(), 2);
        assert_eq!(jwks.keys[0].kid, Some("key1".into()));
        assert_eq!(jwks.keys[1].kid, Some("key2".into()));
    }

    #[test]
    fn test_jwks_serialization() {
        let keypair = generate_ec_keypair("test").unwrap();
        let jwks = to_jwks(&[keypair]).unwrap();

        let json = jwks_to_json(&jwks).unwrap();

        // Should be valid JSON
        let parsed: Jwks = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.keys.len(), 1);
    }
}
