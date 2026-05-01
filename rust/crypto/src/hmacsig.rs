use ring::hmac;

use crate::compare::constant_time_compare_hmac;
use crate::error::CryptoError;
use crate::util::{hex_decode, hex_encode};

const ENVELOPE_VERSION: &str = "v1";

pub fn sign_hex(secret: &[u8], message: &[u8]) -> Result<String, CryptoError> {
    if secret.is_empty() {
        return Err(CryptoError::InvalidInput(
            "secret cannot be empty".to_string(),
        ));
    }

    let key = hmac::Key::new(hmac::HMAC_SHA256, secret);
    let digest = hmac::sign(&key, message);
    Ok(hex_encode(digest.as_ref()))
}

pub fn verify_hex(secret: &[u8], message: &[u8], signature_hex: &str) -> Result<bool, CryptoError> {
    let provided = hex_decode(signature_hex)?;
    let expected = hex_decode(&sign_hex(secret, message)?)?;
    Ok(constant_time_compare_hmac(&expected, &provided))
}

pub fn sign_envelope(
    kind: &str,
    secret: &[u8],
    timestamp: i64,
    payload: &[u8],
) -> Result<String, CryptoError> {
    let message = envelope_message(kind, timestamp, payload);
    let signature = sign_hex(secret, &message)?;
    Ok(format!("{ENVELOPE_VERSION};t={timestamp};s={signature}"))
}

pub fn verify_envelope(
    kind: &str,
    secret: &[u8],
    payload: &[u8],
    envelope: &str,
    max_age_seconds: u64,
    now_unix: i64,
) -> Result<bool, CryptoError> {
    let parsed = parse_envelope(envelope)?;
    if max_age_seconds > 0 {
        if parsed.timestamp > now_unix {
            return Err(CryptoError::InvalidSignature);
        }
        if now_unix - parsed.timestamp > max_age_seconds as i64 {
            return Err(CryptoError::Expired);
        }
    }
    verify_hex(
        secret,
        &envelope_message(kind, parsed.timestamp, payload),
        &parsed.signature,
    )
}

pub fn envelope_message(kind: &str, timestamp: i64, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(kind.len() + payload.len() + 32);
    out.extend_from_slice(ENVELOPE_VERSION.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(kind.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(timestamp.to_string().as_bytes());
    out.push(b'\n');
    out.extend_from_slice(payload);
    out
}

struct ParsedEnvelope {
    timestamp: i64,
    signature: String,
}

fn parse_envelope(envelope: &str) -> Result<ParsedEnvelope, CryptoError> {
    let parts: Vec<&str> = envelope.split(';').collect();
    if parts.len() != 3 || parts[0] != ENVELOPE_VERSION {
        return Err(CryptoError::InvalidSignature);
    }

    let timestamp = parts[1]
        .strip_prefix("t=")
        .ok_or(CryptoError::InvalidSignature)?
        .parse::<i64>()
        .map_err(|_| CryptoError::InvalidSignature)?;
    let signature = parts[2]
        .strip_prefix("s=")
        .ok_or(CryptoError::InvalidSignature)?
        .to_string();

    if signature.is_empty() {
        return Err(CryptoError::InvalidSignature);
    }

    Ok(ParsedEnvelope {
        timestamp,
        signature,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sign_and_verify_hex() {
        let signature = sign_hex(b"secret", b"payload").unwrap();
        assert!(verify_hex(b"secret", b"payload", &signature).unwrap());
        assert!(!verify_hex(b"other", b"payload", &signature).unwrap());
    }

    #[test]
    fn sign_and_verify_envelope() {
        let signed = sign_envelope("identity", b"secret", 1700000000, b"payload").unwrap();
        assert!(
            verify_envelope("identity", b"secret", b"payload", &signed, 60, 1700000030).unwrap()
        );
    }

    #[test]
    fn envelope_expiry() {
        let signed = sign_envelope("identity", b"secret", 1700000000, b"payload").unwrap();
        let err = verify_envelope("identity", b"secret", b"payload", &signed, 60, 1700000100)
            .unwrap_err();
        assert!(matches!(err, CryptoError::Expired));
    }
}
