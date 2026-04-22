use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use base64::Engine;

use crate::error::CryptoError;

pub fn b64_std_encode(input: &[u8]) -> String {
    STANDARD.encode(input)
}

pub fn b64_url_encode(input: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(input)
}

pub fn b64_url_decode(input: &str) -> Result<Vec<u8>, CryptoError> {
    URL_SAFE_NO_PAD
        .decode(input)
        .map_err(|_| CryptoError::InvalidInput("invalid base64url value".to_string()))
}

pub fn hex_encode(input: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(input.len() * 2);
    for byte in input {
        out.push(HEX[(byte >> 4) as usize] as char);
        out.push(HEX[(byte & 0x0f) as usize] as char);
    }
    out
}

pub fn hex_decode(input: &str) -> Result<Vec<u8>, CryptoError> {
    if !input.len().is_multiple_of(2) {
        return Err(CryptoError::InvalidSignature);
    }

    let mut out = Vec::with_capacity(input.len() / 2);
    let bytes = input.as_bytes();
    let mut index = 0;
    while index < bytes.len() {
        let hi = decode_nibble(bytes[index])?;
        let lo = decode_nibble(bytes[index + 1])?;
        out.push((hi << 4) | lo);
        index += 2;
    }
    Ok(out)
}

fn decode_nibble(value: u8) -> Result<u8, CryptoError> {
    match value {
        b'0'..=b'9' => Ok(value - b'0'),
        b'a'..=b'f' => Ok(value - b'a' + 10),
        b'A'..=b'F' => Ok(value - b'A' + 10),
        _ => Err(CryptoError::InvalidSignature),
    }
}
