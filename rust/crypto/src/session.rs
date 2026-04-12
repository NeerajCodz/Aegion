use crate::compare::constant_time_compare_hmac;
use crate::error::CryptoError;
use crate::hmacsig::sign_hex;
use crate::util::{b64_url_decode, b64_url_encode, hex_decode};

const SESSION_COOKIE_KIND: &str = "session_cookie";
const SESSION_COOKIE_VERSION: &str = "v1";

pub fn sign_cookie(secret: &[u8], token: &str, timestamp: i64) -> Result<String, CryptoError> {
    let encoded = b64_url_encode(token.as_bytes());
    let signature = sign_hex(secret, &cookie_message(&encoded, timestamp))?;
    Ok(format!(
        "{SESSION_COOKIE_VERSION}.{encoded}.{timestamp}.{signature}"
    ))
}

pub fn verify_cookie(
    secret: &[u8],
    signed: &str,
    now_unix: i64,
    max_age_seconds: u64,
) -> Result<String, CryptoError> {
    let parts: Vec<&str> = signed.split('.').collect();
    if parts.len() != 4 || parts[0] != SESSION_COOKIE_VERSION {
        return Err(CryptoError::InvalidSignature);
    }

    let timestamp = parts[2]
        .parse::<i64>()
        .map_err(|_| CryptoError::InvalidSignature)?;
    if max_age_seconds > 0 {
        if timestamp > now_unix {
            return Err(CryptoError::InvalidSignature);
        }
        if now_unix - timestamp > max_age_seconds as i64 {
            return Err(CryptoError::Expired);
        }
    }

    let expected = hex_decode(&sign_hex(secret, &cookie_message(parts[1], timestamp))?)?;
    let provided = hex_decode(parts[3])?;
    if !constant_time_compare_hmac(&expected, &provided) {
        return Err(CryptoError::InvalidSignature);
    }

    let token = b64_url_decode(parts[1])?;
    String::from_utf8(token)
        .map_err(|_| CryptoError::InvalidInput("invalid cookie token".to_string()))
}

fn cookie_message(encoded_token: &str, timestamp: i64) -> Vec<u8> {
    let mut out = Vec::with_capacity(encoded_token.len() + 48);
    out.extend_from_slice(SESSION_COOKIE_KIND.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(SESSION_COOKIE_VERSION.as_bytes());
    out.push(b'\n');
    out.extend_from_slice(timestamp.to_string().as_bytes());
    out.push(b'\n');
    out.extend_from_slice(encoded_token.as_bytes());
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_cookie() {
        let signed = sign_cookie(b"secret", "token", 1700000000).unwrap();
        let token = verify_cookie(b"secret", &signed, 1700000005, 60).unwrap();
        assert_eq!(token, "token");
    }
}
