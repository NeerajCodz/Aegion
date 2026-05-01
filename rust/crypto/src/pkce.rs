use ring::digest::{digest, SHA256};

use crate::compare::constant_time_compare_str;
use crate::error::CryptoError;
use crate::util::b64_url_encode;

pub fn compute_challenge(verifier: &str, method: &str) -> Result<String, CryptoError> {
    let normalized = normalize_method(method);
    match normalized.as_str() {
        "plain" => Ok(verifier.to_string()),
        "S256" => {
            let digest = digest(&SHA256, verifier.as_bytes());
            Ok(b64_url_encode(digest.as_ref()))
        }
        _ => Err(CryptoError::Unsupported(format!(
            "unsupported code_challenge_method: {normalized}"
        ))),
    }
}

pub fn verify_pkce(verifier: &str, challenge: &str, method: &str) -> Result<bool, CryptoError> {
    let computed = compute_challenge(verifier, method)?;
    Ok(constant_time_compare_str(&computed, challenge))
}

fn normalize_method(method: &str) -> String {
    if method.trim().is_empty() || method.eq_ignore_ascii_case("plain") {
        "plain".to_string()
    } else if method.eq_ignore_ascii_case("s256") {
        "S256".to_string()
    } else {
        method.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn s256_matches_known_example() {
        let verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
        let challenge = compute_challenge(verifier, "S256").unwrap();
        assert_eq!(challenge, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
    }

    #[test]
    fn verify_plain() {
        assert!(verify_pkce("abc", "abc", "plain").unwrap());
    }
}
