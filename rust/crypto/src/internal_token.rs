use crate::compare::constant_time_compare_hmac;
use crate::error::CryptoError;
use crate::hmacsig::sign_hex;
use crate::util::{b64_url_decode, b64_url_encode, hex_decode};

const INTERNAL_TOKEN_KIND: &str = "internal_token";
const INTERNAL_TOKEN_VERSION: &str = "v1";

pub struct ParsedInternalToken {
    pub module_id: String,
    pub timestamp_millis: i64,
    pub signature_hex: String,
}

pub fn generate(
    secret: &[u8],
    module_id: &str,
    timestamp_millis: i64,
) -> Result<String, CryptoError> {
    if module_id.trim().is_empty() {
        return Err(CryptoError::InvalidInput(
            "module id cannot be empty".to_string(),
        ));
    }

    let encoded = b64_url_encode(module_id.as_bytes());
    let signature = sign_hex(secret, &token_message(&encoded, timestamp_millis))?;
    Ok(format!(
        "{INTERNAL_TOKEN_VERSION}.{encoded}.{timestamp_millis}.{signature}"
    ))
}

pub fn verify(
    secret: &[u8],
    token: &str,
    now_millis: i64,
    ttl_millis: u64,
) -> Result<ParsedInternalToken, CryptoError> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 4 || parts[0] != INTERNAL_TOKEN_VERSION {
        return Err(CryptoError::InvalidSignature);
    }

    let timestamp_millis = parts[2]
        .parse::<i64>()
        .map_err(|_| CryptoError::InvalidSignature)?;
    if timestamp_millis > now_millis {
        return Err(CryptoError::InvalidSignature);
    }
    if ttl_millis > 0 && now_millis - timestamp_millis > ttl_millis as i64 {
        return Err(CryptoError::Expired);
    }

    let expected = hex_decode(&sign_hex(
        secret,
        &token_message(parts[1], timestamp_millis),
    )?)?;
    let provided = hex_decode(parts[3])?;
    if !constant_time_compare_hmac(&expected, &provided) {
        return Err(CryptoError::InvalidSignature);
    }

    let module_id = String::from_utf8(b64_url_decode(parts[1])?)
        .map_err(|_| CryptoError::InvalidInput("invalid module id".to_string()))?;

    Ok(ParsedInternalToken {
        module_id,
        timestamp_millis,
        signature_hex: parts[3].to_string(),
    })
}

fn token_message(encoded_module_id: &str, timestamp_millis: i64) -> Vec<u8> {
    let mut out = Vec::with_capacity(encoded_module_id.len() + 48);
    out.extend_from_slice(INTERNAL_TOKEN_KIND.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(INTERNAL_TOKEN_VERSION.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(timestamp_millis.to_string().as_bytes());
    out.push(b'\n');
    out.extend_from_slice(encoded_module_id.as_bytes());
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_internal_token() {
        let token = generate(b"secret", "admin", 1700000000000).unwrap();
        let parsed = verify(b"secret", &token, 1700000002000, 60_000).unwrap();
        assert_eq!(parsed.module_id, "admin");
    }
}
